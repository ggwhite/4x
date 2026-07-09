package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("token length = %d/%d, want 64", len(a), len(b))
	}
	if a == "" || b == "" {
		t.Fatal("token is empty")
	}
	if a == b {
		t.Fatal("two tokens are equal, want distinct")
	}
}

func TestWriteLiveToken(t *testing.T) {
	t.Run("new file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		const tok = "abc123"
		if err := WriteLiveToken(tok); err != nil {
			t.Fatalf("WriteLiveToken: %v", err)
		}
		path, err := LiveTokenPath()
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 600", perm)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := trimNL(string(data)); got != tok {
			t.Fatalf("content = %q, want %q", got, tok)
		}
	})

	t.Run("existing wide-permission file narrowed to 0600", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, protocol.UserConfigDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, LiveTokenFile)
		if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		const tok = "newtoken"
		if err := WriteLiveToken(tok); err != nil {
			t.Fatalf("WriteLiveToken: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 600 (should be narrowed)", perm)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := trimNL(string(data)); got != tok {
			t.Fatalf("content = %q, want %q", got, tok)
		}
	})
}

func TestWriteLiveTokenError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// 在 ~/.4x 位置放一個「檔案」而非目錄，使 MkdirAll 失敗。
	if err := os.WriteFile(filepath.Join(home, protocol.UserConfigDir), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteLiveToken("tok"); err == nil {
		t.Fatal("WriteLiveToken should return error when parent path is a file")
	}
}

func TestLiveURL(t *testing.T) {
	if got := LiveURL(4567, ""); got != "http://localhost:4567" {
		t.Fatalf("LiveURL empty token = %q", got)
	}
	if got := LiveURL(4567, "tok"); got != "http://localhost:4567/?token=tok" {
		t.Fatalf("LiveURL with token = %q", got)
	}
}

func TestAuthEnabled(t *testing.T) {
	tru, fal := true, false
	cases := []struct {
		name string
		in   *bool
		want bool
	}{
		{"nil defaults to enabled", nil, true},
		{"explicit false", &fal, false},
		{"explicit true", &tru, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AuthEnabled(protocol.UserConfig{DashboardAuth: c.in}); got != c.want {
				t.Fatalf("AuthEnabled = %v, want %v", got, c.want)
			}
		})
	}
}

// newTestMultiMux 建立一個帶單一真實專案的 NewMultiMux，供 middleware 測試打 /api/tasks。
// 單一專案時 multiResolver 走 compat 路徑回傳唯一專案，故無需 project prefix。
func newTestMultiMux(t *testing.T) http.Handler {
	t.Helper()
	reg := NewProjectRegistry()
	reg.Add(setupMultiWorkspace(t, "auth-test"))
	recentPath := filepath.Join(t.TempDir(), "recent.json")
	return NewMultiMux(reg, recentPath)
}

func TestAuthMiddleware(t *testing.T) {
	const token = "secret-token"
	h := authMiddleware(token, newTestMultiMux(t))

	// (a) 無 token → 401
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("401 body should be {\"error\":...}, got %q (err=%v)", rec.Body.String(), err)
	}

	// (b) 正確 Bearer → 非 401，且回應形狀不變（200 + JSON array）
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("correct bearer: got 401, want pass through")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("correct bearer: status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var arr []any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("body should be JSON array: %v (body=%q)", err, rec.Body.String())
	}

	// (c) 錯誤 token → 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", rec.Code)
	}

	// (d) 正確 ?token= query → 非 401
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks?token="+token, nil))
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("correct query token: got 401, want pass through")
	}
}

func TestAuthMiddlewarePublicPaths(t *testing.T) {
	h := authMiddleware("secret", newTestMultiMux(t))
	paths := []string{"/", "/api/version", "/api/locales", "/api/locales/en"}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Fatalf("public path %q: got 401, want reachable without token", p)
		}
	}
}

func TestServeMultiAuthDisabled(t *testing.T) {
	// 不傳 WithAuth（或 token 空）時不安裝 middleware，/api/tasks 無 token 亦可達。
	h := NewMultiMux(NewProjectRegistry(), filepath.Join(t.TempDir(), "recent.json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("auth disabled: got 401, want reachable")
	}
	// 直接驗證 WithAuth("") 不安裝 middleware 的邏輯等價
	var o serveOptions
	WithAuth("")(&o)
	if o.authToken != "" {
		t.Fatalf("WithAuth(\"\") should leave authToken empty")
	}
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
