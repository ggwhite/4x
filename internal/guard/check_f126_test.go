package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// fakeScopeDetector 回傳固定的 changedRepos 清單，供 checkScope 單元測試注入可控輸入。
type fakeScopeDetector struct{ repos []string }

func (f fakeScopeDetector) DetectChangedRepos(string) []string { return f.repos }

// --- Issue 1: build-gate 對缺少的指令優雅降級 ---

// TestCheckBuildGate_CommandMissingDegradesGracefully 驗證 AC-1：
// 某 root 缺少某指令（exit 127 且 summary 含 "command not found"）時，該指令被標記
// Skipped 並排除於整體 pass/fail；在其他指令通過的前提下 build-gate 整體 PASS。
func TestCheckBuildGate_CommandMissingDegradesGracefully(t *testing.T) {
	ws := setupGuardWorkspace(t, "F126-missing")
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Lint:  []string{"nonexistent-cmd-xyz-126"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F126-missing", protocol.State{Phase: protocol.PhaseCoding, Round: 1})
	dir := ws.FeatureDir("F126-missing")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	os.MkdirAll(ws.RoundDir("F126-missing", 1), 0o755)

	r := Check(ws, "F126-missing", nil)
	if !r.Pass {
		t.Fatalf("expected pass when the only failure is a missing command, got errors: %v", r.Errors)
	}

	bgPath := filepath.Join(ws.RoundDir("F126-missing", 1), protocol.BuildGateFile)
	data, err := os.ReadFile(bgPath)
	if err != nil {
		t.Fatalf("build-gate.json not written: %v", err)
	}
	var ev protocol.VerifyEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("invalid build-gate.json: %v", err)
	}
	if !ev.Passed {
		t.Fatal("expected build-gate evidence passed=true")
	}
	found := false
	for _, cmd := range ev.Commands {
		if cmd.Command == "nonexistent-cmd-xyz-126" {
			found = true
			if !cmd.Skipped {
				t.Errorf("missing command should be marked Skipped, got: %+v", cmd)
			}
		}
	}
	if !found {
		t.Fatalf("expected the missing command in build-gate evidence, got: %+v", ev.Commands)
	}

	warnFound := false
	for _, w := range r.Warns {
		if strings.Contains(w, "not found") && strings.Contains(w, "nonexistent-cmd-xyz-126") {
			warnFound = true
		}
	}
	if !warnFound {
		t.Errorf("expected a warn about the missing command, got: %v", r.Warns)
	}
}

// TestCheckBuildGate_RealFailureNotDegraded 驗證 AC-2：真實失敗（exit 非 127）
// 不被降級誤放行，build-gate 整體仍 FAIL。
func TestCheckBuildGate_RealFailureNotDegraded(t *testing.T) {
	ws := setupGuardWorkspace(t, "F126-realfail")
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Lint:  []string{"false"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F126-realfail", protocol.State{Phase: protocol.PhaseCoding, Round: 1})
	dir := ws.FeatureDir("F126-realfail")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	os.MkdirAll(ws.RoundDir("F126-realfail", 1), 0o755)

	r := Check(ws, "F126-realfail", nil)
	if r.Pass {
		t.Fatal("expected fail for a real (non-127) command failure")
	}
}

// TestCheckBuildGate_Exit127WithoutNotFoundStillFails 驗證 AC-2：exit 127 但 summary
// 無 not-found 特徵時，不視為指令缺失，維持 FAIL。
func TestCheckBuildGate_Exit127WithoutNotFoundStillFails(t *testing.T) {
	ws := setupGuardWorkspace(t, "F126-exit127")
	cfg := protocol.Config{
		Project: protocol.ProjectConfig{
			Name:  "test",
			Build: []string{"echo build-ok"},
			Lint:  []string{"echo custom failure && exit 127"},
		},
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	writeFile(t, filepath.Join(ws.Root, ".4x", "settings.json"), string(cfgData))

	writeState(t, ws, "F126-exit127", protocol.State{Phase: protocol.PhaseCoding, Round: 1})
	dir := ws.FeatureDir("F126-exit127")
	writeFile(t, filepath.Join(dir, protocol.TaskBrief), "# Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(dir, protocol.Criteria), "# Criteria")
	os.MkdirAll(ws.RoundDir("F126-exit127", 1), 0o755)

	r := Check(ws, "F126-exit127", nil)
	if r.Pass {
		t.Fatal("exit 127 without a not-found message should NOT be degraded, expected fail")
	}
}

// --- Issue 3: hub_repo 排除於 scope violation ---

// TestCheckScope_HubRepoNotFlagged 驗證 AC-4：hub repo（EffectiveHubRepos 命中）
// 即使不在 feature.Repos，也不回報 scope violation。
func TestCheckScope_HubRepoNotFlagged(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{
		Project:  protocol.ProjectConfig{Name: "hub-test"},
		HubRepos: []string{"docs"},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir("feat-hub"); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: "feat-hub", Name: "feat-hub", Repos: []string{"admin"}}); err != nil {
		t.Fatal(err)
	}

	r := CheckResult{Pass: true}
	checkScope(ws, "feat-hub", fakeScopeDetector{repos: []string{"docs", "admin"}}, &r)

	if !r.Pass {
		t.Fatalf("hub repo changes must not fail scope check, got errors: %v", r.Errors)
	}
	for _, e := range r.Errors {
		if strings.Contains(e, "docs") {
			t.Errorf("hub repo %q must not appear in scope errors: %v", "docs", r.Errors)
		}
	}
}

// TestCheckScope_NonHubOutOfScopeStillFlagged 驗證 AC-5（控制組）：
// 非 hub 且不在 feature.Repos 的 repo 變更仍回報 scope violation，確保沒過度放行。
func TestCheckScope_NonHubOutOfScopeStillFlagged(t *testing.T) {
	root := t.TempDir()
	cfg := protocol.Config{
		Project:  protocol.ProjectConfig{Name: "hub-test"},
		HubRepos: []string{"docs"},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	if err := ws.InitFeatureDir("feat-hub2"); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feature.Feature{ID: "feat-hub2", Name: "feat-hub2", Repos: []string{"admin"}}); err != nil {
		t.Fatal(err)
	}

	r := CheckResult{Pass: true}
	checkScope(ws, "feat-hub2", fakeScopeDetector{repos: []string{"docs", "admin", "sneaky"}}, &r)

	if r.Pass {
		t.Fatal("non-hub out-of-scope repo change should still fail scope check")
	}
	found := false
	for _, e := range r.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "sneaky") {
			found = true
		}
		if strings.Contains(e, "docs") {
			t.Errorf("hub repo %q must not appear in scope errors: %v", "docs", r.Errors)
		}
	}
	if !found {
		t.Errorf("expected scope violation for non-hub repo %q, got: %v", "sneaky", r.Errors)
	}
}

// TestCheckScope_HubDocsSurvivesWorktreeRootMisresolution 是 AC-6 的整合測試，重現 Kairos
// 案例：agent 在 worktree 內執行 `4x check` 時，protocol.Find 可能把 ws.Root 解析成
// worktree container 本身（而非主工作區），導致 multiRepo.DetectChangedRepos 的
// wtActive 判斷失效、retreat 回 filepath.Join(m.root, "docs") 掃到 hub docs 目錄的
// 未提交變更。此測試直接用真實 gitops.Ops（root 故意指向 worktree container，模擬誤解析）
// 呼叫 DetectChangedRepos → guard.Check，斷言修法（checkScope 排除 hub repos）讓結果
// 與 root 解析正確與否無關。
func TestCheckScope_HubDocsSurvivesWorktreeRootMisresolution(t *testing.T) {
	root := t.TempDir()

	initRepo := func(dir string) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		runGitGuard(t, dir, "init")
		runGitGuard(t, dir, "config", "user.email", "t@t.io")
		runGitGuard(t, dir, "config", "user.name", "t")
		writeFile(t, filepath.Join(dir, "keep.txt"), "seed\n")
		runGitGuard(t, dir, "add", ".")
		runGitGuard(t, dir, "commit", "-m", "init")
	}
	initRepo(filepath.Join(root, "core"))
	initRepo(filepath.Join(root, "docs"))

	cfg := protocol.Config{
		Project: protocol.ProjectConfig{Name: "kairos-repro"},
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
				"docs": {Path: "docs", Hub: true},
			},
		},
	}
	if err := protocol.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "feat-kairos"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	// docs 刻意不列入 feature.Repos：hub repo 依設計跨 feature 共用，不由單一 feature 宣告。
	f := feature.Feature{ID: featureID, Name: featureID, Repos: []string{"core"}}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}

	mainOps := gitops.New(root, ws, cfg)
	wtPath, err := mainOps.SetupWorktree(featureID, []string{"core"})
	if err != nil {
		t.Fatalf("SetupWorktree: %v", err)
	}

	// docs 沒有獨立 linked worktree（未列入 feature.Repos），但 hub 目錄本身仍可能存在
	// 未提交變更（例如 agent 依 CLAUDE.md 慣例仍寫入 docs/design/*.md）。在 worktree
	// container 內直接製造一份「無獨立 linked worktree」的 docs 目錄與未提交變更。
	docsInWt := filepath.Join(wtPath, "docs")
	initRepo(docsInWt)
	writeFile(t, filepath.Join(docsInWt, "design", "feat-kairos-spec.md"), "# spec\n")

	// 模擬 bug 根因：ws.Root 誤解析為 worktree container 本身（而非主工作區）。
	// SetupWorktree 只同步 .4x/settings.json（含 hub_repos 設定），不會同步 features/ 與
	// run/ 內容，故需在 misrouted 這一側重建 agent 實際會讀到的 feature YAML 與
	// run artifacts，才能真實反映「CWD 在 worktree 內」時 4x check 看到的狀態。
	misrouted := &protocol.Workspace{Root: wtPath}
	if err := misrouted.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := misrouted.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	writeState(t, misrouted, featureID, protocol.State{FeatureID: featureID, Phase: protocol.PhaseCoding, Round: 1})
	misroutedFeatureDir := misrouted.FeatureDir(featureID)
	writeFile(t, filepath.Join(misroutedFeatureDir, protocol.TaskBrief), "# Task Brief\n## Premise Challenge\n- verified\n")
	writeFile(t, filepath.Join(misroutedFeatureDir, protocol.Criteria), "# Acceptance Criteria\n")

	misroutedOps := gitops.New(wtPath, misrouted, cfg)

	changed := misroutedOps.DetectChangedRepos(featureID)
	foundDocs := false
	for _, r := range changed {
		if r == "docs" {
			foundDocs = true
		}
	}
	if !foundDocs {
		t.Fatalf("test setup did not reproduce the misrouted-root docs detection, changed=%v", changed)
	}

	result := Check(misrouted, featureID, misroutedOps)
	if !result.Pass {
		t.Fatalf("hub docs change must not fail guard.Check even under misrouted root, got errors: %v", result.Errors)
	}
	for _, e := range result.Errors {
		if strings.Contains(e, "scope violation") && strings.Contains(e, "docs") {
			t.Errorf("hub repo docs must not trigger scope violation: %v", result.Errors)
		}
	}
}
