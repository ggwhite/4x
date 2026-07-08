package gitops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// setupMonoWithCfg 建立一個 monorepo workspace 並用給定 cfg 建 Ops，供 post-scaffold 測試注入
// cfg.Worktree.PostScaffold。
func setupMonoWithCfg(t *testing.T, cfg protocol.Config) (root string, ws *protocol.Workspace, ops Ops) {
	t.Helper()
	root = t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "user.email", "test@test")
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "init")
	ws = &protocol.Workspace{Root: root}
	ops = New(root, ws, cfg)
	return
}

// countHookEvents 讀 events.jsonl 回傳 type==hook && action==post-scaffold 的事件。
func countHookEvents(t *testing.T, ws *protocol.Workspace, featureID string) []protocol.Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ws.FeatureDir(featureID), protocol.EventsFile))
	if err != nil {
		return nil
	}
	var events []protocol.Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e protocol.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad event line %q: %v", line, err)
		}
		if e.Type == "hook" && e.Action == "post-scaffold" {
			events = append(events, e)
		}
	}
	return events
}

// TestPostScaffold_RunsInWorktree 驗證 AC-7：命令在 worktree 根目錄執行（cmd.Dir==wtDir）。
func TestPostScaffold_RunsInWorktree(t *testing.T) {
	cfg := protocol.Config{
		Project:  protocol.ProjectConfig{Name: "test"},
		Worktree: protocol.WorktreeConfig{PostScaffold: []string{"touch marker.txt"}},
	}
	_, _, ops := setupMonoWithCfg(t, cfg)
	wtPath, err := ops.SetupWorktree("feat-hook", nil)
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "marker.txt")); err != nil {
		t.Errorf("marker.txt should be created in worktree root: %v", err)
	}
}

// TestPostScaffold_FailureWarnsAndContinues 驗證 AC-8：單一命令失敗只 warn 不中止，後續命令續跑。
func TestPostScaffold_FailureWarnsAndContinues(t *testing.T) {
	cfg := protocol.Config{
		Project:  protocol.ProjectConfig{Name: "test"},
		Worktree: protocol.WorktreeConfig{PostScaffold: []string{"exit 1", "touch after.txt"}},
	}
	_, _, ops := setupMonoWithCfg(t, cfg)
	wtPath, err := ops.SetupWorktree("feat-hook-fail", nil)
	if err != nil {
		t.Fatalf("SetupWorktree should not fail on hook error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "after.txt")); err != nil {
		t.Errorf("later command should still run after a failing one: %v", err)
	}
}

// TestPostScaffold_LogsOutput 驗證 AC-9：命令輸出寫入 <主 .4x>/run/<id>/logs/post-scaffold.log。
func TestPostScaffold_LogsOutput(t *testing.T) {
	cfg := protocol.Config{
		Project:  protocol.ProjectConfig{Name: "test"},
		Worktree: protocol.WorktreeConfig{PostScaffold: []string{"echo HELLO_HOOK"}},
	}
	_, ws, ops := setupMonoWithCfg(t, cfg)
	if _, err := ops.SetupWorktree("feat-hook-log", nil); err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	logPath := filepath.Join(ws.FeatureDir("feat-hook-log"), "logs", "post-scaffold.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("post-scaffold.log should exist: %v", err)
	}
	if !strings.Contains(string(data), "HELLO_HOOK") {
		t.Errorf("log should contain command output, got:\n%s", string(data))
	}
}

// TestPostScaffold_EmitsEvent 驗證 AC-10：每個命令產生一筆 type=hook/action=post-scaffold event。
func TestPostScaffold_EmitsEvent(t *testing.T) {
	cfg := protocol.Config{
		Project:  protocol.ProjectConfig{Name: "test"},
		Worktree: protocol.WorktreeConfig{PostScaffold: []string{"echo one", "echo two"}},
	}
	_, ws, ops := setupMonoWithCfg(t, cfg)
	if _, err := ops.SetupWorktree("feat-hook-evt", nil); err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	events := countHookEvents(t, ws, "feat-hook-evt")
	if len(events) != 2 {
		t.Fatalf("expected 2 hook events, got %d", len(events))
	}
	if events[0].Command == "" || events[0].Status == "" {
		t.Errorf("hook event should carry Command and Status: %+v", events[0])
	}
}

// TestPostScaffold_NoConfigNoOp 驗證 AC-11：未設定 post_scaffold 時零改動（無 log、無 event）。
func TestPostScaffold_NoConfigNoOp(t *testing.T) {
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}
	_, ws, ops := setupMonoWithCfg(t, cfg)
	if _, err := ops.SetupWorktree("feat-nohook", nil); err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}
	logPath := filepath.Join(ws.FeatureDir("feat-nohook"), "logs", "post-scaffold.log")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("post-scaffold.log should NOT exist when unconfigured, stat err = %v", err)
	}
	if events := countHookEvents(t, ws, "feat-nohook"); len(events) != 0 {
		t.Errorf("no hook events expected, got %d", len(events))
	}
}

// TestPostScaffold_OnlyOnNewWorktree 驗證 AC-12：只在新建 worktree 時執行；已存在（early-return）不再跑。
func TestPostScaffold_OnlyOnNewWorktree(t *testing.T) {
	cfg := protocol.Config{
		Project:  protocol.ProjectConfig{Name: "test"},
		Worktree: protocol.WorktreeConfig{PostScaffold: []string{"echo again"}},
	}
	_, ws, ops := setupMonoWithCfg(t, cfg)
	if _, err := ops.SetupWorktree("feat-once", nil); err != nil {
		t.Fatalf("first SetupWorktree: %v", err)
	}
	if _, err := ops.SetupWorktree("feat-once", nil); err != nil {
		t.Fatalf("second SetupWorktree: %v", err)
	}
	if events := countHookEvents(t, ws, "feat-once"); len(events) != 1 {
		t.Errorf("hook should run only once (new worktree), got %d events", len(events))
	}
}
