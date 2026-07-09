// Package doctor 對「合併後的設定」與「workspace 完整性」做一次性、純 read-only 的
// 健康檢查，集中那些原本只在 runtime 才會炸的問題（runner 未安裝、role model 缺漏、
// 孤兒 worktree、stale state、壞掉的 feature YAML 等），讓使用者在 4x run 之前先發現。
//
// 本 package 不呼叫任何 LLM、不依賴 runner 可用，也不寫入任何檔案。
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
	"gopkg.in/yaml.v3"
)

// Severity 是單一檢查項的結果等級。
type Severity string

const (
	// SeverityPass 表示該項檢查通過。
	SeverityPass Severity = "PASS"
	// SeverityWarn 表示有疑慮但不阻擋執行（如 runner 不在本機 PATH、孤兒 worktree）。
	SeverityWarn Severity = "WARN"
	// SeverityFail 表示設定或 workspace 有錯誤，會導致 4x run 失敗。
	SeverityFail Severity = "FAIL"
)

// Check 是一條健康檢查結果。Section 用來在輸出時分組（如 "settings"、"runners"、"roles"、"workspace"）。
type Check struct {
	Section  string   `json:"section"`
	Name     string   `json:"name"`
	Severity Severity `json:"severity"`
	Detail   string   `json:"detail"`
}

// Report 是一次 doctor 執行的完整結果。
type Report struct {
	Checks []Check `json:"checks"`
}

// HasFail 回報報告中是否存在任何 FAIL 項，供呼叫端決定 process exit code。
func (r Report) HasFail() bool {
	for _, c := range r.Checks {
		if c.Severity == SeverityFail {
			return true
		}
	}
	return false
}

// Options 是 Diagnose 的執行參數；LookPath 與 ProcessAlive 留作注入點以利測試。
type Options struct {
	Root         string                       // 專案根目錄（含 .4x/）
	LookPath     func(string) (string, error) // nil 時預設用 exec.LookPath
	ProcessAlive func(int) bool               // nil 時預設用 protocol.ProcessAlive
}

// section 名稱常量，集中避免拼字漂移。
const (
	sectionSettings  = "settings"
	sectionRunners   = "runners"
	sectionRoles     = "roles"
	sectionProfiles  = "profiles"
	sectionWorkspace = "workspace"
	sectionEvolution = "evolution"
)

// canonicalRoles 是 doctor 會逐一解析 model 的標準 role 集合（deep-reviewer 另以 deep_model 檢查）。
// 含 mini-coder：其可透過 roles.mini-coder.model 做獨立 model 路由，需一併驗證能否解析。
var canonicalRoles = []protocol.Role{
	protocol.RoleDesigner,
	protocol.RoleDesignReviewer,
	protocol.RoleCoder,
	protocol.RoleReviewer,
	protocol.RoleTester,
	protocol.RoleFixer,
	protocol.RoleMiniCoder,
	protocol.RoleAcceptor,
}

// Diagnose 執行所有健康檢查並回傳彙整報告；本函式為純 read-only，不寫入任何檔案。
//
// 流程：開 workspace → 讀 project config → 疊上 user config（合併）→ 依序跑
// settings / runners / roles / workspace 四段檢查。即使 settings.json 載入失敗，
// 也只把它轉成一條 settings FAIL，其餘檢查照常進行。
func Diagnose(opts Options) (Report, error) {
	lookPath := opts.LookPath
	processAlive := opts.ProcessAlive
	if processAlive == nil {
		processAlive = protocol.ProcessAlive
	}

	ws := &protocol.Workspace{Root: opts.Root}

	projectCfg, loadErr := ws.ReadConfig()
	cfg := projectCfg
	if userCfg, err := protocol.ReadUserConfig(); err == nil {
		cfg = protocol.MergeConfig(userCfg, projectCfg)
	}

	var report Report
	report.Checks = append(report.Checks, checkSettings(cfg, loadErr)...)
	report.Checks = append(report.Checks, checkRunners(cfg, lookPath)...)
	report.Checks = append(report.Checks, checkRoles(cfg, lookPath)...)
	report.Checks = append(report.Checks, checkProfiles(cfg, lookPath)...)
	report.Checks = append(report.Checks, checkWorkspace(ws, opts.Root, cfg, lookPath, processAlive)...)
	report.Checks = append(report.Checks, checkEvolution(cfg)...)
	return report, nil
}

// checkEvolution 驗證 F097 evolution 設定的數值範圍。read-only。
// Evolution 為 nil 時回一筆 PASS（套預設）；各數值越界各報一筆 FAIL；全合法回一筆 PASS。
func checkEvolution(cfg protocol.Config) []Check {
	if cfg.Evolution == nil {
		return []Check{{Section: sectionEvolution, Name: "config", Severity: SeverityPass, Detail: "evolution not configured (defaults apply)"}}
	}
	var checks []Check
	e := cfg.Evolution
	if e.ValueFloor < 0 || e.ValueFloor > 1 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "value_floor", Severity: SeverityFail, Detail: "must be in [0,1]"})
	}
	if e.MaxAcceptPerRun < 0 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "max_accept_per_run", Severity: SeverityFail, Detail: "must be >= 0"})
	}
	if e.MaxBacklogUndone < 0 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "max_backlog_undone", Severity: SeverityFail, Detail: "must be >= 0"})
	}
	if e.DedupThreshold < 0 || e.DedupThreshold > 1 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "dedup_threshold", Severity: SeverityFail, Detail: "must be in [0,1]"})
	}
	if e.CandidateMaxIdleDays != nil && *e.CandidateMaxIdleDays < 0 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "candidate_max_idle_days", Severity: SeverityFail, Detail: "must be >= 0"})
	}
	if len(checks) == 0 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "config", Severity: SeverityPass, Detail: "evolution settings valid"})
	}
	return checks
}

// checkSettings 驗證合併後 settings 的可載入性與必要欄位。loadErr 為 ReadConfig 的原始錯誤。
func checkSettings(cfg protocol.Config, loadErr error) []Check {
	var checks []Check

	if loadErr != nil {
		checks = append(checks, Check{
			Section:  sectionSettings,
			Name:     "settings.json loadable",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("無法載入 .4x/settings.json：%v", loadErr),
		})
		// 載入失敗時 cfg 為零值，後續欄位檢查意義不大，直接回傳這條 FAIL。
		return checks
	}

	checks = append(checks, Check{
		Section:  sectionSettings,
		Name:     "settings.json loadable",
		Severity: SeverityPass,
		Detail:   ".4x/settings.json 可正常解析",
	})

	if cfg.Project.Name == "" {
		checks = append(checks, Check{
			Section: sectionSettings, Name: "project.name", Severity: SeverityFail,
			Detail: "project.name 為空，請在 settings.json 設定專案名稱",
		})
	} else {
		checks = append(checks, Check{
			Section: sectionSettings, Name: "project.name", Severity: SeverityPass,
			Detail: fmt.Sprintf("project.name = %q", cfg.Project.Name),
		})
	}

	if len(cfg.Runners) == 0 {
		checks = append(checks, Check{
			Section: sectionSettings, Name: "runners defined", Severity: SeverityFail,
			Detail: "未定義任何 runner，4x run 無法執行",
		})
	} else {
		names := make([]string, 0, len(cfg.Runners))
		for name := range cfg.Runners {
			names = append(names, name)
		}
		sort.Strings(names)
		checks = append(checks, Check{
			Section: sectionSettings, Name: "runners defined", Severity: SeverityPass,
			Detail: fmt.Sprintf("已定義 %d 個 runner：%s", len(names), strings.Join(names, ", ")),
		})
	}

	checks = append(checks, checkDefaultRunner(cfg))

	if len(cfg.Project.VerifyCommandAllowlist) == 0 {
		checks = append(checks, Check{
			Section: sectionSettings, Name: "verify_command_allowlist", Severity: SeverityWarn,
			Detail: "verify_command_allowlist 未設定：AI 產出的 verify 命令將無前綴攔截直接執行（建議設定，見 F162）",
		})
	} else {
		checks = append(checks, Check{
			Section: sectionSettings, Name: "verify_command_allowlist", Severity: SeverityPass,
			Detail: fmt.Sprintf("已設定 %d 個允許前綴：%s",
				len(cfg.Project.VerifyCommandAllowlist),
				strings.Join(cfg.Project.VerifyCommandAllowlist, ", ")),
		})
	}
	return checks
}

// checkDefaultRunner 驗證 default_runner 與 runners map 的一致性。
func checkDefaultRunner(cfg protocol.Config) Check {
	switch {
	case cfg.Default == "" && len(cfg.Runners) > 0:
		return Check{
			Section: sectionSettings, Name: "default_runner", Severity: SeverityWarn,
			Detail: "未指定 default_runner，4x run 需以 --runner 指定",
		}
	case cfg.Default == "":
		// 無 runner 的情況已由 runners defined 報 FAIL，這裡不重複。
		return Check{
			Section: sectionSettings, Name: "default_runner", Severity: SeverityWarn,
			Detail: "未指定 default_runner",
		}
	default:
		if _, ok := cfg.Runners[cfg.Default]; !ok {
			return Check{
				Section: sectionSettings, Name: "default_runner", Severity: SeverityFail,
				Detail: fmt.Sprintf("default_runner %q 不在 runners 清單中", cfg.Default),
			}
		}
		return Check{
			Section: sectionSettings, Name: "default_runner", Severity: SeverityPass,
			Detail: fmt.Sprintf("default_runner = %q", cfg.Default),
		}
	}
}

// runnerAvailability 統一 runner 覆寫（roles.{role}.runner / feature phase_overrides.{phase}.runner）
// 的三態可用性判斷，避免門檻語意在多處各自複製後悄悄分歧：
//   - 名稱不在 cfg.Runners → SeverityFail；
//   - runner 存在但 command 經注入的 lookPath 找不到 → SeverityWarn；
//   - 合法且在 PATH（或未設 command）→ SeverityPass。
//
// 回傳的 detail 只描述 runner 本身，呼叫端自行前綴 role/phase 等上下文。
func runnerAvailability(cfg protocol.Config, lookPath func(string) (string, error), runnerName string) (Severity, string) {
	rc, ok := cfg.Runners[runnerName]
	if !ok {
		return SeverityFail, fmt.Sprintf("runner %q 不在 runners 清單中", runnerName)
	}
	if rc.Command != "" {
		if _, err := lookPath(rc.Command); err != nil {
			return SeverityWarn, fmt.Sprintf("runner %q 的 command %q 不在 PATH（若跑在遠端可忽略）", runnerName, rc.Command)
		}
	}
	return SeverityPass, fmt.Sprintf("runner %q 可用", runnerName)
}

// checkRunners 對每個 runner 的 command 用注入的 lookPath 檢查是否可在 PATH 找到。
// 找不到只報 WARN（runner 可能跑在遠端機器），不報 FAIL。
func checkRunners(cfg protocol.Config, lookPath func(string) (string, error)) []Check {
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	names := make([]string, 0, len(cfg.Runners))
	for name := range cfg.Runners {
		names = append(names, name)
	}
	sort.Strings(names)

	var checks []Check
	for _, name := range names {
		rc := cfg.Runners[name]
		if rc.Command == "" {
			checks = append(checks, Check{
				Section: sectionRunners, Name: name, Severity: SeverityWarn,
				Detail: fmt.Sprintf("runner %q 未設定 command", name),
			})
			continue
		}
		path, err := lookPath(rc.Command)
		if err != nil {
			checks = append(checks, Check{
				Section: sectionRunners, Name: name, Severity: SeverityWarn,
				Detail: fmt.Sprintf("runner %q 的 command %q 不在 PATH（若跑在遠端可忽略）", name, rc.Command),
			})
			continue
		}
		checks = append(checks, Check{
			Section: sectionRunners, Name: name, Severity: SeverityPass,
			Detail: fmt.Sprintf("%s → %s", rc.Command, path),
		})
	}
	return checks
}

// configurableRoleNames 是 cfg.Roles 合法 key 的集合（ConfigurableRoles() SoT），供 checkRoles
// 判斷某個 role 名稱是否為 canonical——非 canonical 的 key（如 typo "reviewr"）即使值合法，
// ResolvePhaseRunner／ResolveModel 也不會讀到它（PhaseRole／ResolveModel 只認 canonical role），
// 形同 dead config，須報 FAIL 而非放行成 PASS（post-merge 缺陷 #4）。
var configurableRoleNames = buildConfigurableRoleNameSet()

func buildConfigurableRoleNameSet() map[string]bool {
	m := make(map[string]bool)
	for _, r := range protocol.ConfigurableRoles() {
		m[string(r)] = true
	}
	return m
}

// roleEffectiveRunner 回傳某 role 實際會使用的 runner 名稱：優先 cfg.Roles[role].Runner，
// 否則退回 cfg.Default。語意對齊 ResolvePhaseRunner 的 roles 層（無 profile/feature 覆寫時）；
// 供 checkRoles 對「覆寫後的 runner」而非永遠對 cfg.Default 驗證 model tier 可解析性
// （post-merge 缺陷 #2／#3）。
func roleEffectiveRunner(cfg protocol.Config, role protocol.Role) string {
	if rc, ok := cfg.Roles[string(role)]; ok && rc.Runner != "" {
		return rc.Runner
	}
	return cfg.Default
}

// anyRoleHasRunnerOverride 回報 canonicalRoles 或 deep-reviewer 中是否有任一 role 設了自己的
// runner 覆寫；供 checkRoles 決定「無 default_runner」時是否仍能對個別 role 解析 model，
// 而不是無腦全部降級為單一 WARN。
func anyRoleHasRunnerOverride(cfg protocol.Config) bool {
	for _, role := range canonicalRoles {
		if rc, ok := cfg.Roles[string(role)]; ok && rc.Runner != "" {
			return true
		}
	}
	if rc, ok := cfg.Roles[string(protocol.RoleDeepReviewer)]; ok && rc.Runner != "" {
		return true
	}
	return false
}

// checkRoles 先驗證每個 role 的 runner 覆寫（roles.{role}.runner）可用性，再對每個 canonical
// role「覆寫後實際會用的 runner」解析 model，並檢查 deep_model。runner 覆寫的三態驗證與
// cfg.Default 是否存在完全解耦（DR-9）：即使無 default_runner，未知 runner 仍報 FAIL；
// model 解析部分則在「無 default runner 且無任何 role 覆寫」時降級為單一 WARN，不 panic；
// 只要任一 role 有自己的 runner 覆寫，仍會針對該 role 個別解析。
func checkRoles(cfg protocol.Config, lookPath func(string) (string, error)) []Check {
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	var checks []Check

	// roles.{role}.runner 覆寫可用性驗證：與下方 model 解析、與 cfg.Default 皆解耦。
	roleNames := make([]string, 0, len(cfg.Roles))
	for name, rc := range cfg.Roles {
		if rc.Runner != "" {
			roleNames = append(roleNames, name)
		}
	}
	sort.Strings(roleNames)
	for _, name := range roleNames {
		if !configurableRoleNames[name] {
			checks = append(checks, Check{
				Section: sectionRoles, Name: name + " runner", Severity: SeverityFail,
				Detail: fmt.Sprintf("role 名稱 %q 不是合法 role，此 runner 覆寫不會生效（silent fallback）", name),
			})
			continue
		}
		sev, detail := runnerAvailability(cfg, lookPath, cfg.Roles[name].Runner)
		checks = append(checks, Check{
			Section: sectionRoles, Name: name + " runner", Severity: sev,
			Detail: fmt.Sprintf("role %q：%s", name, detail),
		})
	}

	// 以下 model 解析：若無 default_runner 且没有任何 role 有自己的 runner 覆寫，
	// 全部無從解析，降級為單一 WARN（既有行為不變）；否則逐一用各 role 的實際 runner 解析。
	if cfg.Default == "" && !anyRoleHasRunnerOverride(cfg) {
		checks = append(checks, Check{
			Section: sectionRoles, Name: "role models", Severity: SeverityWarn,
			Detail: "無 default_runner，無法解析各 role 的 model",
		})
		return checks
	}

	for _, role := range canonicalRoles {
		runnerName := roleEffectiveRunner(cfg, role)
		if runnerName == "" {
			checks = append(checks, Check{
				Section: sectionRoles, Name: string(role), Severity: SeverityWarn,
				Detail: "無 default_runner 且未設定 role runner 覆寫，無法解析 model",
			})
			continue
		}
		model, err := protocol.ResolveModel(cfg, runnerName, role)
		if err != nil {
			checks = append(checks, Check{
				Section: sectionRoles, Name: string(role), Severity: SeverityWarn,
				Detail: fmt.Sprintf("無法解析 model（%v），執行時會 fallback 到 runner 預設", err),
			})
			continue
		}
		checks = append(checks, Check{
			Section: sectionRoles, Name: string(role), Severity: SeverityPass,
			Detail: fmt.Sprintf("model = %s", model),
		})
	}

	// deep-reviewer：runner 覆寫在 roles.deep-reviewer.runner（見 ResolvePhaseRunner 的
	// phaseToRoleMap[deep-reviewing]=deep-reviewer），但 deep_model 值設在 reviewer role 上
	// （見 docs/guide/runners.md「deep_model 配置在 reviewer role」——這是刻意設計，非缺陷）。
	// 兩者是正交的兩個軸：驗證 tier 是否可解析時，必須用「deep-reviewing phase 實際會用的 runner」，
	// 而不是永遠用 cfg.Default，否則 roles.deep-reviewer.runner 覆寫到缺少該 tier 的 runner 時，
	// doctor 會誤報 PASS，但 runtime 會静默跳過 deep-reviewing（post-merge 缺陷 #3）。
	deepRunner := roleEffectiveRunner(cfg, protocol.RoleDeepReviewer)
	if deepRunner == "" {
		checks = append(checks, Check{
			Section: sectionRoles, Name: string(protocol.RoleDeepReviewer), Severity: SeverityWarn,
			Detail: "無 default_runner 且未設定 roles.deep-reviewer.runner，無法解析 deep model",
		})
		return checks
	}
	deepModel, err := protocol.ResolveDeepModel(cfg, deepRunner, protocol.RoleReviewer)
	switch {
	case err != nil:
		checks = append(checks, Check{
			Section: sectionRoles, Name: string(protocol.RoleDeepReviewer), Severity: SeverityWarn,
			Detail: fmt.Sprintf("無法解析 deep_model（%v），deep review 會退回一般 model", err),
		})
	case deepModel == "":
		// 未明確設定 deep_model，嘗試 fallback 到 DefaultDeepTier
		if fallback, fbErr := protocol.ResolveTierModel(cfg, deepRunner, protocol.DefaultDeepTier); fbErr == nil && fallback != "" {
			checks = append(checks, Check{
				Section: sectionRoles, Name: string(protocol.RoleDeepReviewer), Severity: SeverityPass,
				Detail: fmt.Sprintf("deep_model 未設定，fallback 到預設 tier %q → %s", protocol.DefaultDeepTier, fallback),
			})
		} else {
			checks = append(checks, Check{
				Section: sectionRoles, Name: string(protocol.RoleDeepReviewer), Severity: SeverityWarn,
				Detail: fmt.Sprintf("未設定 deep_model 且 runner 無法解析預設 tier %q，deep-reviewing 會被跳過", protocol.DefaultDeepTier),
			})
		}
	default:
		checks = append(checks, Check{
			Section: sectionRoles, Name: string(protocol.RoleDeepReviewer), Severity: SeverityPass,
			Detail: fmt.Sprintf("deep_model = %s", deepModel),
		})
	}
	return checks
}

// checkProfiles 對每個自訂 profile 驗證新 phase 結構：phase 在 profileSelectablePhases 白名單、
// 含 coding phase、每個 PhaseSpec.Runner（非空）存在於 runners 且 binary 在 PATH、每個
// PhaseSpec.Model（非空）能被對應 runner 解析。無自訂 profile 時回單一 PASS。
func checkProfiles(cfg protocol.Config, lookPath func(string) (string, error)) []Check {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if len(cfg.Profiles) == 0 {
		return []Check{{
			Section: sectionProfiles, Name: "profiles", Severity: SeverityPass,
			Detail: "未定義自訂 profile，使用內建 full/normal/quick",
		}}
	}

	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	var checks []Check
	for _, name := range names {
		pc := cfg.Profiles[name]
		var issues []Check
		hasCoding := false
		for _, ps := range pc.Phases {
			phase := protocol.Phase(ps.Phase)
			if !protocol.IsSelectablePhase(phase) {
				issues = append(issues, Check{
					Section: sectionProfiles, Name: name, Severity: SeverityFail,
					Detail: fmt.Sprintf("phase %q 不在可選白名單", ps.Phase),
				})
			}
			if phase == protocol.PhaseCoding {
				hasCoding = true
			}
			if ps.Runner != "" {
				// runner 可用性驗證與 checkRoles／checkFeatureYAML 共用同一份三態邏輯
				// （runnerAvailability），避免各處門檻語意各自維護、日後漂移（post-merge 缺陷 #7）。
				sev, detail := runnerAvailability(cfg, lookPath, ps.Runner)
				if sev != SeverityPass {
					issues = append(issues, Check{
						Section: sectionProfiles, Name: name, Severity: sev,
						Detail: fmt.Sprintf("phase %q 的 %s", ps.Phase, detail),
					})
				}
				if _, ok := cfg.Runners[ps.Runner]; ok {
					if ps.Model != "" {
						role := protocol.PhaseRole(phase)
						if _, err := protocol.ResolvePhaseModel(cfg, feature.Feature{}, pc, phase, role, ps.Runner, ""); err != nil {
							issues = append(issues, Check{
								Section: sectionProfiles, Name: name, Severity: SeverityWarn,
								Detail: fmt.Sprintf("phase %q 的 model %q 無法被 runner %q 解析（%v）", ps.Phase, ps.Model, ps.Runner, err),
							})
						}
					}
				}
			}
		}
		if len(pc.Phases) > 0 && !hasCoding {
			issues = append(issues, Check{
				Section: sectionProfiles, Name: name, Severity: SeverityFail,
				Detail: "profile 必須包含 coding phase",
			})
		}
		if len(issues) == 0 {
			checks = append(checks, Check{
				Section: sectionProfiles, Name: name, Severity: SeverityPass,
				Detail: fmt.Sprintf("%d 個 phase 設定正確", len(pc.Phases)),
			})
		} else {
			checks = append(checks, issues...)
		}
	}
	return checks
}

// checkWorkspace 檢查 workspace 完整性：孤兒/懸空 worktree、stale state、壞掉的 feature YAML。
// 全程 read-only，stale state 以注入的 processAlive 判斷，絕不呼叫 ReconcileActive。
func checkWorkspace(ws *protocol.Workspace, root string, cfg protocol.Config, lookPath func(string) (string, error), processAlive func(int) bool) []Check {
	var checks []Check
	checks = append(checks, checkWorktrees(ws, root)...)
	checks = append(checks, checkStaleState(ws, processAlive)...)
	checks = append(checks, checkFeatureYAML(ws, cfg, lookPath)...)
	return checks
}

// checkWorktrees 掃描 .worktrees/4x/ 下實際存在的目錄，比對 feature 狀態找出孤兒與懸空 worktree。
func checkWorktrees(ws *protocol.Workspace, root string) []Check {
	worktreesDir := filepath.Dir(gitops.Dir(root, "x")) // .worktrees/4x
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		// 不存在代表沒有任何 worktree，乾淨。
		return []Check{{
			Section: sectionWorkspace, Name: "worktrees", Severity: SeverityPass,
			Detail: "無遺留 worktree",
		}}
	}

	// statusByID 以 ListFeatures 的 f.ID（YAML 內容的權威 id）為唯一來源，
	// exists 判斷直接看此 map 是否含 key，使「是否存在」與「status 查詢」不可能 diverge。
	statusByID := map[string]feature.Status{}
	if features, err := ws.ListFeatures(); err == nil {
		for _, f := range features {
			statusByID[f.ID] = f.Status
		}
	}
	// ListFeatures 會 silently skip 壞檔（無法解析或缺 id），其 worktree 不應因此被誤判為 dangling。
	// fileSet 以 filename 作為 fallback：只要對應到一個實際存在的 .yaml 檔就不算 dangling。
	fileSet := featureFileSet(ws)

	var checks []Check
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		path := gitops.Dir(root, id)
		status, known := statusByID[id]
		switch {
		case !known && !fileSet[id]:
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: "worktree " + id, Severity: SeverityWarn,
				Detail: fmt.Sprintf("dangling worktree：%s 無對應 feature", path),
			})
		case status == feature.StatusDone || status == feature.StatusAbandoned:
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: "worktree " + id, Severity: SeverityWarn,
				Detail: fmt.Sprintf("orphaned worktree：feature %s 已 %s 但 %s 仍存在", id, status, path),
			})
		}
	}
	if len(checks) == 0 {
		return []Check{{
			Section: sectionWorkspace, Name: "worktrees", Severity: SeverityPass,
			Detail: "無孤兒或懸空 worktree",
		}}
	}
	return checks
}

// checkStaleState 偵測 state.json 標記為 active 但 process 已消失的 feature（read-only）。
func checkStaleState(ws *protocol.Workspace, processAlive func(int) bool) []Check {
	features, err := ws.ListFeatures()
	if err != nil {
		return nil
	}
	var checks []Check
	for _, f := range features {
		state, err := ws.ReadState(f.ID)
		if err != nil {
			continue // 無 state.json 視為未啟動
		}
		if state.Active && !processAlive(state.Pid) {
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: "state " + f.ID, Severity: SeverityWarn,
				Detail: fmt.Sprintf("stale state：標記為執行中但 pid %d 已不存在", state.Pid),
			})
		}
	}
	if len(checks) == 0 {
		return []Check{{
			Section: sectionWorkspace, Name: "state", Severity: SeverityPass,
			Detail: "無 stale state",
		}}
	}
	return checks
}

// checkFeatureYAML 逐檔解析 .4x/features/*.yaml，定位個別壞檔（不用 ListFeatures，避免第一個壞檔中斷）。
// 另對每個 feature 的 phase_overrides.{phase}.runner 覆寫做三態可用性驗證（DR-8，涵蓋最高優先序
// 的 feature 層 runner 覆寫），語意與 checkProfiles / checkRoles 對齊。
func checkFeatureYAML(ws *protocol.Workspace, cfg protocol.Config, lookPath func(string) (string, error)) []Check {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	dir := filepath.Join(ws.DotDir(), protocol.FeaturesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Check{{
			Section: sectionWorkspace, Name: "feature yaml", Severity: SeverityPass,
			Detail: "無 feature YAML",
		}}
	}

	var checks []Check
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: e.Name(), Severity: SeverityFail,
				Detail: fmt.Sprintf("無法讀取 %s：%v", e.Name(), err),
			})
			continue
		}
		var f feature.Feature
		if err := yaml.Unmarshal(data, &f); err != nil {
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: e.Name(), Severity: SeverityFail,
				Detail: fmt.Sprintf("YAML 解析失敗 %s：%v", e.Name(), err),
			})
			continue // Unmarshal 失敗時 f 不可信，不再做語意驗證
		}
		// 語法合法後再做語意驗證：缺 id 為 fatal（FAIL），其餘格式問題為 warning（WARN）。
		warnings, fatalErr := f.ValidateLoose()
		if fatalErr != nil {
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: e.Name(), Severity: SeverityFail,
				Detail: fmt.Sprintf("feature YAML 語意錯誤 %s：%v", e.Name(), fatalErr),
			})
			continue
		}
		if len(warnings) > 0 {
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: e.Name(), Severity: SeverityWarn,
				Detail: fmt.Sprintf("feature YAML 語意警告 %s：%s", e.Name(), strings.Join(warnings, "; ")),
			})
		}

		// phase_overrides.{phase}.runner 覆寫的可用性驗證（DR-8）：三態語意同 checkProfiles，
		// 合法且在 PATH 時不 append（沿用下方「所有 feature YAML 可正常解析」PASS 彙總）。
		// 另先驗證 phase key 本身是否為 canonical phase（post-merge 缺陷 #5）：ResolvePhaseRunner／
		// ResolvePhaseModel 執行時只會用真正的 Phase 常量去查 f.PhaseOverrides，typo key（如
		// "reviewng"）永遠查不到、形同 dead config，須報 FAIL 而非放行成 PASS。
		phaseKeys := make([]string, 0, len(f.PhaseOverrides))
		for phase := range f.PhaseOverrides {
			phaseKeys = append(phaseKeys, phase)
		}
		sort.Strings(phaseKeys)
		for _, phase := range phaseKeys {
			if protocol.PhaseRole(protocol.Phase(phase)) == "" {
				checks = append(checks, Check{
					Section: sectionWorkspace, Name: e.Name(), Severity: SeverityFail,
					Detail: fmt.Sprintf("%s phase_overrides 使用非 canonical phase %q，此覆寫不會生效", e.Name(), phase),
				})
				continue
			}
			po := f.PhaseOverrides[phase]
			if po.Runner == "" {
				continue
			}
			sev, detail := runnerAvailability(cfg, lookPath, po.Runner)
			if sev == SeverityPass {
				continue
			}
			checks = append(checks, Check{
				Section: sectionWorkspace, Name: e.Name(), Severity: sev,
				Detail: fmt.Sprintf("%s phase %q 的 %s", e.Name(), phase, detail),
			})
		}
	}
	if len(checks) == 0 {
		return []Check{{
			Section: sectionWorkspace, Name: "feature yaml", Severity: SeverityPass,
			Detail: "所有 feature YAML 可正常解析",
		}}
	}
	return checks
}

// featureFileSet 回傳 .4x/features/ 下所有 yaml 檔對應的 feature id 集合（含無法解析的壞檔）。
func featureFileSet(ws *protocol.Workspace) map[string]bool {
	set := map[string]bool{}
	dir := filepath.Join(ws.DotDir(), protocol.FeaturesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return set
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		set[strings.TrimSuffix(e.Name(), ".yaml")] = true
	}
	return set
}
