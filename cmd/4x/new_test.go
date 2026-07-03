package main

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestParseIssueRef 涵蓋 AC-15：--issue 支援純 ID、"repo:id"、URL、"repo:URL" 四型。
func TestParseIssueRef(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantRepo string
		wantRef  string
	}{
		{"plain id", "456", "", "456"},
		{"repo:id", "old-game-server:456", "old-game-server", "456"},
		{"plain url", "https://github.com/acme/widget/issues/42", "", "https://github.com/acme/widget/issues/42"},
		{"repo:url", "old-game-server:https://github.com/acme/widget/issues/42", "old-game-server", "https://github.com/acme/widget/issues/42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, ref := parseIssueRef(c.in)
			if repo != c.wantRepo || ref != c.wantRef {
				t.Errorf("parseIssueRef(%q) = (%q, %q), want (%q, %q)", c.in, repo, ref, c.wantRepo, c.wantRef)
			}
		})
	}
}

func TestRepoPath_Monorepo(t *testing.T) {
	cfg := protocol.Config{}
	if got := repoPath("/root", cfg, "."); got != "/root" {
		t.Errorf("repoPath monorepo = %q, want /root", got)
	}
}

func TestRepoPath_MultiRepo(t *testing.T) {
	cfg := protocol.Config{
		Workspace: protocol.WorkspaceConfig{
			Repos: map[string]protocol.RepoConfig{
				"core": {Path: "core"},
			},
		},
	}
	if got := repoPath("/root", cfg, "core"); got != "/root/core" {
		t.Errorf("repoPath multirepo = %q, want /root/core", got)
	}
}
