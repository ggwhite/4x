package prompt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

const (
	// ActiveLearningsQuota 是每角色 prompt 注入的 active learnings 上限。
	ActiveLearningsQuota = 28
	// CandidateLearningsQuota 是每角色 prompt 注入的 candidate learnings 保底名額，不受 active 桶大小擠壓。
	CandidateLearningsQuota = 12
)

// LoadLearningsForRole 依 role 對應的 category，從 learnings.json 篩選出該角色相關的 learnings，
// 分 active/candidate 兩桶取配額後交錯合併回傳（純讀取，不寫入 store）。
// active 桶依「LastUsed 非零則用 LastUsed，否則用 CreatedAt」新到舊排序，取前 ActiveLearningsQuota 筆；
// candidate 桶依 CreatedAt 新到舊排序，取前 CandidateLearningsQuota 筆，為保底名額不受 active 桶大小影響。
// 兩桶交錯合併（round-robin），避免整段 active 排在整段 candidate 之前。
// 讀取失敗或角色無對應 category 時只 warn 並回傳 nil。
func LoadLearningsForRole(dotDir string, role protocol.Role) []learning.Entry {
	storePath := filepath.Join(dotDir, protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings for prompt failed", "error", err)
		return nil
	}

	categories := learning.CategoriesForRole(string(role))
	if len(categories) == 0 {
		return nil
	}
	catSet := make(map[learning.Category]bool, len(categories))
	for _, c := range categories {
		catSet[c] = true
	}

	var active, candidates []learning.Entry
	for _, e := range store.Entries {
		if !catSet[e.Category] {
			continue
		}
		// 此二判定式須與 learning.Store.ActiveEntries()/CandidateEntries() 保持一致；
		// 此處為在單趟迴圈內同時做 category + status 過濾而 inline，日後任一方調整 status 語意時兩處都要同步。
		switch {
		case e.Status == learning.StatusActive && !e.Ineffective:
			active = append(active, e)
		case e.Status == learning.StatusCandidate:
			candidates = append(candidates, e)
		}
	}

	sort.SliceStable(active, func(i, j int) bool {
		return learningRecency(active[i]).After(learningRecency(active[j]))
	})
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})

	if len(active) > ActiveLearningsQuota {
		active = active[:ActiveLearningsQuota]
	}
	if len(candidates) > CandidateLearningsQuota {
		candidates = candidates[:CandidateLearningsQuota]
	}

	return interleaveLearnings(active, candidates)
}

// learningRecency 回傳排序用的參考時間：LastUsed 非零則用 LastUsed，否則用 CreatedAt。
func learningRecency(e learning.Entry) time.Time {
	if !e.LastUsed.IsZero() {
		return e.LastUsed
	}
	return e.CreatedAt
}

// interleaveLearnings 將 active 與 candidate 兩桶 round-robin 交錯合併，任一桶取完後接續另一桶剩餘部分。
func interleaveLearnings(active, candidates []learning.Entry) []learning.Entry {
	result := make([]learning.Entry, 0, len(active)+len(candidates))
	i, j := 0, 0
	for i < len(active) || j < len(candidates) {
		if i < len(active) {
			result = append(result, active[i])
			i++
		}
		if j < len(candidates) {
			result = append(result, candidates[j])
			j++
		}
	}
	return result
}

// MarkLearningsUsed 把 entries 標記為「已注入某角色 prompt」：更新 LastUsed/UsedCount，
// 只呼叫 UpdateUsage，不呼叫 PromoteCandidates——candidate 不因被注入而升級為 active，
// 避免 active/candidate 分桶配額失去意義。entries 為空時為 no-op，不做任何 I/O。任何失敗只 warn。
func MarkLearningsUsed(dotDir string, entries []learning.Entry) {
	if len(entries) == 0 {
		return
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}

	storePath := filepath.Join(dotDir, protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store for usage mark failed", "error", err)
		return
	}
	store.UpdateUsage(ids)
	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store after usage mark failed", "error", err)
	}
}

// HarvestLearnings 收割 feature 的所有 learnings 並追加到 .4x/learnings.json。
// 來源有二：(1) 各角色在 round 目錄產出的 {role}-learnings.json，(2) Acceptor 的 retro-learnings.json。
// 屬 nice-to-have，任何錯誤只 warn，絕不影響 state transition。
func HarvestLearnings(ws *protocol.Workspace, featureID string) {
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store failed", "error", err)
		return
	}

	store.MarkStale(learning.DefaultStaleDays)
	totalAdded := 0

	totalAdded += harvestRoleLearnings(&store, ws, featureID)
	totalAdded += harvestRetroLearnings(&store, ws, featureID)

	ineffective := store.MarkIneffective()

	if totalAdded == 0 && ineffective == 0 {
		return
	}

	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store failed", "error", err)
		return
	}

	if err := GenerateLearningsContext(ws); err != nil {
		slog.Warn("refresh learnings context after harvest failed", "error", err)
	}

	active := len(store.ActiveEntries())
	slog.Info("harvested learnings", "feature", featureID, "added", totalAdded, "total_active", active)
	if active > learning.MaxActiveEntries {
		slog.Warn("learnings store exceeds capacity, consider running '4x learn prune'",
			"active", active, "limit", learning.MaxActiveEntries)
	}
}

func harvestRetroLearnings(store *learning.Store, ws *protocol.Workspace, featureID string) int {
	retroPath := filepath.Join(ws.FeatureDir(featureID), protocol.RetroLearningsFile)
	learnings, err := learning.ParseRetroFile(retroPath)
	if err != nil {
		slog.Warn("skip retro learnings harvest", "feature", featureID, "error", err)
		return 0
	}
	if len(learnings) == 0 {
		return 0
	}
	added := store.Harvest(featureID, "acceptor", learnings)
	if added > 0 {
		slog.Debug("harvested retro learnings", "feature", featureID, "added", added)
	}
	return added
}

func harvestRoleLearnings(store *learning.Store, ws *protocol.Workspace, featureID string) int {
	roundsDir := filepath.Join(ws.FeatureDir(featureID), protocol.RoundsDir)
	entries, err := os.ReadDir(roundsDir)
	if err != nil {
		return 0
	}

	totalAdded := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// glob 出該輪所有 {role}-learnings.json（含舊版 role-learnings.json），
		// 讓同輪多個角色的 learnings 都被收割，而非只留最後寫入者。
		matches, err := filepath.Glob(filepath.Join(roundsDir, entry.Name(), protocol.RoleLearningsGlob))
		if err != nil {
			continue
		}
		for _, rlPath := range matches {
			role, learnings, err := learning.ParseRoleLearningsFile(rlPath)
			if err != nil {
				continue
			}
			if len(learnings) == 0 {
				continue
			}
			added := store.Harvest(featureID, role, learnings)
			if added > 0 {
				slog.Debug("harvested role learnings", "feature", featureID, "role", role, "round", entry.Name(), "added", added)
				totalAdded += added
			}
		}
	}
	return totalAdded
}

// NeedConsolidate 檢查 active learnings 是否超過 consolidate 門檻。
func NeedConsolidate(ws *protocol.Workspace) bool {
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		return false
	}
	return len(store.ActiveEntries()) >= learning.ConsolidateThreshold
}

// PrepareConsolidateInput 將 active learnings 寫入 .4x/consolidate-input.json，供 consolidate runner 讀取。
func PrepareConsolidateInput(ws *protocol.Workspace) error {
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		return err
	}

	type inputEntry struct {
		ID            string            `json:"id"`
		SourceFeature string            `json:"source_feature"`
		Category      learning.Category `json:"category"`
		Content       string            `json:"content"`
		UsedCount     int               `json:"used_count"`
	}

	active := store.ActiveEntries()
	entries := make([]inputEntry, len(active))
	for i, e := range active {
		entries[i] = inputEntry{
			ID:            e.ID,
			SourceFeature: e.SourceFeature,
			Category:      e.Category,
			Content:       e.Content,
			UsedCount:     e.UsedCount,
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	inputPath := filepath.Join(ws.DotDir(), protocol.ConsolidateInputFile)
	return os.WriteFile(inputPath, data, 0o644)
}

// ApplyConsolidateResult 讀取 .4x/consolidate-result.json 並套用到 learnings store。
func ApplyConsolidateResult(ws *protocol.Workspace) (int, int, error) {
	resultPath := filepath.Join(ws.DotDir(), protocol.ConsolidateResultFile)
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return 0, 0, err
	}

	var result learning.ConsolidateResult
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, 0, err
	}
	if len(result.Actions) == 0 {
		return 0, 0, nil
	}

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		return 0, 0, err
	}

	merged, removed := store.ApplyConsolidation(result.Actions)
	if merged+removed == 0 {
		return 0, 0, nil
	}

	store.Prune()
	if err := store.Save(storePath); err != nil {
		return 0, 0, err
	}
	return merged, removed, nil
}

// GenerateLearningsContext 產生 .4x/learnings-context.md，按 category 分組列出所有 active learnings。
// 無 active entries 時產生只含 header 的空檔。
func GenerateLearningsContext(ws *protocol.Workspace) error {
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		return fmt.Errorf("load learnings: %w", err)
	}

	active := store.ActiveEntries()

	var sb strings.Builder
	sb.WriteString("<!-- Auto-generated by 4x learn context. Do not edit manually. -->\n")
	sb.WriteString("# Learnings\n")

	if len(active) == 0 {
		sb.WriteString("\nNo active learnings.\n")
	} else {
		grouped := make(map[learning.Category][]learning.Entry)
		for _, e := range active {
			grouped[e.Category] = append(grouped[e.Category], e)
		}

		categories := make([]string, 0, len(grouped))
		for c := range grouped {
			categories = append(categories, string(c))
		}
		sort.Strings(categories)

		for _, cat := range categories {
			sb.WriteString(fmt.Sprintf("\n## %s\n", cat))
			for _, e := range grouped[learning.Category(cat)] {
				sb.WriteString(fmt.Sprintf("- %s\n", e.Content))
			}
		}
	}

	outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
	return os.WriteFile(outPath, []byte(sb.String()), 0o644)
}
