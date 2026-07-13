package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/runner"
)

// funcRunnerP 把「會用到 prompt」的函式包成 runner.Runner（既有 funcRunner 丟棄 prompt，
// 無法驗證 F185 note 是否進到特定 role 的 prompt，故 note 消費測試改用此型別捕捉 prompt）。
type funcRunnerP func(ctx context.Context, prompt string) (*runner.Result, error)

func (f funcRunnerP) Run(ctx context.Context, prompt string) (*runner.Result, error) {
	return f(ctx, prompt)
}

// writeFakeFiles 依序建立 fake runner 需要寫出的 fixture 檔（先 MkdirAll 各檔父目錄），
// 任一失敗即回傳 error。fake runner 藉此把 fixture 建立失敗顯式暴露為 runner error，
// 而非用 `_ =` 靜默吞掉——後者會讓測試在較晚處以「artifact 缺失」或 prompt assertion
// 失敗，隱藏真正根因。
func writeFakeFiles(files map[string][]byte) error {
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// TestResolvePrompt_RunNoteConsumedOnce 驗證 AC-3：設定 r.runNote 後第一次 resolvePrompt
// 產出的 prompt 含 note、且 r.runNote 被清空；第二次 resolvePrompt 不再含 note。
func TestResolvePrompt_RunNoteConsumedOnce(t *testing.T) {
	id := "F185-rp"
	ws, cfg, feature := setupDeepWS(t, id)
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}}}
	s, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}

	const note = "NOTE-123"
	r.runNote = note

	var pending *prompt.Prefetch
	first := r.resolvePrompt(&pending, protocol.RoleCoder, &s, "claude")
	if !strings.Contains(first, note) {
		t.Errorf("first prompt should contain note %q\n---\n%s", note, first)
	}
	if r.runNote != "" {
		t.Errorf("r.runNote = %q, want empty after first resolvePrompt", r.runNote)
	}

	second := r.resolvePrompt(&pending, protocol.RoleReviewer, &s, "claude")
	if strings.Contains(second, note) {
		t.Errorf("second prompt should NOT contain note %q\n---\n%s", note, second)
	}
}

// TestRunLoop_RunNoteClearedFromDisk 驗證 AC-4：以非空 RunNote 啟動 RunLoop，跑完後重讀
// state.json 其 RunNote 為空字串（disk 已清除，防 crash/resume 與下一次 retry 重播）。
func TestRunLoop_RunNoteClearedFromDisk(t *testing.T) {
	id := "F185-loop"
	ws, cfg, feature := setupConvergeWS(t, id, nil, cleanPassReport)
	// 用終態 phase 讓 RunLoop 進迴圈即 break，聚焦驗證開頭的 disk 清除，不牽動 review/test 邏輯。
	s := protocol.State{FeatureID: id, Phase: protocol.PhasePendingReview, Role: protocol.RoleAcceptor, Round: 1, Active: true, RunNote: "focus X"}
	if err := ws.WriteState(id, s); err != nil {
		t.Fatal(err)
	}
	script := &convergeScript{ws: ws, featureID: feature.ID, round: 1}
	r := newConvergeRunner(ws, cfg, feature, fakeConvergeOps{}, script)

	if _, err := r.RunLoop(context.Background(), s); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	got, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if got.RunNote != "" {
		t.Errorf("RunNote = %q, want empty after RunLoop", got.RunNote)
	}
}

// TestRunReviewTestParallel_RunNoteOnlyReviewer 驗證 AC-3 的特殊路由：從 reviewing 起點
// 進 parallel review/test 時，一次性 note 只注入語意第一個 role（reviewer）的 prompt，
// 併發的 tester 不得收到，且 r.runNote 消費後清空（回歸 round-1 review FAIL：note 洩漏到
// reviewer + tester 兩者）。
func TestRunReviewTestParallel_RunNoteOnlyReviewer(t *testing.T) {
	root := t.TempDir()
	ws, cfg, feature := setupParallelWS(t, root, "F185-par")
	const round = 1

	var mu sync.Mutex
	prompts := map[string]string{}
	factory := func(_, logPath, _ string) runner.Runner {
		return funcRunnerP(func(_ context.Context, p string) (*runner.Result, error) {
			base := filepath.Base(logPath)
			mu.Lock()
			prompts[base] = p
			mu.Unlock()
			roundDir := ws.RoundDir(feature.ID, round)
			files := map[string][]byte{logPath: []byte("exit-0\n")}
			switch {
			case strings.Contains(base, "reviewer"):
				files[filepath.Join(roundDir, protocol.ReviewReport)] = []byte(cleanPassReport)
			case strings.Contains(base, "tester"):
				files[filepath.Join(roundDir, protocol.VerifyFile)] = passingVerify(round)
				files[filepath.Join(roundDir, protocol.TestReport)] = []byte("# Test\n## Verdict\nPASS\n")
				files[filepath.Join(ws.FeatureDir(feature.ID), protocol.FinalReport)] = []byte("# Final\nPASS\n")
			}
			if err := writeFakeFiles(files); err != nil {
				return nil, err
			}
			return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
		})
	}
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}, NewRunner: factory}}

	const note = "PARNOTE-xyz"
	r.runNote = note

	s, err := ws.ReadState(feature.ID)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if _, err := RunReviewTestParallel(context.Background(), r, &s, resolvePC(t, cfg, feature)); err != nil {
		t.Fatalf("RunReviewTestParallel: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	revBase := runner.LogFileName(round, string(protocol.RoleReviewer))
	testBase := runner.IterationLogFileName(round, string(protocol.RoleTester), 1)
	if !strings.Contains(prompts[revBase], note) {
		t.Errorf("reviewer prompt should contain note %q\n---\n%s", note, prompts[revBase])
	}
	if strings.Contains(prompts[testBase], note) {
		t.Errorf("tester prompt should NOT contain note %q\n---\n%s", note, prompts[testBase])
	}
	if r.runNote != "" {
		t.Errorf("r.runNote = %q, want cleared after parallel review", r.runNote)
	}
}

// TestRunDeepReviewParallel_RunNoteOnlyFirstSubReviewer 驗證 AC-3 的特殊路由：從 deep-reviewing
// 起點進 parallel sub-reviewer fan-out 時，一次性 note 只注入第一個 sub-reviewer 的 prompt，
// 其餘 sub-reviewer 與後續 synthesizer 都不得收到，且 r.runNote 消費後清空（回歸 round-1 review
// FAIL：note 洩漏到多個 sub-reviewer / synthesizer）。
func TestRunDeepReviewParallel_RunNoteOnlyFirstSubReviewer(t *testing.T) {
	id := "F185-deeppar"
	ws, cfg, feature := setupDeepWS(t, id)
	const round = 1
	// 兩個 sub-reviewer → 走 runDeepReviewParallel 併發 fan-out 路徑。
	groups := [][]int{{1, 2, 3}, {4, 5, 6}}

	var mu sync.Mutex
	prompts := map[string]string{}
	factory := func(_, logPath, _ string) runner.Runner {
		return funcRunnerP(func(_ context.Context, p string) (*runner.Result, error) {
			base := filepath.Base(logPath)
			mu.Lock()
			prompts[base] = p
			mu.Unlock()
			roundDir := ws.RoundDir(id, round)
			files := map[string][]byte{logPath: []byte("exit-0\n")}
			switch {
			case strings.Contains(base, "deep-reviewer-1"):
				files[filepath.Join(roundDir, prompt.DeepReviewPartialName(1))] = []byte("partial 1\n## Statistics\n")
			case strings.Contains(base, "deep-reviewer-2"):
				files[filepath.Join(roundDir, prompt.DeepReviewPartialName(2))] = []byte("partial 2\n## Statistics\n")
			case strings.Contains(base, "synthesizer"):
				files[filepath.Join(roundDir, protocol.DeepReviewReport)] = []byte(cleanPassReport)
			}
			if err := writeFakeFiles(files); err != nil {
				return nil, err
			}
			return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
		})
	}
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}, NewRunner: factory}}

	const note = "DEEPNOTE-xyz"
	r.runNote = note

	s, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if _, err := r.runDeepReviewParallel(context.Background(), &s, "claude", "claude-sonnet", groups, round); err != nil {
		t.Fatalf("runDeepReviewParallel: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	first := prompts[runner.DeepReviewerLogFileName(round, 1)]
	second := prompts[runner.DeepReviewerLogFileName(round, 2)]
	synth := prompts[runner.LogFileName(round, string(protocol.RoleSynthesizer))]
	if !strings.Contains(first, note) {
		t.Errorf("first sub-reviewer prompt should contain note %q\n---\n%s", note, first)
	}
	if strings.Contains(second, note) {
		t.Errorf("second sub-reviewer prompt should NOT contain note %q\n---\n%s", note, second)
	}
	if strings.Contains(synth, note) {
		t.Errorf("synthesizer prompt should NOT contain note %q\n---\n%s", note, synth)
	}
	if r.runNote != "" {
		t.Errorf("r.runNote = %q, want cleared after deep-review parallel", r.runNote)
	}
}

// TestRunDeepReviewParallel_RunNoteToSynthesizerWhenFirst 驗證 deep_review.go:513 的 consume：
// 當所有 sub-reviewer partial 皆已存在（fan-out 被跳過），synthesizer 就是本次第一個實際產生
// prompt 的 role，note 必須注入 synthesizer 且消費後清空。此為正向回歸——上一個測試（fan-out
// 有 missing）即使刪掉 runSynthesizer 的 clearRunNote() 仍會通過（note 早被第一個 sub-reviewer
// 清除），無法守住此分支；本測試以「r.runNote 清空」斷言鎖住 synthesizer 的 clearRunNote()。
func TestRunDeepReviewParallel_RunNoteToSynthesizerWhenFirst(t *testing.T) {
	id := "F185-synthfirst"
	ws, cfg, feature := setupDeepWS(t, id)
	const round = 1
	groups := [][]int{{1, 2, 3}, {4, 5, 6}}

	// 預先寫出所有 partial → MissingDeepPartials 為空 → 跳過 fan-out，synthesizer 為第一個 executor。
	roundDir := ws.RoundDir(id, round)
	for i := 1; i <= len(groups); i++ {
		if err := os.WriteFile(filepath.Join(roundDir, prompt.DeepReviewPartialName(i)), []byte("partial\n## Statistics\n"), 0o644); err != nil {
			t.Fatalf("write partial %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	prompts := map[string]string{}
	factory := func(_, logPath, _ string) runner.Runner {
		return funcRunnerP(func(_ context.Context, p string) (*runner.Result, error) {
			base := filepath.Base(logPath)
			mu.Lock()
			prompts[base] = p
			mu.Unlock()
			files := map[string][]byte{logPath: []byte("exit-0\n")}
			if strings.Contains(base, "synthesizer") {
				files[filepath.Join(roundDir, protocol.DeepReviewReport)] = []byte(cleanPassReport)
			}
			if err := writeFakeFiles(files); err != nil {
				return nil, err
			}
			return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
		})
	}
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}, NewRunner: factory}}

	const note = "SYNTHNOTE-xyz"
	r.runNote = note

	s, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if _, err := r.runDeepReviewParallel(context.Background(), &s, "claude", "claude-sonnet", groups, round); err != nil {
		t.Fatalf("runDeepReviewParallel: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	synth := prompts[runner.LogFileName(round, string(protocol.RoleSynthesizer))]
	if !strings.Contains(synth, note) {
		t.Errorf("synthesizer (first executor) prompt should contain note %q\n---\n%s", note, synth)
	}
	if r.runNote != "" {
		t.Errorf("r.runNote = %q, want cleared after synthesizer consumed note", r.runNote)
	}
}

// TestRunDeepSubRole_RunNoteOnlyFirstSubRole 驗證 deep_review.go:585 的 consume：single-agent
// deep-review 走 runDeepSubRole（→ runSubRole）時，一次性 note 只注入第一個 sub-role 的 prompt，
// 同輪後續 sub-role（如 re-verifier）不得收到，且第一個 sub-role 消費後 r.runNote 清空。此為正向
// 回歸——鎖住 runSubRole 的 clearRunNote()：若刪除，第二個 sub-role 仍會拿到 note、本測試必敗。
func TestRunDeepSubRole_RunNoteOnlyFirstSubRole(t *testing.T) {
	id := "F185-subrole"
	ws, cfg, feature := setupDeepWS(t, id)
	const round = 1

	var mu sync.Mutex
	prompts := map[string]string{}
	factory := func(_, logPath, _ string) runner.Runner {
		return funcRunnerP(func(_ context.Context, p string) (*runner.Result, error) {
			base := filepath.Base(logPath)
			mu.Lock()
			prompts[base] = p
			mu.Unlock()
			if err := writeFakeFiles(map[string][]byte{logPath: []byte("exit-0\n")}); err != nil {
				return nil, err
			}
			return &runner.Result{ExitCode: 0, LogFile: logPath}, nil
		})
	}
	r := &Runner{Config: Config{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: cfg, Ops: fakeConvergeOps{}, NewRunner: factory}}

	const note = "SUBNOTE-xyz"
	r.runNote = note

	s, err := ws.ReadState(id)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	firstLog := runner.LogFileName(round, string(protocol.RoleDeepReviewer))
	secondLog := runner.DeepReverifyLogFileName(round, 1)

	if _, err := r.runDeepSubRole(context.Background(), &s, protocol.RoleDeepReviewer, "claude", "claude-sonnet", firstLog, round, 0); err != nil {
		t.Fatalf("runDeepSubRole first: %v", err)
	}
	if r.runNote != "" {
		t.Errorf("r.runNote = %q, want cleared after first sub-role", r.runNote)
	}
	if _, err := r.runDeepSubRole(context.Background(), &s, protocol.RoleReVerifier, "claude", "claude-sonnet", secondLog, round, 0); err != nil {
		t.Fatalf("runDeepSubRole second: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(prompts[firstLog], note) {
		t.Errorf("first sub-role prompt should contain note %q\n---\n%s", note, prompts[firstLog])
	}
	if strings.Contains(prompts[secondLog], note) {
		t.Errorf("second sub-role prompt should NOT contain note %q\n---\n%s", note, prompts[secondLog])
	}
}
