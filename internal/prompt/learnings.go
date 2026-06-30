package prompt

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

// selectedLearningsPayload 是 selected-learnings.json 的結構。
type selectedLearningsPayload struct {
	Selected []string `json:"selected"`
}

// LoadActiveLearnings 讀取 learnings.json 中所有 active + candidate 條目，供 Designer prompt 列出供選擇。
// active 條目排前，candidate 排後。任何讀取失敗只 warn 並回傳 nil，不影響 prompt 產生。
func LoadActiveLearnings(dotDir string) []learning.Entry {
	storePath := filepath.Join(dotDir, protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings for prompt failed", "error", err)
		return nil
	}
	active := store.ActiveEntries()
	candidates := store.CandidateEntries()
	if len(candidates) == 0 {
		return active
	}
	result := make([]learning.Entry, 0, len(active)+len(candidates))
	result = append(result, active...)
	result = append(result, candidates...)
	return result
}

// LoadSelectedLearnings 讀取 selected-learnings.json，濾出符合 role category 的 active 條目。
func LoadSelectedLearnings(dotDir, featureID string, role protocol.Role) []learning.Entry {
	selPath := filepath.Join(dotDir, protocol.RunDir, featureID, protocol.SelectedLearningsFile)
	data, err := os.ReadFile(selPath)
	if err != nil {
		return nil
	}

	var payload selectedLearningsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		slog.Warn("parse selected-learnings.json failed", "error", err)
		return nil
	}
	if len(payload.Selected) == 0 {
		return nil
	}

	storePath := filepath.Join(dotDir, protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store for injection failed", "error", err)
		return nil
	}

	entryMap := make(map[string]learning.Entry, len(store.Entries))
	for _, e := range store.Entries {
		entryMap[e.ID] = e
	}

	categories := learning.CategoriesForRole(string(role))
	catSet := make(map[learning.Category]bool, len(categories))
	for _, c := range categories {
		catSet[c] = true
	}

	var result []learning.Entry
	for _, id := range payload.Selected {
		if len(result) >= learning.MaxSelectedPerRole {
			break
		}
		e, ok := entryMap[id]
		if !ok || (e.Status != learning.StatusActive && e.Status != learning.StatusCandidate) {
			continue
		}
		if !catSet[e.Category] {
			continue
		}
		result = append(result, e)
	}
	return result
}

// HarvestLearnings 收割 feature 的所有 learnings 並追加到 .4x/learnings.json。
// 來源有二：(1) 各角色在 round 目錄產出的 role-learnings.json，(2) Acceptor 的 retro-learnings.json。
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
		rlPath := filepath.Join(roundsDir, entry.Name(), protocol.RoleLearningsFileName)
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

// UpdateLearningsUsage 在第一個非 Designer phase 時呼叫一次：讀 selected-learnings.json，
// 更新被選中 learning 的 last_used 與 used_count。任何失敗只 warn，不影響 state transition。
func UpdateLearningsUsage(ws *protocol.Workspace, featureID string) {
	selPath := filepath.Join(ws.FeatureDir(featureID), protocol.SelectedLearningsFile)
	data, err := os.ReadFile(selPath)
	if err != nil {
		return
	}

	var payload selectedLearningsPayload
	if err := json.Unmarshal(data, &payload); err != nil || len(payload.Selected) == 0 {
		return
	}

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store for usage update failed", "error", err)
		return
	}

	store.UpdateUsage(payload.Selected)
	store.PromoteCandidates(payload.Selected)
	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store after usage update failed", "error", err)
	}
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
