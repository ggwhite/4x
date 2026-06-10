package main

import (
	"fmt"
	"os"
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

			featureID := args[0]
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

			data := promptData{
				Feature: feature,
				Role:    r,
				Round:   round,
				Config:  cfg,
				DotDir:  ws.DotDir(),
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
	Feature protocol.Feature
	Role    protocol.Role
	Round   int
	Config  protocol.Config
	DotDir  string
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

	return template.New(string(r)).Parse(tmplStr)
}

const designerTemplate = `You are the Designer for feature "{{.Feature.Name}}" ({{.Feature.ID}}).

Your job: analyze requirements, produce a spec, define acceptance criteria, and a test strategy.

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
{{- if .Feature.Rules}}

== Project Rules ==
{{- range .Feature.Rules}}
- {{.}}
{{- end}}
{{- end}}

== Outputs (write to {{.DotDir}}/{{.Feature.ID}}/) ==
1. task-brief.md — actionable task list for the Coder
2. acceptance-criteria.md — testable criteria for the Tester
3. test-strategy.yaml — which test types to run (web/api/gate/coder_only)

== Constraints ==
- You may NOT modify any source code
- Focus on WHAT to build, not HOW to implement it
- Be specific: name files, functions, endpoints, schemas
`

const coderTemplate = `You are the Coder for feature "{{.Feature.Name}}" ({{.Feature.ID}}), round {{.Round}}.

Read your task brief: {{.DotDir}}/{{.Feature.ID}}/task-brief.md
{{- if gt .Round 1}}
Read the test report from last round: {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{(sub .Round 1)}}/test-report.md
{{- end}}

== Constraints ==
- Only modify files in allowed repos
- Run verify commands after every change
- Report changed files and build status
- Do NOT modify acceptance criteria or test scripts
`

const reviewerTemplate = `You are the Reviewer for feature "{{.Feature.Name}}" ({{.Feature.ID}}), round {{.Round}}.

Review the Coder's changes against the project rules.

Read:
1. The diff of changed files
2. {{.DotDir}}/{{.Feature.ID}}/task-brief.md
3. {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/coder-report.md
{{- if .Feature.Rules}}

== Project Rules to Check ==
{{- range .Feature.Rules}}
- {{.}}
{{- end}}
{{- end}}

== Severity ==
- critical: violates a project rule, will cause bugs or security issues
- warning: code quality issue, should be fixed
- low: style preference, skip

Write review report to: {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/review-report.md
`

const testerTemplate = `You are the Tester for feature "{{.Feature.Name}}" ({{.Feature.ID}}), round {{.Round}}.

Read:
1. {{.DotDir}}/{{.Feature.ID}}/acceptance-criteria.md
2. {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/coder-report.md
3. {{.DotDir}}/{{.Feature.ID}}/test-strategy.yaml

== Workflow ==
1. Read acceptance criteria
2. Write test scripts FIRST
3. Run tests
4. Compile test report from results

== Constraints ==
- Do NOT modify source code — only report issues
- Each AC item must be pass/fail/skip with evidence
- SKIP > 30%% of total items blocks acceptance

Write test report to: {{.DotDir}}/{{.Feature.ID}}/rounds/round-{{.Round}}/test-report.md
`
