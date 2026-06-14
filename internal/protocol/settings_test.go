package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSettingsSchemaValid 驗證 .4x/settings.json 能完整反序列化為 Config，
// 確保 settings.json 的欄位和 Go struct 定義一致
func TestSettingsSchemaValid(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	path := filepath.Join(root, ".4x", "settings.json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no .4x/settings.json found: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("settings.json failed to unmarshal into Config: %v", err)
	}

	if cfg.Project.Name == "" {
		t.Error("project.name is empty")
	}

	for name, rc := range cfg.Roles {
		if rc.Model == "" && name != "acceptor" {
			t.Errorf("role %q has no model configured", name)
		}
	}

	expectedRoles := []string{"designer", "coder", "reviewer", "tester", "acceptor"}
	for _, r := range expectedRoles {
		if _, ok := cfg.Roles[r]; !ok {
			t.Errorf("missing role %q in settings.json", r)
		}
	}
}
