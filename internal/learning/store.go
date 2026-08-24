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
	// DefaultCandidateMaxIdleDays 是判定「從未使用的 candidate」老化為 stale 的預設天數門檻。
	DefaultCandidateMaxIdleDays = 30
	// MaxActiveEntries 是 active learnings 的軟上限，超過只 warn 不自動刪。
	MaxActiveEntries = 100
	// ConsolidateThreshold 是觸發 AI consolidate 的 active 條目門檻。
	ConsolidateThreshold = 30
	// ConsolidateCooldown 是兩次 consolidate 之間的最短間隔。NeedConsolidate 在冷卻期內
	// 一律回 false，避免 v1→v2 migration 後大型 store 的 active 總數恆 ≥ ConsolidateThreshold，
	// 導致每個 feature 完成都觸發一次 120 秒的 LLM consolidate 呼叫。
	ConsolidateCooldown = 24 * time.Hour
	// FuzzyDupThreshold 是判定 learning 語意重複的 Jaccard 相似度門檻。
	FuzzyDupThreshold = 0.7
	// RecurrenceSimilarityThreshold 是 recurrence 判定（見 hasRecurrenceEvidence）使用的 Jaccard 門檻。
	// 必須嚴格小於 FuzzyDupThreshold：Harvest 與 FindSimilar 在寫入前已用 FuzzyDupThreshold 去重，
	// 所以 store 內任兩條經 harvest 進來的條目 Jaccard 必定 < FuzzyDupThreshold；
	// recurrence 若沿用同一個門檻（或直接呼叫 FindSimilar），判定會永遠不成立而形同死碼。
	RecurrenceSimilarityThreshold = 0.3
	// RecurrenceMinDistinctFeatures 是構成 recurrence 所需的相異 SourceFeature 數量下限。
	// 要求「相異」是為了排除「單一 feature 一次吐出多條同 category 心得」這種不構成證據的情況。
	RecurrenceMinDistinctFeatures = 2
	// MaxPerFeatureCategory 是 Harvest 寫入時對單一 (SourceFeature, Category) 桶的准入上限，
	// 避免單一 feature 一輪吐出大量同 category 心得就把 store 灌爆。
	// 本上限只擋新寫入，不裁剪既有超額桶——Prune/DemoteInactiveActive/MarkCandidatesStale 都不看它，
	// 因此「store 內任一桶 <= MaxPerFeatureCategory」不是 store 層級的不變式。
	MaxPerFeatureCategory = 3
	// StoreVersion 是目前的 learnings store 格式版本。
	// v2 代表已跑過 ineffective 旗標的一次性重設（見 LoadStore）。
	StoreVersion = 2
	// ManualSourceFeature 是 `4x learn add` 手動新增條目的 SourceFeature 值。
	// 此來源豁免 MaxPerFeatureCategory 桶上限——手動條目全部擠在固定的 "manual" 桶，
	// 套上限會讓 `4x learn add` 在達到上限後永久失效。
	ManualSourceFeature = "manual"
)

const (
	// InitialConfidence 是新 candidate（及 fuzzy 升 active 的 candidate）落地時的初始 confidence，
	// 確保 confidence 生命週期來源端不留下零值。
	InitialConfidence = 0.3
	// ConfidenceReinforceStep 是 learning 每次被注入 role prompt 時 confidence 的增量。
	ConfidenceReinforceStep = 0.1
	// MaxConfidence 是 confidence 的上限。
	MaxConfidence = 1.0
)

// reinforceConfidence 回傳 learning 被注入 prompt（或 fuzzy 升 active）後提升的 confidence，
// deterministic 且帶地板與上限：current<=0（含舊資料）先以 InitialConfidence 為地板，再加一個 step。
func reinforceConfidence(current float64) float64 {
	if current <= 0 {
		current = InitialConfidence
	}
	next := current + ConfidenceReinforceStep
	if next > MaxConfidence {
		return MaxConfidence
	}
	return next
}

// legacyConfidence 依 UsedCount 推導舊資料（confidence==0）的排序 fallback 分數，deterministic 不依時間。
// 以 InitialConfidence 為地板、每次使用加一個 step、MaxConfidence 為上限。
func legacyConfidence(usedCount int) float64 {
	score := InitialConfidence + float64(usedCount)*ConfidenceReinforceStep
	if score > MaxConfidence {
		return MaxConfidence
	}
	return score
}

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
	// Confidence 是 learning 的可強化信心分數（0~1）；命中注入時提升，供排序/輸出/截斷優先保留高價值條目。
	// 舊 store 無此欄位時載入為 0，代表「未設定」，只在排序時以 SortScore 的 fallback 分數處理，不隱式寫回。
	Confidence float64 `json:"confidence,omitempty"`
}

// SortScore 回傳 entry 的排序分數：Confidence>0 直接採用；==0（舊資料）以 UsedCount 推導 fallback。
// 僅供排序/輸出/截斷判斷使用，不會寫回 store（避免對舊資料做隱式 migration）。
func (e Entry) SortScore() float64 {
	if e.Confidence > 0 {
		return e.Confidence
	}
	return legacyConfidence(e.UsedCount)
}

// Store 是 .4x/learnings.json 的完整結構。
type Store struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
	// IneffectiveResetIDs 記錄被 v1→v2 一次性重設過 Ineffective 旗標的條目 ID，
	// 供 `4x learn list --ineffective-reset` 查詢受影響的條目。
	// 條目被刪除後 nextID 會回收其序號，故 Harvest 寫入新條目時會用 forgetResetID 把該 ID 移出本清單；
	// 已刪除且序號未被回收的 ID 會殘留，但以 ID 反查不到條目，不影響查詢輸出。
	IneffectiveResetIDs []string `json:"ineffective_reset_ids,omitempty"`

	// LastConsolidateAt 記錄上次 consolidate 完成的時間（無論該次結果是否真的合併/移除任何
	// 條目），供 NeedConsolidate 判斷冷卻期（見 ConsolidateCooldown）。零值代表從未 consolidate 過。
	LastConsolidateAt time.Time `json:"last_consolidate_at,omitempty"`

	// migrated 標記本次 LoadStore 是否實際套用過 migration，不參與 JSON 序列化。
	migrated bool

	// crossFeaturePromoted 標記本次 Harvest 是否曾把既有 candidate 就地升級為 active
	// （cross-feature fuzzy match，見 Harvest 內 matched.Status = StatusActive 分支）。
	// 該分支不遞增 added 也不遞增 skipped（AC-12 pin 死 Harvest 簽章為 (added, skipped int)，
	// 不可再加回傳值），若不另外追蹤，HarvestLearnings 的早退條件會把這次真實變動誤判為
	// 「store 無變化」而不存檔，升級結果被靜默丟棄（F187 gap）。範圍刻意只涵蓋這一條分支，
	// 不是涵蓋所有 mutator 的通用 dirty flag。
	crossFeaturePromoted bool
}

// MigrationApplied 回傳本次 LoadStore 是否實際套用過 v1→v2 migration。
// migration 只改記憶體中的 store、不寫檔，呼叫端須以此決定要不要存檔讓重設落地。
func (s *Store) MigrationApplied() bool {
	return s.migrated
}

// CrossFeaturePromoted 回傳本次呼叫 Harvest 是否曾把既有 candidate 就地升級為 active。
func (s *Store) CrossFeaturePromoted() bool {
	return s.crossFeaturePromoted
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

// LoadStore 讀取 learnings.json；檔案不存在時回傳空 store（version=StoreVersion），不視為錯誤。
//
// 注意：本函式有副作用。版本低於 StoreVersion 的 store 會就地套用 v1→v2 migration——
// 把所有 Ineffective==true 的條目重設為 false、把其 ID 填入 IneffectiveResetIDs、
// 並把 Version 抬升到 StoreVersion。migration 只改記憶體、不寫檔，
// 因此唯讀路徑（`4x learn list`、`4x learn context`、dashboard 等）呼叫本函式時
// 同樣會拿到「已被重設過」的資料，只是磁碟上要等下一次 store 寫入才落地；
// 在那之前每次載入都重跑同一份重設，冪等無害。呼叫端可用 MigrationApplied 判斷是否需要存檔。
func LoadStore(path string) (Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{Version: StoreVersion}, nil
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
	if s.Version < StoreVersion {
		for i := range s.Entries {
			if s.Entries[i].Ineffective {
				s.Entries[i].Ineffective = false
				s.IneffectiveResetIDs = append(s.IneffectiveResetIDs, s.Entries[i].ID)
			}
		}
		s.Version = StoreVersion
		s.migrated = true
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

// Harvest 把 learnings 追加到 store，回傳 (added, skipped)：added 是實際新增數量，
// skipped 是通過三層去重卻因 (SourceFeature, Category) 桶上限被擋下的數量（去重擋掉的不計入）。
// sourceRole 標記產出來源角色（"acceptor"、"coder"、"reviewer" 等），空字串表示未知。
// 三層去重：(1) content 完全比對 (2) 正規化比對（大小寫/空白/標點）(3) 詞集 Jaccard ≥ 0.7。
// 第四層為桶上限：同一 (SourceFeature, Category) 桶本次寫入後不會超過 MaxPerFeatureCategory 條，
// 上限對既有 store 條目一併計數（同一 feature 會多次呼叫 Harvest），超出者只計入 skipped、不寫入。
// featureID == ManualSourceFeature 時完全跳過桶上限檢查（三層去重照舊生效），見 ManualSourceFeature。
// category 不在白名單或 content 為空的條目跳過。新條目自動分配 L 序號 ID 並標記為 candidate。
// fuzzy match 到 active entry 時 skip（去重）；match 到同 feature candidate 時 skip；
// match 到不同 feature 的 candidate 時升級該 candidate 為 active，新的不寫入。
func (s *Store) Harvest(featureID, sourceRole string, learnings []RetroLearning) (added, skipped int) {
	exactSet := make(map[string]bool, len(s.Entries))
	normSet := make(map[string]bool, len(s.Entries))
	tokenSets := make([]indexedTokens, 0, len(s.Entries))
	bucket := make(map[string]int, len(s.Entries))
	for i, e := range s.Entries {
		exactSet[e.Content] = true
		normSet[normalizeContent(e.Content)] = true
		tokenSets = append(tokenSets, indexedTokens{idx: i, tokens: tokenize(e.Content)})
		bucket[bucketKey(e.SourceFeature, e.Category)]++
	}

	exemptFromCap := featureID == ManualSourceFeature
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
				matched.Confidence = reinforceConfidence(matched.Confidence)
				s.crossFeaturePromoted = true
			}
			continue
		}

		key := bucketKey(featureID, l.Category)
		if !exemptFromCap && bucket[key] >= MaxPerFeatureCategory {
			skipped++
			continue
		}

		exactSet[l.Content] = true
		normSet[norm] = true
		newIdx := len(s.Entries)
		id := s.nextID()
		s.forgetResetID(id)
		s.Entries = append(s.Entries, Entry{
			ID:            id,
			SourceFeature: featureID,
			SourceRole:    sourceRole,
			Category:      l.Category,
			Content:       l.Content,
			CreatedAt:     now,
			Status:        StatusCandidate,
			Confidence:    InitialConfidence,
		})
		tokenSets = append(tokenSets, indexedTokens{idx: newIdx, tokens: tokens})
		bucket[key]++
		added++
	}
	return added, skipped
}

// bucketKey 組出 (SourceFeature, Category) 桶的 key，以 NUL 分隔避免兩段內容黏連而誤判同桶。
func bucketKey(sourceFeature string, category Category) string {
	return sourceFeature + "\x00" + string(category)
}

// forgetResetID 把 id 從 IneffectiveResetIDs 移除。nextID 會回收已刪除條目的序號，
// 新條目若拿到被回收的 ID，`4x learn list --ineffective-reset` 會把這條與 migration 無關的
// 新條目誤列為「被 v1→v2 一次性重設過」。新條目必定不是被重設的舊條目，故寫入時直接移除。
func (s *Store) forgetResetID(id string) {
	for i, rid := range s.IneffectiveResetIDs {
		if rid == id {
			s.IneffectiveResetIDs = append(s.IneffectiveResetIDs[:i], s.IneffectiveResetIDs[i+1:]...)
			return
		}
	}
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

// DemoteInactiveActive 把久未命中的 active 條目 demote 回 candidate（交由 F147 candidate 老化後續處理），
// 回傳 demote 數量。時間基準依序：LastUsed 非零 → LastUsed；否則 ActivatedAt 非零 → ActivatedAt；再否則 CreatedAt。
// maxIdleDays<=0 視為停用（回 0，不動任何條目）。promoted/stale/既有 candidate 一律不碰——
// active 老化不再直接進 stale（見 F159）。
func (s *Store) DemoteInactiveActive(maxIdleDays int) int {
	if maxIdleDays <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-time.Duration(maxIdleDays) * 24 * time.Hour)
	demoted := 0
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Status != StatusActive {
			continue
		}
		ref := e.LastUsed
		if ref.IsZero() {
			ref = e.ActivatedAt
		}
		if ref.IsZero() {
			ref = e.CreatedAt
		}
		if ref.Before(cutoff) {
			e.Status = StatusCandidate
			demoted++
		}
	}
	return demoted
}

// MarkCandidatesStale 把「從未被使用（UsedCount==0）且 CreatedAt 超過 maxIdleDays 天」的
// candidate 條目標記為 stale，回傳新標記數。maxIdleDays <= 0 時視為停用老化，直接回傳 0 不動任何條目。
// 只掃 status==candidate；active/promoted/既有 stale 一律不碰（見 F147 約束）。
func (s *Store) MarkCandidatesStale(maxIdleDays int) int {
	if maxIdleDays <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-time.Duration(maxIdleDays) * 24 * time.Hour)
	marked := 0
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Status == StatusCandidate && e.UsedCount == 0 && e.CreatedAt.Before(cutoff) {
			e.Status = StatusStale
			marked++
		}
	}
	return marked
}

// ActiveEntries 回傳所有 status==active 且非 ineffective 的條目，保持原始順序。
func (s *Store) ActiveEntries() []Entry {
	var result []Entry
	for _, e := range s.Entries {
		if e.Status == StatusActive && !e.Ineffective {
			result = append(result, e)
		}
	}
	return result
}

// AllActiveEntries 回傳所有 status==active 的條目（**含** Ineffective==true 者），保持原始順序。
// 與 ActiveEntries 的差異：ActiveEntries 會過濾掉 ineffective 條目，因為那是 prompt 注入口徑
// ——ineffective 的語意就是「不要再注入」。本方法供 consolidate 判定與 consolidate 輸入使用：
// consolidate 衡量的是「該不該合併的資料總量」，被 merge/remove 的正是那批 ineffective 條目，
// 排除它們會讓 consolidate 永遠拿不到要處理的對象。不供 prompt 注入使用。
func (s *Store) AllActiveEntries() []Entry {
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

// UpdateUsage 更新指定 ID 的 LastUsed 為現在、UsedCount 遞增，並提升 Confidence，
// 標示這些 learnings 被選用（注入 role prompt）過。未傳入的 ID 不變。
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
			s.Entries[i].Confidence = reinforceConfidence(s.Entries[i].Confidence)
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
	"mini-coder":      {CategoryCodeQuality, CategoryTooling, CategoryOps},
	"acceptor":        {CategoryProcess},
}

// CategoriesForRole 回傳指定 role 應注入的 category 列表；未知 role 回傳 nil。
func CategoriesForRole(role string) []Category {
	return roleCategoryMap[role]
}

// activationRef 回傳條目的啟用時間基準：ActivatedAt 非零時用它，否則退回 CreatedAt。
// ReevaluateIneffective 的 30 天觀察期閘門與 hasRecurrenceEvidence 的「證據不早於啟用時間」閘門
// 必須用同一個基準，故集中在此定義，避免兩處各留一份 fallback 而日後靜默錯位。
func (e Entry) activationRef() time.Time {
	if e.ActivatedAt.IsZero() {
		return e.CreatedAt
	}
	return e.ActivatedAt
}

// ReevaluateIneffective 對所有 active 條目**雙向重評** Ineffective 旗標，回傳 (marked, reset)：
// marked 是本次由 false 轉 true 的數量，reset 是本次由 true 轉 false 的數量。
// 三條件全部成立才算 ineffective：
//  1. UsedCount >= 3（已被注入多次）
//  2. ActivatedAt（零值時用 CreatedAt）距今 > 30 天（有足夠觀察期）
//  3. hasRecurrenceEvidence 成立（相似內容仍反覆從相異 feature 冒出來）
//
// 與舊版 MarkIneffective 的關鍵差異是可逆：條件不再全部成立時立刻把旗標撤銷回 false，
// 而非把 ineffective 當成「進去就出不來」的終態。非 active 條目一律不碰。
func (s *Store) ReevaluateIneffective() (marked, reset int) {
	now := time.Now()
	cutoff := now.Add(-30 * 24 * time.Hour)

	// tokenCache 由此處預建：hasRecurrenceEvidence 對每個 active 條目都要掃全部 Entries，
	// 不快取就是 O(active × total) 次 tokenize。沿用 Harvest 的 tokenSets 預建模式。
	tokenCache := make([]map[string]bool, len(s.Entries))
	for i := range s.Entries {
		tokenCache[i] = tokenize(s.Entries[i].Content)
	}

	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Status != StatusActive {
			continue
		}
		want := e.UsedCount >= 3 && !e.activationRef().After(cutoff) && s.hasRecurrenceEvidence(i, tokenCache)
		switch {
		case want && !e.Ineffective:
			e.Ineffective = true
			marked++
		case !want && e.Ineffective:
			e.Ineffective = false
			reset++
		}
	}
	return marked, reset
}

// hasRecurrenceEvidence 判定 target 條目是否仍有「同一教訓反覆重演」的證據：
// 掃描全部 Entries，找出 status 為 active 或 candidate、SourceFeature 非空且與 target 相異、
// CreatedAt 不早於 target 啟用時間、同 category、且與 target 內容 Jaccard 相似度
// >= RecurrenceSimilarityThreshold 的條目，收集其 SourceFeature；
// 相異 feature 數達 RecurrenceMinDistinctFeatures 即成立。
//
// 用獨立的 RecurrenceSimilarityThreshold 而非 FuzzyDupThreshold：harvest 期去重保證
// store 內任兩條的 Jaccard < FuzzyDupThreshold，沿用該門檻會讓判定永遠不成立（見該常量說明）。
// 要求「相異 feature」是因為單一 feature 一次吐出多條同 category 心得不構成「反覆重演」的證據。
//
// tokenCache 須由呼叫端預建且與 s.Entries 同索引；本函式不呼叫 tokenize。
func (s *Store) hasRecurrenceEvidence(targetIdx int, tokenCache []map[string]bool) bool {
	target := s.Entries[targetIdx]
	activated := target.activationRef()
	targetTokens := tokenCache[targetIdx]

	features := make(map[string]bool)
	for j := range s.Entries {
		if j == targetIdx {
			continue
		}
		other := s.Entries[j]
		if other.Status != StatusActive && other.Status != StatusCandidate {
			continue
		}
		if other.SourceFeature == "" || other.SourceFeature == target.SourceFeature {
			continue
		}
		if other.CreatedAt.Before(activated) {
			continue
		}
		// category 是便宜的前置過濾，擋掉大部分不需要算相似度的組合。
		if other.Category != target.Category {
			continue
		}
		if JaccardSimilarity(targetTokens, tokenCache[j]) < RecurrenceSimilarityThreshold {
			continue
		}
		features[other.SourceFeature] = true
		if len(features) >= RecurrenceMinDistinctFeatures {
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
