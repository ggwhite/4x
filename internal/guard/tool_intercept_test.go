package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AC-7：ToolHookInput 能正確 unmarshal Edit/Write/MultiEdit 帶的 file_path。
func TestToolHookInput_UnmarshalFilePath(t *testing.T) {
	raw := `{"tool_name":"Edit","tool_input":{"file_path":"cmd/4x/foo.go"}}`
	var in ToolHookInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if in.ToolName != "Edit" {
		t.Errorf("tool_name = %q, want Edit", in.ToolName)
	}
	if in.ToolInput.FilePath != "cmd/4x/foo.go" {
		t.Errorf("file_path = %q, want cmd/4x/foo.go", in.ToolInput.FilePath)
	}

	// command 與 file_path 可並存互不干擾。
	raw2 := `{"tool_name":"Bash","tool_input":{"command":"git diff"}}`
	var in2 ToolHookInput
	if err := json.Unmarshal([]byte(raw2), &in2); err != nil {
		t.Fatalf("unmarshal bash: %v", err)
	}
	if in2.ToolInput.Command != "git diff" || in2.ToolInput.FilePath != "" {
		t.Errorf("bash input parsed wrong: cmd=%q file_path=%q", in2.ToolInput.Command, in2.ToolInput.FilePath)
	}
}

// mkInput 建構 Bash 工具的 ToolHookInput。
func mkInput(command string) ToolHookInput {
	in := ToolHookInput{ToolName: "Bash"}
	in.ToolInput.Command = command
	return in
}

func TestEvaluateReviewerToolIntercept(t *testing.T) {
	// 準備一個實際存在的 review-package 檔。
	dir := t.TempDir()
	pkg := filepath.Join(dir, "review-package.md")
	if err := os.WriteFile(pkg, []byte("pkg"), 0o644); err != nil {
		t.Fatalf("write pkg: %v", err)
	}

	tests := []struct {
		name     string
		in       ToolHookInput
		role     string
		pkg      string
		wantDeny bool
	}{
		// AC-5：reviewer/deep-reviewer + git diff/log/show + package 存在 → deny。
		{"reviewer git diff", mkInput("git diff HEAD"), "reviewer", pkg, true},
		{"reviewer git log", mkInput("git log --oneline"), "reviewer", pkg, true},
		{"reviewer git show", mkInput("git show HEAD"), "reviewer", pkg, true},
		{"deep-reviewer git diff", mkInput("git diff"), "deep-reviewer", pkg, true},
		{"leading whitespace", mkInput("   git diff"), "reviewer", pkg, true},
		{"env prefix", mkInput("env GIT_PAGER=cat git diff"), "reviewer", pkg, true},
		{"env var prefix", mkInput("FOO=1 git log"), "reviewer", pkg, true},
		{"multi space", mkInput("git   diff   HEAD"), "reviewer", pkg, true},

		// AC-6：不影響 build/test/lint。
		{"make build", mkInput("make build"), "reviewer", pkg, false},
		{"make test", mkInput("make test"), "reviewer", pkg, false},
		{"make lint", mkInput("make lint"), "reviewer", pkg, false},
		{"go test", mkInput("go test ./..."), "reviewer", pkg, false},
		{"git status", mkInput("git status"), "reviewer", pkg, false},
		{"git commit", mkInput("git commit -m x"), "reviewer", pkg, false},
		{"git add", mkInput("git add ."), "reviewer", pkg, false},

		// AC-7：只針對 reviewer/deep-reviewer。
		{"coder git diff", mkInput("git diff"), "coder", pkg, false},
		{"designer git diff", mkInput("git diff"), "designer", pkg, false},
		{"tester git diff", mkInput("git diff"), "tester", pkg, false},
		{"fixer git diff", mkInput("git diff"), "fixer", pkg, false},
		{"empty role git diff", mkInput("git diff"), "", pkg, false},

		// AC-8：保留 fallback（package 空或不存在）。
		{"reviewer empty pkg", mkInput("git diff"), "reviewer", "", false},
		{"reviewer missing pkg", mkInput("git diff"), "reviewer", filepath.Join(dir, "nope.md"), false},

		// 非 Bash 工具一律放行。
		{"read tool", ToolHookInput{ToolName: "Read"}, "reviewer", pkg, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deny, reason := EvaluateReviewerToolIntercept(tt.in, tt.role, tt.pkg)
			if deny != tt.wantDeny {
				t.Errorf("EvaluateReviewerToolIntercept() deny = %v, want %v", deny, tt.wantDeny)
			}
			if deny && reason == "" {
				t.Error("deny=true should carry a non-empty reason")
			}
			if deny && !strings.Contains(reason, tt.pkg) {
				t.Errorf("reason should point to review-package path %q, got %q", tt.pkg, reason)
			}
		})
	}
}
