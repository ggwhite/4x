// Package learning 管理 retro learnings 的儲存與生命週期。
// learnings 是跨 feature 累積的開發教訓，由 CLI 全權讀寫（runner 不直接碰），
// 透過 prompt 層注入到後續 feature 的各 role，藉以迭代 prompt 品質。
package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	CategoryOps         Category = "ops"
)

// ValidCategories 回傳所有合法的 category 列舉，供 CLI 與驗證使用。
func ValidCategories() []Category {
	return []Category{
		CategoryDesign, CategoryCodeQuality, CategoryTesting,
		CategoryReview, CategoryTooling, CategoryProcess, CategoryOps,
	}
}

var validCategorySet = func() map[Category]bool {
	m := make(map[Category]bool, len(ValidCategories()))
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
	StatusCandidate Status = "candidate"
	StatusActive    Status = "active"
	StatusStale     Status = "stale"
	StatusPromoted  Status = "promoted"
)

const (
	// DefaultStaleDays 是判定 learning 過期的天數門檻。
	DefaultStaleDays = 90
	// MaxActiveEntries 是 active learnings 的軟上限，超過只 warn 不自動刪。
	MaxActiveEntries = 100
	// MaxSelectedPerRole 是單一 role prompt 注入 learnings 的硬上限。
	MaxSelectedPerRole = 10
	// ConsolidateThreshold 是觸發 AI consolidate 的 active 條目門檻。
	ConsolidateThreshold = 30
	// FuzzyDupThreshold 是判定 learning 語意重複的 Jaccard 相似度門檻。
	FuzzyDupThreshold = 0.7
)

// ConsolidateAction 是 AI 對單一 learning 的處理決策。
type ConsolidateAction struct {
	ID      string `json:"id"`
	Action  string `json:"action"`
	MergeID string `json:"merge_into,omitempty"`
	Content string `json:"merged_content,omitempty"`
	Reason  string `json:"reason"`
}

// ConsolidateResult 是 consolidate runner 的輸出結構。
type ConsolidateResult struct {
	Actions []ConsolidateAction `json:"actions"`
}

// Entry 是 learnings.json 中的一個條目，含完整 metadata。
type Entry struct {
	ID            string    `json:"id"`
	SourceFeature string    `json:"source_feature"`
	SourceRole    string    `json:"source_role,omitempty"`
	Category      Category  `json:"category"`
	Content       string    `json:"content"`
	CreatedAt     time.Time `json:"created_at"`
	ActivatedAt   time.Time `json:"activated_at,omitempty"`
	LastUsed      time.Time `json:"last_used,omitempty"`
	UsedCount     int       `json:"used_count"`
	Status        Status    `json:"status"`
	Ineffective   bool      `json:"ineffective,omitempty"`
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
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// FindSimilar 在 store 既有條目中以三層比對（exact → normalized → Jaccard ≥ 0.7）
// 搜尋與 content 相似的條目，回傳第一個命中的 Entry 指標，未命中回傳 nil。
func (s *Store) FindSimilar(content string) *Entry {
	for i := range s.Entries {
		if s.Entries[i].Content == content {
			return &s.Entries[i]
		}
	}
	norm := normalizeContent(content)
	for i := range s.Entries {
		if normalizeContent(s.Entries[i].Content) == norm {
			return &s.Entries[i]
		}
	}
	tokens := tokenize(content)
	for i := range s.Entries {
		if JaccardSimilarity(tokens, tokenize(s.Entries[i].Content)) >= FuzzyDupThreshold {
			return &s.Entries[i]
		}
	}
	return nil
}

// Harvest 把 learnings 追加到 store，回傳實際新增數量。
// sourceRole 標記產出來源角色（"acceptor"、"coder"、"reviewer" 等），空字串表示未知。
// 三層去重：(1) content 完全比對 (2) 正規化比對（大小寫/空白/標點）(3) 詞集 Jaccard ≥ 0.7。
// category 不在白名單或 content 為空的條目跳過。新條目自動分配 L 序號 ID 並標記為 candidate。
// fuzzy match 到 active entry 時 skip（去重）；match 到同 feature candidate 時 skip；
// match 到不同 feature 的 candidate 時升級該 candidate 為 active，新的不寫入。
func (s *Store) Harvest(featureID, sourceRole string, learnings []RetroLearning) int {
	exactSet := make(map[string]bool, len(s.Entries))
	normSet := make(map[string]bool, len(s.Entries))
	tokenSets := make([]indexedTokens, 0, len(s.Entries))
	for i, e := range s.Entries {
		exactSet[e.Content] = true
		normSet[normalizeContent(e.Content)] = true
		tokenSets = append(tokenSets, indexedTokens{idx: i, tokens: tokenize(e.Content)})
	}

	added := 0
	now := time.Now()
	for _, l := range learnings {
		if !IsValidCategory(l.Category) || l.Content == "" {
			continue
		}
		if exactSet[l.Content] {
			continue
		}
		norm := normalizeContent(l.Content)
		if normSet[norm] {
			continue
		}
		tokens := tokenize(l.Content)
		if matchIdx := findFuzzyMatch(tokens, tokenSets); matchIdx >= 0 {
			matched := &s.Entries[matchIdx]
			if matched.Status == StatusCandidate && matched.SourceFeature != featureID {
				matched.Status = StatusActive
				matched.ActivatedAt = now
			}
			continue
		}

		exactSet[l.Content] = true
		normSet[norm] = true
		newIdx := len(s.Entries)
		s.Entries = append(s.Entries, Entry{
			ID:            s.nextID(),
			SourceFeature: featureID,
			SourceRole:    sourceRole,
			Category:      l.Category,
			Content:       l.Content,
			CreatedAt:     now,
			Status:        StatusCandidate,
		})
		tokenSets = append(tokenSets, indexedTokens{idx: newIdx, tokens: tokens})
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

// CandidateEntries 回傳所有 status==candidate 的條目，保持原始順序。
func (s *Store) CandidateEntries() []Entry {
	var result []Entry
	for _, e := range s.Entries {
		if e.Status == StatusCandidate {
			result = append(result, e)
		}
	}
	return result
}

// PromoteCandidates 將指定 ID 中 status==candidate 的條目升級為 active，並設定 ActivatedAt。
func (s *Store) PromoteCandidates(ids []string) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	now := time.Now()
	for i := range s.Entries {
		if idSet[s.Entries[i].ID] && s.Entries[i].Status == StatusCandidate {
			s.Entries[i].Status = StatusActive
			s.Entries[i].ActivatedAt = now
		}
	}
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

// ApplyConsolidation 套用 AI consolidate 的結果：merge 把被合併條目標記為 stale 並更新目標條目的 content，
// remove 直接移除條目。回傳 (merged, removed) 數量。
func (s *Store) ApplyConsolidation(actions []ConsolidateAction) (int, int) {
	entryIdx := make(map[string]int, len(s.Entries))
	for i, e := range s.Entries {
		entryIdx[e.ID] = i
	}

	merged, removed := 0, 0
	removeSet := make(map[string]bool)

	for _, a := range actions {
		switch a.Action {
		case "merge":
			srcIdx, srcOK := entryIdx[a.ID]
			dstIdx, dstOK := entryIdx[a.MergeID]
			if !srcOK || !dstOK {
				continue
			}
			s.Entries[srcIdx].Status = StatusStale
			if a.Content != "" {
				s.Entries[dstIdx].Content = a.Content
			}
			if s.Entries[srcIdx].UsedCount > s.Entries[dstIdx].UsedCount {
				s.Entries[dstIdx].UsedCount = s.Entries[srcIdx].UsedCount
			}
			merged++
		case "remove":
			if _, ok := entryIdx[a.ID]; ok {
				removeSet[a.ID] = true
				removed++
			}
		}
	}

	if len(removeSet) > 0 {
		var kept []Entry
		for _, e := range s.Entries {
			if !removeSet[e.ID] {
				kept = append(kept, e)
			}
		}
		s.Entries = kept
	}
	return merged, removed
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

// RoleLearningsFile 是各角色在 round 目錄內產出的 role-learnings.json 結構。
type RoleLearningsFile struct {
	Role      string          `json:"role"`
	Learnings []RetroLearning `json:"learnings"`
}

// ParseRoleLearningsFile 讀取角色產出的 role-learnings.json，回傳角色名稱與 learnings。
func ParseRoleLearningsFile(path string) (string, []RetroLearning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var rf RoleLearningsFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return "", nil, fmt.Errorf("parse role-learnings file: %w", err)
	}
	return rf.Role, rf.Learnings, nil
}

var roleCategoryMap = map[string][]Category{
	"designer":        {CategoryDesign, CategoryProcess},
	"design-reviewer": {CategoryDesign, CategoryReview},
	"coder":           {CategoryDesign, CategoryCodeQuality, CategoryTooling, CategoryOps},
	"reviewer":        {CategoryCodeQuality, CategoryReview},
	"deep-reviewer":   {CategoryCodeQuality, CategoryReview, CategoryDesign},
	"tester":          {CategoryTesting, CategoryTooling, CategoryOps},
	"fixer":           {CategoryCodeQuality, CategoryTooling, CategoryOps},
	"acceptor":        {CategoryProcess},
}

// CategoriesForRole 回傳指定 role 應注入的 category 列表；未知 role 回傳 nil。
func CategoriesForRole(role string) []Category {
	return roleCategoryMap[role]
}

// MarkIneffective 掃描所有 active 條目，滿足以下三條件的標記 Ineffective=true，回傳新標記數：
// 1. UsedCount >= 3
// 2. ActivatedAt（零值時用 CreatedAt）距今 > 30 天
// 3. 最近 3 個來自不同 feature 的 entries 中有同 category 的新 learning
func (s *Store) MarkIneffective() int {
	now := time.Now()
	cutoff := now.Add(-30 * 24 * time.Hour)
	marked := 0

	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Status != StatusActive {
			continue
		}
		if e.Ineffective {
			continue
		}
		if e.UsedCount < 3 {
			continue
		}
		activated := e.ActivatedAt
		if activated.IsZero() {
			activated = e.CreatedAt
		}
		if activated.After(cutoff) {
			continue
		}
		if s.hasCategoryContinuation(i) {
			e.Ineffective = true
			marked++
		}
	}
	return marked
}

// hasCategoryContinuation 檢查最近 3 個來自不同 feature 的 entries 是否包含同 category 的 learning。
func (s *Store) hasCategoryContinuation(targetIdx int) bool {
	target := s.Entries[targetIdx]
	count := 0
	for j := len(s.Entries) - 1; j >= 0 && count < 3; j-- {
		if j == targetIdx {
			continue
		}
		other := s.Entries[j]
		if other.SourceFeature == target.SourceFeature {
			continue
		}
		count++
		if other.Category == target.Category {
			return true
		}
	}
	return false
}

// normalizeContent 正規化 content：小寫、去前後空白、收合連續空白。
func normalizeContent(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

// tokenize 把 content 切成去標點的小寫詞集。
func tokenize(s string) map[string]bool {
	words := strings.Fields(normalizeContent(s))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		w = strings.TrimRight(w, ".,;:!?。，；：！？、")
		if w != "" {
			set[w] = true
		}
	}
	return set
}

// JaccardSimilarity 計算兩個詞集的 Jaccard 相似度 = |交集| / |聯集|。
func JaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	intersection := 0
	for w := range a {
		if b[w] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

type indexedTokens struct {
	idx    int
	tokens map[string]bool
}

// findFuzzyMatch 在既有 token 集合中尋找第一個 Jaccard ≥ 門檻的匹配，回傳其在 Entries 中的 index；未命中回傳 -1。
func findFuzzyMatch(candidate map[string]bool, existing []indexedTokens) int {
	for _, e := range existing {
		if JaccardSimilarity(candidate, e.tokens) >= FuzzyDupThreshold {
			return e.idx
		}
	}
	return -1
}
