package vcshub

import (
	"encoding/json"
	"fmt"
	"strings"
)

// githubHub 透過 gh CLI 與 GitHub 互動。
type githubHub struct{}

// Preflight 確認 gh CLI 已安裝且已登入。
func (h *githubHub) Preflight(repoPath string) error {
	if _, err := lookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found: %w", err)
	}
	if out, err := execCommand(repoPath, "gh", "auth", "status"); err != nil {
		return fmt.Errorf("gh not authenticated: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateIssue 執行 `gh issue create`，其輸出即為新建 issue 的 URL。
func (h *githubHub) CreateIssue(repoPath, title, body string) (id, url string, err error) {
	out, err := execCommand(repoPath, "gh", "issue", "create", "--title", title, "--body", body)
	if err != nil {
		return "", "", fmt.Errorf("gh issue create: %s", strings.TrimSpace(string(out)))
	}
	url = extractURL(string(out))
	if url == "" {
		return "", "", fmt.Errorf("gh issue create: no URL in output: %s", strings.TrimSpace(string(out)))
	}
	return issueIDFromURL(url), url, nil
}

// githubIssueView 對應 `gh issue view --json number,url` 的輸出結構。
type githubIssueView struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// GetIssue 透過 `gh issue view` 驗證既有 issue 是否存在，回傳規範化的 id/url。
func (h *githubHub) GetIssue(repoPath, ref string) (id, url string, err error) {
	issueID := issueIDFromURL(ref)
	out, err := execCommand(repoPath, "gh", "issue", "view", issueID, "--json", "number,url")
	if err != nil {
		return "", "", fmt.Errorf("gh issue view %s: %s", issueID, strings.TrimSpace(string(out)))
	}
	var view githubIssueView
	if err := json.Unmarshal(out, &view); err != nil {
		return "", "", fmt.Errorf("gh issue view %s: parse output: %w", issueID, err)
	}
	return fmt.Sprintf("%d", view.Number), view.URL, nil
}

// OpenMR 執行 `gh pr create`；對已有開啟中 PR 的 branch 重複呼叫時 gh 回非 0 且輸出含
// "already exists"，此時解析並回傳既有 PR URL 而非視為錯誤（idempotent，見 AC-9）。
func (h *githubHub) OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error) {
	out, err := execCommand(repoPath, "gh", "pr", "create",
		"--base", targetBranch, "--head", sourceBranch, "--title", title, "--body", body)
	if err != nil {
		if strings.Contains(string(out), "already exists") {
			if existing := extractURL(string(out)); existing != "" {
				return existing, nil
			}
		}
		return "", fmt.Errorf("gh pr create: %s", strings.TrimSpace(string(out)))
	}
	url = extractURL(string(out))
	if url == "" {
		return "", fmt.Errorf("gh pr create: no URL in output: %s", strings.TrimSpace(string(out)))
	}
	return url, nil
}
