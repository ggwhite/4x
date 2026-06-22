// Package learning 管理 retro learnings 的儲存與生命週期。
// learnings 是跨 feature 累積的開發教訓，由 CLI 全權讀寫（runner 不直接碰），
// 透過 prompt 層注入到後續 feature 的各 role，藉以迭代 prompt 品質。
package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Category 是 learning 的分類，限定為固定白名單以利 CLI 過濾與 role 對應。
type Category string

const (
	CategoryDesign      Category = "design"
	CategoryCodeQuality Category = "code-quality"
	CategoryTesting     Category = "testing"
	CategoryReview      Category = "review"
	CategoryTooling     Category = "tooling"
	CategoryProcess     Category = "process"
)

// ValidCategories 回傳所有合法的 category 列舉，供 CLI 與驗證使用。
func ValidCategories() []Category {
	return []Category{
		CategoryDesign, CategoryCodeQuality, CategoryTesting,
		CategoryReview, CategoryTooling, CategoryProcess,
	}
}

var validCategorySet = func() map[Category]bool {
	m := make(map[Category]bool, 6)
	for _, c := range ValidCategories() {
		m[c] = true
	}
	return m
}()

// IsValidCategory 檢查 category 是否在白名單中。
func IsValidCategory(c Category) bool {
	return validCategorySet[c]
}

// Status 是 learning 的生命週期狀態。
type Status string

const (
	StatusActive   Status = "active"
	StatusStale    Status = "stale"
	StatusPromoted Status = "promoted"
)

const (
	// DefaultStaleDays 是判定 learning 過期的天數門檻。
	DefaultStaleDays = 90
	// MaxActiveEntries 是 active learnings 的軟上限，超過只 warn 不自動刪。
	MaxActiveEntries = 100
	// MaxSelectedPerRole 是單一 role prompt 注入 learnings 的硬上限。
	MaxSelectedPerRole = 10
)

// Entry 是 learnings.json 中的一個條目，含完整 metadata。
type Entry struct {
	ID            string    `json:"id"`
	SourceFeature string    `json:"source_feature"`
	Category      Category  `json:"category"`
	Content       string    `json:"content"`
	CreatedAt     time.Time `json:"created_at"`
	LastUsed      time.Time `json:"last_used,omitempty"`
	UsedCount     int       `json:"used_count"`
	Status        Status    `json:"status"`
}

// Store 是 .4x/learnings.json 的完整結構。
type Store struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// RetroLearning 是 Acceptor 產出的單一 learning（不含 ID 與 metadata）。
type RetroLearning struct {
	Category Category `json:"category"`
	Content  string   `json:"content"`
}

// RetroFile 是 .4x/{feature-id}/retro-learnings.json 的結構。
type RetroFile struct {
	Learnings []RetroLearning `json:"learnings"`
}

// LoadStore 讀取 learnings.json；檔案不存在時回傳空 store（version=1），不視為錯誤。
func LoadStore(path string) (Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: 1}, nil
		}
		return Store{}, fmt.Errorf("read learnings: %w", err)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return Store{}, fmt.Errorf("parse learnings: %w", err)
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return s, nil
}

// Save 把 store 以 atomic（temp + rename）方式寫入 learnings.json。
func (s *Store) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal learnings: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "learnings-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// Harvest 把 Acceptor 產出的 learnings 追加到 store，回傳實際新增數量。
// 去重規則：content 完全相同即跳過（不做模糊比對）；category 不在白名單或
// content 為空的條目也跳過。新條目自動分配 L 序號 ID 並標記為 active。
func (s *Store) Harvest(featureID string, learnings []RetroLearning) int {
	existing := make(map[string]bool, len(s.Entries))
	for _, e := range s.Entries {
		existing[e.Content] = true
	}

	added := 0
	now := time.Now()
	for _, l := range learnings {
		if !IsValidCategory(l.Category) || l.Content == "" {
			continue
		}
		if existing[l.Content] {
			continue
		}
		existing[l.Content] = true
		s.Entries = append(s.Entries, Entry{
			ID:            s.nextID(),
			SourceFeature: featureID,
			Category:      l.Category,
			Content:       l.Content,
			CreatedAt:     now,
			Status:        StatusActive,
		})
		added++
	}
	return added
}

// nextID 掃描現有 ID 取最大序號 +1，產生下一個 L%03d 格式 ID。
func (s *Store) nextID() string {
	maxNum := 0
	for _, e := range s.Entries {
		var n int
		if _, err := fmt.Sscanf(e.ID, "L%d", &n); err == nil && n > maxNum {
			maxNum = n
		}
	}
	return fmt.Sprintf("L%03d", maxNum+1)
}

// MarkStale 掃描所有 active 條目，超過 staleDays 天未使用的標記為 stale。
// 判斷依據：LastUsed 非零時用 LastUsed，否則用 CreatedAt。promoted/stale 不動。
func (s *Store) MarkStale(staleDays int) {
	cutoff := time.Now().Add(-time.Duration(staleDays) * 24 * time.Hour)
	for i := range s.Entries {
		if s.Entries[i].Status != StatusActive {
			continue
		}
		ref := s.Entries[i].LastUsed
		if ref.IsZero() {
			ref = s.Entries[i].CreatedAt
		}
		if ref.Before(cutoff) {
			s.Entries[i].Status = StatusStale
		}
	}
}

// ActiveEntries 回傳所有 status==active 的條目，保持原始順序。
func (s *Store) ActiveEntries() []Entry {
	var result []Entry
	for _, e := range s.Entries {
		if e.Status == StatusActive {
			result = append(result, e)
		}
	}
	return result
}

// Promote 將指定 ID 標記為 promoted（已升級到 template/instructions，保留記錄不再注入）。
func (s *Store) Promote(id string) error {
	for i := range s.Entries {
		if s.Entries[i].ID == id {
			s.Entries[i].Status = StatusPromoted
			return nil
		}
	}
	return fmt.Errorf("learning %s not found", id)
}

// Remove 移除指定 ID 的條目。
func (s *Store) Remove(id string) error {
	for i, e := range s.Entries {
		if e.ID == id {
			s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("learning %s not found", id)
}

// Prune 移除所有 stale 條目，回傳移除數量。
func (s *Store) Prune() int {
	var kept []Entry
	removed := 0
	for _, e := range s.Entries {
		if e.Status == StatusStale {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	s.Entries = kept
	return removed
}

// UpdateUsage 更新指定 ID 的 LastUsed 為現在、UsedCount 遞增，標示這些 learnings 被選用過。
func (s *Store) UpdateUsage(ids []string) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	now := time.Now()
	for i := range s.Entries {
		if idSet[s.Entries[i].ID] {
			s.Entries[i].LastUsed = now
			s.Entries[i].UsedCount++
		}
	}
}

// ParseRetroFile 讀取 Acceptor 產出的 retro-learnings.json，回傳其中的 learnings 列表。
func ParseRetroFile(path string) ([]RetroLearning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rf RetroFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse retro file: %w", err)
	}
	return rf.Learnings, nil
}

var roleCategoryMap = map[string][]Category{
	"designer":        {CategoryDesign, CategoryProcess},
	"design-reviewer": {CategoryDesign, CategoryReview},
	"coder":           {CategoryDesign, CategoryCodeQuality, CategoryTooling},
	"reviewer":        {CategoryCodeQuality, CategoryReview},
	"deep-reviewer":   {CategoryCodeQuality, CategoryReview, CategoryDesign},
	"tester":          {CategoryTesting, CategoryTooling},
	"acceptor":        {CategoryProcess},
}

// CategoriesForRole 回傳指定 role 應注入的 category 列表；未知 role 回傳 nil。
func CategoriesForRole(role string) []Category {
	return roleCategoryMap[role]
}
