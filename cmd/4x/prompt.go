package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
	"github.com/ggwhite/4x/templates"
	"github.com/spf13/cobra"
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

			cfg, _ := ws.ReadConfig()
			if userCfg, err := protocol.ReadUserConfig(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
			} else {
				cfg = protocol.MergeConfig(userCfg, cfg)
			}
			locale, localeName := resolveLocale()

			var roleInc []string
			if rc, ok := cfg.Roles[string(r)]; ok {
				roleInc = rc.Includes
			}

			data := promptData{
				Feature:          feature,
				Project:          cfg.Project,
				Role:             r,
				Round:            round,
				Config:           cfg,
				DotDir:           ws.DotDir(),
				Locale:           locale,
				LocaleName:       localeName,
				RoleInstructions: roleInstructions(cfg, r),
				ProjectIncludes:  loadIncludes(ws.Root, cfg.Project.Includes),
				RoleIncludes:     loadIncludes(ws.Root, roleInc),
				PlanningDoc:      loadPlanningDocs(ws.Root, feature.ID),
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
	Config           protocol.Config
	DotDir           string
	Locale           string
	LocaleName       string
	RoleInstructions []string
	ProjectIncludes  []includeContent
	RoleIncludes     []includeContent
	PlanningDoc      string
}

type includeContent struct {
	Path    string
	Content string
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
			fmt.Fprintf(os.Stderr, "warning: include %s: %v\n", p, err)
			continue
		}
		result = append(result, includeContent{Path: p, Content: string(data)})
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
		fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
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
}

var roleTemplateFiles = map[protocol.Role]string{
	protocol.RoleDesigner:     "designer.md.tmpl",
	protocol.RoleCoder:        "coder.md.tmpl",
	protocol.RoleReviewer:     "reviewer.md.tmpl",
	protocol.RoleDeepReviewer: "deep-reviewer.md.tmpl",
	protocol.RoleTester:       "tester.md.tmpl",
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
