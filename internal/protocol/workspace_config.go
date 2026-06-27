package protocol

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// ReadConfig 讀取 .4x/settings.json
func (w *Workspace) ReadConfig() (Config, error) {
	return ReadConfig(w.DotDir())
}

// LoadMergedConfig 讀取 project config 並合併 user config，封裝散落各處的三行 boilerplate。
// project config 讀取失敗時回傳 error 由呼叫端決定如何處理；
// user config 讀取失敗時印 slog.Warn 但不中斷（沿用既有各站點的處理慣例）。
func (w *Workspace) LoadMergedConfig() (Config, error) {
	cfg, err := w.ReadConfig()
	if err != nil {
		return Config{}, err
	}
	if userCfg, err := ReadUserConfig(); err != nil {
		slog.Warn("failed to read user config", "error", err)
	} else {
		cfg = MergeConfig(userCfg, cfg)
	}
	return cfg, nil
}

// ReadConfig 讀取 .4x/settings.json
func ReadConfig(dotDir string) (Config, error) {
	cfg, err := readJSON[Config](filepath.Join(dotDir, ConfigFile))
	if err != nil {
		return Config{}, err
	}
	if err := ValidateConfig(cfg); err != nil {
		return Config{}, fmt.Errorf("settings.json: %w", err)
	}
	return cfg, nil
}

// WriteConfig 寫入 .4x/settings.json
func WriteConfig(dotDir string, cfg Config) error {
	return writeJSON(filepath.Join(dotDir, ConfigFile), cfg)
}

// UserConfigPath 回傳 ~/.4x/settings.json 的路徑
func UserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, UserConfigDir, UserConfigFile), nil
}

// ReadUserConfig 讀取 ~/.4x/settings.json，不存在時回傳零值
func ReadUserConfig() (UserConfig, error) {
	path, err := UserConfigPath()
	if err != nil {
		return UserConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UserConfig{}, nil
		}
		return UserConfig{}, fmt.Errorf("read user config: %w", err)
	}
	var cfg UserConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return UserConfig{}, fmt.Errorf("parse user config: %w", err)
	}
	return cfg, nil
}

// WriteUserConfig 寫入 ~/.4x/settings.json
func WriteUserConfig(cfg UserConfig) error {
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSON(path, cfg)
}
