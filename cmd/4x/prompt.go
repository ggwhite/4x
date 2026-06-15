package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/ggwhite/4x/templates"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newPromptCmd() *cobra.Command {
	var role string
	var round int

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

			cfg, _ := ws.LoadMergedConfig()
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
				ProjectIncludes:     append(loadIncludes(ws.Root, cfg.Project.Includes), discoverConventionFiles(ws.Root, cfg.Project.Includes)...),
				RoleIncludes:        loadIncludes(ws.Root, roleInc),
				PlanningDoc:         loadPlanningDocs(ws.Root, feature.ID),
				ProfileInstructions: loadProfiles(ws, featureID, cfg),
			}

			tmpl, err := loadRoleTemplate(r)
			if err != nil {
				return err
			}

			return tmpl.Execute(os.Stdout, data)
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "role (designer/coder/reviewer/tester)")
	cmd.Flags().IntVar(&round, "round", 0, "round number")
	return cmd
}

type promptData struct {
	Feature          protocol.Feature
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

// designDocPath 嘗試找 docs/design/{featureID}{suffix}，
// 找不到時 fallback 去掉 FNNN- prefix 再找一次
func designDocPath(root, featureID, suffix string) string {
	p := filepath.Join(root, "docs", "design", featureID+suffix)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	slug := stripFeaturePrefix(featureID)
	if slug != featureID {
		p = filepath.Join(root, "docs", "design", slug+suffix)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// stripFeaturePrefix 移除 FNNN- prefix（如 F022-multi-project → multi-project）
func stripFeaturePrefix(id string) string {
	if len(id) > 5 && id[0] == 'F' && id[4] == '-' {
		return id[5:]
	}
	return id
}

// loadPlanningDocs 嘗試讀取 docs/design/{featureID}-spec.md 和 -plan.md
// 檔案不存在時跳過，不報錯
func loadPlanningDocs(root, featureID string) string {
	var parts []string
	for _, suffix := range []string{"-spec.md", "-plan.md"} {
		p := designDocPath(root, featureID, suffix)
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		parts = append(parts, string(data))
	}
	return strings.Join(parts, "\n\n---\n\n")
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

// discoverConventionFiles 探測專案根目錄下的 agent 慣例檔案（CLAUDE.md、AGENTS.md 等），
// 排除已在 explicit includes 中列出的檔案，避免重複注入
func discoverConventionFiles(root string, explicitIncludes []string) []includeContent {
	conventionFiles := []string{
		"CLAUDE.md",
		"AGENTS.md",
		"GEMINI.md",
		"COPILOT.md",
		"CURSORRULES",
		".cursorrules",
	}

	explicit := make(map[string]bool, len(explicitIncludes))
	for _, p := range explicitIncludes {
		explicit[p] = true
	}

	var result []includeContent
	for _, name := range conventionFiles {
		if explicit[name] {
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
	protocol.RoleDesigner:     "designer.md.tmpl",
	protocol.RoleCoder:        "coder.md.tmpl",
	protocol.RoleReviewer:     "reviewer.md.tmpl",
	protocol.RoleDeepReviewer: "deep-reviewer.md.tmpl",
	protocol.RoleTester:       "tester.md.tmpl",
	protocol.RoleAcceptor:     "acceptor.md.tmpl",
	protocol.RoleMiniCoder:    "mini-coder.md.tmpl",
	protocol.RoleReVerifier:   "re-verifier.md.tmpl",
	protocol.RoleSynthesizer:  "synthesizer.md.tmpl",
}

func loadRoleTemplate(r protocol.Role) (*template.Template, error) {
	filename, ok := roleTemplateFiles[r]
	if !ok {
		return nil, fmt.Errorf("unknown role: %s", r)
	}

	locale, err := templates.FS.ReadFile("locale.tmpl")
	if err != nil {
		return nil, fmt.Errorf("read locale template: %w", err)
	}

	role, err := templates.FS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read role template %s: %w", filename, err)
	}

	return template.New(string(r)).Funcs(tmplFuncs).Parse(string(locale) + string(role))
}
