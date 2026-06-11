package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const maxRecentProjects = 20

// ProjectEntry 記錄一個最近開過的專案
type ProjectEntry struct {
	Path       string    `json:"path"`
	LastOpened time.Time `json:"lastOpened"`
}

// RecentProjects 管理最近開過的專案列表（LRU 順序）
type RecentProjects struct {
	Projects []ProjectEntry `json:"projects"`
}

// Touch 將路徑加到最前面（若已存在則移到最前），超過上限時淘汰最舊
func (rp *RecentProjects) Touch(path string) {
	now := time.Now()
	filtered := make([]ProjectEntry, 0, len(rp.Projects))
	for _, p := range rp.Projects {
		if p.Path != path {
			filtered = append(filtered, p)
		}
	}
	rp.Projects = append([]ProjectEntry{{Path: path, LastOpened: now}}, filtered...)
	if len(rp.Projects) > maxRecentProjects {
		rp.Projects = rp.Projects[:maxRecentProjects]
	}
}

// Remove 從列表移除指定路徑
func (rp *RecentProjects) Remove(path string) {
	filtered := make([]ProjectEntry, 0, len(rp.Projects))
	for _, p := range rp.Projects {
		if p.Path != path {
			filtered = append(filtered, p)
		}
	}
	rp.Projects = filtered
}

// DefaultRecentProjectsPath 回傳 ~/.4x/recent-projects.json
func DefaultRecentProjectsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".4x", "recent-projects.json"), nil
}

// LoadRecentProjects 讀取 recent-projects.json，檔案不存在時回傳空列表
func LoadRecentProjects(path string) (*RecentProjects, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RecentProjects{}, nil
		}
		return nil, err
	}
	var rp RecentProjects
	if err := json.Unmarshal(data, &rp); err != nil {
		return nil, err
	}
	return &rp, nil
}

// SaveRecentProjects 寫入 recent-projects.json，自動建立父目錄
func SaveRecentProjects(path string, rp *RecentProjects) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
