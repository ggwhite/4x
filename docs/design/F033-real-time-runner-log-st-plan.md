# F033: Real-time Runner Log Streaming — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 讓 Claude Code runner 的 log 即時寫入檔案，dashboard log viewer 能即時顯示 runner 的工作狀態。

**Architecture:** Runner 層新增 `streamJSONProcessor`（`io.Writer`），解析 Claude Code 的 `--output-format stream-json` 輸出，逐行寫入人類可讀 `.log` 和原始 `.stream.jsonl` 雙檔。Dashboard 零改動——既有 log SSE 自動 tail `.log` 新增內容。

**Tech Stack:** Go 1.22+, `encoding/json`, `bufio`, Go standard testing

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/protocol/types.go:227-235` | Modify | `RunnerConfig` 加 `OutputFormat` 欄位 |
| `internal/runner/stream.go` | Create | `streamJSONProcessor` 實作——逐行解析 stream-json，雙檔寫入 |
| `internal/runner/stream_test.go` | Create | processor 完整單元測試 |
| `internal/runner/runner.go:44-121` | Modify | `Run()` 加 stream-json 分支 |
| `internal/runner/runner.go:347-350` | Modify | 新增 `StreamLogFileName()` helper |
| `internal/runner/runner_test.go` | Modify | 驗證舊路徑不受影響 + stream-json 整合測試 |
| `.4x/settings.json` | Modify | Claude runner config 更新 |

---

### Task 1: RunnerConfig 加 OutputFormat 欄位

**Files:**
- Modify: `internal/protocol/types.go:227-235`

- [ ] **Step 1: 加欄位**

在 `RunnerConfig` struct（`internal/protocol/types.go:227`）的 `Quiet` 後面加一行：

```go
// RunnerConfig 是 LLM runner 的設定
type RunnerConfig struct {
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	Model        string            `json:"model,omitempty"`
	ModelMap     map[string]string `json:"model_map,omitempty"`
	Stdin        bool              `json:"stdin,omitempty"`
	Tty          bool              `json:"tty,omitempty"`
	Quiet        bool              `json:"quiet,omitempty"`
	OutputFormat string            `json:"output_format,omitempty"`
}
```

- [ ] **Step 2: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 3: 跑測試確認無 regression**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat(F033): add OutputFormat field to RunnerConfig"
```

---

### Task 2: streamJSONProcessor — 核心解析邏輯

**Files:**
- Create: `internal/runner/stream.go`
- Create: `internal/runner/stream_test.go`

- [ ] **Step 1: 寫 stream_test.go — assistant text 測試**

```go
package runner

import (
	"bytes"
	"testing"
)

func TestStreamProcessor_AssistantText(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}` + "\n"
	p.Write([]byte(line))

	if got := logBuf.String(); got != "[assistant] Hello world\n" {
		t.Errorf("log = %q, want %q", got, "[assistant] Hello world\n")
	}
	if got := rawBuf.String(); got != line {
		t.Errorf("raw = %q, want %q", got, line)
	}
}
```

- [ ] **Step 2: 跑測試確認 FAIL**

Run: `go test ./internal/runner/ -run TestStreamProcessor_AssistantText -v`
Expected: FAIL — `newStreamJSONProcessor` 未定義

- [ ] **Step 3: 寫 stream.go — 基本結構與 assistant text 解析**

```go
package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// streamJSONProcessor 解析 Claude Code stream-json 輸出，
// 將人類可讀摘要寫入 logW，原始 JSON 寫入 rawW。
type streamJSONProcessor struct {
	logW    io.Writer
	rawW    io.Writer
	scanner *bufio.Scanner
	pr      *io.PipeReader
	pw      *io.PipeWriter
	done    chan struct{}
}

type streamEvent struct {
	Type    string       `json:"type"`
	Subtype string       `json:"subtype,omitempty"`
	Message *streamMsg   `json:"message,omitempty"`
	Result  string       `json:"result,omitempty"`
	Duration float64     `json:"duration_ms,omitempty"`
	Cost    float64      `json:"total_cost_usd,omitempty"`
}

type streamMsg struct {
	Content []streamContent `json:"content,omitempty"`
}

type streamContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// newStreamJSONProcessor 建立 processor，在背景 goroutine 中逐行解析
func newStreamJSONProcessor(logW, rawW io.Writer) *streamJSONProcessor {
	pr, pw := io.Pipe()
	p := &streamJSONProcessor{
		logW:    logW,
		rawW:    rawW,
		scanner: bufio.NewScanner(pr),
		pr:      pr,
		pw:      pw,
		done:    make(chan struct{}),
	}
	p.scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	go p.loop()
	return p
}

func (p *streamJSONProcessor) loop() {
	defer close(p.done)
	for p.scanner.Scan() {
		line := p.scanner.Bytes()
		p.rawW.Write(line)
		p.rawW.Write([]byte("\n"))
		p.processLine(line)
	}
}

func (p *streamJSONProcessor) processLine(line []byte) {
	var ev streamEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}

	switch ev.Type {
	case "assistant":
		p.handleAssistant(&ev)
	case "result":
		p.handleResult(&ev)
	}
}

func (p *streamJSONProcessor) handleAssistant(ev *streamEvent) {
	if ev.Message == nil {
		return
	}
	for _, c := range ev.Message.Content {
		switch c.Type {
		case "text":
			if c.Text != "" {
				fmt.Fprintf(p.logW, "[assistant] %s\n", c.Text)
			}
		case "tool_use":
			summary := summarizeToolInput(c.Input)
			if summary != "" {
				fmt.Fprintf(p.logW, "[tool_use] %s: %s\n", c.Name, summary)
			} else {
				fmt.Fprintf(p.logW, "[tool_use] %s\n", c.Name)
			}
		case "tool_result":
			content := c.Text
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			lines := strings.Count(content, "\n") + 1
			fmt.Fprintf(p.logW, "[tool_result] (%d lines)\n", lines)
		}
	}
}

func (p *streamJSONProcessor) handleResult(ev *streamEvent) {
	result := ev.Result
	if len(result) > 200 {
		result = result[:200] + "..."
	}
	fmt.Fprintf(p.logW, "[result] %s (%.1fs, $%.4f)\n", ev.Subtype, ev.Duration/1000, ev.Cost)
}

// summarizeToolInput 從 tool_use 的 input JSON 提取摘要
func summarizeToolInput(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	if cmd, ok := m["command"].(string); ok {
		if len(cmd) > 120 {
			cmd = cmd[:120] + "..."
		}
		return cmd
	}
	if fp, ok := m["file_path"].(string); ok {
		return fp
	}
	return ""
}

// Write 實作 io.Writer，把 bytes 餵給背景 goroutine
func (p *streamJSONProcessor) Write(data []byte) (int, error) {
	return p.pw.Write(data)
}

// Close 關閉 writer 並等待背景 goroutine 結束
func (p *streamJSONProcessor) Close() error {
	p.pw.Close()
	<-p.done
	return nil
}
```

- [ ] **Step 4: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run TestStreamProcessor_AssistantText -v`
Expected: PASS

- [ ] **Step 5: 寫測試 — tool_use event**

在 `stream_test.go` 加：

```go
func TestStreamProcessor_ToolUse(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"runner.go","old_string":"a","new_string":"b"}}]}}` + "\n"
	p.Write([]byte(line))
	p.Close()

	got := logBuf.String()
	if got != "[tool_use] Edit: runner.go\n" {
		t.Errorf("log = %q, want %q", got, "[tool_use] Edit: runner.go\n")
	}
}
```

- [ ] **Step 6: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run TestStreamProcessor_ToolUse -v`
Expected: PASS

- [ ] **Step 7: 寫測試 — tool_use with command input (Bash)**

```go
func TestStreamProcessor_ToolUseBash(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n"
	p.Write([]byte(line))
	p.Close()

	got := logBuf.String()
	if got != "[tool_use] Bash: go test ./...\n" {
		t.Errorf("log = %q, want %q", got, "[tool_use] Bash: go test ./...\n")
	}
}
```

- [ ] **Step 8: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run TestStreamProcessor_ToolUseBash -v`
Expected: PASS

- [ ] **Step 9: 寫測試 — result event**

```go
func TestStreamProcessor_Result(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	line := `{"type":"result","subtype":"success","duration_ms":45200,"total_cost_usd":0.12}` + "\n"
	p.Write([]byte(line))
	p.Close()

	got := logBuf.String()
	if got != "[result] success (45.2s, $0.1200)\n" {
		t.Errorf("log = %q, want %q", got, "[result] success (45.2s, $0.1200)\n")
	}
}
```

- [ ] **Step 10: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run TestStreamProcessor_Result -v`
Expected: PASS

- [ ] **Step 11: 寫測試 — system events 被過濾**

```go
func TestStreamProcessor_SystemEventsFiltered(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	events := []string{
		`{"type":"system","subtype":"init","session_id":"abc"}`,
		`{"type":"system","subtype":"hook_started","hook_name":"test"}`,
		`{"type":"system","subtype":"hook_response","hook_name":"test"}`,
		`{"type":"system","subtype":"thinking_tokens","estimated_tokens":50}`,
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
	}
	for _, e := range events {
		p.Write([]byte(e + "\n"))
	}
	p.Close()

	if logBuf.Len() != 0 {
		t.Errorf("log should be empty for system events, got %q", logBuf.String())
	}
	// raw 應有全部 5 行
	lines := strings.Count(rawBuf.String(), "\n")
	if lines != 5 {
		t.Errorf("raw line count = %d, want 5", lines)
	}
}
```

（需在 test file 加 `"strings"` import）

- [ ] **Step 12: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run TestStreamProcessor_SystemEventsFiltered -v`
Expected: PASS

- [ ] **Step 13: 寫測試 — 不合法 JSON 不 crash**

```go
func TestStreamProcessor_InvalidJSON(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	p.Write([]byte("not json at all\n"))
	p.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"after bad line"}]}}` + "\n"))
	p.Close()

	if logBuf.String() != "[assistant] after bad line\n" {
		t.Errorf("log = %q, want only the valid assistant line", logBuf.String())
	}
	// raw 有兩行
	if lines := strings.Count(rawBuf.String(), "\n"); lines != 2 {
		t.Errorf("raw line count = %d, want 2", lines)
	}
}
```

- [ ] **Step 14: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run TestStreamProcessor_InvalidJSON -v`
Expected: PASS

- [ ] **Step 15: 寫測試 — 跨 Write 呼叫的行拼接**

```go
func TestStreamProcessor_SplitWrite(t *testing.T) {
	var logBuf, rawBuf bytes.Buffer
	p := newStreamJSONProcessor(&logBuf, &rawBuf)

	full := `{"type":"assistant","message":{"content":[{"type":"text","text":"split test"}]}}` + "\n"
	mid := len(full) / 2
	p.Write([]byte(full[:mid]))
	p.Write([]byte(full[mid:]))
	p.Close()

	if got := logBuf.String(); got != "[assistant] split test\n" {
		t.Errorf("log = %q, want %q", got, "[assistant] split test\n")
	}
}
```

- [ ] **Step 16: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run TestStreamProcessor_SplitWrite -v`
Expected: PASS

- [ ] **Step 17: 跑全部 runner 測試**

Run: `go test ./internal/runner/ -v`
Expected: 全部 PASS

- [ ] **Step 18: Commit**

```bash
git add internal/runner/stream.go internal/runner/stream_test.go
git commit -m "feat(F033): add streamJSONProcessor for real-time log parsing"
```

---

### Task 3: Runner.Run() 加 stream-json 分支

**Files:**
- Modify: `internal/runner/runner.go:44-121` (Run method)
- Modify: `internal/runner/runner.go:347-350` (add StreamLogFileName)

- [ ] **Step 1: 新增 StreamLogFileName helper**

在 `internal/runner/runner.go` 的 `LogFileName` 函式後面加：

```go
// StreamLogFileName 產生 stream-json log 檔名：round-<N>-<role>.stream.jsonl
func StreamLogFileName(round int, role string) string {
	return fmt.Sprintf("round-%d-%s.stream.jsonl", round, role)
}
```

- [ ] **Step 2: 修改 Run() 方法，加 stream-json 分支**

在 `Run()` 中 `start := time.Now()` 之後、`cmd := exec.CommandContext(...)` 之前，插入 stream-json 路徑判斷。完整的 `Run()` 改為：

```go
func (r *SubprocessRunner) Run(ctx context.Context, prompt string) (*Result, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	args, cleanup := r.buildArgs(prompt)
	if cleanup != nil {
		defer cleanup()
	}

	var logFile *os.File
	if r.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(r.LogPath), 0o755); err == nil {
			if f, err := os.Create(r.LogPath); err == nil {
				logFile = f
				defer logFile.Close()
			}
		}
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, r.Config.Command, args...)
	cmd.Dir = r.Workspace.Root

	if r.Config.OutputFormat == "stream-json" && logFile != nil {
		return r.runStreamJSON(ctx, cmd, logFile, start)
	}

	usePty := r.Config.Tty && logFile != nil
	var ptmx *os.File

	if usePty {
		attrs := &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
		var err error
		ptmx, err = pty.StartWithAttrs(cmd, &pty.Winsize{Rows: 50, Cols: 120}, attrs)
		if err != nil {
			return nil, fmt.Errorf("runner %s failed to start (pty): %w", r.Name, err)
		}

		stripW := newAnsiStripper(logFile)
		copyDone := make(chan struct{})
		go func() {
			io.Copy(io.MultiWriter(os.Stdout, stripW), ptmx)
			close(copyDone)
		}()

		err = cmd.Wait()
		ptmx.Close()
		<-copyDone

		duration := time.Since(start).Seconds()
		return r.buildResult(ctx, err, duration)
	}

	if logFile != nil {
		if r.Config.Quiet {
			stripped := newPromptStripper(logFile)
			cmd.Stdout = stripped
			cmd.Stderr = stripped
		} else {
			cmd.Stdout = io.MultiWriter(os.Stdout, logFile)
			cmd.Stderr = io.MultiWriter(os.Stderr, logFile)
		}
	} else {
		if r.Config.Quiet {
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
		} else {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = os.Stderr
	}
	if r.Config.Stdin {
		cmd.Stdin = strings.NewReader(prompt)
	}

	err := cmd.Run()
	duration := time.Since(start).Seconds()
	return r.buildResult(ctx, err, duration)
}
```

- [ ] **Step 3: 實作 runStreamJSON 方法**

在 `runner.go` 中（`buildResult` 之前）加：

```go
// runStreamJSON 用 stream-json processor 執行命令，即時解析輸出到 .log 和 .stream.jsonl
func (r *SubprocessRunner) runStreamJSON(ctx context.Context, cmd *exec.Cmd, logFile *os.File, start time.Time) (*Result, error) {
	rawPath := strings.TrimSuffix(r.LogPath, ".log") + ".stream.jsonl"
	rawFile, err := os.Create(rawPath)
	if err != nil {
		return nil, fmt.Errorf("runner %s failed to create stream log: %w", r.Name, err)
	}
	defer rawFile.Close()

	proc := newStreamJSONProcessor(logFile, rawFile)
	cmd.Stdout = proc
	cmd.Stderr = logFile

	err = cmd.Run()
	proc.Close()

	duration := time.Since(start).Seconds()
	return r.buildResult(ctx, err, duration)
}
```

- [ ] **Step 4: 驗證編譯**

Run: `go build ./cmd/4x && go vet ./...`
Expected: 無錯誤

- [ ] **Step 5: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS（既有 runner 測試不受影響，因為它們的 config 沒設 OutputFormat）

- [ ] **Step 6: Commit**

```bash
git add internal/runner/runner.go
git commit -m "feat(F033): add stream-json branch in SubprocessRunner.Run()"
```

---

### Task 4: Runner 整合測試 — stream-json 路徑

**Files:**
- Modify: `internal/runner/runner_test.go`

- [ ] **Step 1: 寫整合測試 — stream-json 輸出寫入雙檔**

在 `runner_test.go` 底部加：

```go
func TestSubprocessRunner_StreamJSON(t *testing.T) {
	binDir := t.TempDir()
	// 模擬 claude --output-format stream-json 的輸出
	script := `#!/bin/sh
echo '{"type":"system","subtype":"init","session_id":"test"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello from stream"}]}}'
echo '{"type":"result","subtype":"success","duration_ms":1000,"total_cost_usd":0.05}'
exit 0
`
	writeScript(t, binDir, "test-runner", script)

	root := t.TempDir()
	protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "t"}})
	ws := &protocol.Workspace{Root: root}

	logDir := filepath.Join(root, ".4x", "test-feature", "logs")
	os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "round-1-coder.log")

	r := &SubprocessRunner{
		Workspace: ws,
		Name:      "test",
		Config: protocol.RunnerConfig{
			Command:      filepath.Join(binDir, "test-runner"),
			Args:         []string{"-p", "{prompt}"},
			OutputFormat: "stream-json",
		},
		LogPath: logPath,
	}

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	// 驗證 .log 只有人類可讀內容（system event 被過濾）
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logStr := string(logContent)
	if !strings.Contains(logStr, "[assistant] Hello from stream") {
		t.Errorf("log missing assistant text, got:\n%s", logStr)
	}
	if strings.Contains(logStr, "init") {
		t.Errorf("log should not contain system init event, got:\n%s", logStr)
	}

	// 驗證 .stream.jsonl 有全部 3 行
	rawPath := filepath.Join(logDir, "round-1-coder.stream.jsonl")
	rawContent, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("read stream log: %v", err)
	}
	rawLines := strings.Count(strings.TrimSpace(string(rawContent)), "\n") + 1
	if rawLines != 3 {
		t.Errorf("stream.jsonl line count = %d, want 3", rawLines)
	}
}

func TestSubprocessRunner_NoOutputFormat_UsesOldPath(t *testing.T) {
	r, cleanup := setupRunner(t, "#!/bin/sh\necho hello\nexit 0\n")
	defer cleanup()

	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "test.log")
	r.LogPath = logPath

	result, err := r.Run(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	// 舊路徑不產生 .stream.jsonl
	rawPath := strings.TrimSuffix(logPath, ".log") + ".stream.jsonl"
	if _, err := os.Stat(rawPath); err == nil {
		t.Error("stream.jsonl should not exist for non-stream-json runner")
	}
}
```

- [ ] **Step 2: 跑測試確認 PASS**

Run: `go test ./internal/runner/ -run "TestSubprocessRunner_StreamJSON|TestSubprocessRunner_NoOutputFormat" -v`
Expected: 全部 PASS

- [ ] **Step 3: 跑全部測試**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/runner/runner_test.go
git commit -m "test(F033): add runner integration tests for stream-json path"
```

---

### Task 5: 更新 Claude runner config

**Files:**
- Modify: `.4x/settings.json`

- [ ] **Step 1: 更新 settings.json 中的 claude runner**

把 `.4x/settings.json` 中的 `runners.claude` 從：

```json
"claude": {
    "command": "claude",
    "args": [
        "--dangerously-skip-permissions",
        "-p",
        "{prompt}"
    ],
    "model": "opus",
    "tty": true
}
```

改為：

```json
"claude": {
    "command": "claude",
    "args": [
        "--dangerously-skip-permissions",
        "-p",
        "{prompt}",
        "--output-format",
        "stream-json",
        "--verbose"
    ],
    "model": "opus",
    "output_format": "stream-json"
}
```

變更：移除 `tty: true`，args 加 `--output-format stream-json --verbose`，加 `output_format: "stream-json"`。

- [ ] **Step 2: 驗證 JSON 合法**

Run: `python3 -m json.tool .4x/settings.json > /dev/null`
Expected: 無錯誤

- [ ] **Step 3: Commit**

```bash
git add .4x/settings.json
git commit -m "feat(F033): update Claude runner config for stream-json output"
```

---

### Task 6: 全量驗證

- [ ] **Step 1: 跑完整建置與測試**

Run: `make build && make test && make lint`
Expected: 全部 PASS，無 warning

- [ ] **Step 2: 確認 docs 同步**

Run: `make check-docs 2>/dev/null || echo "no check-docs target"`
Expected: PASS 或無此 target

- [ ] **Step 3: 最終 commit（如有遺漏修正）**

只在有修正時 commit。否則跳過。
