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

// TestParseSubtask 涵蓋 name 含冒號的 regression：時間（10:00）、
// group:artifact coordinate、URL 都不可被切進 description。
func TestParseSubtask(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		wantID string
		wantNm string
	}{
		{"simple", "extract-mw:Extract middleware", "extract-mw", "Extract middleware"},
		{"name with time colon", "festival-job:節日禮金排程 Job（每日 10:00 checkFestival，含農曆判斷）", "festival-job", "節日禮金排程 Job（每日 10:00 checkFestival，含農曆判斷）"},
		{"name with coordinate colons", "lib-selection:套件選型與引入（對應 cn.6tail:lunar:1.7 能力）", "lib-selection", "套件選型與引入（對應 cn.6tail:lunar:1.7 能力）"},
		{"name with url", "doc-link:依 https://example.com/spec#a:b 實作", "doc-link", "依 https://example.com/spec#a:b 實作"},
		{"trims spaces", " impl : Do the thing ", "impl", "Do the thing"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, err := parseSubtask(c.in)
			if err != nil {
				t.Fatalf("parseSubtask(%q) error: %v", c.in, err)
			}
			if st.ID != c.wantID || st.Name != c.wantNm {
				t.Errorf("parseSubtask(%q) = (%q, %q), want (%q, %q)", c.in, st.ID, st.Name, c.wantID, c.wantNm)
			}
			if st.Description != "" {
				t.Errorf("parseSubtask(%q) description = %q, want empty", c.in, st.Description)
			}
		})
	}
}

func TestParseSubtask_Invalid(t *testing.T) {
	for _, in := range []string{"no-colon", ":name-only", "id-only:", "  :  ", ""} {
		if _, err := parseSubtask(in); err == nil {
			t.Errorf("parseSubtask(%q) expected error, got nil", in)
		}
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
