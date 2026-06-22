package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// setupRunProfileWorkspace 建立含 runners + profiles 的 workspace，供 run preview / profile 測試。
func setupRunProfileWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "f093-test"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "opus", "sonnet": "sonnet"}},
			"gemini": {Command: "gemini", Tiers: map[string]string{"opus": "gemini-pro", "sonnet": "gemini-flash"}},
		},
		Roles: map[string]protocol.RoleConfig{"coder": {Model: "sonnet"}},
		Profiles: map[string]protocol.ProfileConfig{
			"full":   protocol.DefaultProfiles()["full"],
			"normal": protocol.DefaultProfiles()["normal"],
			"quick":  protocol.DefaultProfiles()["quick"],
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	f := feature.Feature{ID: "F001", Name: "Test", Status: "in-progress"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F001"); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestHandleRunPreview_Success(t *testing.T) {
	ws := setupRunProfileWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	body := `{"featureId":"F001","profile":"quick","overrides":[{"phase":"reviewing","runner":"gemini","model":"opus"}]}`
	rec := serveRequest(t, mux, http.MethodPost, "/api/run/preview", body)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var pipeline []protocol.ResolvedPhase
	if err := json.Unmarshal(rec.Body.Bytes(), &pipeline); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pipeline) != 2 {
		t.Fatalf("got %d phases, want 2 (quick = coding, reviewing)", len(pipeline))
	}
	if pipeline[0].Phase != "coding" || pipeline[0].Runner != "claude" {
		t.Errorf("coding: got %+v, want runner=claude", pipeline[0])
	}
	// reviewing 被臨時覆寫成 gemini + opus tier → gemini-pro。
	if pipeline[1].Phase != "reviewing" || pipeline[1].Runner != "gemini" || pipeline[1].Model != "gemini-pro" {
		t.Errorf("reviewing: got %+v, want runner=gemini model=gemini-pro", pipeline[1])
	}
}

func TestHandleRunPreview_BadProfile(t *testing.T) {
	ws := setupRunProfileWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	rec := serveRequest(t, mux, http.MethodPost, "/api/run/preview", `{"featureId":"F001","profile":"ghost"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleRunPreview_BadOverridePhase(t *testing.T) {
	ws := setupRunProfileWorkspace(t)
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil))

	body := `{"featureId":"F001","profile":"quick","overrides":[{"phase":"init","runner":"gemini"}]}`
	rec := serveRequest(t, mux, http.MethodPost, "/api/run/preview", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for non-selectable phase", rec.Code)
	}
}

func TestHandlePostRun_BadProfile(t *testing.T) {
	ws := setupRunProfileWorkspace(t)
	pm := NewProcessManager(ws, 1, "echo")
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), pm))

	rec := serveRequest(t, mux, http.MethodPost, "/api/run", `{"featureId":"F001","profile":"ghost"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown profile", rec.Code)
	}
}

func TestHandlePostRun_BadOverridePhase(t *testing.T) {
	ws := setupRunProfileWorkspace(t)
	pm := NewProcessManager(ws, 1, "echo")
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), pm))

	body := `{"featureId":"F001","profile":"quick","overrides":[{"phase":"bogus","runner":"gemini"}]}`
	rec := serveRequest(t, mux, http.MethodPost, "/api/run", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid override phase", rec.Code)
	}
}

// TestHandlePostRun_OverridesDoNotPersist 確認帶 overrides 的 run 不改寫 settings.json（AC-13）。
func TestHandlePostRun_OverridesDoNotPersist(t *testing.T) {
	ws := setupRunProfileWorkspace(t)
	pm := NewProcessManager(ws, 1, "echo")
	defer pm.Shutdown()
	mux := NewMux(singleResolver(protocol.NewCachedWorkspace(ws), pm))

	settingsPath := filepath.Join(ws.Root, ".4x", "settings.json")
	before, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"featureId":"F001","profile":"quick","overrides":[{"phase":"reviewing","runner":"gemini","model":"opus"}],"maxRounds":1}`
	rec := serveRequest(t, mux, http.MethodPost, "/api/run", body)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	after, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("settings.json changed after run with overrides; temp overrides must not persist")
	}
}
