package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	web "github.com/ggwhite/4x/dashboard/web"
	"github.com/ggwhite/4x/internal/protocol"
)

var userConfigMu sync.Mutex

// handleGetSettings 讀取 .4x/settings.json 原始內容並回傳，保留所有欄位（含 Config struct 未定義的）。
func handleGetSettings(ws *protocol.CachedWorkspace, w http.ResponseWriter) {
	slog.Debug("config loaded", "project", filepath.Base(ws.Root))
	settingsPath := filepath.Join(ws.DotDir(), protocol.ConfigFile)
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePutSettings 接受完整的設定 JSON，驗證後備份並原子寫入 .4x/settings.json。
// 全量替換：前端送什麼就寫什麼，不做 merge。
func handlePutSettings(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read error: "+err.Error(), http.StatusBadRequest)
		}
		return
	}

	// 驗證結構相容 protocol.Config
	var cfg protocol.Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.Project.Name == "" {
		http.Error(w, "project.name is required", http.StatusBadRequest)
		return
	}

	// 重新格式化以確保一致的縮排
	var raw json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		http.Error(w, "marshal error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	newData := append(result, '\n')

	settingsLock := settingsMu.get(ws.Root)
	settingsLock.Lock()
	defer settingsLock.Unlock()

	settingsPath := filepath.Join(ws.DotDir(), protocol.ConfigFile)
	oldData, err := os.ReadFile(settingsPath)
	if err != nil {
		http.Error(w, "cannot read current settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 內容沒變就不寫
	if bytes.Equal(oldData, newData) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
		return
	}

	// 備份原始設定
	if err := os.WriteFile(settingsPath+".bak", oldData, 0o644); err != nil {
		http.Error(w, "backup failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 原子寫入：先寫 temp file 再 rename
	tmpPath := settingsPath + ".tmp"
	if err := os.WriteFile(tmpPath, newData, 0o644); err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpPath, settingsPath); err != nil {
		http.Error(w, "rename error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Debug("config saved", "project", filepath.Base(ws.Root))
	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

func reloadProcessManager(ws *protocol.CachedWorkspace, pm *ProcessManager) {
	if pm == nil {
		return
	}
	cfg, err := ws.ReadConfig()
	if err != nil {
		return
	}
	if cfg.MaxConcurrentRuns > 0 {
		pm.SetMaxParallel(cfg.MaxConcurrentRuns)
	}
}

// handleGetUserConfig 讀取 ~/.4x/settings.json 回傳 user config
func handleGetUserConfig(w http.ResponseWriter) {
	userConfigMu.Lock()
	cfg, err := protocol.ReadUserConfig()
	userConfigMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handlePutUserConfig 接受 user config JSON，驗證後備份並寫入 ~/.4x/settings.json
func handlePutUserConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read error: "+err.Error(), http.StatusBadRequest)
		}
		return
	}

	var cfg protocol.UserConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	userConfigMu.Lock()
	defer userConfigMu.Unlock()

	path, err := protocol.UserConfigPath()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if oldData, err := os.ReadFile(path); err == nil {
		os.WriteFile(path+".bak", oldData, 0o644)
	}

	if err := protocol.WriteUserConfig(cfg); err != nil {
		http.Error(w, "write error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result, _ := json.MarshalIndent(cfg, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

// handleGetMergedConfig 回傳 user + project merge 後的最終 config
func handleGetMergedConfig(ws *protocol.CachedWorkspace, w http.ResponseWriter) {
	projectCfg, err := ws.ReadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	userConfigMu.Lock()
	userCfg, _ := protocol.ReadUserConfig()
	userConfigMu.Unlock()
	merged := protocol.MergeConfig(userCfg, projectCfg)

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handleGetLocales 回傳支援的語言清單。
func handleGetLocales(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(supportedLocales)
}

// handleGetLocale 回傳對應語言的翻譯 JSON；不存在則 fallback 回 en.json。
func handleGetLocale(w http.ResponseWriter, r *http.Request) {
	lang := strings.TrimPrefix(r.URL.Path, "/api/locales/")
	if strings.ContainsAny(lang, "/\\") || strings.Contains(lang, "..") {
		http.Error(w, "invalid locale", http.StatusBadRequest)
		return
	}
	filename := "locales/" + lang + ".json"
	data, err := web.Assets.ReadFile(filename)
	if err != nil {
		data, _ = web.Assets.ReadFile("locales/en.json")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}
