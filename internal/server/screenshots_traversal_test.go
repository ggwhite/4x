package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestServeScreenshot_PathTraversalRejected 以表格驅動的惡意路徑樣本集，驗證
// handleServeScreenshot（經 NewMux 路由）對 path traversal 一律 fail-closed：
// 回應 HTTP status != 200，且不洩漏 workspace root 外的檔案內容。
//
// 樣本以確定性方式構造逃脫路徑（如 filepath.Rel 產生帶 .. 的相對路徑），
// 不寫死 ../../../../，以免因 root 深度變動而失真。
func TestServeScreenshot_PathTraversalRejected(t *testing.T) {
	ws := setupServerWorkspace(t)

	// 於 workspace root 外建立含秘密標記的真實影像檔，供樣本 (a) 嘗試讀取。
	outside := t.TempDir()
	secret := filepath.Join(outside, "leak.png")
	if err := os.WriteFile(secret, []byte("SECRET-OUTSIDE-ROOT"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 以 filepath.Rel 得到由 root 指向 root 外秘密檔、帶 .. 分量的相對路徑。
	relSecret, err := filepath.Rel(ws.Root, secret)
	if err != nil {
		t.Fatal(err)
	}
	relSecret = filepath.ToSlash(relSecret)
	if !strings.HasPrefix(relSecret, "..") {
		t.Fatalf("expected relative escape path to start with .., got %q", relSecret)
	}

	cases := []struct {
		name string
		// rawPath 為 base64 編碼前的原始路徑（token 內容）。
		rawPath string
		// wantSecretAbsent 為 true 時，額外斷言回應 body 不含 root 外秘密標記。
		wantSecretAbsent bool
	}{
		{
			name:             "relative-dotdot-escape-to-real-image",
			rawPath:          relSecret,
			wantSecretAbsent: true,
		},
		{
			name:    "absolute-path-image",
			rawPath: "/tmp/4x-does-not-exist.png",
		},
		{
			name:    "non-image-ext-dotdot-traversal",
			rawPath: "../../../../etc/passwd",
		},
		{
			name:    "dotdot-traversal-to-nonexistent-image",
			rawPath: "../../nonexistent.png",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := encodeScreenshotToken(tc.rawPath)
			if token == "" {
				t.Fatalf("failed to encode screenshot token for %q", tc.rawPath)
			}
			rec := serveRequest(t, NewMux(singleResolver(protocol.NewCachedWorkspace(ws), nil)), http.MethodGet, "/api/features/test-feat/screenshots/"+token, "")
			if rec.Code == http.StatusOK {
				t.Fatalf("status = 200 for traversal path %q, want non-200", tc.rawPath)
			}
			if tc.wantSecretAbsent && strings.Contains(rec.Body.String(), "SECRET-OUTSIDE-ROOT") {
				t.Fatalf("response leaked out-of-root file content for %q", tc.rawPath)
			}
		})
	}
}
