package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// findCheck 在 checks 中尋找第一條符合 section+name 的項，找不到回傳 nil。
func findCheck(checks []Check, section, name string) *Check {
	for i := range checks {
		if checks[i].Section == section && checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

// hasSeverity 回報指定 section 是否存在任一條符合 severity 的 check。
func intPtr(n int) *int { return &n }

func hasSeverity(checks []Check, section string, sev Severity) bool {
	for _, c := range checks {
		if c.Section == section && c.Severity == sev {
			return true
		}
	}
	return false
}

func baseConfig() protocol.Config {
	return protocol.Config{
		Project: protocol.ProjectConfig{Name: "demo"},
		Default: "claude",
		Runners: map[string]protocol.RunnerConfig{
			"claude": {
				Command: "claude",
				Tiers:   map[string]string{"opus": "claude-opus", "sonnet": "claude-sonnet"},
			},
		},
		Roles: map[string]protocol.RoleConfig{
			"designer": {Model: "opus"},
			"coder":    {Model: "opus"},
			"reviewer": {Model: "sonnet", DeepModel: "opus"},
			"tester":   {Model: "sonnet"},
			"acceptor": {Model: "opus"},
		},
	}
}

// --- S2: settings ---

func TestCheckSettings_ProjectName(t *testing.T) {
	cfg := baseConfig()
	if c := findCheck(checkSettings(cfg, nil), sectionSettings, "project.name"); c == nil || c.Severity != SeverityPass {
		t.Fatalf("非空 project.name 應 PASS，得 %+v", c)
	}

	cfg.Project.Name = ""
	if c := findCheck(checkSettings(cfg, nil), sectionSettings, "project.name"); c == nil || c.Severity != SeverityFail {
		t.Fatalf("空 project.name 應 FAIL，得 %+v", c)
	}
}

func TestCheckSettings_Runners(t *testing.T) {
	cfg := baseConfig()
	if c := findCheck(checkSettings(cfg, nil), sectionSettings, "runners defined"); c == nil || c.Severity != SeverityPass {
		t.Fatalf("有 runner 應 PASS，得 %+v", c)
	}

	cfg.Runners = nil
	if c := findCheck(checkSettings(cfg, nil), sectionSettings, "runners defined"); c == nil || c.Severity != SeverityFail {
		t.Fatalf("無 runner 應 FAIL，得 %+v", c)
	}
}

func TestCheckSettings_DefaultRunner(t *testing.T) {
	// 存在 → PASS
	cfg := baseConfig()
	if c := findCheck(checkSettings(cfg, nil), sectionSettings, "default_runner"); c == nil || c.Severity != SeverityPass {
		t.Fatalf("default 存在應 PASS，得 %+v", c)
	}

	// 非空但不在 runners → FAIL
	cfg.Default = "ghost"
	if c := findCheck(checkSettings(cfg, nil), sectionSettings, "default_runner"); c == nil || c.Severity != SeverityFail {
		t.Fatalf("default 不存在應 FAIL，得 %+v", c)
	}

	// 空但有 runner → WARN
	cfg = baseConfig()
	cfg.Default = ""
	if c := findCheck(checkSettings(cfg, nil), sectionSettings, "default_runner"); c == nil || c.Severity != SeverityWarn {
		t.Fatalf("default 空但有 runner 應 WARN，得 %+v", c)
	}
}

func TestCheckSettings_VerifyAllowlist(t *testing.T) {
	cfg := baseConfig()
	c := findCheck(checkSettings(cfg, nil), sectionSettings, "verify_command_allowlist")
	if c == nil {
		t.Fatal("missing verify_command_allowlist check")
	}
	if c.Severity != SeverityWarn {
		t.Fatalf("empty allowlist should WARN, got %+v", c)
	}

	cfg.Project.VerifyCommandAllowlist = []string{"make", "go test"}
	c = findCheck(checkSettings(cfg, nil), sectionSettings, "verify_command_allowlist")
	if c == nil {
		t.Fatal("missing verify_command_allowlist check")
	}
	if c.Severity != SeverityPass {
		t.Fatalf("non-empty allowlist should PASS, got %+v", c)
	}
	if !strings.Contains(c.Detail, "make") || !strings.Contains(c.Detail, "go test") {
		t.Fatalf("PASS detail should include allowlist entries, got %q", c.Detail)
	}
}

func TestCheckSettings_LoadError(t *testing.T) {
	checks := checkSettings(protocol.Config{}, errors.New("invalid character"))
	c := findCheck(checks, sectionSettings, "settings.json loadable")
	if c == nil || c.Severity != SeverityFail {
		t.Fatalf("載入失敗應 FAIL，得 %+v", c)
	}
	if !strings.Contains(c.Detail, "invalid character") {
		t.Fatalf("detail 應含原始錯誤，得 %q", c.Detail)
	}
}

// --- S3: runners ---

func TestCheckRunners(t *testing.T) {
	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"ok":    {Command: "found-bin"},
			"miss":  {Command: "missing-bin"},
			"empty": {Command: ""},
		},
	}
	lookPath := func(cmd string) (string, error) {
		if cmd == "found-bin" {
			return "/usr/bin/found-bin", nil
		}
		return "", errors.New("not found")
	}
	checks := checkRunners(cfg, lookPath)

	if c := findCheck(checks, sectionRunners, "ok"); c == nil || c.Severity != SeverityPass || !strings.Contains(c.Detail, "/usr/bin/found-bin") {
		t.Fatalf("在 PATH 應 PASS 且 detail 含路徑，得 %+v", c)
	}
	if c := findCheck(checks, sectionRunners, "miss"); c == nil || c.Severity != SeverityWarn {
		t.Fatalf("不在 PATH 應 WARN，得 %+v", c)
	}
	if c := findCheck(checks, sectionRunners, "empty"); c == nil || c.Severity != SeverityWarn {
		t.Fatalf("空 command 應 WARN，得 %+v", c)
	}
}

// --- S4: roles ---

func TestCheckRoles_Resolved(t *testing.T) {
	checks := checkRoles(baseConfig())
	coder := findCheck(checks, sectionRoles, "coder")
	if coder == nil || coder.Severity != SeverityPass || !strings.Contains(coder.Detail, "claude-opus") {
		t.Fatalf("coder 應 PASS 且 detail 含實際 model，得 %+v", coder)
	}
	deep := findCheck(checks, sectionRoles, "deep-reviewer")
	if deep == nil || deep.Severity != SeverityPass || !strings.Contains(deep.Detail, "claude-opus") {
		t.Fatalf("deep-reviewer 應 PASS，得 %+v", deep)
	}
}

func TestCheckRoles_ResolveFail(t *testing.T) {
	cfg := baseConfig()
	// 移除 tiers 使 model 無法解析。
	cfg.Runners["claude"] = protocol.RunnerConfig{Command: "claude"}
	checks := checkRoles(cfg)
	if c := findCheck(checks, sectionRoles, "coder"); c == nil || c.Severity != SeverityWarn {
		t.Fatalf("無法解析 model 應 WARN，得 %+v", c)
	}
}

func TestCheckRoles_DeepModelUnset_FallbackResolves(t *testing.T) {
	cfg := baseConfig()
	cfg.Roles["reviewer"] = protocol.RoleConfig{Model: "sonnet"} // 無 deep_model，但 runner 有 opus tier
	checks := checkRoles(cfg)
	if c := findCheck(checks, sectionRoles, "deep-reviewer"); c == nil || c.Severity != SeverityPass {
		t.Fatalf("runner 可解析預設 tier 應 PASS，得 %+v", c)
	}
}

func TestCheckRoles_DeepModelUnset_FallbackFails(t *testing.T) {
	cfg := baseConfig()
	cfg.Roles["reviewer"] = protocol.RoleConfig{Model: "sonnet"}
	cfg.Runners["claude"] = protocol.RunnerConfig{Command: "claude"} // 無 tiers，fallback 無法解析
	checks := checkRoles(cfg)
	if c := findCheck(checks, sectionRoles, "deep-reviewer"); c == nil || c.Severity != SeverityWarn {
		t.Fatalf("runner 無法解析預設 tier 應 WARN，得 %+v", c)
	}
}

func TestCheckRoles_NoDefaultRunner(t *testing.T) {
	cfg := baseConfig()
	cfg.Default = ""
	checks := checkRoles(cfg)
	if len(checks) != 1 || checks[0].Severity != SeverityWarn {
		t.Fatalf("無 default runner 應降級為單一 WARN，得 %+v", checks)
	}
}

// --- S5: workspace ---

// setupWorkspace 建臨時 root 與 .4x/ 結構，回傳 root。
func setupWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".4x", "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFeature(t *testing.T, root, id string, status feature.Status) {
	t.Helper()
	body := "id: " + id + "\nname: " + id + "\nstatus: " + string(status) + "\n"
	path := filepath.Join(root, ".4x", "features", id+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckWorkspace_OrphanedWorktree(t *testing.T) {
	root := setupWorkspace(t)
	writeFeature(t, root, "F001", feature.StatusDone)
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", "4x", "F001"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	checks := checkWorktrees(ws, root)
	c := findCheck(checks, sectionWorkspace, "worktree F001")
	if c == nil || c.Severity != SeverityWarn || !strings.Contains(c.Detail, "orphaned") {
		t.Fatalf("done feature 的 worktree 應 orphaned WARN，得 %+v", c)
	}
}

func TestCheckWorkspace_DanglingWorktree(t *testing.T) {
	root := setupWorkspace(t)
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", "4x", "F999"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	checks := checkWorktrees(ws, root)
	c := findCheck(checks, sectionWorkspace, "worktree F999")
	if c == nil || c.Severity != SeverityWarn || !strings.Contains(c.Detail, "dangling") {
		t.Fatalf("無對應 feature 的 worktree 應 dangling WARN，得 %+v", c)
	}
}

func TestCheckWorkspace_ActiveWorktreeNotReported(t *testing.T) {
	root := setupWorkspace(t)
	writeFeature(t, root, "F002", feature.StatusInProgress)
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", "4x", "F002"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	checks := checkWorktrees(ws, root)
	if findCheck(checks, sectionWorkspace, "worktree F002") != nil {
		t.Fatalf("進行中 feature 的 worktree 不應被報")
	}
}

func TestCheckWorkspace_StaleState(t *testing.T) {
	root := setupWorkspace(t)
	writeFeature(t, root, "F003", feature.StatusInProgress)
	ws := &protocol.Workspace{Root: root}
	if err := os.MkdirAll(filepath.Join(root, ".4x", "run", "F003"), 0o755); err != nil {
		t.Fatal(err)
	}
	state := protocol.State{FeatureID: "F003", Active: true, Pid: 4242}
	if err := ws.WriteState("F003", state); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".4x", "run", "F003", "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	checks := checkStaleState(ws, func(int) bool { return false })
	if !hasSeverity(checks, sectionWorkspace, SeverityWarn) {
		t.Fatalf("active 但 process 不存在應 stale WARN，得 %+v", checks)
	}

	// AC-14/AC-16：doctor 為 read-only，state.json 不可被改寫。
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("state.json 不應被修改")
	}
}

func TestCheckWorkspace_MalformedYAML(t *testing.T) {
	root := setupWorkspace(t)
	writeFeature(t, root, "F004", feature.StatusInProgress)
	bad := filepath.Join(root, ".4x", "features", "F005.yaml")
	if err := os.WriteFile(bad, []byte("id: F005\nname: [unterminated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	checks := checkFeatureYAML(ws)
	c := findCheck(checks, sectionWorkspace, "F005.yaml")
	if c == nil || c.Severity != SeverityFail || !strings.Contains(c.Detail, "F005.yaml") {
		t.Fatalf("壞 YAML 應 FAIL 且 detail 含檔名，得 %+v", c)
	}
}

// writeRawFeature 以指定 filename 與原始內容寫一個 feature YAML，
// 供測試 filename 與內部 id 不一致、或語意錯誤等情境。
func writeRawFeature(t *testing.T, root, filename, body string) {
	t.Helper()
	path := filepath.Join(root, ".4x", "features", filename)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFeatureYAML_MissingIDFails(t *testing.T) {
	root := setupWorkspace(t)
	writeRawFeature(t, root, "F020.yaml", "name: 沒有 id\nstatus: in-progress\n")
	ws := &protocol.Workspace{Root: root}
	checks := checkFeatureYAML(ws)
	c := findCheck(checks, sectionWorkspace, "F020.yaml")
	if c == nil || c.Severity != SeverityFail || !strings.Contains(c.Detail, "F020.yaml") {
		t.Fatalf("缺 id 的 feature 應 FAIL 且 detail 含檔名，得 %+v", c)
	}
}

func TestCheckFeatureYAML_SemanticWarn(t *testing.T) {
	root := setupWorkspace(t)
	// 語法合法但 status 無效、且 subtask 缺 name → 應彙整為單一 WARN。
	body := "id: F021\nname: 語意問題\nstatus: bogus-status\nsubtasks:\n  - id: s1\n"
	writeRawFeature(t, root, "F021.yaml", body)
	ws := &protocol.Workspace{Root: root}
	checks := checkFeatureYAML(ws)
	c := findCheck(checks, sectionWorkspace, "F021.yaml")
	if c == nil || c.Severity != SeverityWarn {
		t.Fatalf("語意問題應 WARN，得 %+v", c)
	}
	if !strings.Contains(c.Detail, "bogus-status") || !strings.Contains(c.Detail, "subtasks[0]") {
		t.Fatalf("WARN detail 應彙整各條 warning，得 %q", c.Detail)
	}
}

func TestCheckFeatureYAML_AllCleanSummaryPass(t *testing.T) {
	root := setupWorkspace(t)
	writeFeature(t, root, "F022", feature.StatusInProgress)
	ws := &protocol.Workspace{Root: root}
	checks := checkFeatureYAML(ws)
	if findCheck(checks, sectionWorkspace, "F022.yaml") != nil {
		t.Fatalf("乾淨的 feature 不應有單檔 check")
	}
	c := findCheck(checks, sectionWorkspace, "feature yaml")
	if c == nil || c.Severity != SeverityPass {
		t.Fatalf("全乾淨應回傳單一 summary PASS，得 %+v", c)
	}
}

func TestCheckWorktrees_IDSourceFilenameMismatch(t *testing.T) {
	root := setupWorkspace(t)
	// filename 與內部 id 不一致：worktree 走內部 id（F100），不應因 filename 不同被誤判 dangling。
	writeRawFeature(t, root, "feature-100.yaml", "id: F100\nname: 不一致\nstatus: in-progress\n")
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", "4x", "F100"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	checks := checkWorktrees(ws, root)
	if findCheck(checks, sectionWorkspace, "worktree F100") != nil {
		t.Fatalf("有對應 feature 的 worktree 不應被報 dangling")
	}
}

func TestCheckWorktrees_BadFileFallbackNotDangling(t *testing.T) {
	root := setupWorkspace(t)
	// 壞檔（ListFeatures 會 skip），但 filename 對應 worktree → 靠 filename fallback 不算 dangling。
	writeRawFeature(t, root, "F101.yaml", "id: F101\nname: [unterminated\n")
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", "4x", "F101"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	checks := checkWorktrees(ws, root)
	if findCheck(checks, sectionWorkspace, "worktree F101") != nil {
		t.Fatalf("filename 對應實際 .yaml 檔的 worktree 不應被報 dangling")
	}
}

// --- Diagnose 整合 ---

func TestDiagnose_BadSettingsDoesNotAbort(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // 隔離真實 ~/.4x/settings.json
	root := setupWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, ".4x", "settings.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFeature(t, root, "F010", feature.StatusInProgress)

	report, err := Diagnose(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSeverity(report.Checks, sectionSettings, SeverityFail) {
		t.Fatalf("壞 settings.json 應有 settings FAIL")
	}
	if !report.HasFail() {
		t.Fatalf("HasFail 應為 true")
	}
	// 其他 section 仍應有檢查（未中斷）。
	if !hasSeverity(report.Checks, sectionWorkspace, SeverityPass) && !hasSeverity(report.Checks, sectionWorkspace, SeverityWarn) {
		t.Fatalf("settings 失敗不應中斷 workspace 檢查")
	}
}

func TestDiagnose_ReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := setupWorkspace(t)
	cfg := baseConfig()
	if err := protocol.WriteConfig(filepath.Join(root, ".4x"), cfg); err != nil {
		t.Fatal(err)
	}
	writeFeature(t, root, "F020", feature.StatusInProgress)
	ws := &protocol.Workspace{Root: root}
	if err := os.MkdirAll(filepath.Join(root, ".4x", "run", "F020"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F020", protocol.State{FeatureID: "F020", Active: true, Pid: 999999}); err != nil {
		t.Fatal(err)
	}

	before := snapshotDir(t, filepath.Join(root, ".4x"))
	if _, err := Diagnose(Options{Root: root, ProcessAlive: func(int) bool { return false }}); err != nil {
		t.Fatal(err)
	}
	after := snapshotDir(t, filepath.Join(root, ".4x"))

	for path, sum := range before {
		if after[path] != sum {
			t.Fatalf("檔案 %s 被修改（doctor 必須 read-only）", path)
		}
	}
	if len(before) != len(after) {
		t.Fatalf("doctor 不應新增或刪除 .4x/ 內檔案")
	}
}

// snapshotDir 回傳目錄下所有檔案的 path→內容 map，供 read-only 驗證。
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// okLookPath 模擬所有 command 都在 PATH。
func okLookPath(string) (string, error) { return "/usr/bin/x", nil }

func TestCheckProfiles_NoCustomProfiles(t *testing.T) {
	cfg := protocol.Config{}
	checks := checkProfiles(cfg, okLookPath)
	if c := findCheck(checks, sectionProfiles, "profiles"); c == nil || c.Severity != SeverityPass {
		t.Fatalf("expected PASS for no custom profiles, got %+v", c)
	}
}

func TestCheckProfiles_NonSelectablePhase(t *testing.T) {
	cfg := protocol.Config{
		Profiles: map[string]protocol.ProfileConfig{
			"bad": {Phases: []protocol.PhaseSpec{
				{Phase: string(protocol.PhaseCoding)},
				{Phase: string(protocol.PhaseAmending)},
			}},
		},
	}
	checks := checkProfiles(cfg, okLookPath)
	if !hasSeverity(checks, sectionProfiles, SeverityFail) {
		t.Fatal("expected FAIL for non-selectable phase")
	}
}

func TestCheckProfiles_MissingCoding(t *testing.T) {
	cfg := protocol.Config{
		Profiles: map[string]protocol.ProfileConfig{
			"bad": {Phases: []protocol.PhaseSpec{
				{Phase: string(protocol.PhaseReviewing)},
			}},
		},
	}
	checks := checkProfiles(cfg, okLookPath)
	if !hasSeverity(checks, sectionProfiles, SeverityFail) {
		t.Fatal("expected FAIL for profile missing coding phase")
	}
}

func TestCheckProfiles_UnknownRunner(t *testing.T) {
	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{"claude": {Command: "claude"}},
		Profiles: map[string]protocol.ProfileConfig{
			"bad": {Phases: []protocol.PhaseSpec{
				{Phase: string(protocol.PhaseCoding), Runner: "ghost"},
			}},
		},
	}
	checks := checkProfiles(cfg, okLookPath)
	if !hasSeverity(checks, sectionProfiles, SeverityFail) {
		t.Fatal("expected FAIL for unknown runner in profile phase")
	}
}

func TestCheckProfiles_Valid(t *testing.T) {
	cfg := protocol.Config{
		Runners: map[string]protocol.RunnerConfig{
			"claude": {Command: "claude", Tiers: map[string]string{"opus": "opus"}},
		},
		Profiles: map[string]protocol.ProfileConfig{
			"custom": {Phases: []protocol.PhaseSpec{
				{Phase: string(protocol.PhaseCoding), Runner: "claude", Model: "opus"},
				{Phase: string(protocol.PhaseReviewing)},
			}},
		},
	}
	checks := checkProfiles(cfg, okLookPath)
	if c := findCheck(checks, sectionProfiles, "custom"); c == nil || c.Severity != SeverityPass {
		t.Fatalf("expected PASS for valid profile, got %+v", c)
	}
}

func TestCheckEvolution(t *testing.T) {
	cases := []struct {
		name     string
		cfg      protocol.Config
		wantFail bool
	}{
		{"nil ok", protocol.Config{}, false},
		{"valid", protocol.Config{Evolution: &protocol.EvolutionConfig{ValueFloor: 0.6, MaxAcceptPerRun: 3, MaxBacklogUndone: 10, DedupThreshold: 0.6}}, false},
		{"floor too high", protocol.Config{Evolution: &protocol.EvolutionConfig{ValueFloor: 1.5}}, true},
		{"negative cap", protocol.Config{Evolution: &protocol.EvolutionConfig{MaxAcceptPerRun: -1}}, true},
		{"dedup out of range", protocol.Config{Evolution: &protocol.EvolutionConfig{DedupThreshold: 2}}, true},
		{"candidate_max_idle_days negative", protocol.Config{Evolution: &protocol.EvolutionConfig{CandidateMaxIdleDays: intPtr(-1)}}, true},
		{"candidate_max_idle_days zero ok", protocol.Config{Evolution: &protocol.EvolutionConfig{CandidateMaxIdleDays: intPtr(0)}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checks := checkEvolution(tc.cfg)
			if len(checks) == 0 {
				t.Fatal("checkEvolution returned no checks")
			}
			gotFail := hasSeverity(checks, sectionEvolution, SeverityFail)
			if gotFail != tc.wantFail {
				t.Errorf("fail = %v, want %v (checks=%+v)", gotFail, tc.wantFail, checks)
			}
		})
	}
}

// TestCheckEvolution_CandidateMaxIdleDaysDetail 驗證負值 FAIL 具正確 Name 與 Detail（AC-7）。
func TestCheckEvolution_CandidateMaxIdleDaysDetail(t *testing.T) {
	checks := checkEvolution(protocol.Config{Evolution: &protocol.EvolutionConfig{CandidateMaxIdleDays: intPtr(-5)}})
	found := false
	for _, c := range checks {
		if c.Section == sectionEvolution && c.Name == "candidate_max_idle_days" {
			found = true
			if c.Severity != SeverityFail {
				t.Errorf("severity = %v, want FAIL", c.Severity)
			}
			if !strings.Contains(c.Detail, "must be >= 0") {
				t.Errorf("detail = %q, want contains 'must be >= 0'", c.Detail)
			}
		}
	}
	if !found {
		t.Errorf("no candidate_max_idle_days check found (checks=%+v)", checks)
	}
}
