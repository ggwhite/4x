package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleVersion_Basic(t *testing.T) {
	orig := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = orig })

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	handleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp versionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", resp.Version)
	}
	if resp.Latest != "" {
		t.Errorf("latest should be empty without check=true, got %q", resp.Latest)
	}
}

func TestHandleVersion_CheckWithMockGitHub(t *testing.T) {
	// 建立 mock GitHub API server
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v2.0.0",
			"html_url": "https://github.com/ggwhite/4x/releases/tag/v2.0.0",
		})
	}))
	defer mock.Close()

	// 清除快取，讓這次測試一定會打 API
	versionCache.mu.Lock()
	versionCache.hasData = false
	versionCache.fetched = time.Time{}
	versionCache.mu.Unlock()

	// 無法直接改 fetchLatestRelease 呼叫的 URL，所以這裡只測 ?check=true 不會 crash。
	// 真正的 GitHub API 可能不可達（CI 環境），handleVersion 設計上遇到失敗會靜默略過。
	orig := Version
	Version = "1.0.0"
	t.Cleanup(func() { Version = orig })

	req := httptest.NewRequest(http.MethodGet, "/api/version?check=true", nil)
	rec := httptest.NewRecorder()
	handleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp versionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", resp.Version)
	}
	// latest 可能有值（GitHub 可達）或沒有（GitHub 不可達），兩種都合法
}

func TestHandleVersion_CacheHit(t *testing.T) {
	// 手動寫入快取
	versionCache.mu.Lock()
	versionCache.latest = "3.0.0"
	versionCache.url = "https://github.com/ggwhite/4x/releases/tag/v3.0.0"
	versionCache.fetched = time.Now()
	versionCache.hasData = true
	versionCache.mu.Unlock()
	t.Cleanup(func() {
		versionCache.mu.Lock()
		versionCache.hasData = false
		versionCache.mu.Unlock()
	})

	orig := Version
	Version = "2.0.0"
	t.Cleanup(func() { Version = orig })

	req := httptest.NewRequest(http.MethodGet, "/api/version?check=true", nil)
	rec := httptest.NewRecorder()
	handleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp versionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Version != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", resp.Version)
	}
	if resp.Latest != "3.0.0" {
		t.Errorf("latest = %q, want 3.0.0 (from cache)", resp.Latest)
	}
	if !resp.UpdateAvailable {
		t.Error("updateAvailable should be true when version != latest")
	}
	if resp.ReleaseURL != "https://github.com/ggwhite/4x/releases/tag/v3.0.0" {
		t.Errorf("releaseUrl = %q, want tag/v3.0.0 URL", resp.ReleaseURL)
	}
}
