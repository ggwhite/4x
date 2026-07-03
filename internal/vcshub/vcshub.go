// Package vcshub 封裝與程式碼託管平台（GitHub/GitLab）互動的 CLI 呼叫（gh/glab），
// 依 git remote hostname 自動判斷平台。與 internal/gitops 的純 git plumbing 職責切開：
// forge API（issue/MR/PR）呼叫全部在此 package。
package vcshub

import (
	"os/exec"
	"regexp"
	"strings"
)

// Hub 是與程式碼託管平台互動的介面，githubHub/glabHub 各自實作對應 CLI 的參數與輸出解析。
type Hub interface {
	// Preflight 檢查對應 CLI 是否已安裝且已登入，用於 `4x new` 動手前快速失敗。
	Preflight(repoPath string) error
	// CreateIssue 在 repoPath 對應的 repo 建立新 issue，回傳規範化的 id 與 url。
	CreateIssue(repoPath, title, body string) (id, url string, err error)
	// GetIssue 驗證 ref（純數字 ID 或完整 issue URL）對應的既有 issue 存在，回傳規範化的 id 與 url。
	GetIssue(repoPath, ref string) (id, url string, err error)
	// OpenMR 對 repoPath 開一個從 sourceBranch 到 targetBranch 的 MR/PR，回傳其 URL。
	// 對已存在開啟中 MR 的 branch 重複呼叫是 idempotent 的：回傳既有 MR 的 URL 而非 error。
	OpenMR(repoPath, sourceBranch, targetBranch, title, body string) (url string, err error)
}

// execCommand 執行外部 CLI 並回傳合併的 stdout/stderr；測試可覆寫以注入假輸出。
var execCommand = func(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// lookPath 對應 exec.LookPath，測試可覆寫以模擬 CLI 未安裝。
var lookPath = exec.LookPath

// New 依 repoPath 的 git remote origin hostname 判斷回傳對應平台的 Hub 實作。
// hostname 含 "github.com" 回 githubHub；其餘（含自架 GitLab、無 remote）回 glabHub。
func New(repoPath string) Hub {
	out, err := execCommand(repoPath, "git", "remote", "get-url", "origin")
	if err == nil && strings.Contains(string(out), "github.com") {
		return &githubHub{}
	}
	return &glabHub{}
}

// issueIDFromURL 規範化 issue 參照：純數字 ID 原樣回傳；完整 issue URL 取路徑末段數字。
func issueIDFromURL(ref string) string {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		return ref
	}
	trimmed := strings.TrimRight(ref, "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx == -1 {
		return ref
	}
	return trimmed[idx+1:]
}

var urlPattern = regexp.MustCompile(`https?://\S+`)

// extractURL 從 CLI 輸出中抓出第一個 http(s) URL，並去除尾端標點符號（如句尾句號、括號）。
func extractURL(s string) string {
	match := urlPattern.FindString(s)
	if match == "" {
		return ""
	}
	return strings.TrimRight(match, ".,;:!?)]}\"'")
}
