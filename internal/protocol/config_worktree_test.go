package protocol

import (
	"encoding/json"
	"testing"
)

// TestConfig_WorktreePostScaffold 驗證 AC-1：settings.json 的 worktree.post_scaffold
// 能正確反序列化到 Config.Worktree.PostScaffold。
func TestConfig_WorktreePostScaffold(t *testing.T) {
	src := `{"worktree":{"post_scaffold":["a","b"]}}`
	var cfg Config
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := cfg.Worktree.PostScaffold
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("PostScaffold = %v, want [a b]", got)
	}
}

// TestConfig_WorktreeOmittedWhenEmpty 驗證未設定 worktree 時欄位為 nil（omitempty 行為）。
func TestConfig_WorktreeOmittedWhenEmpty(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.Worktree.PostScaffold != nil {
		t.Errorf("PostScaffold should be nil when unset, got %v", cfg.Worktree.PostScaffold)
	}
}
