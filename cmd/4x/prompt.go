package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/state"
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
	"en":      "English",
	"zh-Hant": "繁體中文",
	"zh-Hans": "简体中文",
	"ja":      "日本語",
	"ko":      "한국어",
	"es":      "Español",
	"fr":      "Français",
	"de":      "Deutsch",
	"pt":      "Português",
	"vi":      "Tiếng Việt",
	"th":      "ภาษาไทย",
}

func resolveLocale() (code, name string) {
	ucfg, _ := protocol.ReadUserConfig()
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

const localeDirective = `{{- if and .Locale (ne .Locale "en")}}
== Language ==
You MUST write ALL output (reports, summaries, descriptions, comments) in {{.LocaleName}}.
Technical terms, file paths, and command names stay in their original form.

{{end -}}`

var tmplFuncs = template.FuncMap{
	"sub": func(a, b int) int { return a - b },
}

func loadRoleTemplate(r protocol.Role) (*template.Template, error) {
	templates := map[protocol.Role]string{
		protocol.RoleDesigner: designerTemplate,
		protocol.RoleCoder:    coderTemplate,
		protocol.RoleReviewer: reviewerTemplate,
		protocol.RoleTester:   testerTemplate,
	}

	tmplStr, ok := templates[r]
	if !ok {
		return nil, fmt.Errorf("unknown role: %s", r)
	}

	return template.New(string(r)).Funcs(tmplFuncs).Parse(localeDirective + tmplStr)
}

const designerTemplate = `You are the Designer for feature "{{.Feature.Name}}" ({{.Feature.ID}}).

== MANDATORY — write these files or the task fails ==
You MUST create these 3 files before finishing. The system checks they exist.

1. {{.DotDir}}/{{.Feature.ID}}/task-brief.md
2. {{.DotDir}}/{{.Feature.ID}}/acceptance-criteria.md
3. {{.DotDir}}/{{.Feature.ID}}/test-strategy.yaml

Use the Write tool to create each file. Do NOT just print the content — write to disk.

== Feature ==
ID: {{.Feature.ID}}
Name: {{.Feature.Name}}
Description: {{.Feature.Description}}
{{- if .Feature.Repos}}
Repos:
{{- range $repo, $desc := .Feature.Repos}}
  - {{$repo}}{{if $desc}}: {{$desc}}{{end}}
{{- end}}
{{- end}}
{{- if .Feature.Subtasks}}
Subtasks:
{{- range .Feature.Subtasks}}
  - {{.ID}}: {{.Name}}{{if .Description}} — {{.Description}}{{end}}
{{- end}}
{{- end}}
{{- if .PlanningDoc}}

== Planning Document (pre-design analysis — follow this closely) ==
{{.PlanningDoc}}
{{- end}}
{{- if .Project.Docs}}

== Project Documentation (read these for context) ==
{{- range .Project.Docs}}
- {{.}}
{{- end}}
{{- end}}
{{- if .ProjectIncludes}}
{{range .ProjectIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}
{{- if .Project.Rules}}

== Project Rules ==
{{- range .Project.Rules}}
- {{.}}
{{- end}}
{{- end}}
{{- if .Feature.Rules}}

== Feature Rules ==
{{- range .Feature.Rules}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleInstructions}}

== Role Instructions ==
{{- range .RoleInstructions}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleIncludes}}
{{range .RoleIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}
{{- if .Project.Test}}

== Existing Test Commands (use in test-strategy.yaml verify_commands) ==
{{- range .Project.Test}}
- {{.}}
{{- end}}
{{- end}}

== task-brief.md format ==
# Task Brief — {title}
## Goal
## Tasks (numbered, specific: name files, functions, endpoints)
## Scope (which files/dirs to modify)
## Out of Scope

== acceptance-criteria.md format ==
# Acceptance Criteria
| # | Criterion | Verification Method |
|---|---|---|
| AC-1 | ... | ... |

== test-strategy.yaml format ==
web: false
api: false
coder_only: true
verify_commands:
  - "command here"

== Constraints ==
- You may NOT modify any source code
- Focus on WHAT to build, not HOW to implement
- Be specific: name files, functions, endpoints, schemas
`

const coderTemplate = `You are the Coder for feature "{{.Feature.Name}}" ({{.Feature.ID}}), round {{.Round}}.

== MANDATORY — write this file or the task fails ==
You MUST create this file before finishing. The system checks it exists.

  {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/coder-report.md

Use the Write tool to create the file. Do NOT just print the content — write to disk.

== Inputs ==
Read your task brief: {{.DotDir}}/{{.Feature.ID}}/task-brief.md
{{- if gt .Round 1}}
Read the previous test report: {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{(sub .Round 1)}}/test-report.md
{{- end}}
{{- if .PlanningDoc}}

== Planning Document (design context) ==
{{.PlanningDoc}}
{{- end}}
{{- if .Project.Setup}}

== Dev Environment Setup ==
{{- range .Project.Setup}}
- {{.}}
{{- end}}
{{- end}}
{{- if or .Project.Build .Project.Test .Project.Lint}}

== Verify Commands (run after EVERY change) ==
{{- range .Project.Build}}
- Build: {{.}}
{{- end}}
{{- range .Project.Lint}}
- Lint: {{.}}
{{- end}}
{{- range .Project.Test}}
- Test: {{.}}
{{- end}}
{{- end}}
{{- if .ProjectIncludes}}
{{range .ProjectIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}
{{- if .Project.Rules}}

== Project Rules ==
{{- range .Project.Rules}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleInstructions}}

== Role Instructions ==
{{- range .RoleInstructions}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleIncludes}}
{{range .RoleIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}

== Workflow ==
1. Read task-brief.md
2. Implement changes
3. Run verify commands
4. Write coder-report.md with: changed files, build/test results, summary

== coder-report.md format ==
# Coder Report — Round {{.Round}}
## What Was Done
## Files Changed
- path/to/file — description
## Verification
- command: result

== Constraints ==
- Only modify files in allowed repos
- Run verify commands after every change
- Do NOT modify acceptance criteria or test scripts
`

const reviewerTemplate = `You are the Reviewer for feature "{{.Feature.Name}}" ({{.Feature.ID}}), round {{.Round}}.

== MANDATORY — write this file or the task fails ==
You MUST create this file before finishing:

  {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/review-report.md

Use the Write tool. Do NOT just print the content — write to disk.

== Inputs ==
1. Run: git diff HEAD (or git diff --cached) to see changed files
2. Read: {{.DotDir}}/{{.Feature.ID}}/task-brief.md
3. Read: {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/coder-report.md
{{- if .ProjectIncludes}}
{{range .ProjectIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}
{{- if or .Project.Rules .Feature.Rules}}

== Rules to Check ==
{{- range .Project.Rules}}
- [project] {{.}}
{{- end}}
{{- range .Feature.Rules}}
- [feature] {{.}}
{{- end}}
{{- end}}
{{- if .Project.Docs}}

== Project Documentation ==
{{- range .Project.Docs}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleInstructions}}

== Role Instructions ==
{{- range .RoleInstructions}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleIncludes}}
{{range .RoleIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}

== review-report.md format ==
# Review Report — Round {{.Round}}
## Summary
PASS / FAIL
## Checklist
| Item | Status | Notes |
## Issues
### [SEVERITY] Rule — file/path
Description
## Verdict
PASS / FAIL / CONDITIONAL PASS

== Severity ==
- critical/HIGH: blocks transition, must fix
- warning/MEDIUM: should fix
- low/INFO: log only

== Constraints ==
- You may NOT modify source code
- PASS only when zero critical AND zero warning issues
`

const testerTemplate = `You are the Tester for feature "{{.Feature.Name}}" ({{.Feature.ID}}), round {{.Round}}.

== MANDATORY — write these files or the task fails ==
You MUST create these files before finishing:

  {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/test-report.md
  {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/verify.json

If verify.json passed is true, you MUST also create:

  {{.DotDir}}/{{.Feature.ID}}/final-report.md
  {{.DotDir}}/{{.Feature.ID}}/commit-plan.md

Use the Write tool. Do NOT just print the content — write to disk.

== Inputs ==
1. Read: {{.DotDir}}/{{.Feature.ID}}/acceptance-criteria.md
2. Read: {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/coder-report.md
3. Read: {{.DotDir}}/{{.Feature.ID}}/test-strategy.yaml (for verify_commands)
{{- if .ProjectIncludes}}
{{range .ProjectIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}
{{- if .Project.Test}}

== Project Test Commands ==
{{- range .Project.Test}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleInstructions}}

== Role Instructions ==
{{- range .RoleInstructions}}
- {{.}}
{{- end}}
{{- end}}
{{- if .RoleIncludes}}
{{range .RoleIncludes}}

== Included: {{.Path}} ==
{{.Content}}
{{- end}}
{{- end}}

== Workflow (strict order) ==
1. Read acceptance criteria — list every AC item
2. Run verify_commands from test-strategy.yaml
3. For each AC item, collect evidence (command output, file check, etc.)
4. Write test-report.md
5. Write verify.json with pass/fail and command evidence
6. If verify.json passed is true, write final-report.md and commit-plan.md

== test-report.md format ==
# Test Report — Round {{.Round}}
## Summary
PASS / FAIL — N/N criteria met
## Results
| # | Criterion | Status | Evidence |
|---|---|---|---|
| AC-1 | ... | PASS/FAIL/SKIP | actual output |
## Verdict
PASS / FAIL

== verify.json format ==
{
  "passed": true,
  "round": {{.Round}},
  "role": "tester",
  "commands": [
    {
      "command": "make test",
      "exitCode": 0,
      "durationMs": 1234,
      "summary": "short factual result",
      "startedAt": "RFC3339 timestamp",
      "finishedAt": "RFC3339 timestamp"
    }
  ]
}

== Constraints ==
- Do NOT modify source code — only run tests and report
- Each AC item must have: status + evidence
- SKIP > 30% of items blocks acceptance
- Do NOT fabricate results — mark SKIP if you cannot test
- final-report.md and commit-plan.md are REQUIRED when verify.json passed is true
`
