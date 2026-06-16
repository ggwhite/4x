package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Version 是目前 CLI binary 的版本號，由 main 在啟動時透過 ldflags 注入。
var Version = "dev"

// versionCache 快取 GitHub latest release 查詢結果，避免頻繁打 API。
var versionCache struct {
	mu       sync.Mutex
	fetched  time.Time
	latest   string
	url      string
	hasData  bool
	cacheTTL time.Duration
}

func init() {
	versionCache.cacheTTL = 1 * time.Hour
}

// githubRelease 僅解析 GitHub releases/latest 回應中需要的欄位。
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// versionResponse 是 /api/version 的 JSON 回應結構。
type versionResponse struct {
	Version         string `json:"version"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable,omitempty"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
}

// handleVersion 回傳目前版本資訊；帶 ?check=true 時另外查詢 GitHub 最新 release。
func handleVersion(w http.ResponseWriter, r *http.Request) {
	normalizedVersion := strings.TrimPrefix(Version, "v")
	resp := versionResponse{Version: normalizedVersion}

	if r.URL.Query().Get("check") == "true" {
		latest, releaseURL, ok := fetchLatestRelease()
		if ok {
			resp.Latest = latest
			resp.ReleaseURL = releaseURL
			resp.UpdateAvailable = latest != normalizedVersion
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// fetchLatestRelease 回傳 GitHub 上最新 release 的版本號（去掉 v 前綴）與 release 頁面 URL。
// 結果快取 1 小時；GitHub API 失敗時靜默回傳 ok=false。
func fetchLatestRelease() (latest string, releaseURL string, ok bool) {
	versionCache.mu.Lock()
	defer versionCache.mu.Unlock()

	if versionCache.hasData && time.Since(versionCache.fetched) < versionCache.cacheTTL {
		return versionCache.latest, versionCache.url, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/ggwhite/4x/releases/latest", nil)
	if err != nil {
		return "", "", false
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", false
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", false
	}

	tag := strings.TrimPrefix(release.TagName, "v")

	versionCache.latest = tag
	versionCache.url = release.HTMLURL
	versionCache.fetched = time.Now()
	versionCache.hasData = true

	return tag, release.HTMLURL, true
}
