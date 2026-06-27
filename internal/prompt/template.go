package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/templates"
)

var tmplFuncs = template.FuncMap{
	"sub": func(a, b int) int { return a - b },
	"seq": func(n int) []int {
		out := make([]int, 0, n)
		for i := 1; i <= n; i++ {
			out = append(out, i)
		}
		return out
	},
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
	protocol.RoleConsolidator:   "consolidate.md.tmpl",
}

// LoadRoleTemplate 載入指定 role 的 prompt template。
// 查找順序：先查專案目錄 .4x/templates/{filename}，找不到才 fallback
// 到 go:embed 內建 template。dotDir 為空字串時等同只用內建 template。
func LoadRoleTemplate(dotDir string, r protocol.Role) (*template.Template, error) {
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
