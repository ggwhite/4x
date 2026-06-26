package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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

			rc := &runContext{ws: ws, runnerWs: ws, feature: feature, cfg: cfg}
			prompt, err := generatePrompt(rc, r, round, 0, runner)
			if err != nil {
				return err
			}
			_, err = os.Stdout.WriteString(prompt)
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
	// CodeMap 是專案的 exported symbol 摘要（每個 package 一行），讓 agent 不用探索就知道 codebase 結構。
	CodeMap string
	// SkippedDesigner 為 true 代表 profile 跳過了 designer phase，template 應從 feature YAML 讀需求。
	SkippedDesigner bool
	// TaskBrief 是 task-brief.md 的完整內容，直接內嵌到 prompt 省掉 agent 的 Read tool call。
	TaskBrief string
	// PrevReviewReport 是上一輪 review-report.md 的完整內容（amending 時使用）。
	PrevReviewReport string
	// PrevTestReport 是上一輪 test-report.md 的完整內容（amending 時使用，可能不存在）。
	PrevTestReport string
	// PrevDiff 是前幾輪 coder 從 baseline 到目前的 git diff（amending 時使用），
	// 讓 coder 不用重新探索就知道已經改了什麼。超過 maxDiffLines 行時截斷。
	PrevDiff string
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
// condense 為 true 時對 plan 做精簡：保留架構決策與 Task 標題，去掉詳細步驟和 code block，
// 適用於 coder role（已有 task-brief 提供實作細節，plan 僅作交叉驗證用）。
func loadPlanningDocs(root string, feature feat.Feature, designDocDirs []string, condense bool) string {
	var parts []string
	for _, docType := range []string{"spec", "plan"} {
		doc := protocol.ResolveDesignDoc(root, feature, docType, designDocDirs...)
		if doc.Content != "" {
			content := doc.Content
			if condense && docType == "plan" {
				content = condensePlan(content)
			}
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// condensePlan 精簡 plan 文件：保留 Task 前的架構段落（Goal、Constraints、File Structure），
// 每個 Task 只保留標題與描述區（Files、Interfaces），去掉 `- [ ] Step` 詳細步驟與 code block。
func condensePlan(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	inTask := false
	skipRest := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### Task ") {
			inTask = true
			skipRest = false
			out = append(out, line)
			continue
		}
		if inTask && strings.HasPrefix(trimmed, "- [ ]") {
			skipRest = true
			continue
		}
		if skipRest {
			if strings.HasPrefix(trimmed, "### Task ") || strings.HasPrefix(trimmed, "## ") {
				skipRest = false
				inTask = strings.HasPrefix(trimmed, "### Task ")
				out = append(out, line)
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
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

// loadIncludes 讀取指定路徑的檔案內容，路徑相對於 root 解析。
// skip 列出要跳過的檔名（basename），用於避免載入 runner 會自動讀取的慣例檔。
func loadIncludes(root string, paths []string, skip ...string) []includeContent {
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}
	var result []includeContent
	for _, p := range paths {
		if skipSet[filepath.Base(p)] {
			continue
		}
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

func generatePrompt(rc *runContext, role protocol.Role, round, iteration int, runnerName string, opts ...promptOption) (string, error) {
	ws := rc.ws
	runnerWs := rc.runnerWs
	feature := rc.feature
	cfg := rc.cfg

	tmpl, err := loadRoleTemplate(runnerWs.DotDir(), role)
	if err != nil {
		return "", fmt.Errorf("no template for role %s: %w", role, err)
	}
	locale, localeName := resolveLocale()
	var roleInc []string
	if roleCfg, ok := cfg.Roles[string(role)]; ok {
		roleInc = roleCfg.Includes
	}
	var repoMap map[string]string
	if len(cfg.Workspace.Repos) > 0 {
		if runnerWs.Root != ws.Root {
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
		ProjectIncludes:     append(loadIncludes(ws.Root, cfg.Project.Includes, runnerAutoReads[runnerName]...), discoverConventionFiles(ws.Root, runnerName, cfg.Project.Includes)...),
		RoleIncludes:        loadIncludes(ws.Root, roleInc),
		RepoMap:             repoMap,
		ProfileInstructions: loadProfiles(ws, feature.ID, cfg),
	}
	briefPath := filepath.Join(ws.FeatureDir(feature.ID), protocol.TaskBrief)
	skippedDesigner := false
	if _, err := os.Stat(briefPath); err != nil {
		skippedDesigner = true
	}
	condensePlan := role != protocol.RoleDesigner && role != protocol.RoleDesignReviewer && !skippedDesigner
	data.PlanningDoc = loadPlanningDocs(ws.Root, feature, cfg.DesignDocDirs, condensePlan)
	if role == protocol.RoleDesigner {
		data.Learnings = loadActiveLearnings(ws.DotDir())
	} else if skippedDesigner && round == 1 && role == protocol.RoleCoder {
		data.Learnings = loadActiveLearnings(ws.DotDir())
	} else {
		data.SelectedLearnings = loadSelectedLearnings(ws.DotDir(), feature.ID, role)
	}
	data.SkippedDesigner = skippedDesigner
	data.CodeMap = buildCodeMap(ws.Root)
	if role == protocol.RoleCoder || role == protocol.RoleMiniCoder {
		if !skippedDesigner {
			if b, err := os.ReadFile(briefPath); err == nil {
				data.TaskBrief = string(b)
			}
		}
		if round > 1 {
			prevRound := ws.RoundDir(feature.ID, round-1)
			if b, err := os.ReadFile(filepath.Join(prevRound, protocol.ReviewReport)); err == nil {
				data.PrevReviewReport = string(b)
			}
			if b, err := os.ReadFile(filepath.Join(prevRound, protocol.TestReport)); err == nil {
				data.PrevTestReport = string(b)
			}
			data.PrevDiff = baselineDiff(ws, feature.ID)
		}
	}
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

const maxDiffLines = 200

const maxCodeMapLines = 40

// buildCodeMap 掃描專案的 exported symbol，產出每個 package 一行的精簡摘要。
// 讓 agent 不用花 token 探索就知道 codebase 結構。非 Go 專案或掃描失敗回空字串。
func buildCodeMap(root string) string {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return ""
	}
	out, err := exec.Command("grep", "-rEn", `^(func|type) [A-Z]`,
		"--include=*.go", root).Output()
	if err != nil || len(out) == 0 {
		return ""
	}

	type entry struct {
		dir  string
		syms []string
	}
	dirOrder := []string{}
	groups := map[string][]string{}
	seen := map[string]map[string]bool{}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "_test.go") || strings.Contains(line, "vendor/") ||
			strings.Contains(line, ".worktrees/") || strings.Contains(line, ".claude/") {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		fpath := strings.TrimPrefix(parts[0], root+"/")
		dir := filepath.Dir(fpath)
		code := parts[2]

		var name string
		for _, prefix := range []string{"func ", "type "} {
			if strings.HasPrefix(code, prefix) {
				rest := code[len(prefix):]
				rest = strings.TrimPrefix(rest, "(")
				idx := strings.IndexAny(rest, " ([{")
				if idx > 0 {
					name = rest[:idx]
				} else {
					name = rest
				}
				break
			}
		}
		if name == "" {
			continue
		}

		if seen[dir] == nil {
			seen[dir] = map[string]bool{}
			dirOrder = append(dirOrder, dir)
		}
		if !seen[dir][name] {
			seen[dir][name] = true
			groups[dir] = append(groups[dir], name)
		}
	}

	var b strings.Builder
	for _, dir := range dirOrder {
		syms := groups[dir]
		symStr := strings.Join(syms, " ")
		if len(syms) > 15 {
			symStr = strings.Join(syms[:15], " ") + " ..."
		}
		fmt.Fprintf(&b, "%s/ — %s\n", dir, symStr)
	}

	result := b.String()
	lines := strings.Split(result, "\n")
	if len(lines) > maxCodeMapLines {
		result = strings.Join(lines[:maxCodeMapLines], "\n") + "\n"
	}
	return strings.TrimSpace(result)
}

// baselineDiff 從 baseline.json 讀取起始 commit，用 git diff 算出 coder 歷來的變更。
// 超過 maxDiffLines 行時截斷並附註。失敗回空字串（靜默降級）。
func baselineDiff(ws *protocol.Workspace, featureID string) string {
	blPath := filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile)
	blData, err := os.ReadFile(blPath)
	if err != nil {
		return ""
	}
	var bl protocol.Baseline
	if err := json.Unmarshal(blData, &bl); err != nil || len(bl.Repos) == 0 {
		return ""
	}
	head := bl.Repos[0].Head
	if head == "" {
		return ""
	}

	diff, err := exec.Command("git", "-C", ws.Root, "diff", head+"..HEAD",
		"--stat", "--patch", "--", ".", ":(exclude).4x").CombinedOutput()
	if err != nil || len(diff) == 0 {
		return ""
	}

	lines := strings.Split(string(diff), "\n")
	if len(lines) <= maxDiffLines {
		return string(diff)
	}
	return strings.Join(lines[:maxDiffLines], "\n") +
		fmt.Sprintf("\n\n... (truncated, showing %d of %d lines) ...\n", maxDiffLines, len(lines))
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
