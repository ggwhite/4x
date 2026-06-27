package prompt

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

// selectedLearningsPayload 是 selected-learnings.json 的結構。
type selectedLearningsPayload struct {
	Selected []string `json:"selected"`
}

// LoadActiveLearnings 讀取 learnings.json 中所有 active 條目，供 Designer prompt 列出供選擇。
// 任何讀取失敗只 warn 並回傳 nil，不影響 prompt 產生。
func LoadActiveLearnings(dotDir string) []learning.Entry {
	storePath := filepath.Join(dotDir, protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings for prompt failed", "error", err)
		return nil
	}
	return store.ActiveEntries()
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
		if !ok || e.Status != learning.StatusActive {
			continue
		}
		if !catSet[e.Category] {
			continue
		}
		result = append(result, e)
	}
	return result
}

// HarvestLearnings 讀取 Acceptor 產出的 retro-learnings.json，追加到 .4x/learnings.json。
// learnings 屬 nice-to-have，任何錯誤只 warn，絕不影響 state transition。
func HarvestLearnings(ws *protocol.Workspace, featureID string) {
	retroPath := filepath.Join(ws.FeatureDir(featureID), protocol.RetroLearningsFile)
	learnings, err := learning.ParseRetroFile(retroPath)
	if err != nil {
		slog.Warn("skip learnings harvest", "feature", featureID, "error", err)
		return
	}
	if len(learnings) == 0 {
		return
	}

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		slog.Warn("load learnings store failed", "error", err)
		return
	}

	store.MarkStale(learning.DefaultStaleDays)
	added := store.Harvest(featureID, learnings)
	if added == 0 {
		return
	}

	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store failed", "error", err)
		return
	}

	active := len(store.ActiveEntries())
	slog.Info("harvested learnings", "feature", featureID, "added", added, "total_active", active)
	if active > learning.MaxActiveEntries {
		slog.Warn("learnings store exceeds capacity, consider running '4x learn prune'",
			"active", active, "limit", learning.MaxActiveEntries)
	}
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
	if err := store.Save(storePath); err != nil {
		slog.Warn("save learnings store after usage update failed", "error", err)
	}
}

