package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/ggwhite/4x/templates"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newPromptCmd() *cobra.Command {
	var role string
	var round int
	var runner string

	cmd := &cobra.Command{
		Use:   "prompt <feature-id>",
		Short: "Generate a role prompt for the current phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			featureID, err := ws.ResolveFeatureID(args[0])
			if err != nil {
				return err
			}
			feature, err := ws.LoadFeature(featureID)
			if err != nil {
				return err
			}

			r := protocol.Role(role)
			if r == "" {
				s, err := ws.ReadState(featureID)
				if err != nil {
					return fmt.Errorf("no --role specified and cannot read state: %w", err)
				}
				r = state.PhaseToRole(s.Phase)
				if round == 0 {
					round = s.Round
				}
			}

			cfg, err := ws.LoadMergedConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			locale, localeName := resolveLocale()

			var roleInc []string
			if rc, ok := cfg.Roles[string(r)]; ok {
				roleInc = rc.Includes
			}

			data := promptData{
				Feature:             feature,
				Project:             cfg.Project,
				Role:                r,
				Round:               round,
				Config:              cfg,
				DotDir:              ws.DotDir(),
				Locale:              locale,
				LocaleName:          localeName,
				RoleInstructions:    roleInstructions(cfg, r),
				ProjectIncludes:     append(loadIncludes(ws.Root, cfg.Project.Includes), discoverConventionFiles(ws.Root, runner, cfg.Project.Includes)...),
				RoleIncludes:        loadIncludes(ws.Root, roleInc),
				PlanningDoc:         loadPlanningDocs(ws.Root, feature, cfg.DesignDocDirs),
				ProfileInstructions: loadProfiles(ws, featureID, cfg),
			}

			if r == protocol.RoleDesigner {
				data.Learnings = loadActiveLearnings(ws.DotDir())
			} else {
				data.SelectedLearnings = loadSelectedLearnings(ws.DotDir(), featureID, r)
			}

			tmpl, err := loadRoleTemplate(ws.DotDir(), r)
			if err != nil {
				return err
			}

			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, data); err != nil {
				return err
			}
			_, err = os.Stdout.WriteString(compactBlankLines(buf.String()))
			return err
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "role (designer/coder/reviewer/tester)")
	cmd.Flags().IntVar(&round, "round", 0, "round number")
	cmd.Flags().StringVar(&runner, "runner", "", "runner name (skip auto-read convention files)")
	return cmd
}

type promptData struct {
	Feature          feat.Feature
	Project          protocol.ProjectConfig
	Role             protocol.Role
	Round            int
	Iteration        int
	Config           protocol.Config
	DotDir           string
	Locale           string
	LocaleName       string
	RoleInstructions []string
	ProjectIncludes  []includeContent
	RoleIncludes     []includeContent
	PlanningDoc      string
	RepoMap          map[string]string
	// ProfileInstructions 是 test-strategy.yaml profiles 載入後的測試方法論，供 Tester template 注入。
	ProfileInstructions []profileContent
	// Learnings 是所有 active learnings，供 Designer template 列出供選擇。
	Learnings []learning.Entry
	// SelectedLearnings 是已選中且符合本 role category 的 learnings，供後續 role template 注入。
	SelectedLearnings []learning.Entry
	// SkippedDesigner 為 true 代表 profile 跳過了 designer phase，template 應從 feature YAML 讀需求。
	SkippedDesigner bool
	// 以下欄位僅平行 deep review 模式使用：
	// ReviewerIndex/ReviewerCount 標示本 sub-reviewer 是第幾個、共幾個；
	// AssignedAngles 為本 sub-reviewer 負責的 angle 編號清單（為空代表 fallback 單 agent 跑全部）；
	// PartialReportName 為本 sub-reviewer 要寫入的 partial report 檔名；
	// PartialReports 為 synthesizer 要合併的所有 partial report 完整內文。
	ReviewerIndex     int
	ReviewerCount     int
	AssignedAngles    []int
	PartialReportName string
	PartialReports    []includeContent
}

type includeContent struct {
	Path    string
	Content string
}

// profileContent 是單一 test profile 載入後的名稱與內容，供 Tester template 注入。
type profileContent struct {
	Name    string
	Content string
}

// loadProfiles 讀取 test-strategy.yaml 的 profiles 欄位，依序載入各 profile 的測試方法論內容。
// 沒有 profiles（或讀不到 test-strategy.yaml）時回傳 nil，行為與舊版一致。
// 載入優先序見 resolveProfileContent；找不到內容的 profile 印 warning 並略過。
func loadProfiles(ws *protocol.Workspace, featureID string, cfg protocol.Config) []profileContent {
	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	data, err := os.ReadFile(stratPath)
	if err != nil {
		return nil
	}

	var ts protocol.TestStrategy
	if err := yaml.Unmarshal(data, &ts); err != nil {
		slog.Warn("invalid test-strategy.yaml", "path", stratPath, "error", err)
		return nil
	}

	if len(ts.Profiles) == 0 {
		return nil
	}

	var result []profileContent
	for _, name := range ts.Profiles {
		content := resolveProfileContent(ws.Root, name, cfg)
		if content == "" {
			slog.Warn("unknown test profile, skipping", "profile", name)
			continue
		}
		result = append(result, profileContent{Name: name, Content: content})
	}
	return result
}

// resolveProfileContent 解析單一 profile 的內容：
// settings.json test_profiles[name]（content 優先，其次 include 讀檔）→ 內建 templates/profiles/{name}.md。
// 都找不到時回傳空字串。
func resolveProfileContent(root, name string, cfg protocol.Config) string {
	if override, ok := cfg.TestProfiles[name]; ok {
		if override.Content != "" {
			return override.Content
		}
		if override.Include != "" {
			p := override.Include
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				slog.Warn("test profile include read failed", "include", override.Include, "error", err)
				return ""
			}
			return string(data)
		}
	}

	data, err := templates.ProfilesFS.ReadFile("profiles/" + name + ".md")
	if err != nil {
		return ""
	}
	return string(data)
}

// roleInstructions 從 Config 取出指定角色的 instructions
func roleInstructions(cfg protocol.Config, r protocol.Role) []string {
	if rc, ok := cfg.Roles[string(r)]; ok {
		return rc.Instructions
	}
	return nil
}

// loadPlanningDocs 解析 feature 的 spec 與 plan 設計文件並串接，供 prompt 注入。
// 解析優先序統一走 protocol.ResolveDesignDoc；找不到的文件跳過，不報錯。
func loadPlanningDocs(root string, feature feat.Feature, designDocDirs []string) string {
	var parts []string
	for _, docType := range []string{"spec", "plan"} {
		doc := protocol.ResolveDesignDoc(root, feature, docType, designDocDirs...)
		if doc.Content != "" {
			parts = append(parts, doc.Content)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// compactBlankLines 把連續多個空行壓成一個，保留 Markdown 段落分隔語意。
func compactBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// loadIncludes 讀取指定路徑的檔案內容，路徑相對於 root 解析
func loadIncludes(root string, paths []string) []includeContent {
	var result []includeContent
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(root, p)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			slog.Warn("include file read failed", "path", p, "error", err)
			continue
		}
		result = append(result, includeContent{Path: p, Content: string(data)})
	}
	return result
}

// runnerAutoReads 列出各 runner CLI 會自動讀取的慣例檔，4x prompt 不需重複注入。
var runnerAutoReads = map[string][]string{
	"claude":  {"CLAUDE.md"},
	"codex":   {"AGENTS.md"},
	"gemini":  {"GEMINI.md"},
	"copilot": {"AGENTS.md"},
	"cursor":  {".cursorrules", "CURSORRULES"},
	"opencode": {"AGENTS.md"},
}

// discoverConventionFiles 探測專案根目錄下的 agent 慣例檔案（CLAUDE.md、AGENTS.md 等），
// 排除已在 explicit includes 中列出的檔案與當前 runner 會自動讀取的檔案，避免重複注入。
func discoverConventionFiles(root, runner string, explicitIncludes []string) []includeContent {
	conventionFiles := []string{
		"CLAUDE.md",
		"AGENTS.md",
		"GEMINI.md",
		"COPILOT.md",
		"CURSORRULES",
		".cursorrules",
	}

	skip := make(map[string]bool, len(explicitIncludes))
	for _, p := range explicitIncludes {
		skip[p] = true
	}
	for _, p := range runnerAutoReads[runner] {
		skip[p] = true
	}

	var result []includeContent
	for _, name := range conventionFiles {
		if skip[name] {
			continue
		}
		abs := filepath.Join(root, name)
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		result = append(result, includeContent{Path: name, Content: string(data)})
	}
	return result
}

var localeNames = map[string]string{
	"en":    "English",
	"zh-TW": "繁體中文",
	"zh-CN": "简体中文",
	"ja":    "日本語",
	"ko":    "한국어",
	"es":    "Español",
	"fr":    "Français",
	"de":    "Deutsch",
	"pt":    "Português",
	"vi":    "Tiếng Việt",
	"th":    "ภาษาไทย",
}

func resolveLocale() (code, name string) {
	ucfg, err := protocol.ReadUserConfig()
	if err != nil {
		slog.Warn("failed to read user config", "error", err)
	}
	if ucfg.Locale != "" {
		code = ucfg.Locale
	} else {
		code = localeFromEnv()
	}
	name = localeNames[code]
	if name == "" {
		name = code
	}
	return code, name
}

func localeFromEnv() string {
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}
	if lang == "" {
		return "en"
	}
	for i, c := range lang {
		if c == '.' || c == '@' {
			lang = lang[:i]
			break
		}
	}
	zhMapping := map[string]string{
		"zh_TW": "zh-Hant", "zh_HK": "zh-Hant",
		"zh_CN": "zh-Hans", "zh": "zh-Hans",
	}
	if mapped, ok := zhMapping[lang]; ok {
		return mapped
	}
	if lang == "C" || lang == "POSIX" {
		return "en"
	}
	for i, c := range lang {
		if c == '_' || c == '-' {
			return lang[:i]
		}
	}
	return lang
}

var tmplFuncs = template.FuncMap{
	"sub": func(a, b int) int { return a - b },
	// seq 回傳 1..n 的整數切片，供 deep-reviewer 模板在 fallback 單 agent 模式下
	// 動態組出全部 angle 編號（無須在模板硬寫 1 2 3 ... 11）。
	"seq": func(n int) []int {
		out := make([]int, 0, n)
		for i := 1; i <= n; i++ {
			out = append(out, i)
		}
		return out
	},
	// dict 把成對的 key/value 組成 map，供模板在呼叫 sub-template 時一次帶入多個值
	// （Go template 的 template action 只能傳單一 pipeline）。
	"dict": func(pairs ...any) map[string]any {
		m := make(map[string]any, len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			key, _ := pairs[i].(string)
			m[key] = pairs[i+1]
		}
		return m
	},
}

var roleTemplateFiles = map[protocol.Role]string{
	protocol.RoleDesigner:       "designer.md.tmpl",
	protocol.RoleDesignReviewer: "design-reviewer.md.tmpl",
	protocol.RoleCoder:          "coder.md.tmpl",
	protocol.RoleReviewer:       "reviewer.md.tmpl",
	protocol.RoleDeepReviewer:   "deep-reviewer.md.tmpl",
	protocol.RoleTester:         "tester.md.tmpl",
	protocol.RoleAcceptor:       "acceptor.md.tmpl",
	protocol.RoleMiniCoder:      "mini-coder.md.tmpl",
	protocol.RoleReVerifier:     "re-verifier.md.tmpl",
	protocol.RoleSynthesizer:    "synthesizer.md.tmpl",
	protocol.RoleGate:           "gate.md.tmpl",
}

// loadRoleTemplate 載入指定 role 的 prompt template。
// 查找順序為兩階段：先查專案目錄 .4x/templates/{filename}，找不到才 fallback
// 到 go:embed 內建 template。locale.tmpl 與 role template 各自獨立 fallback，
// 讓專案可只覆寫其中一個。dotDir 為空字串時等同只用內建 template。
func loadRoleTemplate(dotDir string, r protocol.Role) (*template.Template, error) {
	filename, ok := roleTemplateFiles[r]
	if !ok {
		return nil, fmt.Errorf("unknown role: %s", r)
	}

	locale := loadTemplateFile(dotDir, "locale.tmpl")
	role := loadTemplateFile(dotDir, filename)

	if locale == "" {
		data, err := templates.FS.ReadFile("locale.tmpl")
		if err != nil {
			return nil, fmt.Errorf("read locale template: %w", err)
		}
		locale = string(data)
	}
	if role == "" {
		data, err := templates.FS.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read role template %s: %w", filename, err)
		}
		role = string(data)
	}

	return template.New(string(r)).Funcs(tmplFuncs).Parse(locale + role)
}

// loadTemplateFile 嘗試從 .4x/templates/ 讀取 template 檔案，找不到時回傳空字串
// （由呼叫端 fallback 到內建 template）。dotDir 為空時直接回傳空字串。
func loadTemplateFile(dotDir, filename string) string {
	if dotDir == "" {
		return ""
	}
	path := filepath.Join(dotDir, "templates", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// loadActiveLearnings 讀取 learnings.json 中所有 active 條目，供 Designer prompt 列出供選擇。
// 任何讀取失敗只 warn 並回傳 nil，不影響 prompt 產生。
func loadActiveLearnings(dotDir string) []learning.Entry {
	storePath := filepath.Join(dotDir, protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings for prompt failed", "error", err)
		return nil
	}
	return store.ActiveEntries()
}

func loadSelectedLearnings(dotDir, featureID string, role protocol.Role) []learning.Entry {
	selPath := filepath.Join(dotDir, protocol.RunDir, featureID, protocol.SelectedLearningsFile)
	data, err := os.ReadFile(selPath)
	if err != nil {
		return nil
	}

	var payload selectedLearningsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		slog.Warn("parse selected-learnings.json failed", "error", err)
		return nil
	}
	if len(payload.Selected) == 0 {
		return nil
	}

	storePath := filepath.Join(dotDir, protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store for injection failed", "error", err)
		return nil
	}

	entryMap := make(map[string]learning.Entry, len(store.Entries))
	for _, e := range store.Entries {
		entryMap[e.ID] = e
	}

	categories := learning.CategoriesForRole(string(role))
	catSet := make(map[learning.Category]bool, len(categories))
	for _, c := range categories {
		catSet[c] = true
	}

	var result []learning.Entry
	for _, id := range payload.Selected {
		if len(result) >= learning.MaxSelectedPerRole {
			break
		}
		e, ok := entryMap[id]
		if !ok || e.Status != learning.StatusActive {
			continue
		}
		if !catSet[e.Category] {
			continue
		}
		result = append(result, e)
	}
	return result
}

// selectedLearningsPayload 是 selected-learnings.json 的結構。
type selectedLearningsPayload struct {
	Selected []string `json:"selected"`
}

// promptOption 在 promptData 組好、模板 render 前對其做最後調整，
// 供平行 deep review 注入 ReviewerIndex / AssignedAngles / PartialReports 等額外欄位。
type promptOption func(*promptData)

// deepReviewPartialName 回傳平行 deep review 中第 index 個 sub-reviewer 的 partial report
// 檔名（deep-review-partial-<index>.md，index 為 1-based）。
func deepReviewPartialName(index int) string {
	return fmt.Sprintf("deep-review-partial-%d.md", index)
}

// deepPartialComplete 判斷單一 deep-review-partial 檔是否已完整寫出。
// 完整判準：檔案存在、去空白後非空，且含 partial 模板的結尾段落標記 `## Statistics`
// （sub-reviewer 半截輸出時通常缺此段，避免半成品被誤判為完整而漏跑）。
func deepPartialComplete(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return false
	}
	return strings.Contains(string(data), "## Statistics")
}

// missingDeepPartials 回傳 1..want 中尚未完整的 partial index 清單（升冪）。
// resume 時用來判斷哪些 sub-reviewer 需補跑；首次執行時所有 index 皆缺，回傳完整清單。
func missingDeepPartials(roundDir string, want int) []int {
	var missing []int
	for i := 1; i <= want; i++ {
		if !deepPartialComplete(filepath.Join(roundDir, deepReviewPartialName(i))) {
			missing = append(missing, i)
		}
	}
	return missing
}

// withParallelDeepReviewer 把第 index/count 個 sub-reviewer 的 angle 指派與 partial report
// 檔名注入 promptData，讓 deep-reviewer 模板只 render 被分配的 angle 並輸出到 partial report。
func withParallelDeepReviewer(index, count int, angles []int, partialName string) promptOption {
	return func(d *promptData) {
		d.ReviewerIndex = index
		d.ReviewerCount = count
		d.AssignedAngles = angles
		d.PartialReportName = partialName
	}
}

// withSynthesizerReports 把所有 sub-reviewer 的 partial report 完整內文注入 promptData，
// 供 synthesizer 模板內嵌合併（不是只給路徑）。
func withSynthesizerReports(reports []includeContent) promptOption {
	return func(d *promptData) {
		d.ReviewerCount = len(reports)
		d.PartialReports = reports
	}
}

// harvestLearnings 讀取 Acceptor 產出的 retro-learnings.json，追加到 .4x/learnings.json。
// learnings 屬 nice-to-have，任何錯誤只 warn，絕不影響 state transition。
func harvestLearnings(ws *protocol.Workspace, featureID string) {
	retroPath := filepath.Join(ws.FeatureDir(featureID), protocol.RetroLearningsFile)
	learnings, err := learning.ParseRetroFile(retroPath)
	if err != nil {
		slog.Warn("skip learnings harvest", "feature", featureID, "error", err)
		return
	}
	if len(learnings) == 0 {
		return
	}

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store failed", "error", err)
		return
	}

	store.MarkStale(learning.DefaultStaleDays)
	added := store.Harvest(featureID, learnings)
	if added == 0 {
		return
	}

	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store failed", "error", err)
		return
	}

	active := len(store.ActiveEntries())
	slog.Info("harvested learnings", "feature", featureID, "added", added, "total_active", active)
	if active > learning.MaxActiveEntries {
		slog.Warn("learnings store exceeds capacity, consider running '4x learn prune'",
			"active", active, "limit", learning.MaxActiveEntries)
	}
}

// updateLearningsUsage 在第一個非 Designer phase 時呼叫一次：讀 selected-learnings.json，
// 更新被選中 learning 的 last_used 與 used_count。任何失敗只 warn，不影響 state transition。
func updateLearningsUsage(ws *protocol.Workspace, featureID string) {
	selPath := filepath.Join(ws.FeatureDir(featureID), protocol.SelectedLearningsFile)
	data, err := os.ReadFile(selPath)
	if err != nil {
		return
	}

	var payload selectedLearningsPayload
	if err := json.Unmarshal(data, &payload); err != nil || len(payload.Selected) == 0 {
		return
	}

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store for usage update failed", "error", err)
		return
	}

	store.UpdateUsage(payload.Selected)
	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store after usage update failed", "error", err)
	}
}

func generatePrompt(ws *protocol.Workspace, runnerWs *protocol.Workspace, feature feat.Feature, cfg protocol.Config, role protocol.Role, round, iteration int, runner string, opts ...promptOption) (string, error) {
	tmpl, err := loadRoleTemplate(runnerWs.DotDir(), role)
	if err != nil {
		return "", fmt.Errorf("no template for role %s: %w", role, err)
	}
	locale, localeName := resolveLocale()
	var roleInc []string
	if rc, ok := cfg.Roles[string(role)]; ok {
		roleInc = rc.Includes
	}
	var repoMap map[string]string
	if len(cfg.Workspace.Repos) > 0 {
		if runnerWs.Root != ws.Root {
			// worktree 模式：組合目錄下 repo 子目錄以 name 命名，使用相對路徑讓 coder 在正確邊界內作業
			featureRepos := make(map[string]bool, len(feature.Repos))
			for _, r := range feature.Repos {
				featureRepos[r] = true
			}
			repoMap = make(map[string]string, len(cfg.Workspace.Repos))
			for name := range cfg.Workspace.Repos {
				if len(feature.Repos) > 0 && !featureRepos[name] {
					continue
				}
				repoMap[name] = name
			}
		} else {
			repoMap = protocol.ResolveFeatureRepoPaths(feature, cfg, ws.Root)
		}
	}
	data := promptData{
		Feature:             feature,
		Project:             cfg.Project,
		Role:                role,
		Round:               round,
		Iteration:           iteration,
		Config:              cfg,
		DotDir:              runnerWs.DotDir(),
		Locale:              locale,
		LocaleName:          localeName,
		RoleInstructions:    roleInstructions(cfg, role),
		ProjectIncludes:     append(loadIncludes(ws.Root, cfg.Project.Includes), discoverConventionFiles(ws.Root, runner, cfg.Project.Includes)...),
		RoleIncludes:        loadIncludes(ws.Root, roleInc),
		PlanningDoc:         loadPlanningDocs(ws.Root, feature, cfg.DesignDocDirs),
		RepoMap:             repoMap,
		ProfileInstructions: loadProfiles(ws, feature.ID, cfg),
	}
	// 兩者皆從主 workspace（ws.DotDir()）讀取，而非 runnerWs.DotDir()：
	//   - learnings.json 由 CLI 在主 workspace 管理，本來就不會 sync 到 worktree。
	//   - selected-learnings.json 由 Designer 寫在 worktree，但會由 syncFeatureFromWorktree
	//     帶回主 workspace，因此後續 role 一律從主 workspace 讀取最新選擇。
	briefPath := filepath.Join(ws.FeatureDir(feature.ID), protocol.TaskBrief)
	skippedDesigner := false
	if _, err := os.Stat(briefPath); err != nil {
		skippedDesigner = true
	}
	if role == protocol.RoleDesigner {
		data.Learnings = loadActiveLearnings(ws.DotDir())
	} else if skippedDesigner && round == 1 && role == protocol.RoleCoder {
		data.Learnings = loadActiveLearnings(ws.DotDir())
	} else {
		data.SelectedLearnings = loadSelectedLearnings(ws.DotDir(), feature.ID, role)
	}
	data.SkippedDesigner = skippedDesigner
	for _, opt := range opts {
		opt(&data)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return compactBlankLines(buf.String()), nil
}

// promptResult 是 prefetch goroutine 透過 channel 回傳的生成結果。
type promptResult struct {
	prompt string
	err    error
}

// promptPrefetch 保存一個已在背景啟動的 prompt 預生成任務；以 role+round 為 key，
// 消費端 mismatch 時丟棄並退回同步生成，確保不會用錯 prompt。
type promptPrefetch struct {
	role  protocol.Role
	round int
	ch    chan promptResult
}

// prefetchablePhase 回報某 phase 是否會在主迴圈頂端走同步 generatePrompt（line 530），
// 只有這些 phase 值得預生成 prompt。reviewing 僅在非平行模式下才走頂端路徑
// （平行 runReviewTestParallel 自行呼叫 generatePrompt，不經頂端）。
func prefetchablePhase(phase protocol.Phase, cfg protocol.Config) bool {
	switch phase {
	case protocol.PhaseDesignReviewing, protocol.PhaseCoding, protocol.PhaseAmending, protocol.PhaseTesting, protocol.PhaseAccepting:
		return true
	case protocol.PhaseReviewing:
		return !cfg.ParallelReviewTest
	default:
		return false
	}
}
