package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func setupMultiWorkspace(t *testing.T, name string) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: name}}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	f := protocol.Feature{ID: "feat-1", Name: "Feature One", Status: "in-progress"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("feat-1"); err != nil {
		t.Fatal(err)
	}
	state := protocol.State{FeatureID: "feat-1", Phase: protocol.PhaseCoding, Round: 1, Active: true}
	if err := ws.WriteState("feat-1", state); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestNewProjectRegistry_IDFromBaseName(t *testing.T) {
	ws := setupMultiWorkspace(t, "alpha")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	if id == "" {
		t.Fatal("id should not be empty")
	}
	projects := reg.List()
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	if projects[0].Name != "alpha" {
		t.Errorf("name = %s, want alpha", projects[0].Name)
	}
}

func TestNewProjectRegistry_DuplicateBaseName(t *testing.T) {
	ws1 := setupMultiWorkspace(t, "app")
	ws2 := setupMultiWorkspace(t, "app")
	reg := NewProjectRegistry()
	id1 := reg.Add(ws1)
	id2 := reg.Add(ws2)

	if id1 == id2 {
		t.Errorf("duplicate IDs: %s", id1)
	}
}

func TestMultiMux_GetProjects(t *testing.T) {
	ws := setupMultiWorkspace(t, "my-project")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/projects", "")

	var projects []ProjectListItem
	if err := json.NewDecoder(rec.Body).Decode(&projects); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	if projects[0].Name != "my-project" {
		t.Errorf("name = %s, want my-project", projects[0].Name)
	}
}

func TestMultiMux_PrefixRouting(t *testing.T) {
	ws := setupMultiWorkspace(t, "my-project")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/project/"+id+"/api/tasks", "")

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
	if tasks[0].ID != "feat-1" {
		t.Errorf("ID = %s, want feat-1", tasks[0].ID)
	}
}

func TestMultiMux_PostProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "existing")
	reg := NewProjectRegistry()
	reg.Add(ws)

	newRoot := t.TempDir()
	newCfg := protocol.Config{Project: protocol.ProjectConfig{Name: "new-proj"}}
	if err := protocol.Init(newRoot, newCfg); err != nil {
		t.Fatal(err)
	}

	recentPath := t.TempDir() + "/recent.json"

	body := `{"path":"` + newRoot + `"}`
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodPost, "/api/projects", body)

	if rec.Code != 201 {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	projects := reg.List()
	if len(projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(projects))
	}
}

func TestMultiMux_DeleteProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "to-remove")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodDelete, "/api/projects/"+id, "")

	if rec.Code != 204 {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if len(reg.List()) != 0 {
		t.Error("project should be removed")
	}
}

func TestMultiMux_IndexHTML(t *testing.T) {
	reg := NewProjectRegistry()
	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %s, want text/html*", ct)
	}
}

func TestMultiMux_BackwardCompat_SingleProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "only-one")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/tasks", "")

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
}

func TestMultiMux_BackwardCompat_MultiProject(t *testing.T) {
	ws1 := setupMultiWorkspace(t, "proj-a")
	ws2 := setupMultiWorkspace(t, "proj-b")
	reg := NewProjectRegistry()
	reg.Add(ws1)
	reg.Add(ws2)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/tasks", "")

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMultiMux_BackwardCompat_ZeroProject(t *testing.T) {
	reg := NewProjectRegistry()

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/tasks", "")

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMultiMux_SettingsEndpoint_Single(t *testing.T) {
	ws := setupMultiWorkspace(t, "single-proj")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	// 單一專案時，GET /api/settings 應回傳 200
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/settings", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var cfg map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestMultiMux_SettingsEndpoint_Multi(t *testing.T) {
	ws1 := setupMultiWorkspace(t, "proj-a")
	ws2 := setupMultiWorkspace(t, "proj-b")
	reg := NewProjectRegistry()
	reg.Add(ws1)
	reg.Add(ws2)

	recentPath := t.TempDir() + "/recent.json"
	// 多個專案時，GET /api/settings 應回傳 400
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/settings", "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMultiMux_SettingsEndpoint_PrefixRoute(t *testing.T) {
	ws := setupMultiWorkspace(t, "prefix-proj")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	// prefix route 應正確轉發
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/project/"+id+"/api/settings", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestMultiMux_OverviewPrefixRoute(t *testing.T) {
	ws := setupMultiWorkspace(t, "overview-proj")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/project/"+id+"/api/overview/feat-1", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var info overviewInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.ID != "feat-1" {
		t.Errorf("ID = %s, want feat-1", info.ID)
	}
}

func TestMultiMux_OverviewBackwardCompat(t *testing.T) {
	ws := setupMultiWorkspace(t, "single-ov")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/overview/feat-1", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestGetLocalesMultiMux(t *testing.T) {
	ws := setupMultiWorkspace(t, "locale-proj")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/locales", "")

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

func TestGetLocaleEnMultiMux(t *testing.T) {
	ws := setupMultiWorkspace(t, "locale-en-proj")
	reg := NewProjectRegistry()
	reg.Add(ws)

	recentPath := t.TempDir() + "/recent.json"
	rec := serveRequest(t, NewMultiMux(reg, recentPath), http.MethodGet, "/api/locales/en", "")

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var translations map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&translations); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if translations["app.title"] != "4x Live" {
		t.Errorf("app.title = %q, want '4x Live'", translations["app.title"])
	}

	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %s, want immutable", cc)
	}
}
