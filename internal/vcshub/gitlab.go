package vcshub

import (
	"fmt"
	"strings"
)

// glabHub 透過 glab CLI 與 GitLab（含自架）互動。
type glabHub struct{}

// Preflight 確認 glab CLI 已安裝且已登入。
func (h *glabHub) Preflight(repoPath string) error {
	if _, err := lookPath("glab"); err != nil {
		return fmt.Errorf("glab CLI not found: %w", err)
	}
	if out, err := execCommand(repoPath, "glab", "auth", "status"); err != nil {
		return fmt.Errorf("glab not authenticated: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// CreateIssue 執行 `glab issue create`，輸出含新建 issue 的 URL。
func (h *glabHub) CreateIssue(repoPath, title, body string) (id, url string, err error) {
	out, err := execCommand(repoPath, "glab", "issue", "create",
		"--title", title, "--description", body, "--yes")
	if err != nil {
		return "", "", fmt.Errorf("glab issue create: %s", strings.TrimSpace(string(out)))
	}
	url = extractURL(string(out))
	if url == "" {
		return "", "", fmt.Errorf("glab issue create: no URL in output: %s", strings.TrimSpace(string(out)))
	}
	return issueIDFromURL(url), url, nil
}

// GetIssue 透過 `glab issue view`（純文字輸出）驗證既有 issue 是否存在，回傳規範化的 id/url。
func (h *glabHub) GetIssue(repoPath, ref string) (id, url string, err error) {
	issueID := issueIDFromURL(ref)
	out, err := execCommand(repoPath, "glab", "issue", "view", issueID)
	if err != nil {
		return "", "", fmt.Errorf("glab issue view %s: %s", issueID, strings.TrimSpace(string(out)))
	}
	url = extractURL(string(out))
	if url == "" {
		return "", "", fmt.Errorf("glab issue view %s: no URL in output", issueID)
	}
	return issueID, url, nil
}

// OpenMR 執行 `glab mr create`；glab 用當前 checkout 的 branch 當 source，不支援 --head，
// 故不使用 sourceBranch 參數（呼叫端須確保呼叫時的工作目錄已在該 branch）。對已有開啟中 MR
// 的 branch 重複呼叫時 glab 回非 0 且輸出含 "already exists"，此時解析並回傳既有 MR URL
// 而非視為錯誤（idempotent，見 AC-9）。
func (h *glabHub) OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error) {
	_ = sourceBranch
	out, err := execCommand(repoPath, "glab", "mr", "create",
		"--target-branch", targetBranch, "--title", title, "--description", body, "--yes")
	if err != nil {
		if strings.Contains(string(out), "already exists") {
			if existing := extractURL(string(out)); existing != "" {
				return existing, nil
			}
		}
		return "", fmt.Errorf("glab mr create: %s", strings.TrimSpace(string(out)))
	}
	url = extractURL(string(out))
	if url == "" {
		return "", fmt.Errorf("glab mr create: no URL in output: %s", strings.TrimSpace(string(out)))
	}
	return url, nil
}
