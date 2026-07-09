package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// AC-1：role×path 矩陣——非 coder/fixer 寫 source → deny；coder/fixer 寫 source → allow；
// 任一 role 寫 `.4x/` 底下路徑 → allow。
func TestEvaluateWritePath_RoleMatrix(t *testing.T) {
	f := feature.Feature{ID: "F999-x", Name: "t"} // repos 空 → 不觸發 scope 規則
	srcPath := "cmd/4x/foo.go"
	artifactPath := ".4x/run/F999-x/coder-report.md"

	nonSource := []protocol.Role{
		protocol.RoleDesigner,
		protocol.RoleDesignReviewer,
		protocol.RoleReviewer,
		protocol.RoleDeepReviewer,
		protocol.RoleTester,
		protocol.RoleAcceptor,
	}
	sourceWriters := []protocol.Role{protocol.RoleCoder, protocol.RoleFixer}

	for _, role := range nonSource {
		deny, reason := EvaluateWritePath(f, role, srcPath, nil)
		if !deny {
			t.Errorf("role %q writing source %q: want deny, got allow", role, srcPath)
		}
		if reason == "" {
			t.Errorf("role %q deny reason should be non-empty", role)
		}
		// artifact 一律放行
		if deny, _ := EvaluateWritePath(f, role, artifactPath, nil); deny {
			t.Errorf("role %q writing artifact %q: want allow, got deny", role, artifactPath)
		}
	}

	for _, role := range sourceWriters {
		if deny, _ := EvaluateWritePath(f, role, srcPath, nil); deny {
			t.Errorf("role %q writing source %q: want allow, got deny", role, srcPath)
		}
		if deny, _ := EvaluateWritePath(f, role, artifactPath, nil); deny {
			t.Errorf("role %q writing artifact %q: want allow, got deny", role, artifactPath)
		}
	}

	// `.4x` 本身（無尾斜線）也算 artifact 放行。
	if deny, _ := EvaluateWritePath(f, protocol.RoleReviewer, ".4x", nil); deny {
		t.Errorf("writing %q should be allowed", ".4x")
	}
}

// AC-2：repo-scope 規則（role 固定 coder 以隔離 scope 規則）。
func TestEvaluateWritePath_RepoScope(t *testing.T) {
	role := protocol.RoleCoder

	// Repos 空（monorepo）→ 一律不因 scope deny。
	if deny, _ := EvaluateWritePath(feature.Feature{ID: "F1", Name: "t"}, role, "any/deep/path.go", nil); deny {
		t.Errorf("empty repos should never deny by scope")
	}

	fa := feature.Feature{ID: "F1", Name: "t", Repos: []string{"a"}}

	// repo 不在 Repos 也不在 hubRepos → deny（scope violation）。
	deny, reason := EvaluateWritePath(fa, role, "b/x.go", nil)
	if !deny {
		t.Errorf("repo b not in feature repos should deny")
	}
	if reason == "" || !strings.Contains(reason, "scope violation") {
		t.Errorf("scope deny reason should mention 'scope violation', got %q", reason)
	}

	// repo 在 Repos → allow。
	if deny, _ := EvaluateWritePath(fa, role, "a/x.go", nil); deny {
		t.Errorf("repo a in feature repos should allow")
	}

	// repo 在 hubRepos → allow（即使不在 Repos）。
	hub := map[string]bool{"shared": true}
	if deny, _ := EvaluateWritePath(fa, role, "shared/x.go", hub); deny {
		t.Errorf("repo shared in hubRepos should allow")
	}
}

// AC-9：即時 gate fail-open——ReadState 失敗（無 state.json）、LoadFeature 失敗（壞 YAML）皆回 deny=false。
func TestEvaluateWritePathForRun_FailOpen(t *testing.T) {
	t.Run("missing state.json", func(t *testing.T) {
		ws := newFixtureWorkspace(t)
		writeFeatureYAML(t, ws, "F1-x", "id: F1-x\nname: t\n")
		// 不寫 state.json
		if deny, _ := EvaluateWritePathForRun(ws, "F1-x", "cmd/4x/foo.go"); deny {
			t.Errorf("missing state.json should fail-open (allow)")
		}
	})

	t.Run("broken feature YAML", func(t *testing.T) {
		ws := newFixtureWorkspace(t)
		writeFeatureYAML(t, ws, "F1-x", "id: F1-x\nname: [unterminated\n")
		writeStateJSON(t, ws, "F1-x", `{"featureId":"F1-x","phase":"reviewing","role":"reviewer"}`)
		if deny, _ := EvaluateWritePathForRun(ws, "F1-x", "cmd/4x/foo.go"); deny {
			t.Errorf("broken feature YAML should fail-open (allow)")
		}
	})

	// config 讀取失敗（壞 settings.json）→ fail-open：不可因暫時性 config 錯誤把寫入
	// hub/out-of-repos 的合法路徑誤判為 scope violation，也不可因空 hubRepos 而 deny。
	t.Run("broken config fails open even for out-of-repos path", func(t *testing.T) {
		ws := newFixtureWorkspace(t)
		// multi-repo feature：正常情況下 repo "other" 不在 repos 會 deny；但 config 壞掉應 fail-open。
		writeFeatureYAML(t, ws, "F1-x", "id: F1-x\nname: t\nrepos:\n  - a\n")
		writeStateJSON(t, ws, "F1-x", `{"featureId":"F1-x","phase":"coding","role":"coder"}`)
		writeConfigJSON(t, ws, "{not valid json")
		if deny, _ := EvaluateWritePathForRun(ws, "F1-x", "other/x.go"); deny {
			t.Errorf("broken config should fail-open (allow), even for out-of-repos path")
		}
	})
}

// hub_repos 來自 config：EvaluateWritePathForRun 確實消費 config 的 EffectiveHubRepos，
// 讓寫入 hub repo 的路徑對 multi-repo feature 放行。
func TestEvaluateWritePathForRun_HubReposFromConfig(t *testing.T) {
	ws := newFixtureWorkspace(t)
	writeFeatureYAML(t, ws, "F1-x", "id: F1-x\nname: t\nrepos:\n  - a\n")
	writeStateJSON(t, ws, "F1-x", `{"featureId":"F1-x","phase":"coding","role":"coder"}`)
	writeConfigJSON(t, ws, `{"project":{"name":"t"},"isolation":"worktree","commit":"per-round","hub_repos":["shared"]}`)

	// hub_repos 內的 repo → allow。
	if deny, _ := EvaluateWritePathForRun(ws, "F1-x", "shared/x.go"); deny {
		t.Errorf("path under hub repo should be allowed via config hub_repos")
	}
	// 既不在 repos 也不在 hub_repos → deny。
	if deny, _ := EvaluateWritePathForRun(ws, "F1-x", "other/x.go"); !deny {
		t.Errorf("out-of-repos path (not hub) should deny")
	}
}

// AC-12：state.json 的 Role 為空時以 PhaseToRole(Phase) 推導 role——
// Phase=coding → coder（allow source）；Phase=reviewing → reviewer（deny source）。
// 必須用真實 state.json（含空 Role 欄位），證明判定確吃「由 Phase 推導的 role」。
func TestEvaluateWritePathForRun_EmptyRoleDerivesFromPhase(t *testing.T) {
	srcPath := "cmd/4x/foo.go"

	t.Run("empty role + phase=coding derives coder (allow)", func(t *testing.T) {
		ws := newFixtureWorkspace(t)
		writeFeatureYAML(t, ws, "F1-x", "id: F1-x\nname: t\n") // repos 空
		writeStateJSON(t, ws, "F1-x", `{"featureId":"F1-x","phase":"coding","role":""}`)
		if deny, reason := EvaluateWritePathForRun(ws, "F1-x", srcPath); deny {
			t.Errorf("phase=coding (→coder) should allow source write, got deny: %s", reason)
		}
	})

	t.Run("empty role + phase=reviewing derives reviewer (deny)", func(t *testing.T) {
		ws := newFixtureWorkspace(t)
		writeFeatureYAML(t, ws, "F1-x", "id: F1-x\nname: t\n")
		writeStateJSON(t, ws, "F1-x", `{"featureId":"F1-x","phase":"reviewing","role":""}`)
		if deny, _ := EvaluateWritePathForRun(ws, "F1-x", srcPath); !deny {
			t.Errorf("phase=reviewing (→reviewer) should deny source write")
		}
	})
}

// --- fixture helpers ---

func newFixtureWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, protocol.DirName, protocol.FeaturesDir), 0o755); err != nil {
		t.Fatalf("mkdir features: %v", err)
	}
	ws := &protocol.Workspace{Root: root}
	// 預設寫入最小合法 settings.json，讓 EvaluateWritePathForRun 的 ReadConfig 不會 error；
	// 需要測 config 讀取失敗的案例再用 writeConfigJSON 覆寫成壞內容。
	writeConfigJSON(t, ws, `{"project":{"name":"t"},"isolation":"worktree","commit":"per-round"}`)
	return ws
}

func writeConfigJSON(t *testing.T, ws *protocol.Workspace, content string) {
	t.Helper()
	path := filepath.Join(ws.Root, protocol.DirName, protocol.ConfigFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
}

func writeFeatureYAML(t *testing.T, ws *protocol.Workspace, id, content string) {
	t.Helper()
	path := filepath.Join(ws.Root, protocol.DirName, protocol.FeaturesDir, id+".yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write feature yaml: %v", err)
	}
}

func writeStateJSON(t *testing.T, ws *protocol.Workspace, id, content string) {
	t.Helper()
	dir := ws.FeatureDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, protocol.StateFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}
}
