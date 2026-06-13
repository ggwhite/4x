package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

func testContextWithTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 100*time.Millisecond)
}

func setupServerWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "server-test"}, Default: "claude"}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	f := protocol.Feature{ID: "test-feat", Name: "Test Feature", Status: "in-progress"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("test-feat"); err != nil {
		t.Fatal(err)
	}
	state := protocol.State{
		FeatureID: "test-feat",
		Phase:     protocol.PhaseCoding,
		Role:      protocol.RoleCoder,
		Round:     1,
		Active:    true,
		Pid:       os.Getpid(),
		Runner:    "claude",
	}
	if err := ws.WriteState("test-feat", state); err != nil {
		t.Fatal(err)
	}

	return ws
}

func serveRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestGetTasks(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/tasks", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var tasks []taskInfo
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if tasks[0].ID != "test-feat" {
		t.Errorf("ID = %s, want test-feat", tasks[0].ID)
	}
	if tasks[0].Phase != "coding" {
		t.Errorf("Phase = %s, want coding", tasks[0].Phase)
	}
	if !tasks[0].Active {
		t.Error("Active should be true")
	}
}

func TestGetEvents_Empty(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/events/test-feat", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var events []json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events = %d, want 0", len(events))
	}
}

func TestGetEvents_WithData(t *testing.T) {
	ws := setupServerWorkspace(t)

	evt := protocol.Event{Type: "phase-start", Phase: protocol.PhaseDesigning, Round: 1}
	if err := ws.AppendEvent("test-feat", evt); err != nil {
		t.Fatal(err)
	}
	evt2 := protocol.Event{Type: "step", Detail: "analyzing", Round: 1}
	if err := ws.AppendEvent("test-feat", evt2); err != nil {
		t.Fatal(err)
	}

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/events/test-feat", "")

	var events []json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("events = %d, want 2", len(events))
	}
}

func TestGetMessages_Empty(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/messages/test-feat", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestIndexHTML(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %s, want text/html*", ct)
	}
}

func TestSSEEndpoint_ContentType(t *testing.T) {
	ws := setupServerWorkspace(t)
	ctx, cancel := testContextWithTimeout(t)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/sse/events/test-feat", nil).WithContext(ctx)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()

	NewMux(ws, nil).ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", ct)
	}
}

func setupServerWithPM(t *testing.T) (*protocol.Workspace, *ProcessManager) {
	t.Helper()
	ws := setupServerWorkspace(t)
	pm := NewProcessManager(ws, 2, fakeRunCommand(t))
	return ws, pm
}

func TestPostRun(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()

	body := `{"featureId":"test-feat","maxRounds":3}`
	rec := serveRequest(t, NewMux(ws, pm), http.MethodPost, "/api/run", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info RunInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.FeatureID != "test-feat" {
		t.Errorf("FeatureID = %q, want test-feat", info.FeatureID)
	}
	if info.Runner != "claude" {
		t.Errorf("Runner = %q, want claude", info.Runner)
	}
}

func TestPostRun_Conflict(t *testing.T) {
	ws := setupServerWorkspace(t)
	pm := NewProcessManager(ws, 1, fakeRunCommand(t))
	defer pm.Shutdown()
	handler := NewMux(ws, pm)

	body := `{"featureId":"test-feat"}`
	serveRequest(t, handler, http.MethodPost, "/api/run", body)

	rec := serveRequest(t, handler, http.MethodPost, "/api/run", body)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestPostRun_NotFound(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()

	body := `{"featureId":"missing"}`
	rec := serveRequest(t, NewMux(ws, pm), http.MethodPost, "/api/run", body)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPostDone_MergeConflictKeepsPendingReview(t *testing.T) {
	ws := setupServerWorkspace(t)
	makePendingReview(t, ws, "test-feat")
	setupConflictingWorktree(t, ws.Root, "test-feat")

	rec := serveRequest(t, NewMux(ws, nil), http.MethodPost, "/api/done", `{"id":"test-feat"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Status        string   `json:"status"`
		MergeConflict bool     `json:"merge_conflict"`
		Conflicts     []string `json:"conflicts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.MergeConflict {
		t.Fatalf("merge_conflict = false, body = %+v", body)
	}
	if body.Status != string(protocol.PhasePendingReview) {
		t.Fatalf("status = %q, want pending-review", body.Status)
	}

	s, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != protocol.PhasePendingReview {
		t.Fatalf("state phase = %q, want pending-review", s.Phase)
	}

	f, err := ws.LoadFeature("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != "ready-for-review" {
		t.Fatalf("feature status = %q, want ready-for-review", f.Status)
	}
}

func makePendingReview(t *testing.T, ws *protocol.Workspace, featureID string) {
	t.Helper()
	s, err := ws.ReadState(featureID)
	if err != nil {
		t.Fatal(err)
	}
	s.Phase = protocol.PhasePendingReview
	s.Active = false
	if err := ws.WriteState(featureID, s); err != nil {
		t.Fatal(err)
	}
	f, err := ws.LoadFeature(featureID)
	if err != nil {
		t.Fatal(err)
	}
	f.Status = "ready-for-review"
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
}

func setupConflictingWorktree(t *testing.T, root, featureID string) {
	t.Helper()
	runGit(t, root, "init")
	if out, err := exec.Command("git", "-C", root, "config", "user.name", "test").CombinedOutput(); err != nil {
		t.Fatalf("git config user.name: %s: %v", out, err)
	}
	if out, err := exec.Command("git", "-C", root, "config", "user.email", "test@test.com").CombinedOutput(); err != nil {
		t.Fatalf("git config user.email: %s: %v", out, err)
	}
	if err := os.WriteFile(filepath.Join(root, "conflict.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "conflict.txt")
	runGit(t, root, "commit", "-m", "base")

	wtDir := filepath.Join(root, ".worktrees", "4x", featureID)
	runGit(t, root, "worktree", "add", wtDir, "-b", "4x/"+featureID)
	if err := os.WriteFile(filepath.Join(wtDir, "conflict.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wtDir, "add", "conflict.txt")
	runGit(t, wtDir, "commit", "-m", "feature")

	if err := os.WriteFile(filepath.Join(root, "conflict.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "conflict.txt")
	runGit(t, root, "commit", "-m", "main")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func TestGetRuns(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	handler := NewMux(ws, pm)

	body := `{"featureId":"test-feat"}`
	serveRequest(t, handler, http.MethodPost, "/api/run", body)

	rec := serveRequest(t, handler, http.MethodGet, "/api/runs", "")

	var runs []RunInfo
	if err := json.NewDecoder(rec.Body).Decode(&runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
}

func TestPostStop(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()
	handler := NewMux(ws, pm)

	runBody := `{"featureId":"test-feat"}`
	resp := serveRequest(t, handler, http.MethodPost, "/api/run", runBody)
	var info RunInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}

	stopBody := fmt.Sprintf(`{"id":%q}`, info.ID)
	rec := serveRequest(t, handler, http.MethodPost, "/api/stop", stopBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestPostStop_NotFound(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()

	body := `{"id":"nonexistent"}`
	rec := serveRequest(t, NewMux(ws, pm), http.MethodPost, "/api/stop", body)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPostNew(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()

	body := `{"name":"My New Feature","description":"test desc"}`
	rec := serveRequest(t, NewMux(ws, pm), http.MethodPost, "/api/new", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Name != "My New Feature" {
		t.Errorf("Name = %q, want My New Feature", result.Name)
	}
	if _, err := ws.LoadFeature(result.ID); err != nil {
		t.Errorf("LoadFeature(%q) failed: %v", result.ID, err)
	}
}

func TestPostNew_MissingName(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()

	body := `{"description":"no name"}`
	rec := serveRequest(t, NewMux(ws, pm), http.MethodPost, "/api/new", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetSettings(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/settings", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var cfg protocol.Config
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Project.Name != "server-test" {
		t.Errorf("project.name = %q, want server-test", cfg.Project.Name)
	}
}

func TestPutSettings_Valid(t *testing.T) {
	ws := setupServerWorkspace(t)
	body := `{"project":{"name":"updated-name","description":"new desc"},"default_runner":"claude"}`
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/settings", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	// 驗證設定已更新
	cfg, err := ws.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Project.Name != "updated-name" {
		t.Errorf("project.name = %q, want updated-name", cfg.Project.Name)
	}
}

func TestPutSettings_BackupCreated(t *testing.T) {
	ws := setupServerWorkspace(t)
	body := `{"project":{"name":"backup-test"}}`
	serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/settings", body)

	bakPath := filepath.Join(ws.DotDir(), "settings.json.bak")
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("backup file not found: %v", err)
	}
}

func TestPutSettings_InvalidJSON(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/settings", `{broken json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutSettings_MissingProjectName(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/settings", `{"project":{"name":""}}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutSettings_FullReplacement(t *testing.T) {
	ws := setupServerWorkspace(t)

	settingsPath := filepath.Join(ws.DotDir(), "settings.json")
	oldContent := `{"project":{"name":"test"},"custom_key":"custom_value"}`
	if err := os.WriteFile(settingsPath, []byte(oldContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// PUT 不含 custom_key — 全量替換，custom_key 應被移除
	body := `{"project":{"name":"test"}}`
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/settings", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "custom_key") {
		t.Errorf("custom_key should not be preserved with full replacement; got: %s", string(data))
	}
}

func TestPutSettings_MethodNotAllowed(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPost, "/api/settings", `{}`)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func seedScreenshots(t *testing.T, ws *protocol.Workspace, featureID string) {
	t.Helper()
	round2Dir := ws.RoundDir(featureID, 2)
	if err := os.MkdirAll(round2Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	verify := protocol.VerifyEvidence{
		Passed: true,
		Round:  2,
		Role:   protocol.RoleTester,
		Screenshots: []protocol.Screenshot{
			{Path: "e2e/test-feat/screenshot/02-round-two.png", Step: "02", Description: "round two"},
		},
	}
	verifyData, _ := json.Marshal(verify)
	if err := os.WriteFile(filepath.Join(round2Dir, protocol.VerifyFile), verifyData, 0o644); err != nil {
		t.Fatal(err)
	}

	shotDir := filepath.Join(ws.DotDir(), "e2e", featureID, "screenshot")
	if err := os.MkdirAll(shotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shotDir, "01-round-one.png"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shotDir, "02-round-two.png"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGetScreenshots(t *testing.T) {
	ws := setupServerWorkspace(t)
	seedScreenshots(t, ws, "test-feat")

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/features/test-feat/screenshots", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var got screenshotsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != 2 {
		t.Fatalf("total = %d, want 2", got.Total)
	}
	if len(got.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(got.Groups))
	}
	if got.Groups[0].Round != 1 || len(got.Groups[0].Screenshots) != 1 {
		t.Fatalf("group[0] = %+v, want round 1 with 1 screenshot", got.Groups[0])
	}
	if got.Groups[1].Round != 2 || len(got.Groups[1].Screenshots) != 1 {
		t.Fatalf("group[1] = %+v, want round 2 with 1 screenshot", got.Groups[1])
	}
}

func TestServeScreenshot(t *testing.T) {
	ws := setupServerWorkspace(t)
	seedScreenshots(t, ws, "test-feat")

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/features/test-feat/screenshots/01-round-one.png", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content-type = %s, want image/png", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "one" {
		t.Fatalf("body = %q, want one", rec.Body.String())
	}
}

func TestServeScreenshot_InvalidExtensionRejected(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/features/test-feat/screenshots/state.json", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetOverview(t *testing.T) {
	ws := setupServerWorkspace(t)
	designDir := filepath.Join(ws.Root, "docs", "design")
	if err := os.MkdirAll(designDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designDir, "test-feat-spec.md"), []byte("# Spec\ntest spec content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designDir, "test-feat-plan.md"), []byte("# Plan\ntest plan content"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/test-feat", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.ID != "test-feat" {
		t.Errorf("ID = %s, want test-feat", info.ID)
	}
	if info.Name != "Test Feature" {
		t.Errorf("Name = %s, want Test Feature", info.Name)
	}
	if info.Spec != "# Spec\ntest spec content" {
		t.Errorf("Spec = %q, want spec content", info.Spec)
	}
	if info.SpecSource != "docs/design/test-feat-spec.md" {
		t.Errorf("SpecSource = %q, want docs/design/test-feat-spec.md", info.SpecSource)
	}
	if info.Plan != "# Plan\ntest plan content" {
		t.Errorf("Plan = %q, want plan content", info.Plan)
	}
	if info.PlanSource != "docs/design/test-feat-plan.md" {
		t.Errorf("PlanSource = %q, want docs/design/test-feat-plan.md", info.PlanSource)
	}
}

func TestGetOverview_NotFound(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/nonexistent", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetOverview_NoDocs(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/test-feat", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Spec != "" {
		t.Errorf("Spec should be empty, got %q", info.Spec)
	}
	if info.SpecSource != "" {
		t.Errorf("SpecSource should be empty, got %q", info.SpecSource)
	}
	if info.Plan != "" {
		t.Errorf("Plan should be empty, got %q", info.Plan)
	}
	if info.PlanSource != "" {
		t.Errorf("PlanSource should be empty, got %q", info.PlanSource)
	}
}

func TestGetOverview_YAMLPathOverride(t *testing.T) {
	ws := setupServerWorkspace(t)
	specDir := filepath.Join(ws.Root, "custom")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "my-spec.md"), []byte("custom spec"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ws.LoadFeature("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	f.Spec = "custom/my-spec.md"
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/test-feat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Spec != "custom spec" {
		t.Errorf("Spec = %q, want custom spec", info.Spec)
	}
	if info.SpecSource != "custom/my-spec.md" {
		t.Errorf("SpecSource = %q, want custom/my-spec.md", info.SpecSource)
	}
}

func TestGetOverview_YAMLPathOverrideEmptyFile(t *testing.T) {
	ws := setupServerWorkspace(t)
	specDir := filepath.Join(ws.Root, "custom")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "empty-spec.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	designDir := filepath.Join(ws.Root, "docs", "design")
	if err := os.MkdirAll(designDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(designDir, "test-feat-spec.md"), []byte("fallback spec"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := ws.LoadFeature("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	f.Spec = "custom/empty-spec.md"
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/overview/test-feat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Spec != "" {
		t.Errorf("Spec = %q, want empty string", info.Spec)
	}
	if info.SpecSource != "custom/empty-spec.md" {
		t.Errorf("SpecSource = %q, want custom/empty-spec.md", info.SpecSource)
	}
}

func TestGetLocales(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}

	var locales []string
	if err := json.NewDecoder(rec.Body).Decode(&locales); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := []string{"en", "zh-TW", "zh-CN", "ja", "ko", "es"}
	if len(locales) != len(want) {
		t.Fatalf("locales = %v, want %v", locales, want)
	}
	for i, w := range want {
		if locales[i] != w {
			t.Errorf("locales[%d] = %s, want %s", i, locales[i], w)
		}
	}
}

func TestGetLocaleEN(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales/en", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}

	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %s, want immutable", cc)
	}

	var translations map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&translations); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if translations["app.title"] != "4x Live" {
		t.Errorf("app.title = %q, want '4x Live'", translations["app.title"])
	}
}

func TestGetLocaleZhTW(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales/zh-TW", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var translations map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&translations); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := translations["app.title"]; !ok {
		t.Error("zh-TW missing key app.title")
	}
}

func TestGetLocaleUnknownFallback(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/locales/nonexistent", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var translations map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&translations); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if translations["app.title"] != "4x Live" {
		t.Errorf("fallback should return en.json, got app.title = %q", translations["app.title"])
	}
}

func TestGetUserConfig(t *testing.T) {
	ws := setupServerWorkspace(t)

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	userCfg := protocol.UserConfig{Locale: "zh-TW", Theme: "dark"}
	protocol.WriteUserConfig(userCfg)

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/user-config", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got protocol.UserConfig
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Locale != "zh-TW" {
		t.Errorf("Locale = %q", got.Locale)
	}
	if got.Theme != "dark" {
		t.Errorf("Theme = %q", got.Theme)
	}
}

func TestGetUserConfig_NotExists(t *testing.T) {
	ws := setupServerWorkspace(t)

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/user-config", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (empty config)", rec.Code)
	}
}

func TestPutUserConfig(t *testing.T) {
	ws := setupServerWorkspace(t)

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	protocol.WriteUserConfig(protocol.UserConfig{Locale: "en"})

	body := `{"locale":"ja","theme":"light"}`
	rec := serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/user-config", body)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	cfg, _ := protocol.ReadUserConfig()
	if cfg.Locale != "ja" {
		t.Errorf("Locale = %q, want ja", cfg.Locale)
	}
	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want light", cfg.Theme)
	}
}

func TestPutUserConfig_BackupCreated(t *testing.T) {
	ws := setupServerWorkspace(t)

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	protocol.WriteUserConfig(protocol.UserConfig{Locale: "en"})

	body := `{"locale":"ja"}`
	serveRequest(t, NewMux(ws, nil), http.MethodPut, "/api/user-config", body)

	path, _ := protocol.UserConfigPath()
	bakPath := path + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestGetMergedConfig(t *testing.T) {
	ws := setupServerWorkspace(t)

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	userCfg := protocol.UserConfig{
		DefaultRunner: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "/opt/claude"},
		},
	}
	protocol.WriteUserConfig(userCfg)

	rec := serveRequest(t, NewMux(ws, nil), http.MethodGet, "/api/merged-config", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Runners["claude"].Command != "/opt/claude" {
		t.Errorf("merged runner command = %q", got.Runners["claude"].Command)
	}
}
