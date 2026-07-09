package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// LiveTokenFile 是 4x live session token 檔的檔名，放在 ~/.4x/ 下。
const LiveTokenFile = "live-token"

// LiveTokenPath 回傳 ~/.4x/live-token 的絕對路徑。
// 重用 protocol.UserConfigDir，與 UserConfig（~/.4x/settings.json）同目錄。
func LiveTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, protocol.UserConfigDir, LiveTokenFile), nil
}

// GenerateToken 用 crypto/rand 讀取 32 bytes 亂數，以 hex 編碼回傳長度 64 的字串。
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// WriteLiveToken 將 token 寫入 ~/.4x/live-token，並保證最終檔案權限為 0600，
// 即使目標檔已存在且為寬權限（如 0644）。
//
// Unix 上 os.WriteFile 的 perm 只在 create 時生效，覆寫既有檔會保留其原有寬權限，
// 故採 atomic temp file + rename：先在同目錄建立 0600 的 temp 檔寫入 token，再以
// os.Rename 原子替換目標。rename 用新 inode（0600）取代舊 inode，因此既有寬權限檔
// 的最終權限必為 0600，且 token 內容全程只存在 0600 的 temp 檔，無可讀空窗。
func WriteLiveToken(token string) error {
	path, err := LiveTokenPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "live-token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // rename 成功後為 no-op；失敗則清掉殘檔
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil { // 保險（CreateTemp 已 0600），對抗特殊 umask/平台差異
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// LiveURL 依 token 是否為空構造顯示 URL。token 為空回 http://localhost:PORT，
// 非空回 http://localhost:PORT/?token=TOKEN（瀏覽器 Jupyter 模式，前端從 URL 讀 token）。
func LiveURL(port int, token string) string {
	if token == "" {
		return fmt.Sprintf("http://localhost:%d", port)
	}
	return fmt.Sprintf("http://localhost:%d/?token=%s", port, token)
}

// AuthEnabled 為 dashboard bearer-token 認證的三態 resolver：
// cfg.DashboardAuth == nil → true（secure by default）；否則取 *cfg.DashboardAuth 的值。
func AuthEnabled(cfg protocol.UserConfig) bool {
	if cfg.DashboardAuth == nil {
		return true
	}
	return *cfg.DashboardAuth
}

// serveOptions 承載 ServeMulti 的可選參數。
type serveOptions struct {
	authToken string
}

// ServeOption 是 ServeMulti 的 functional option。
type ServeOption func(*serveOptions)

// WithAuth 讓 ServeMulti 以 token 安裝 bearer-token 認證 middleware。
// token 為空字串時不安裝 middleware（對應 auth 停用情境）。
func WithAuth(token string) ServeOption {
	return func(o *serveOptions) {
		o.authToken = token
	}
}

// wrapAuth 解析 opts，authToken 非空時將 next 包上 authMiddleware，否則原樣回傳。
// 各 server 建構層（NewMultiMux、Start）共用此 helper，確保 opts 語意一致。
func wrapAuth(next http.Handler, opts ...ServeOption) http.Handler {
	var o serveOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.authToken == "" {
		return next
	}
	return authMiddleware(o.authToken, next)
}

// isPublicPath 判斷路徑是否為免 token 的公開豁免路徑。
// static 靜態資產（path 不以 /api/ 或 /sse/ 開頭）、/api/version（更新檢查）、
// /api/locales 與 /api/locales/…（i18n bootstrap）為公開；其餘一律需 token。
func isPublicPath(p string) bool {
	if !strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/sse/") {
		return true
	}
	if p == "/api/version" {
		return true
	}
	if p == "/api/locales" || strings.HasPrefix(p, "/api/locales/") {
		return true
	}
	return false
}

// authMiddleware 包住 next，對非公開路徑要求正確的 session token。
// token 先取 Authorization: Bearer <t> header，取不到再讀 ?token= query param，
// 以 crypto/subtle.ConstantTimeCompare 常數時間比對；不符回 401 JSON 錯誤。
func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		got := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			got = strings.TrimPrefix(h, "Bearer ")
		}
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
	})
}
