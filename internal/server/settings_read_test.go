package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestHandleGetSettings_ReturnsValidJSON 驗證 workspace 有合法 settings.json 時，
// handleGetSettings 回傳 HTTP 200 且 body 可解析為 protocol.Config，
// 且 project.name 與寫入值相符。
func TestHandleGetSettings_ReturnsValidJSON(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "settings-test"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude"},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := protocol.NewCachedWorkspace(&protocol.Workspace{Root: root})

	rec := httptest.NewRecorder()
	handleGetSettings(ws, rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got protocol.Config
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Project.Name != "settings-test" {
		t.Errorf("project.name = %q, want settings-test", got.Project.Name)
	}
	if got.Default != "claude" {
		t.Errorf("default_runner = %q, want claude", got.Default)
	}
}

// TestHandleGetSettings_PreservesUnknownFields 驗證 handleGetSettings 直接回傳原始檔案內容，
// 不會因 unmarshal/remarshal 遺失 Config struct 未定義的自訂欄位。
func TestHandleGetSettings_PreservesUnknownFields(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "field-preserve-test"},
		Default: "claude",
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}

	// 直接覆寫 settings.json，加入 Config struct 未定義的自訂欄位
	settingsPath := filepath.Join(root, ".4x", protocol.ConfigFile)
	rawJSON := `{"project":{"name":"field-preserve-test"},"default_runner":"claude","custom_extra_field":"keep-me"}`
	if err := os.WriteFile(settingsPath, []byte(rawJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := protocol.NewCachedWorkspace(&protocol.Workspace{Root: root})
	rec := httptest.NewRecorder()
	handleGetSettings(ws, rec)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "custom_extra_field") {
		t.Errorf("response body should preserve unknown fields; got: %s", body)
	}
	if !strings.Contains(body, "keep-me") {
		t.Errorf("response body should preserve unknown field value; got: %s", body)
	}
}
