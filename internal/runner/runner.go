package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

const ExitSoftFail = 1
const ExitHardError = 2

// Result 是 plugin 執行的結果
type Result struct {
	ExitCode    int
	DurationSec float64
	LogFile     string
}

// Runner 定義 plugin 的呼叫介面
type Runner interface {
	Run(ctx context.Context, prompt string) (*Result, error)
}

// SubprocessRunner 透過 config 定義的 command + args 呼叫 LLM CLI
type SubprocessRunner struct {
	Workspace     *protocol.Workspace
	Config        protocol.RunnerConfig
	Name          string
	Timeout       time.Duration
	LogPath       string
	ModelOverride string
	// MaxTransientRetries 是暫態錯誤的重試上限：0（零值）表示「用 DefaultTransientRetries」、
	// 負值表示停用重試、正值為自訂上限。實際值由 resolveMaxRetries() 解析。
	MaxTransientRetries int
	// backoffBase 是指數退避的基準間隔；<=0 時套用 defaultBackoffBase。
	// 測試可注入極小值（如 1ms）加速重試迴圈。
	backoffBase time.Duration
	// ExtraEnv 是額外注入子程序環境變數（如 FOURX_ROLE / FOURX_REVIEW_PACKAGE）。
	// 由 orchestrator 只對 reviewer/deep-reviewer 角色的 SubprocessRunner 設定；其他角色為 nil。
	// 當 Config.Command == "claude" 時（不論 ExtraEnv 是否為空），buildArgs 額外注入 PreToolUse
	// hook settings（guard-tool），攔截 reviewer 自跑 git diff/log/show 與越界 / 非法寫入 source。
	// 不改任何函式簽章。
	ExtraEnv []string
}

// resolveMaxRetries 解析有效的暫態重試上限：
// 零值 → DefaultTransientRetries、負值 → 0（停用）、正值 → 原值。
func (r *SubprocessRunner) resolveMaxRetries() int {
	if r.MaxTransientRetries == 0 {
		return DefaultTransientRetries
	}
	if r.MaxTransientRetries < 0 {
		return 0
	}
	return r.MaxTransientRetries
}

// shouldRetry 判斷第 attempt 次嘗試（1 起算）失敗後是否還能重試。
// 僅檢查「次數、context、結果形狀、exit code」四項硬條件；是否為暫態錯誤
// 由呼叫端再以 isTransientError(stderrTail) 做最終判定。
//
//   - attempt 已達 resolveMaxRetries()+1（= 1 次原始 + N 次重試）→ false
//   - ctx 已取消 / 逾時（ctx.Err() != nil）→ false
//   - res == nil（命令啟動失敗）→ false
//   - res.ExitCode == 0（成功）→ false
//   - 其餘（exit code 非零）→ true
func (r *SubprocessRunner) shouldRetry(ctx context.Context, res *Result, attempt int) bool {
	if attempt >= r.resolveMaxRetries()+1 {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if res == nil {
		return false
	}
	return res.ExitCode != 0
}

// Run 用 config 的 command/args 執行，替換 {prompt} 和 {promptFile}。
//
// 子程序因暫態 API 錯誤（socket closed、connection reset、rate limit、5xx 等）非零退出時，
// 會以指數退避自動重試同一命令（上限見 MaxTransientRetries / RunnerConfig.TransientRetries），
// 重試成功後透明回傳 exit 0 的 Result，讓 batch 排程不因網路抖動中斷。非暫態失敗、exit 0、
// context 取消 / 逾時、命令啟動失敗一律不重試，且最終一次嘗試的 Result 形狀與 exit code 語義
// 與不重試時完全一致。
func (r *SubprocessRunner) Run(ctx context.Context, prompt string) (*Result, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	for attempt := 1; ; attempt++ {
		res, stderrTail, err := r.runOnce(ctx, prompt)
		if !r.shouldRetry(ctx, res, attempt) || !isTransientError(stderrTail) {
			return res, err
		}

		wait := backoffDuration(attempt, r.backoffBase)
		slog.Warn("runner transient error, retrying",
			"runner", r.Name,
			"attempt", attempt,
			"backoff", wait.String(),
			"exitCode", res.ExitCode,
			"stderrTail", transientLogSnippet(stderrTail))

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return res, err
		case <-timer.C:
		}
	}
}

// runOnce 執行單次嘗試：建 args → 建 cmd → 依輸出模式執行 → buildResult。
// 第二個回傳值是本次捕捉到的 stderr 尾段（pty / quiet 模式為合併輸出），供重試判定使用。
func (r *SubprocessRunner) runOnce(ctx context.Context, prompt string) (*Result, string, error) {
	tail := newTailWriter(maxStderrCapture)

	args, cleanup, err := r.buildArgs(prompt)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, "", fmt.Errorf("runner %s failed to build args: %w", r.Name, err)
	}

	var logFile *os.File
	if r.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(r.LogPath), 0o755); err == nil {
			if f, err := os.OpenFile(r.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				logFile = f
				defer logFile.Close()
				if info, _ := f.Stat(); info != nil && info.Size() > 0 {
					fmt.Fprintf(f, "\n\n--- retry %s ---\n\n", time.Now().Format("2006-01-02 15:04:05"))
				}
			}
		}
	}

	start := time.Now()
	usePty := protocol.BoolVal(r.Config.Tty) && logFile != nil

	env := enrichedEnv()
	env = append(env, r.ExtraEnv...)
	env = gitops.ApplyWorktreeEnv(env, r.Workspace.Root)
	command := resolveCommand(r.Config.Command, env)

	var cmd *exec.Cmd
	if usePty {
		cmd = exec.Command(command, args...)
	} else {
		cmd = exec.CommandContext(ctx, command, args...)
		setupProcGroup(cmd)
	}
	cmd.Dir = r.Workspace.Root
	cmd.Env = env

	if r.Config.OutputFormat == "stream-json" && logFile != nil {
		res, err := r.runStreamJSON(ctx, cmd, logFile, start, prompt, tail)
		return res, tail.String(), err
	}

	var ptmx *os.File

	if usePty {
		var err error
		var stopWatch func()
		ptmx, stopWatch, err = startPty(ctx, cmd)
		if err != nil {
			return nil, "", fmt.Errorf("runner %s failed to start (pty): %w", r.Name, err)
		}

		stripW := newAnsiStripper(logFile)
		copyDone := make(chan struct{})
		go func() {
			io.Copy(io.MultiWriter(os.Stdout, stripW, tail), ptmx)
			close(copyDone)
		}()

		err = cmd.Wait()
		stopWatch()
		ptmx.Close()
		<-copyDone

		duration := time.Since(start).Seconds()
		res, err := r.buildResult(ctx, err, duration)
		return res, tail.String(), err
	}

	if logFile != nil {
		if protocol.BoolVal(r.Config.Quiet) {
			// quiet 模式下 stdout/stderr 合併進同一個 stripper；用同一個 MultiWriter 值同時
			// 指派給兩者，os/exec 才會共用單一 fd，避免對 stripper 的並行寫入。tail 一併 tee。
			merged := io.MultiWriter(newPromptStripper(logFile), tail)
			cmd.Stdout = merged
			cmd.Stderr = merged
		} else {
			cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
			cmd.Stderr = io.MultiWriter(os.Stderr, logFile, tail)
		}
	} else {
		if protocol.BoolVal(r.Config.Quiet) {
			cmd.Stdout = io.Discard
		} else {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = io.MultiWriter(os.Stderr, tail)
	}
	if protocol.BoolVal(r.Config.Stdin) {
		cmd.Stdin = strings.NewReader(prompt)
	}

	err = cmd.Run()
	duration := time.Since(start).Seconds()
	res, err := r.buildResult(ctx, err, duration)
	return res, tail.String(), err
}

// runStreamJSON 用 stream-json processor 執行命令，即時解析輸出到 .log 與 .stream.jsonl。
// tail 用來捕捉合併輸出尾段供重試判定；用同一個 MultiWriter 值同時指派給 stdout/stderr，
// 讓 os/exec 共用單一 fd，避免對非執行緒安全的 processor 並行寫入。
func (r *SubprocessRunner) runStreamJSON(ctx context.Context, cmd *exec.Cmd, logFile *os.File, start time.Time, prompt string, tail io.Writer) (*Result, error) {
	rawPath := strings.TrimSuffix(r.LogPath, ".log") + ".stream.jsonl"
	rawFile, err := os.OpenFile(rawPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("runner %s failed to create stream log: %w", r.Name, err)
	}
	defer rawFile.Close()

	processor := newStreamJSONProcessor(logFile, rawFile)

	merged := io.MultiWriter(processor, tail)
	cmd.Stdout = merged
	cmd.Stderr = merged
	if protocol.BoolVal(r.Config.Stdin) {
		cmd.Stdin = strings.NewReader(prompt)
	}

	err = cmd.Run()
	closeErr := processor.Close()
	if closeErr != nil && err == nil {
		return nil, fmt.Errorf("runner %s failed to process stream-json output: %w", r.Name, closeErr)
	}

	duration := time.Since(start).Seconds()
	return r.buildResult(ctx, err, duration)
}

func (r *SubprocessRunner) buildResult(ctx context.Context, err error, duration float64) (*Result, error) {
	result := &Result{DurationSec: duration}
	if r.LogPath != "" {
		result.LogFile = r.LogPath
	}

	if err != nil {
		if ctx.Err() == context.Canceled {
			return &Result{ExitCode: 0, DurationSec: duration, LogFile: r.LogPath}, context.Canceled
		}
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{ExitCode: ExitSoftFail, DurationSec: duration, LogFile: r.LogPath},
				fmt.Errorf("runner %s timed out after %v", r.Name, r.Timeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			if result.ExitCode < 0 {
				result.ExitCode = ExitHardError
			}
		} else {
			return nil, fmt.Errorf("runner %s failed to start: %w", r.Name, err)
		}
	}

	return result, nil
}

// buildArgs 把 config args 中的 {prompt}、{promptFile}、{model} placeholder 展開成實際值。
//
// 任一 placeholder 無法解析時回傳 error 而非把字面 placeholder 傳給 CLI：
//   - {promptFile} 建立 temp file 失敗 → 包裝原始 error 回傳（過去靜默 fallback，報錯不指向真因）。
//   - {model} 但 ModelOverride 為空 → 回傳 "model not resolved" error（過去會把 "--model {model}" 傳給 CLI 致其報錯）。
//
// 回傳的 cleanup 負責移除已建立的 prompt temp file；error 發生在建檔之後時，呼叫端須先呼叫 cleanup 再返回。
func (r *SubprocessRunner) buildArgs(prompt string) ([]string, func(), error) {
	args := make([]string, len(r.Config.Args))
	var cleanup func()
	modelHandled := false

	for i, arg := range r.Config.Args {
		switch {
		case strings.Contains(arg, "{prompt}"):
			args[i] = strings.ReplaceAll(arg, "{prompt}", prompt)
		case strings.Contains(arg, "{promptFile}"):
			f, err := os.CreateTemp("", "4x-prompt-*.md")
			if err != nil {
				return nil, cleanup, fmt.Errorf("runner %s: create prompt temp file: %w", r.Name, err)
			}
			if _, err := f.WriteString(prompt); err != nil {
				f.Close()
				os.Remove(f.Name())
				return nil, cleanup, fmt.Errorf("runner %s: write prompt temp file: %w", r.Name, err)
			}
			f.Close()
			args[i] = strings.ReplaceAll(arg, "{promptFile}", f.Name())
			cleanup = func() { os.Remove(f.Name()) }
		case strings.Contains(arg, "{model}"):
			if r.ModelOverride == "" {
				return nil, cleanup, fmt.Errorf("model not resolved for runner %s", r.Name)
			}
			args[i] = strings.ReplaceAll(arg, "{model}", r.ModelOverride)
			modelHandled = true
		default:
			args[i] = arg
		}
	}

	if r.ModelOverride != "" && !modelHandled {
		args = append(args, "--model", r.ModelOverride)
	}

	// 對所有 claude role 寫一個 PreToolUse hook settings temp file 並 append --settings，
	// 讓 Claude Code 呼叫 `4x guard-tool` 攔截：Bash 分支攔 reviewer 自跑 git diff/log/show，
	// Edit/Write/MultiEdit 分支攔越界 / 非法寫入 source。非 reviewer 的 Bash 攔截因 FOURX_ROLE
	// 空而自然放行，故一律注入無副作用。非 claude runner 不注入。
	if r.Config.Command == "claude" {
		settingsPath, settingsCleanup, err := writeGuardToolSettings()
		if err != nil {
			return nil, cleanup, fmt.Errorf("runner %s: write guard-tool settings: %w", r.Name, err)
		}
		args = append(args, "--settings", settingsPath)
		cleanup = chainCleanup(cleanup, settingsCleanup)
	}

	return args, cleanup, nil
}

// guardToolSettingsJSON 是注入 claude runner 的 PreToolUse hook settings 內容：對每個 Bash
// 以及 Edit/Write/MultiEdit 工具呼叫先跑 `$FOURX_BIN guard-tool`（由 enrichedEnv 提供
// FOURX_BIN，經 shell 展開）。Bash matcher 攔 reviewer git 探索，Edit|Write|MultiEdit matcher
// 攔越界 / 非法寫入 source。
const guardToolSettingsJSON = `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"\"$FOURX_BIN\" guard-tool"}]},{"matcher":"Edit|Write|MultiEdit","hooks":[{"type":"command","command":"\"$FOURX_BIN\" guard-tool"}]}]}}`

// writeGuardToolSettings 寫出 PreToolUse hook settings temp file，回傳路徑與 cleanup。
func writeGuardToolSettings() (string, func(), error) {
	f, err := os.CreateTemp("", "4x-guard-settings-*.json")
	if err != nil {
		return "", nil, err
	}
	if _, err := f.WriteString(guardToolSettingsJSON); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	f.Close()
	path := f.Name()
	return path, func() { os.Remove(path) }, nil
}

// chainCleanup 把兩個 cleanup func 串成一個（任一為 nil 時回傳另一個），維持與既有 cleanup 契約一致。
func chainCleanup(a, b func()) func() {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return func() {
		a()
		b()
	}
}

func IsSoftFail(r *Result) bool {
	return r != nil && r.ExitCode == ExitSoftFail
}

func IsHardError(r *Result) bool {
	return r != nil && r.ExitCode == ExitHardError
}

// NewRunner 建立 SubprocessRunner，logPath 為空字串時不產生 log file，model 為空字串時不帶 --model flag。
//
// 暫態重試上限由 cfg.TransientRetries 決定：nil → 預設（DefaultTransientRetries）、
// 0 → 停用重試、>0 → 自訂。config 的 0（停用）會映射成 SubprocessRunner.MaxTransientRetries 的
// 負值，以區別於零值（零值代表「用預設」）。
func NewRunner(ws *protocol.Workspace, name string, cfg protocol.RunnerConfig, timeout time.Duration, logPath string, model string) Runner {
	r := &SubprocessRunner{
		Workspace:     ws,
		Config:        cfg,
		Name:          name,
		Timeout:       timeout,
		LogPath:       logPath,
		ModelOverride: model,
	}
	if cfg.TransientRetries != nil {
		if v := *cfg.TransientRetries; v == 0 {
			r.MaxTransientRetries = -1 // config 0 = 停用 → 用負值表達，避免被當成零值（預設）
		} else {
			r.MaxTransientRetries = v
		}
	}
	return r
}

// Factory 依 (name, logPath, model) 建構一個 Runner，供 run.go/evolve.go/batch.go
// 共用同一組「從 workspace + config + timeout 組出 runnerFactory」的邏輯。
type Factory func(name, logPath, model string) Runner

// NewFactory 回傳一個 Factory：呼叫時以 cfg.Runners[name] 解析該 runner 的設定，
// 搭配固定的 ws 與 timeoutSec 建構 Runner。取代原本在 run.go/evolve.go/batch.go
// 各自重複撰寫的 runnerFactory 閉包。
func NewFactory(ws *protocol.Workspace, cfg protocol.Config, timeoutSec int) Factory {
	return func(name, logPath, model string) Runner {
		return NewRunner(ws, name, cfg.Runners[name], time.Duration(timeoutSec)*time.Second, logPath, model)
	}
}

// ansiStripper 以狀態機跨 Write 呼叫正確剝除 ANSI escape sequence，
// 涵蓋 CSI（含 private mode ?）、OSC（BEL 或 ST 結尾）、單字元 ESC 序列。
type ansiStripper struct {
	w     io.Writer
	state stripState
	err   error // inner writer 第一次回報的 error，之後 Write 一律短路
}

type stripState int

const (
	stGround stripState = iota
	stEscape
	stCSI
	stOSC
	stOscEsc  // OSC 裡遇到 ESC，等 backslash 組成 ST
	stCharset // charset designation（ESC ( / ESC )），等吃掉 final byte
)

func newAnsiStripper(w io.Writer) *ansiStripper {
	return &ansiStripper{w: w}
}

func (a *ansiStripper) Write(p []byte) (int, error) {
	if a.err != nil {
		return 0, a.err
	}
	// emit 把已過濾的片段寫入 inner writer；失敗時記下 error 供後續短路。
	emit := func(b []byte) bool {
		if len(b) == 0 {
			return true
		}
		if _, err := a.w.Write(b); err != nil {
			a.err = err
			return false
		}
		return true
	}

	start := 0
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch a.state {
		case stGround:
			if b == 0x1b {
				if i > start && !emit(p[start:i]) {
					return len(p), a.err
				}
				a.state = stEscape
				start = i
			}
		case stEscape:
			switch {
			case b == '[':
				a.state = stCSI
			case b == ']':
				a.state = stOSC
			case b == '(' || b == ')':
				// charset designation（ESC ( / ESC )）：intro byte 已在此，
				// final byte 交由 stCharset 消費，天然跨 Write buffer 邊界。
				a.state = stCharset
				start = i + 1
			default:
				// single-char ESC sequence (e.g. \x1b7, \x1bM)
				a.state = stGround
				start = i + 1
			}
		case stCharset:
			// 消費恰好一個 byte（charset final byte，如 B/0/A）後回 ground
			a.state = stGround
			start = i + 1
		case stCSI:
			// CSI 參數與中間位元組：0x20-0x3F（含 ?;digits space 等）
			// 結束位元組：0x40-0x7E
			if b >= 0x40 && b <= 0x7E {
				a.state = stGround
				start = i + 1
			}
		case stOSC:
			if b == 0x07 {
				a.state = stGround
				start = i + 1
			} else if b == 0x1b {
				a.state = stOscEsc
			}
		case stOscEsc:
			// ST = ESC + backslash
			a.state = stGround
			start = i + 1
		}
	}

	if a.state == stGround && start < len(p) {
		if !emit(p[start:]) {
			return len(p), a.err
		}
	}
	// state != stGround 時，未完成的 escape 序列暫存到下次 Write
	return len(p), nil
}

// promptStripper 過濾 stdin-echo runner（如 codex）輸出中的 prompt 回顯。
// 偵測第一個獨立 "user" 行到下一個獨立 "codex" 行之間的內容並丟棄。
type promptStripper struct {
	dst   io.Writer
	buf   []byte
	state int   // 0=header, 1=skipping, 2=passthrough
	err   error // inner writer 第一次回報的 error，之後 Write 一律短路
}

func newPromptStripper(dst io.Writer) *promptStripper {
	return &promptStripper{dst: dst}
}

func (s *promptStripper) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}

	if s.state == 2 {
		if _, err := s.dst.Write(p); err != nil {
			s.err = err
			return len(p), err
		}
		return len(p), nil
	}

	s.buf = append(s.buf, p...)

	for {
		idx := indexByte(s.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(s.buf[:idx])
		s.buf = s.buf[idx+1:]
		trimmed := strings.TrimSpace(line)

		switch s.state {
		case 0:
			if trimmed == "user" {
				s.state = 1
			} else {
				if _, err := s.dst.Write([]byte(line + "\n")); err != nil {
					s.err = err
					return len(p), err
				}
			}
		case 1:
			if trimmed == "codex" {
				s.state = 2
				if _, err := s.dst.Write([]byte(line + "\n")); err != nil {
					s.err = err
					return len(p), err
				}
				if len(s.buf) > 0 {
					if _, err := s.dst.Write(s.buf); err != nil {
						s.err = err
						return len(p), err
					}
					s.buf = nil
				}
				return len(p), nil
			}
		}
	}
	return len(p), nil
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// LogDir 回傳 .4x/<featureId>/logs/ 的路徑
func LogDir(ws *protocol.Workspace, featureID string) string {
	return filepath.Join(ws.FeatureDir(featureID), "logs")
}

// LogFileName 產生 log 檔名：round-<N>-<role>.log
func LogFileName(round int, role string) string {
	return fmt.Sprintf("round-%d-%s.log", round, role)
}

// StreamLogFileName 產生 stream-json log 檔名：round-<N>-<role>.stream.jsonl。
func StreamLogFileName(round int, role string) string {
	return fmt.Sprintf("round-%d-%s.stream.jsonl", round, role)
}

// IterationLogFileName 產生「同一 round 內第 iteration 次執行」的 log 檔名。
// designer / design-reviewer 這類 role 在 round 不變的情況下也可能重複執行
// （例如 design-reviewing FAIL 打回 designing），iteration 用來避免同名 log
// 檔案互相覆寫。iteration<=1 沿用既有 round-<N>-<role>.log 格式（向下相容），
// iteration>1 才加上 -<iteration> 後綴。
func IterationLogFileName(round int, role string, iteration int) string {
	if iteration <= 1 {
		return LogFileName(round, role)
	}
	return fmt.Sprintf("round-%d-%s-%d.log", round, role, iteration)
}

// DeepFixLogFileName 產生 deep-reviewing 自癒循環中 mini-coder 的 log 檔名：
// round-<round>-deep-fix-<iteration>.log。
func DeepFixLogFileName(round, iteration int) string {
	return fmt.Sprintf("round-%d-deep-fix-%d.log", round, iteration)
}

// DeepReverifyLogFileName 產生 deep-reviewing 自癒循環中 re-verifier 的 log 檔名：
// round-<round>-deep-reverify-<iteration>.log。
func DeepReverifyLogFileName(round, iteration int) string {
	return fmt.Sprintf("round-%d-deep-reverify-%d.log", round, iteration)
}

// ReviewFixLogFileName 產生 reviewing phase 同輪收斂循環中 mini-coder 的 log 檔名：
// round-<round>-review-fix-<iteration>.log。命名刻意與主 reviewer log
// （IterationLogFileName 產生的 round-<round>-reviewer[-<iter>].log）區隔，
// 避免 CONDITIONAL PASS 收斂的 mini-coder log 覆蓋主 reviewer log。
func ReviewFixLogFileName(round, iteration int) string {
	return fmt.Sprintf("round-%d-review-fix-%d.log", round, iteration)
}

// DeepReviewerLogFileName 產生平行 deep review 模式下第 index 個 sub-reviewer 的 log 檔名：
// round-<round>-deep-reviewer-<index>.log（index 為 1-based）。每個 sub-reviewer 用各自的
// log 檔，不與其他 sub-reviewer 共用，讓 dashboard 能分檔即時追蹤。
func DeepReviewerLogFileName(round, index int) string {
	return fmt.Sprintf("round-%d-deep-reviewer-%d.log", round, index)
}
