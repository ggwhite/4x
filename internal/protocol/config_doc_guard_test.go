package protocol

import (
	"encoding/json"
	"testing"
)

// TestF160DocGuardConfig 驗證 doc_guard.protected_paths 能被正確反序列化，
// 且未設定時 DocGuard 為 nil（AC-6）。
func TestF160DocGuardConfig(t *testing.T) {
	t.Run("unset-is-nil", func(t *testing.T) {
		var cfg Config
		if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.DocGuard != nil {
			t.Errorf("DocGuard zero-value should be nil, got %+v", cfg.DocGuard)
		}
	})

	t.Run("protected-paths-parsed", func(t *testing.T) {
		raw := `{"doc_guard":{"protected_paths":["docs/","spec/"]}}`
		var cfg Config
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
		if cfg.DocGuard == nil {
			t.Fatal("DocGuard should be non-nil")
		}
		want := []string{"docs/", "spec/"}
		if len(cfg.DocGuard.ProtectedPaths) != len(want) {
			t.Fatalf("ProtectedPaths = %v, want %v", cfg.DocGuard.ProtectedPaths, want)
		}
		for i, p := range want {
			if cfg.DocGuard.ProtectedPaths[i] != p {
				t.Errorf("ProtectedPaths[%d] = %q, want %q", i, cfg.DocGuard.ProtectedPaths[i], p)
			}
		}
	})
}
