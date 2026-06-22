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

	"github.com/ggwhite/4x/internal/protocol"
)

var errInvalidSettings = errors.New("invalid settings")

type invalidSettingsError struct {
	err error
}

func (e invalidSettingsError) Error() string {
	return e.err.Error()
}

func (e invalidSettingsError) Unwrap() error {
	return e.err
}

func (e invalidSettingsError) Is(target error) bool {
	return target == errInvalidSettings
}

// clientErr 把 merge callback 內因 client 送入非法 payload（型別不符、JSON decode 失敗）
// 產生的錯誤包成 invalidSettingsError，讓 handler 統一映射為 HTTP 400 而非 500。
// merge callback 只在處理 client 輸入時失敗，屬於 bad request 而非伺服器內部錯誤。
func clientErr(msg string) error {
	return invalidSettingsError{err: errors.New(msg)}
}

// readMergeWriteSettings 執行 read-merge-write 流程：取得 workspace-level lock、讀取
// settings.json、unmarshal 為 protocol.Config、呼叫 merge callback 修改設定、
// 寫入暫存檔並以 os.Rename 原子替換、最後回傳更新後的 JSON bytes。
//
// merge callback 由呼叫者提供，負責修改 *protocol.Config 的欄位。
// 若 merge 回傳 error，整個 readMergeWriteSettings 亦回傳該 error，不寫入磁碟。
//
// 注意：暫存檔為 .4x/settings.json.tmp，不保留任何備份檔。
func readMergeWriteSettings(root string, merge func(*protocol.Config) error) ([]byte, error) {
	// 取得 per-project lock
	settingsLock := settingsMu.get(root)
	settingsLock.Lock()
	defer settingsLock.Unlock()

	dotDir := filepath.Join(root, ".4x")
	settingsPath := filepath.Join(dotDir, protocol.ConfigFile)

	// 讀取現有設定
	oldData, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, err
	}

	// Unmarshal 為 Config
	var cfg protocol.Config
	if err := json.Unmarshal(oldData, &cfg); err != nil {
		return nil, err
	}

	// 呼叫 merge callback
	if err := merge(&cfg); err != nil {
		return nil, err
	}

	if err := protocol.ValidateConfig(cfg); err != nil {
		return nil, invalidSettingsError{err: err}
	}

	// Marshal 回 JSON，保持一致的縮排
	newData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	newData = append(newData, '\n')

	// 內容沒變就直接回傳，不寫檔
	if bytes.Equal(oldData, newData) {
		return newData, nil
	}

	// 原子寫入：先寫 temp file 再 rename
	tmpPath := settingsPath + ".tmp"
	if err := os.WriteFile(tmpPath, newData, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, settingsPath); err != nil {
		os.Remove(tmpPath) // 盡量清理 tmp 檔
		return nil, err
	}

	slog.Debug("settings saved", "project", filepath.Base(root))
	return newData, nil
}

// handlePutProfile 處理 PUT /api/settings/profiles/{name}：
// 新增或覆寫單一 profile，body 為 ProfileConfig JSON。
func handlePutProfile(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/settings/profiles/")
	if name == "" || name == "/" {
		http.Error(w, "profile name cannot be empty", http.StatusBadRequest)
		return
	}

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

	// 驗證 body 為有效 ProfileConfig JSON
	var profileCfg protocol.ProfileConfig
	if err := json.Unmarshal(body, &profileCfg); err != nil {
		http.Error(w, "invalid profile JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// read-merge-write
	resultData, err := readMergeWriteSettings(ws.Root, func(cfg *protocol.Config) error {
		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]protocol.ProfileConfig)
		}
		cfg.Profiles[name] = profileCfg
		return nil
	})

	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidSettings) {
			status = http.StatusBadRequest
		}
		http.Error(w, "update failed: "+err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resultData)
}

// handleDeleteProfile 處理 DELETE /api/settings/profiles/{name}：
// 刪除單一 profile。若被刪的 profile 是 default_profile，則同時清空 default_profile。
func handleDeleteProfile(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/settings/profiles/")
	if name == "" || name == "/" {
		http.Error(w, "profile name cannot be empty", http.StatusBadRequest)
		return
	}

	// read-merge-write
	resultData, err := readMergeWriteSettings(ws.Root, func(cfg *protocol.Config) error {
		if cfg.Profiles != nil {
			delete(cfg.Profiles, name)
		}
		// 若被刪的 profile 是 default_profile，清空 default_profile
		if cfg.DefaultProfile == name {
			cfg.DefaultProfile = ""
		}
		return nil
	})

	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidSettings) {
			status = http.StatusBadRequest
		}
		http.Error(w, "delete failed: "+err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resultData)
}

// handlePutRole 處理 PUT /api/settings/roles/{role}：
// 新增或覆寫單一 role config，body 為 RoleConfig JSON。
func handlePutRole(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
	role := strings.TrimPrefix(r.URL.Path, "/api/settings/roles/")
	if role == "" || role == "/" {
		http.Error(w, "role cannot be empty", http.StatusBadRequest)
		return
	}

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

	// 驗證 body 為有效 RoleConfig JSON
	var roleCfg protocol.RoleConfig
	if err := json.Unmarshal(body, &roleCfg); err != nil {
		http.Error(w, "invalid role JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// read-merge-write
	resultData, err := readMergeWriteSettings(ws.Root, func(cfg *protocol.Config) error {
		if cfg.Roles == nil {
			cfg.Roles = make(map[string]protocol.RoleConfig)
		}
		cfg.Roles[role] = roleCfg
		return nil
	})

	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidSettings) {
			status = http.StatusBadRequest
		}
		http.Error(w, "update failed: "+err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resultData)
}

// handlePatchSettings 處理 PATCH /api/settings：
// 部分更新（merge 而非全量替換），僅更新 body 中存在的欄位。
// 支援 default_profile 與 default_runner 等 scalar 欄位，以及 Profiles / Roles 等 map 欄位的 key-level merge。
func handlePatchSettings(ws *protocol.CachedWorkspace, w http.ResponseWriter, r *http.Request) {
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

	// 解析 patch 內容為 map，判斷哪些欄位需要更新
	var patchMap map[string]interface{}
	if err := json.Unmarshal(body, &patchMap); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// read-merge-write
	resultData, err := readMergeWriteSettings(ws.Root, func(cfg *protocol.Config) error {
		// 逐個欄位 merge
		for key, val := range patchMap {
			switch key {
			case "default_runner":
				// scalar field replace
				if str, ok := val.(string); ok {
					cfg.Default = str
				} else if val != nil {
					return clientErr("default_runner must be a string")
				}
			case "default_profile":
				// scalar field replace
				if str, ok := val.(string); ok {
					cfg.DefaultProfile = str
				} else if val != nil {
					return clientErr("default_profile must be a string")
				}
			case "project":
				// nested object merge
				if projMap, ok := val.(map[string]interface{}); ok {
					projJSON, err := json.Marshal(projMap)
				if err != nil {
					return clientErr("marshal project config: " + err.Error())
				}
					var projCfg protocol.ProjectConfig
					if err := json.Unmarshal(projJSON, &projCfg); err != nil {
						return clientErr("invalid project config: " + err.Error())
					}
					cfg.Project = projCfg
				} else if val != nil {
					return clientErr("project must be an object")
				}
			case "runners":
				// map field: key-level merge
				if runnersMap, ok := val.(map[string]interface{}); ok {
					if cfg.Runners == nil {
						cfg.Runners = make(map[string]protocol.RunnerConfig)
					}
					for runnerName, runnerVal := range runnersMap {
						runnerJSON, err := json.Marshal(runnerVal)
						if err != nil {
							return clientErr("marshal runner config for " + runnerName + ": " + err.Error())
						}
						var runnerCfg protocol.RunnerConfig
						if err := json.Unmarshal(runnerJSON, &runnerCfg); err != nil {
							return clientErr("invalid runner config for " + runnerName + ": " + err.Error())
						}
						cfg.Runners[runnerName] = runnerCfg
					}
				} else if val != nil {
					return clientErr("runners must be an object")
				}
			case "roles":
				// map field: key-level merge
				if rolesMap, ok := val.(map[string]interface{}); ok {
					if cfg.Roles == nil {
						cfg.Roles = make(map[string]protocol.RoleConfig)
					}
					for roleName, roleVal := range rolesMap {
						roleJSON, err := json.Marshal(roleVal)
						if err != nil {
							return clientErr("marshal role config for " + roleName + ": " + err.Error())
						}
						var roleCfg protocol.RoleConfig
						if err := json.Unmarshal(roleJSON, &roleCfg); err != nil {
							return clientErr("invalid role config for " + roleName + ": " + err.Error())
						}
						cfg.Roles[roleName] = roleCfg
					}
				} else if val != nil {
					return clientErr("roles must be an object")
				}
			case "profiles":
				// map field: key-level merge
				if profilesMap, ok := val.(map[string]interface{}); ok {
					if cfg.Profiles == nil {
						cfg.Profiles = make(map[string]protocol.ProfileConfig)
					}
					for profileName, profileVal := range profilesMap {
						profileJSON, err := json.Marshal(profileVal)
						if err != nil {
							return clientErr("marshal profile config for " + profileName + ": " + err.Error())
						}
						var profileCfg protocol.ProfileConfig
						if err := json.Unmarshal(profileJSON, &profileCfg); err != nil {
							return clientErr("invalid profile config for " + profileName + ": " + err.Error())
						}
						cfg.Profiles[profileName] = profileCfg
					}
				} else if val != nil {
					return clientErr("profiles must be an object")
				}
			case "isolation", "parallel_review_test", "auto_discover_features":
				// 其他欄位直接更新
				switch key {
				case "isolation":
					if str, ok := val.(string); ok {
						cfg.Isolation = str
					} else if val != nil {
						return clientErr("isolation must be a string")
					}
				case "parallel_review_test":
					if b, ok := val.(bool); ok {
						cfg.ParallelReviewTest = b
					} else if val != nil {
						return clientErr("parallel_review_test must be a boolean")
					}
				case "auto_discover_features":
					if b, ok := val.(bool); ok {
						cfg.AutoDiscoverFeatures = b
					} else if val != nil {
						return clientErr("auto_discover_features must be a boolean")
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidSettings) {
			status = http.StatusBadRequest
		}
		http.Error(w, "patch failed: "+err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(resultData)
}
