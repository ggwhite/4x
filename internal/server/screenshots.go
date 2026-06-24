package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

type screenshotItem struct {
	Path        string `json:"path"`
	Step        string `json:"step"`
	Description string `json:"description"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
}

type screenshotGroupResponse struct {
	Round       int              `json:"round"`
	Screenshots []screenshotItem `json:"screenshots"`
}

type screenshotsResponse struct {
	Groups []screenshotGroupResponse `json:"groups"`
	Total  int                       `json:"total"`
}

func handleFeatureScreenshots(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/features/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "screenshots" {
		http.NotFound(w, r)
		return
	}
	featureID := parts[0]
	if strings.ContainsAny(featureID, "/\\") || strings.Contains(featureID, "..") {
		http.Error(w, "invalid feature id", http.StatusBadRequest)
		return
	}
	if len(parts) == 2 || (len(parts) == 3 && parts[2] == "") {
		handleGetScreenshots(ws, featureID, w)
		return
	}
	if len(parts) == 3 {
		handleServeScreenshot(ws, featureID, parts[2], w, r)
		return
	}
	http.NotFound(w, r)
}

// getMergedScreenshotDir 讀取 project config 並合併 user config，回傳 screenshotDir。
func getMergedScreenshotDir(ws *protocol.CachedWorkspace) string {
	cfg, err := ws.LoadMergedConfig()
	if err != nil {
		return feature.DefaultScreenshotDir
	}
	return protocol.ScreenshotDir(cfg)
}

func handleGetScreenshots(ws *protocol.CachedWorkspace, featureID string, w http.ResponseWriter) {
	screenshotDir := getMergedScreenshotDir(ws)
	groups, err := ws.DiscoverScreenshots(featureID, screenshotDir)
	if err != nil {
		http.Error(w, "discover screenshots: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := screenshotsResponse{Groups: make([]screenshotGroupResponse, 0, len(groups))}
	for _, group := range groups {
		items := make([]screenshotItem, 0, len(group.Screenshots))
		for _, shot := range group.Screenshots {
			filename := filepath.Base(shot.Path)
			urlToken := encodeScreenshotToken(shot.Path)
			if urlToken == "" {
				urlToken = filename
			}
			items = append(items, screenshotItem{
				Path:        shot.Path,
				Step:        shot.Step,
				Description: shot.Description,
				Filename:    filename,
				URL:         "/api/features/" + featureID + "/screenshots/" + url.PathEscape(urlToken),
			})
		}
		resp.Groups = append(resp.Groups, screenshotGroupResponse{
			Round:       group.Round,
			Screenshots: items,
		})
		resp.Total += len(items)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleServeScreenshot(ws *protocol.CachedWorkspace, featureID, filename string, w http.ResponseWriter, r *http.Request) {
	token, err := url.PathUnescape(filename)
	if err != nil {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	// base64 token 已含完整路徑，直接解碼，不需 re-discover。
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		http.Error(w, "invalid screenshot token", http.StatusBadRequest)
		return
	}
	relPath := feature.NormalizeScreenshotPath(string(data))
	if relPath == "" || !feature.IsScreenshotFile(filepath.Base(relPath)) {
		http.Error(w, "unsupported screenshot type", http.StatusBadRequest)
		return
	}

	// 先嘗試以 workspace root 為基準解析路徑（支援 .4x/ 以外的截圖目錄）；
	// 若不存在則 fallback 到 .4x/（NormalizeScreenshotPath 已去除 .4x/ 前綴）。
	abs := filepath.Join(ws.Root, filepath.FromSlash(relPath))
	abs, err = filepath.Abs(abs)
	if err != nil {
		http.Error(w, "invalid screenshot path", http.StatusInternalServerError)
		return
	}
	rootAbs, err := filepath.Abs(ws.Root)
	if err != nil {
		http.Error(w, "invalid workspace path", http.StatusInternalServerError)
		return
	}

	if _, statErr := os.Stat(abs); statErr != nil {
		dotAbs := filepath.Join(rootAbs, protocol.DirName)
		abs2 := filepath.Join(dotAbs, filepath.FromSlash(relPath))
		abs2, err = filepath.Abs(abs2)
		if err == nil {
			abs = abs2
		}
	}
	if _, statErr := os.Stat(abs); statErr != nil {
		wtAbs := filepath.Join(gitops.Dir(rootAbs, featureID), protocol.DirName, filepath.FromSlash(relPath))
		if a, e := filepath.Abs(wtAbs); e == nil {
			abs = a
		}
	}

	// 安全檢查：解析 symlink 後確認路徑仍在 workspace root 內。
	resolvedAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		http.Error(w, "invalid workspace path", http.StatusInternalServerError)
		return
	}
	if resolvedAbs != resolvedRoot && !strings.HasPrefix(resolvedAbs, resolvedRoot+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", screenshotContentType(filepath.Base(relPath)))
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	http.ServeFile(w, r, resolvedAbs)
}

func encodeScreenshotToken(path string) string {
	normalized := feature.NormalizeScreenshotPath(path)
	if normalized == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(normalized))
}

func screenshotContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
