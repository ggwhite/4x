package guard

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// writeE2EStrategy 寫一份含（或不含）e2e_repos 的 test-strategy.yaml 到 feature 目錄，
// 供 checkScope 透過 ws.ReadTestStrategy 讀取。用 hand-written yaml 以真實走 e2e_repos tag 的反序列化。
func writeE2EStrategy(t *testing.T, ws *protocol.Workspace, featureID string, e2eRepos []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("verify_commands:\n  - make test\n")
	if len(e2eRepos) > 0 {
		b.WriteString("e2e_repos:\n")
		for _, r := range e2eRepos {
			b.WriteString("  - " + r + "\n")
		}
	}
	writeFile(t, filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile), b.String())
}

// setupE2EWorkspace 建立含 feature YAML（featureRepos）、test-strategy.yaml（e2eRepos）、
// state.json（phase）的 workspace，供 checkScope 直接單元測試。
func setupE2EWorkspace(t *testing.T, featureID string, featureRepos, e2eRepos []string, phase protocol.Phase) *protocol.Workspace {
	t.Helper()
	ws := setupGuardWorkspace(t, featureID)
	if err := ws.SaveFeature(feature.Feature{ID: featureID, Name: featureID, Repos: featureRepos}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	writeE2EStrategy(t, ws, featureID, e2eRepos)
	writeState(t, ws, featureID, protocol.State{FeatureID: featureID, Phase: phase, Round: 1})
	return ws
}

// TestF149_TestStrategyE2EReposUnmarshal 驗證 AC-1：TestStrategy.E2ERepos 以 e2e_repos tag
// 正確反序列化；省略欄位時為 nil（向後相容）。
func TestF149_TestStrategyE2EReposUnmarshal(t *testing.T) {
	ws := setupGuardWorkspace(t, "F149-unmarshal")

	writeE2EStrategy(t, ws, "F149-unmarshal", []string{"kairos-e2e"})
	ts, err := ws.ReadTestStrategy("F149-unmarshal")
	if err != nil {
		t.Fatalf("ReadTestStrategy: %v", err)
	}
	if len(ts.E2ERepos) != 1 || ts.E2ERepos[0] != "kairos-e2e" {
		t.Fatalf("expected E2ERepos == [kairos-e2e], got %v", ts.E2ERepos)
	}

	writeE2EStrategy(t, ws, "F149-unmarshal", nil)
	ts2, err := ws.ReadTestStrategy("F149-unmarshal")
	if err != nil {
		t.Fatalf("ReadTestStrategy: %v", err)
	}
	if ts2.E2ERepos != nil {
		t.Fatalf("expected E2ERepos == nil when omitted, got %v", ts2.E2ERepos)
	}
}

// TestF149_TestingPhaseE2ERepoAllowed 驗證 AC-2：testing phase 下，列於 e2e_repos 的 changed repo
// （不在 feature.Repos、非 hub）不回報 scope violation。
func TestF149_TestingPhaseE2ERepoAllowed(t *testing.T) {
	ws := setupE2EWorkspace(t, "F149-t", []string{"app"}, []string{"kairos-e2e"}, protocol.PhaseTesting)

	r := CheckResult{Pass: true}
	checkScope(ws, "F149-t", fakeScopeDetector{repos: []string{"kairos-e2e"}}, &r)

	if !r.Pass {
		t.Fatalf("e2e repo in testing phase must not fail scope check, got errors: %v", r.Errors)
	}
	for _, e := range r.Errors {
		if strings.Contains(e, "kairos-e2e") {
			t.Errorf("e2e repo must not appear in scope errors: %v", r.Errors)
		}
	}
}

// TestF149_TestingPhaseNonE2ERepoStillFlagged 驗證 AC-3：testing phase 下，不在 feature.Repos、
// 非 hub、且不在 e2e_repos 的 changed repo 仍回報 scope violation（不過度放行）。
func TestF149_TestingPhaseNonE2ERepoStillFlagged(t *testing.T) {
	ws := setupE2EWorkspace(t, "F149-t2", []string{"app"}, []string{"kairos-e2e"}, protocol.PhaseTesting)

	r := CheckResult{Pass: true}
	checkScope(ws, "F149-t2", fakeScopeDetector{repos: []string{"kairos-e2e", "sneaky"}}, &r)

	if r.Pass {
		t.Fatal("non-e2e out-of-scope repo should still fail scope check")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "sneaky") {
			found = true
		}
		if strings.Contains(e, "kairos-e2e") {
			t.Errorf("e2e repo must not appear in scope errors: %v", r.Errors)
		}
	}
	if !found {
		t.Errorf("expected scope violation for non-e2e repo %q, got: %v", "sneaky", r.Errors)
	}
}

// TestF149_CodingPhaseE2ERepoStillFlagged 驗證 AC-4：放行僅限 testing 起。coding phase 下，
// 列於 e2e_repos 但不在 feature.Repos 的 changed repo 仍回報 scope violation。
func TestF149_CodingPhaseE2ERepoStillFlagged(t *testing.T) {
	ws := setupE2EWorkspace(t, "F149-c", []string{"app"}, []string{"kairos-e2e"}, protocol.PhaseCoding)

	r := CheckResult{Pass: true}
	checkScope(ws, "F149-c", fakeScopeDetector{repos: []string{"kairos-e2e"}}, &r)

	if r.Pass {
		t.Fatal("e2e repo in coding phase (before testing) should still fail scope check")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "kairos-e2e") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scope violation for e2e repo in coding phase, got: %v", r.Errors)
	}
}

// TestF149_AmendingAfterTestingE2ERepoAllowed 驗證 AC-5：amending（testing 之後）下，
// 當本 run 已有任一 rounds/round-{n}/verify.json 時，列於 e2e_repos 的 changed repo 不回報 violation。
func TestF149_AmendingAfterTestingE2ERepoAllowed(t *testing.T) {
	ws := setupE2EWorkspace(t, "F149-a", []string{"app"}, []string{"kairos-e2e"}, protocol.PhaseAmending)
	// 模擬 testing 已跑過：在某 round 寫入 verify.json。
	writeFile(t, filepath.Join(ws.RoundDir("F149-a", 2), protocol.VerifyFile), "{}")

	r := CheckResult{Pass: true}
	checkScope(ws, "F149-a", fakeScopeDetector{repos: []string{"kairos-e2e"}}, &r)

	if !r.Pass {
		t.Fatalf("e2e repo in amending after testing must not fail scope check, got errors: %v", r.Errors)
	}
	for _, e := range r.Errors {
		if strings.Contains(e, "kairos-e2e") {
			t.Errorf("e2e repo must not appear in scope errors: %v", r.Errors)
		}
	}
}

// TestF149_AmendingBeforeTestingE2ERepoStillFlagged 驗證 AC-6：amending（testing 之前）下，
// 當本 run 無任何 verify.json 時，列於 e2e_repos 的 changed repo 仍回報 scope violation。
func TestF149_AmendingBeforeTestingE2ERepoStillFlagged(t *testing.T) {
	ws := setupE2EWorkspace(t, "F149-a2", []string{"app"}, []string{"kairos-e2e"}, protocol.PhaseAmending)
	// 不寫任何 verify.json：模擬 reviewing→amending，testing 從未跑過。

	r := CheckResult{Pass: true}
	checkScope(ws, "F149-a2", fakeScopeDetector{repos: []string{"kairos-e2e"}}, &r)

	if r.Pass {
		t.Fatal("e2e repo in amending before testing should still fail scope check")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "kairos-e2e") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scope violation for e2e repo in amending before testing, got: %v", r.Errors)
	}
}

// TestF149_CheckConsumesStrategyAndState 驗證 AC-7：e2e 放行由 checkScope 在真實呼叫路徑
// （guard.Check → ws.ReadTestStrategy + ws.ReadState）消費：phase=testing 放行、phase=coding 攔。
func TestF149_CheckConsumesStrategyAndState(t *testing.T) {
	ws := setupE2EWorkspace(t, "F149-e2e", []string{"app"}, []string{"kairos-e2e"}, protocol.PhaseTesting)
	// testing phase 需 designer 產出物，補齊以免 required-files 檢查干擾。
	dir := ws.FeatureDir("F149-e2e")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria\n")

	detector := fakeScopeDetector{repos: []string{"kairos-e2e"}}

	res := Check(ws, "F149-e2e", detector)
	for _, e := range res.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "kairos-e2e") {
			t.Errorf("e2e repo in testing phase must not trigger scope violation via Check: %v", res.Errors)
		}
	}

	// 把 phase 改回 coding，同 repo 必須被攔（證明 phase 確實被讀取消費）。
	writeState(t, ws, "F149-e2e", protocol.State{FeatureID: "F149-e2e", Phase: protocol.PhaseCoding, Round: 1})
	res2 := Check(ws, "F149-e2e", detector)
	found := false
	for _, e := range res2.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "kairos-e2e") {
			found = true
		}
	}
	if !found {
		t.Errorf("e2e repo in coding phase must be flagged via Check, got: %v", res2.Errors)
	}
}

// TestF149_EmptyE2EReposBackwardCompatible 驗證 AC-8：未宣告 e2e_repos（空）時行為與改動前一致，
// 既有 out-of-scope（非 hub）違規仍被攔。
func TestF149_EmptyE2EReposBackwardCompatible(t *testing.T) {
	ws := setupE2EWorkspace(t, "F149-empty", []string{"app"}, nil, protocol.PhaseTesting)

	r := CheckResult{Pass: true}
	checkScope(ws, "F149-empty", fakeScopeDetector{repos: []string{"out-of-scope"}}, &r)

	if r.Pass {
		t.Fatal("out-of-scope repo with empty e2e_repos should still fail scope check")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "out-of-scope") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scope violation for out-of-scope repo, got: %v", r.Errors)
	}
}

// TestF149_DetectChangedReposImmuneToCommittedHistory 驗證 AC-9（DR-2）：scope 檢查以
// working-tree `git diff HEAD` 為基準，對「有 committed 歷史但無 uncommitted 變更」不產生假 violation；
// 同情境對 scope 外 repo 有真實 uncommitted 變更時仍被偵測。
func TestF149_DetectChangedReposImmuneToCommittedHistory(t *testing.T) {
	root := t.TempDir()
	runGitGuard(t, root, "init")
	runGitGuard(t, root, "config", "user.email", "t@t.io")
	runGitGuard(t, root, "config", "user.name", "t")

	// svc-a 有 committed 歷史（模擬 branch 落後 main 的殘留 committed 變更），但無 uncommitted。
	writeFile(t, filepath.Join(root, "svc-a", "a.go"), "package a\n")
	writeFile(t, filepath.Join(root, "svc-b", "b.go"), "package b\n")
	runGitGuard(t, root, "add", ".")
	runGitGuard(t, root, "commit", "-m", "init svc-a and svc-b")

	if repos := detectChangedRepos(root); len(repos) != 0 {
		t.Fatalf("committed-only history must not surface changed repos, got: %v", repos)
	}

	// 對 scope 外 repo 留 untracked 檔 → 仍被偵測。
	writeFile(t, filepath.Join(root, "svc-b", "new.go"), "package b\n// uncommitted\n")
	repos := detectChangedRepos(root)
	found := false
	for _, r := range repos {
		if r == "svc-b" {
			found = true
		}
	}
	if !found {
		t.Errorf("real uncommitted change in svc-b should be detected, got: %v", repos)
	}
}
