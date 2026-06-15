package server

import (
	"bufio"
	"bytes"
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
		Active:    false,
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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/tasks", "")

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
	if tasks[0].Active {
		t.Error("Active should be false")
	}
}

func TestGetEvents_Empty(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/events/test-feat", "")

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

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/events/test-feat", "")

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/messages/test-feat", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestIndexHTML(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/", "")

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

	NewMux(protocol.NewCachedWorkspace(ws), nil).ServeHTTP(rec, req)

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), pm), http.MethodPost, "/api/run", body)

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
	handler := NewMux(protocol.NewCachedWorkspace(ws), pm)

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), pm), http.MethodPost, "/api/run", body)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPostDone_MergeConflictKeepsPendingReview(t *testing.T) {
	ws := setupServerWorkspace(t)
	makePendingReview(t, ws, "test-feat")
	setupConflictingWorktree(t, ws.Root, "test-feat")

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPost, "/api/done", `{"id":"test-feat"}`)
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
	handler := NewMux(protocol.NewCachedWorkspace(ws), pm)

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
	handler := NewMux(protocol.NewCachedWorkspace(ws), pm)

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), pm), http.MethodPost, "/api/stop", body)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPostNew(t *testing.T) {
	ws, pm := setupServerWithPM(t)
	defer pm.Shutdown()

	body := `{"name":"My New Feature","description":"test desc"}`
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), pm), http.MethodPost, "/api/new", body)

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), pm), http.MethodPost, "/api/new", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetSettings(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/settings", "")

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPut, "/api/settings", body)

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
	serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPut, "/api/settings", body)

	bakPath := filepath.Join(ws.DotDir(), "settings.json.bak")
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("backup file not found: %v", err)
	}
}

func TestPutSettings_InvalidJSON(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPut, "/api/settings", `{broken json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPutSettings_MissingProjectName(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPut, "/api/settings", `{"project":{"name":""}}`)

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPut, "/api/settings", body)

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPost, "/api/settings", `{}`)

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

func seedSameNameScreenshots(t *testing.T, ws *protocol.Workspace, featureID string) {
	t.Helper()
	for _, round := range []int{1, 2} {
		roundDir := ws.RoundDir(featureID, round)
		if err := os.MkdirAll(roundDir, 0o755); err != nil {
			t.Fatal(err)
		}
		verify := protocol.VerifyEvidence{
			Passed: true,
			Round:  round,
			Role:   protocol.RoleTester,
			Screenshots: []protocol.Screenshot{
				{
					Path:        fmt.Sprintf("e2e/%s/round-%d/01-login.png", featureID, round),
					Step:        "01",
					Description: fmt.Sprintf("round %d", round),
				},
			},
		}
		verifyData, _ := json.Marshal(verify)
		if err := os.WriteFile(filepath.Join(roundDir, protocol.VerifyFile), verifyData, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, round := range []int{1, 2} {
		shotDir := filepath.Join(ws.DotDir(), "e2e", featureID, fmt.Sprintf("round-%d", round))
		if err := os.MkdirAll(shotDir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "one"
		if round == 2 {
			body = "two"
		}
		if err := os.WriteFile(filepath.Join(shotDir, "01-login.png"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGetScreenshots(t *testing.T) {
	ws := setupServerWorkspace(t)
	seedScreenshots(t, ws, "test-feat")

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/features/test-feat/screenshots", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var got screenshotsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// seedScreenshots 的截圖目錄沒有 {round} 佔位符，dir 掃描分配到最新 round（2）。
	// round 2 = verify 的 02-round-two.png + dir 的 01-round-one.png = 2 screenshots。
	if got.Total != 2 {
		t.Fatalf("total = %d, want 2", got.Total)
	}
	if len(got.Groups) != 1 {
		t.Fatalf("groups = %d, want 1 (all assigned to latest round 2)", len(got.Groups))
	}
	if got.Groups[0].Round != 2 || len(got.Groups[0].Screenshots) != 2 {
		t.Fatalf("group[0] = %+v, want round 2 with 2 screenshots", got.Groups[0])
	}
}

func TestServeScreenshot(t *testing.T) {
	ws := setupServerWorkspace(t)
	seedScreenshots(t, ws, "test-feat")

	mux := NewMux(protocol.NewCachedWorkspace(ws), nil)

	// 先從 listing 取得 opaque token URL
	listRec := httptest.NewRecorder()
	listReq := httptest.NewRequest("GET", "/api/features/test-feat/screenshots", nil)
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", listRec.Code, listRec.Body.String())
	}
	var listResp screenshotsResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total == 0 {
		t.Fatal("no screenshots found in listing")
	}
	// 找 01-round-one.png 的 URL
	var imgURL string
	for _, g := range listResp.Groups {
		for _, s := range g.Screenshots {
			if s.Filename == "01-round-one.png" {
				imgURL = s.URL
			}
		}
	}
	if imgURL == "" {
		t.Fatal("01-round-one.png not found in listing")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", imgURL, nil)
	mux.ServeHTTP(rec, req)
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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/features/test-feat/screenshots/state.json", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetScreenshots_UniqueURLForSameFilenameAcrossRounds(t *testing.T) {
	ws := setupServerWorkspace(t)
	seedSameNameScreenshots(t, ws, "test-feat")

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/features/test-feat/screenshots", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var got screenshotsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(got.Groups))
	}
	u1 := got.Groups[0].Screenshots[0].URL
	u2 := got.Groups[1].Screenshots[0].URL
	if u1 == u2 {
		t.Fatalf("urls should be unique, got %q", u1)
	}
	if strings.Contains(u1, "/round-1/") || strings.Contains(u2, "/round-2/") {
		t.Fatalf("urls should use opaque token, got %q and %q", u1, u2)
	}
}

func TestServeScreenshot_SameFilenameAcrossRoundsByEncodedPath(t *testing.T) {
	ws := setupServerWorkspace(t)
	seedSameNameScreenshots(t, ws, "test-feat")

	round1 := encodeScreenshotToken("e2e/test-feat/round-1/01-login.png")
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/features/test-feat/screenshots/"+round1, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "one" {
		t.Fatalf("body = %q, want one", rec.Body.String())
	}

	round2 := encodeScreenshotToken("e2e/test-feat/round-2/01-login.png")
	rec = serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/features/test-feat/screenshots/"+round2, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "two" {
		t.Fatalf("body = %q, want two", rec.Body.String())
	}
}

func TestServeScreenshot_AmbiguousBasenameRejected(t *testing.T) {
	ws := setupServerWorkspace(t)
	seedSameNameScreenshots(t, ws, "test-feat")

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/features/test-feat/screenshots/01-login.png", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestScreenshots_InvalidFeatureID(t *testing.T) {
	ws := setupServerWorkspace(t)
	mux := NewMux(protocol.NewCachedWorkspace(ws), NewProcessManager(ws, 1, "echo"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/features/feat..evil/screenshots", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("GET feat..evil = %d, want 400", rec.Code)
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

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/overview/test-feat", "")

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/overview/nonexistent", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestGetOverview_NoDocs(t *testing.T) {
	ws := setupServerWorkspace(t)
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/overview/test-feat", "")

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

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/overview/test-feat", "")
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

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/overview/test-feat", "")
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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/locales", "")

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/locales/en", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %s, want application/json", ct)
	}

	cc := rec.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %s, want no-cache", cc)
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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/locales/zh-TW", "")

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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/locales/nonexistent", "")

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

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/user-config", "")
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

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/user-config", "")
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
	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPut, "/api/user-config", body)
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
	serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPut, "/api/user-config", body)

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

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodGet, "/api/merged-config", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var got protocol.Config
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Runners["claude"].Command != "/opt/claude" {
		t.Errorf("merged runner command = %q", got.Runners["claude"].Command)
	}
}

func TestScreenshots_ServeImageDirectly(t *testing.T) {
	ws := setupServerWorkspace(t)
	shotDir := filepath.Join(ws.Root, ".4x", "e2e", "test-feat", "screenshot")
	os.MkdirAll(shotDir, 0o755)
	pngData := []byte("\x89PNG\r\n\x1a\ntest-image-data")
	os.WriteFile(filepath.Join(shotDir, "01-overview.png"), pngData, 0o644)

	mux := NewMux(protocol.NewCachedWorkspace(ws), NewProcessManager(ws, 1, "echo"))

	// 先取得 listing 以獲得 URL token
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/features/test-feat/screenshots", nil)
	mux.ServeHTTP(rec, req)
	var resp screenshotsResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total == 0 {
		t.Fatal("no screenshots found")
	}
	imgURL := resp.Groups[0].Screenshots[0].URL

	// 直接以 token 取圖，不應觸發 re-discover
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", imgURL, nil)
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("serve image: status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	if ct := rec2.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if cc := rec2.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("Cache-Control = %q, want max-age", cc)
	}
}

// TestSSE_TruncationResetsOffset 驗證 W9：events.jsonl 被 truncate 後（size < lastOffset），
// SSE 會 reset offset 從頭重讀，後續新 event 仍能正常推送，而非永遠卡住。
func TestSSE_TruncationResetsOffset(t *testing.T) {
	ws := setupServerWorkspace(t)
	path := filepath.Join(ws.FeatureDir("test-feat"), protocol.EventsFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	dataCh := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataCh <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	waitFor := func(want string) {
		t.Helper()
		deadline := time.After(6 * time.Second)
		for {
			select {
			case d := <-dataCh:
				if strings.Contains(d, want) {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for event containing %q", want)
			}
		}
	}

	// 寫兩個 event，應收到
	if err := ws.AppendEvent("test-feat", protocol.Event{Type: "run-output", Detail: "evt-one"}); err != nil {
		t.Fatal(err)
	}
	waitFor("evt-one")
	if err := ws.AppendEvent("test-feat", protocol.Event{Type: "run-output", Detail: "evt-two"}); err != nil {
		t.Fatal(err)
	}
	waitFor("evt-two")

	// truncate 模擬 4x transition --to init 重置 events.jsonl
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	// truncate 後寫入的新 event 必須能被推送
	if err := ws.AppendEvent("test-feat", protocol.Event{Type: "run-output", Detail: "evt-after-truncate"}); err != nil {
		t.Fatal(err)
	}
	waitFor("evt-after-truncate")
}

// setupMergeableWorktree 建立一個可成功 merge 的 monorepo worktree：
// root 在 base commit（state.json=pending-review），worktree branch 依 mutate 內容修改後 commit。
func setupMergeableWorktree(t *testing.T, ws *protocol.Workspace, featureID string, mutate func(wtRoot string)) {
	t.Helper()
	root := ws.Root
	runGit(t, root, "init")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "user.email", "test@test.com")

	makePendingReview(t, ws, featureID)
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "base")

	wtDir := filepath.Join(root, ".worktrees", "4x", featureID)
	runGit(t, root, "worktree", "add", wtDir, "-b", "4x/"+featureID)

	mutate(wtDir)
	runGit(t, wtDir, "add", "-A")
	runGit(t, wtDir, "commit", "-m", "feature change")
}

// TestPostDone_HappyPathReReadsState 驗證 W15 正常路徑：merge 成功後 re-read
// 仍是 pending-review，呼叫 transitionDone，response 回 done。
func TestPostDone_HappyPathReReadsState(t *testing.T) {
	ws := setupServerWorkspace(t)
	// worktree 只改一個無關檔案，state.json 維持 pending-review
	setupMergeableWorktree(t, ws, "test-feat", func(wtRoot string) {
		if err := os.WriteFile(filepath.Join(wtRoot, "unrelated.txt"), []byte("change\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPost, "/api/done", `{"id":"test-feat"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"status":"done"`) {
		t.Errorf("body = %s, want status done", rec.Body.String())
	}

	s, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != protocol.PhaseDone {
		t.Errorf("phase = %q, want done", s.Phase)
	}
}

// TestPostDone_StateChangedDuringMergeReturns409 驗證 W15：merge 把 state.json
// 改成非 pending-review 的 phase 後，re-read 偵測到變動 → 回 409，不呼叫 transitionDone。
func TestPostDone_StateChangedDuringMergeReturns409(t *testing.T) {
	ws := setupServerWorkspace(t)
	// worktree 把 state.json 的 phase 改為 coding，merge 後 root 的 state.json 會變 coding
	setupMergeableWorktree(t, ws, "test-feat", func(wtRoot string) {
		wtWs := &protocol.Workspace{Root: wtRoot}
		s, err := wtWs.ReadState("test-feat")
		if err != nil {
			t.Fatal(err)
		}
		s.Phase = protocol.PhaseCoding
		if err := wtWs.WriteState("test-feat", s); err != nil {
			t.Fatal(err)
		}
	})

	rec := serveRequest(t, NewMux(protocol.NewCachedWorkspace(ws), nil), http.MethodPost, "/api/done", `{"id":"test-feat"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "state changed during merge") {
		t.Errorf("body = %s, want state-changed error", rec.Body.String())
	}

	s, err := ws.ReadState("test-feat")
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != protocol.PhaseCoding {
		t.Errorf("phase = %q, want coding (not transitioned to done)", s.Phase)
	}
}

// setupLogSSEWorkspace 建立帶有 logs 目錄的測試 workspace，回傳 ws 與 logsDir 路徑。
func setupLogSSEWorkspace(t *testing.T) (*protocol.Workspace, string) {
	t.Helper()
	ws := setupServerWorkspace(t)
	logsDir := filepath.Join(ws.FeatureDir("test-feat"), "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return ws, logsDir
}

// collectLogSSEMessages 啟動 httptest server 執行 handleLogSSE，於 ctx 取消前收集所有 data 行，
// 回傳解析出的 content 清單（每個 SSE message 的 content 欄位）。
func collectLogSSEMessages(t *testing.T, ws *protocol.Workspace, ctx context.Context) []string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// context 取消會導致 request error，屬正常
		return nil
	}
	defer resp.Body.Close()

	var contents []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var msg map[string]string
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if c, ok := msg["content"]; ok {
			contents = append(contents, c)
		}
	}
	return contents
}

// TestHandleLogSSE_SmallDelta 驗證 delta < 32KB 時收到單一 message，content 等於寫入內容，
// 且 SSE message 格式為 {"file":"...","content":"..."}。
func TestHandleLogSSE_SmallDelta(t *testing.T) {
	ws, logsDir := setupLogSSEWorkspace(t)
	logPath := filepath.Join(logsDir, "run.log")
	written := []byte("hello log content")
	if err := os.WriteFile(logPath, written, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 等待至少一個 message，再取消連線
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var gotMsg map[string]string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var msg map[string]string
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			t.Fatalf("invalid JSON: %s", raw)
		}
		gotMsg = msg
		cancel() // 收到第一個 message 就夠了
		break
	}

	if gotMsg == nil {
		t.Fatal("no SSE message received")
	}
	if _, ok := gotMsg["file"]; !ok {
		t.Errorf("message missing 'file' key: %v", gotMsg)
	}
	if _, ok := gotMsg["content"]; !ok {
		t.Errorf("message missing 'content' key: %v", gotMsg)
	}
	if gotMsg["content"] != string(written) {
		t.Errorf("content = %q, want %q", gotMsg["content"], string(written))
	}
}

// TestHandleLogSSE_MultiFile 驗證未帶 ?file= 時，SSE 會同時 tail 多個活躍 log
// （平行 sub-reviewer / reviewer+tester 場景），每筆 message 帶正確 file 欄位，
// 且能收到兩個不同 file 值的內容（修復 ParallelReviewTest 只看得到一個 log 的問題）。
func TestHandleLogSSE_MultiFile(t *testing.T) {
	ws, logsDir := setupLogSSEWorkspace(t)

	// 兩個平行 sub-reviewer log，mtime 皆在 activeLogWindow 內 → findActiveLogs 應一併回傳。
	files := map[string]string{
		"round-2-deep-reviewer-1.log": "content from reviewer one",
		"round-2-deep-reviewer-2.log": "content from reviewer two",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(logsDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cws := protocol.NewCachedWorkspace(ws)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(cws, "test-feat", w, r)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// 收集每個 file 收到的 content，直到兩個 file 都有內容為止。
	gotByFile := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var msg map[string]string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &msg); err != nil {
			t.Fatalf("invalid JSON: %s", line)
		}
		file, content := msg["file"], msg["content"]
		if file == "" {
			t.Errorf("message missing 'file' key: %v", msg)
		}
		gotByFile[file] += content
		if len(gotByFile) >= 2 {
			cancel()
			break
		}
	}

	if len(gotByFile) < 2 {
		t.Fatalf("want messages from 2 distinct files, got %d: %v", len(gotByFile), gotByFile)
	}
	for name, want := range files {
		if got := gotByFile[name]; got != want {
			t.Errorf("file %q content = %q, want %q", name, got, want)
		}
	}
}

// TestHandleLogSSE_LargeDelta 驗證 delta > 32KB（100KB）時收到多個 message，
// content 拼接後 byte 完全等於寫入內容，無遺漏/重複。
func TestHandleLogSSE_LargeDelta(t *testing.T) {
	ws, logsDir := setupLogSSEWorkspace(t)
	logPath := filepath.Join(logsDir, "run.log")
	written := bytes.Repeat([]byte("x"), 100*1024) // 100KB
	if err := os.WriteFile(logPath, written, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var collected []string
	totalLen := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var msg map[string]string
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if c, ok := msg["content"]; ok {
			collected = append(collected, c)
			totalLen += len(c)
		}
		if totalLen >= len(written) {
			cancel()
			break
		}
	}

	if len(collected) < 2 {
		t.Errorf("want ≥2 messages for 100KB delta, got %d", len(collected))
	}
	joined := strings.Join(collected, "")
	if joined != string(written) {
		t.Errorf("joined content len=%d, want %d; content mismatch", len(joined), len(written))
	}
}

// TestHandleLogSSE_Boundary 驗證 delta 剛好 32KB 與 32KB+1 位元組的邊界行為。
func TestHandleLogSSE_Boundary(t *testing.T) {
	cases := []struct {
		name    string
		size    int
		wantMsgs int
	}{
		{"exactly-32KB", 32 * 1024, 1},
		{"32KB-plus-one", 32*1024 + 1, 2},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ws, logsDir := setupLogSSEWorkspace(t)
			logPath := filepath.Join(logsDir, "run.log")
			written := bytes.Repeat([]byte("b"), tc.size)
			if err := os.WriteFile(logPath, written, 0o644); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
			}))
			defer srv.Close()

			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			var collected []string
			totalLen := 0
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				raw := strings.TrimPrefix(line, "data: ")
				var msg map[string]string
				if err := json.Unmarshal([]byte(raw), &msg); err != nil {
					continue
				}
				if c, ok := msg["content"]; ok {
					collected = append(collected, c)
					totalLen += len(c)
				}
				if totalLen >= tc.size {
					cancel()
					break
				}
			}

			if len(collected) != tc.wantMsgs {
				t.Errorf("messages = %d, want %d", len(collected), tc.wantMsgs)
			}
			joined := strings.Join(collected, "")
			if joined != string(written) {
				t.Errorf("content mismatch: len=%d, want %d", len(joined), len(written))
			}
		})
	}
}

// TestHandleLogSSE_MultibyteBoundary 驗證含 CJK／emoji 的 delta >32KB 時，
// 多位元組字元即使橫跨 32KB chunk 邊界也不會被切半損毀為 U+FFFD，
// 拼接後 byte 完全等於寫入內容（AC-6 回歸測試）。
func TestHandleLogSSE_MultibyteBoundary(t *testing.T) {
	ws, logsDir := setupLogSSEWorkspace(t)
	logPath := filepath.Join(logsDir, "run.log")

	// 混用 3-byte CJK 與 4-byte emoji，使各 32KB 邊界落點不對齊字元邊界。
	unit := []byte("中文測試🎉日本語パターン")
	var written []byte
	for len(written) < 96*1024 { // 跨越多個 32KB 邊界
		written = append(written, unit...)
	}
	if err := os.WriteFile(logPath, written, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var joined []byte
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var msg map[string]string
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if c, ok := msg["content"]; ok {
			joined = append(joined, []byte(c)...)
		}
		if len(joined) >= len(written) {
			cancel()
			break
		}
	}

	if strings.ContainsRune(string(joined), '�') {
		t.Errorf("content 含 U+FFFD，多位元組字元在 32KB 邊界被損毀")
	}
	if !bytes.Equal(joined, written) {
		t.Errorf("拼接後 byte 不一致：joined len=%d, want %d", len(joined), len(written))
	}
}

// TestHandleLogSSE_TwoTicks 驗證兩次 tick 之間追加內容時，lastOffset 正確推進，
// 第二次 tick 只推送新增的部分，不重複舊內容。
func TestHandleLogSSE_TwoTicks(t *testing.T) {
	ws, logsDir := setupLogSSEWorkspace(t)
	logPath := filepath.Join(logsDir, "run.log")
	firstWrite := []byte("first chunk\n")
	if err := os.WriteFile(logPath, firstWrite, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleLogSSE(protocol.NewCachedWorkspace(ws), "test-feat", w, r)
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	msgCh := make(chan string, 32)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			raw := strings.TrimPrefix(line, "data: ")
			var msg map[string]string
			if err := json.Unmarshal([]byte(raw), &msg); err != nil {
				continue
			}
			if c, ok := msg["content"]; ok {
				msgCh <- c
			}
		}
		close(msgCh)
	}()

	waitMsg := func(want string) {
		t.Helper()
		deadline := time.After(6 * time.Second)
		for {
			select {
			case got, ok := <-msgCh:
				if !ok {
					t.Fatalf("channel closed waiting for %q", want)
				}
				if strings.Contains(got, want) {
					return
				}
			case <-deadline:
				t.Fatalf("timeout waiting for message containing %q", want)
			}
		}
	}

	// 第一次 tick 後收到 firstWrite
	waitMsg("first chunk")

	// 追加第二段內容
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	secondWrite := []byte("second chunk\n")
	if _, err := f.Write(secondWrite); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	// 第二次 tick 後只收到 secondWrite，不含 firstWrite
	deadline := time.After(6 * time.Second)
	for {
		select {
		case got, ok := <-msgCh:
			if !ok {
				t.Fatal("channel closed before second message")
			}
			if strings.Contains(got, "first chunk") {
				t.Errorf("second message should not contain first chunk, got: %q", got)
			}
			if strings.Contains(got, "second chunk") {
				cancel()
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for second message containing 'second chunk'")
		}
	}
}
