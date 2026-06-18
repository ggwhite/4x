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
)

// canonicalRoles 是 doctor 會逐一解析 model 的標準 role 集合（deep-reviewer 另以 deep_model 檢查）。
var canonicalRoles = []protocol.Role{
	protocol.RoleDesigner,
	protocol.RoleCoder,
	protocol.RoleReviewer,
	protocol.RoleTester,
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
	report.Checks = append(report.Checks, checkRoles(cfg)...)
	report.Checks = append(report.Checks, checkProfiles(cfg, lookPath)...)
	report.Checks = append(report.Checks, checkWorkspace(ws, opts.Root, processAlive)...)
	return report, nil
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

// checkRoles 用 default runner 解析每個 canonical role 實際使用的 model，並檢查 deep_model。
// 無 default runner 時整段降級為單一 WARN，不 panic。
func checkRoles(cfg protocol.Config) []Check {
	if cfg.Default == "" {
		return []Check{{
			Section: sectionRoles, Name: "role models", Severity: SeverityWarn,
			Detail: "無 default_runner，無法解析各 role 的 model",
		}}
	}

	var checks []Check
	for _, role := range canonicalRoles {
		model, err := protocol.ResolveModel(cfg, cfg.Default, role)
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

	// deep-reviewer：deep_model 設在 reviewer role 上（見 run.go 的用法）。
	deepModel, err := protocol.ResolveDeepModel(cfg, cfg.Default, protocol.RoleReviewer)
	switch {
	case err != nil:
		checks = append(checks, Check{
			Section: sectionRoles, Name: string(protocol.RoleDeepReviewer), Severity: SeverityWarn,
			Detail: fmt.Sprintf("無法解析 deep_model（%v），deep review 會退回一般 model", err),
		})
	case deepModel == "":
		checks = append(checks, Check{
			Section: sectionRoles, Name: string(protocol.RoleDeepReviewer), Severity: SeverityWarn,
			Detail: "未設定 deep_model，deep review 會退回一般 model",
		})
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
				rc, ok := cfg.Runners[ps.Runner]
				if !ok {
					issues = append(issues, Check{
						Section: sectionProfiles, Name: name, Severity: SeverityFail,
						Detail: fmt.Sprintf("phase %q 的 runner %q 不在 runners 清單中", ps.Phase, ps.Runner),
					})
				} else {
					if rc.Command != "" {
						if _, err := lookPath(rc.Command); err != nil {
							issues = append(issues, Check{
								Section: sectionProfiles, Name: name, Severity: SeverityWarn,
								Detail: fmt.Sprintf("phase %q 的 runner %q command %q 不在 PATH（若跑在遠端可忽略）", ps.Phase, ps.Runner, rc.Command),
							})
						}
					}
					if ps.Model != "" {
						if _, err := protocol.ResolvePhaseModel(cfg, feature.Feature{}, pc, phase, protocol.RoleCoder, ps.Runner, ""); err != nil {
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
func checkWorkspace(ws *protocol.Workspace, root string, processAlive func(int) bool) []Check {
	var checks []Check
	checks = append(checks, checkWorktrees(ws, root)...)
	checks = append(checks, checkStaleState(ws, processAlive)...)
	checks = append(checks, checkFeatureYAML(ws)...)
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
func checkFeatureYAML(ws *protocol.Workspace) []Check {
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
