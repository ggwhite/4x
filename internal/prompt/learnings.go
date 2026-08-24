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

	"github.com/ggwhite/4x/internal/evolution"
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

const (
	// ActiveLearningsQuota 是每角色 prompt 注入的 active learnings 上限。
	ActiveLearningsQuota = 28
	// CandidateLearningsQuota 是每角色 prompt 注入的 candidate learnings 保底名額，不受 active 桶大小擠壓。
	CandidateLearningsQuota = 12
	// LearningsTokenBudget 是 learnings 注入 prompt 與寫入 learnings-context.md 的近似 token 預算上限，
	// 超出時先截斷低分/較舊條目；deterministic 估算，不引入 tokenizer 依賴。
	LearningsTokenBudget = 1800
)

// EstimateLearningTokens 以近似法估算一條 learning 注入時佔用的 token 數（約每 4 個 rune 一個 token，
// 另加固定 overhead 涵蓋前綴與換行），deterministic 且不依賴外部 tokenizer。
func EstimateLearningTokens(e learning.Entry) int {
	return len([]rune(e.Content))/4 + 8
}

// rankLearnings 就地依 confidence score（高→低）、recency（新→舊）、ID（asc）排序，deterministic
// 不依賴 map iteration order。供 LoadLearningsForRole 與 GenerateLearningsContext 共用同一 ranking 語意。
func rankLearnings(entries []learning.Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		si, sj := entries[i].SortScore(), entries[j].SortScore()
		if si != sj {
			return si > sj
		}
		ri, rj := learningRecency(entries[i]), learningRecency(entries[j])
		if !ri.Equal(rj) {
			return ri.After(rj)
		}
		return entries[i].ID < entries[j].ID
	})
}

// budgetPrefixIDs 依序累計 entries 的 token 估算，一旦累加超過 budget 即停止，回傳存活條目的
// ID 集合。呼叫端須自行決定傳入順序（全域排序或維持原輸入序），此函式只負責「累加直到超過 budget
// 就停」這一份核心邏輯，供 selectWithinBudget 與 budgetSurvivors 共用。
// budget 為硬上限：若首筆估算已超過 budget，回傳空集合而非破例保留該單筆，
// 確保注入內容不突破 LearningsTokenBudget（前一輪 review 反例）。
func budgetPrefixIDs(entries []learning.Entry, budget int) map[string]bool {
	survivors := make(map[string]bool, len(entries))
	total := 0
	for _, e := range entries {
		t := EstimateLearningTokens(e)
		if total+t > budget {
			break
		}
		total += t
		survivors[e.ID] = true
	}
	return survivors
}

// selectWithinBudget 依序累計 token 估算，回傳嚴格不超過 budget 的前綴，
// entries 須已依優先序排好——被截斷者即為排序在後的低分/較舊條目。
// 核心累加邏輯見 budgetPrefixIDs；此處再依原輸入順序過濾回 slice，保留呼叫端需要的順序。
func selectWithinBudget(entries []learning.Entry, budget int) []learning.Entry {
	survivors := budgetPrefixIDs(entries, budget)
	if len(survivors) == 0 {
		return nil
	}
	kept := make([]learning.Entry, 0, len(survivors))
	for _, e := range entries {
		if survivors[e.ID] {
			kept = append(kept, e)
		}
	}
	return kept
}

// budgetSurvivors 依全域 ranking（confidence score 優先、recency 次之、ID tie-breaker）貪婪選取
// entries，回傳嚴格不超過 budget 的存活 entry ID 集合。
// 與 selectWithinBudget 的差異：後者只保留輸入序列前綴，若輸入是 active/candidate 交錯序，
// budget 滿時會保留交錯前綴而非全域最高分——可能截掉排在交錯序後段的高分 active。
// budgetSurvivors 先對整個合併集合重新全域排序再貪婪選取，確保 budget 超出時「先淘汰全域最低分/最舊」，
// 供呼叫端在保留原顯示順序的前提下據此過濾。
// budget 為硬上限：若最高分那筆估算已超過 budget，回傳空集合而非破例保留該單筆，
// 確保注入內容不突破 LearningsTokenBudget（前一輪 review 反例）。
// 核心累加邏輯見 budgetPrefixIDs。
func budgetSurvivors(merged []learning.Entry, budget int) map[string]bool {
	ranked := make([]learning.Entry, len(merged))
	copy(ranked, merged)
	rankLearnings(ranked)
	return budgetPrefixIDs(ranked, budget)
}

// LoadLearningsForRole 依 role 對應的 category，從 learnings.json 篩選出該角色相關的 learnings，
// 分 active/candidate 兩桶取配額後交錯合併回傳（純讀取，不寫入 store）。
// 兩桶各依 confidence score 優先、recency 次之、ID tie-breaker 排序（見 rankLearnings），
// active 取前 ActiveLearningsQuota 筆，candidate 取前 CandidateLearningsQuota 筆為保底名額不受 active 桶大小影響；
// 兩桶交錯合併（round-robin）避免整段 active 排在整段 candidate 之前；交錯序僅決定「顯示順序」，
// budget 截斷則另以全域 ranking 判定存活（見 budgetSurvivors），確保超出 LearningsTokenBudget 時
// 先淘汰全域最低分/最舊條目、而非只保留交錯前綴——避免高分 active 被排在後段的低分 candidate 擠出預算。
// 最終回傳「交錯顯示順序中通過 budget 的存活條目」（只影響回傳 slice，不改 learnings.json）。
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
		// 注意：learning.Store.AllActiveEntries() 含 ineffective 條目、供 consolidate 判定與輸入使用，
		// **不是**本 switch 的鏡像對象——本 switch 是 prompt 注入口徑，ineffective 的語意就是不再注入。
		switch {
		case e.Status == learning.StatusActive && !e.Ineffective:
			active = append(active, e)
		case e.Status == learning.StatusCandidate:
			candidates = append(candidates, e)
		}
	}

	rankLearnings(active)
	rankLearnings(candidates)

	if len(active) > ActiveLearningsQuota {
		active = active[:ActiveLearningsQuota]
	}
	if len(candidates) > CandidateLearningsQuota {
		candidates = candidates[:CandidateLearningsQuota]
	}

	merged := interleaveLearnings(active, candidates)
	survivors := budgetSurvivors(merged, LearningsTokenBudget)
	result := make([]learning.Entry, 0, len(merged))
	for _, e := range merged {
		if survivors[e.ID] {
			result = append(result, e)
		}
	}
	return result
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

	// active 老化改為 demote 回 candidate（F159），不再直接標 stale；候選老化與 prune
	// （見下方）沿用同一份設定。設定載入失敗只 warn/skip 這整段，不阻塞 harvest
	// （learnings 屬 nice-to-have）。
	demoted := 0
	staleMarked, pruned := 0, 0
	if cfg, cerr := ws.LoadMergedConfig(); cerr != nil {
		slog.Warn("load config for active demote failed, skip demote/prune", "error", cerr)
	} else {
		resolved := evolution.ResolveEvolution(cfg)

		preActive := make(map[string]bool, len(store.Entries))
		for _, e := range store.Entries {
			if e.Status == learning.StatusActive {
				preActive[e.ID] = true
			}
		}
		if demoted = store.DemoteInactiveActive(resolved.ActiveDemoteDays); demoted > 0 {
			slog.Info("demoted inactive active learnings", "feature", featureID, "demoted", demoted)
		}
		demotedThisRound := make(map[string]bool, demoted)
		for _, e := range store.Entries {
			if preActive[e.ID] && e.Status == learning.StatusCandidate {
				demotedThisRound[e.ID] = true
			}
		}

		// 正常 4x run 流程從不呼叫 Prune，此前只有 `4x learn prune` 手動觸發，
		// MarkCandidatesStale 標出的 stale 條目在 store 內無限累積（F187 gap）。
		// 在每次 harvest 收尾順帶做候選老化 + prune，讓生命週期自動運轉。
		// 保護本輪剛 demote 回 candidate 的條目，避免同一輪就被老化判 stale 刪除（比照
		// cmd/4x/learn.go 的 prune 邏輯，同一份 F147 約束）。
		staleMarked = store.MarkCandidatesStale(resolved.CandidateMaxIdleDays)
		for i := range store.Entries {
			if demotedThisRound[store.Entries[i].ID] && store.Entries[i].Status == learning.StatusStale {
				store.Entries[i].Status = learning.StatusCandidate
				staleMarked--
			}
		}
		if staleMarked > 0 {
			pruned = store.Prune()
			slog.Info("aged and pruned stale candidate learnings", "feature", featureID,
				"staleMarked", staleMarked, "pruned", pruned)
		}
	}

	roleAdded, roleSkipped := harvestRoleLearnings(&store, ws, featureID)
	retroAdded, retroSkipped := harvestRetroLearnings(&store, ws, featureID)
	totalAdded := roleAdded + retroAdded
	totalSkipped := roleSkipped + retroSkipped

	// 桶上限的 log 必須在早退檢查之前：added == 0 且 skipped > 0（整輪心得全被上限擋掉）
	// 正是本 log 最主要的用途，放在早退之後等於永遠不印。
	if totalSkipped > 0 {
		slog.Info("harvest skipped over-quota learnings", "feature", featureID,
			"skipped", totalSkipped, "cap", learning.MaxPerFeatureCategory)
	}

	marked, resetCount := store.ReevaluateIneffective()
	if marked > 0 || resetCount > 0 {
		slog.Info("reevaluated ineffective learnings", "feature", featureID,
			"marked", marked, "reset", resetCount)
	}

	// totalSkipped 不納入早退條件——被上限擋掉不代表 store 有變動。
	// store.MigrationApplied() 為 true 時即使四個計數器全為 0 也必須存檔，
	// 否則 v1→v2 的一次性重設永遠不會落地。
	// store.CrossFeaturePromoted() 同理：cross-feature fuzzy 升級（candidate→active）
	// 不計入 totalAdded/skipped，若不檢查會讓一輪全數命中升級路徑的 harvest 略過存檔，
	// 升級結果被靜默丟棄（F187 gap）。pruned > 0 代表本輪已刪除條目，同樣須存檔。
	if totalAdded == 0 && marked == 0 && resetCount == 0 && demoted == 0 && pruned == 0 &&
		!store.MigrationApplied() && !store.CrossFeaturePromoted() {
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

func harvestRetroLearnings(store *learning.Store, ws *protocol.Workspace, featureID string) (added, skipped int) {
	retroPath := filepath.Join(ws.FeatureDir(featureID), protocol.RetroLearningsFile)
	learnings, err := learning.ParseRetroFile(retroPath)
	if err != nil {
		slog.Warn("skip retro learnings harvest", "feature", featureID, "error", err)
		return 0, 0
	}
	if len(learnings) == 0 {
		return 0, 0
	}
	added, skipped = store.Harvest(featureID, "acceptor", learnings)
	if added > 0 {
		slog.Debug("harvested retro learnings", "feature", featureID, "added", added)
	}
	return added, skipped
}

func harvestRoleLearnings(store *learning.Store, ws *protocol.Workspace, featureID string) (int, int) {
	roundsDir := filepath.Join(ws.FeatureDir(featureID), protocol.RoundsDir)
	entries, err := os.ReadDir(roundsDir)
	if err != nil {
		return 0, 0
	}

	totalAdded, totalSkipped := 0, 0
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
			added, skipped := store.Harvest(featureID, role, learnings)
			if added > 0 {
				slog.Debug("harvested role learnings", "feature", featureID, "role", role, "round", entry.Name(), "added", added)
			}
			// 兩個累加都必須在 guard 之外：added == 0、skipped > 0 是整輪心得全被桶上限
			// 擋掉的樣態，累加寫進 guard 內會讓最需要被觀測的情境回報 skipped == 0。
			totalAdded += added
			totalSkipped += skipped
		}
	}
	return totalAdded, totalSkipped
}

// NeedConsolidate 檢查 active learnings（含 ineffective）是否超過 consolidate 門檻，
// 且已過冷卻期（ConsolidateCooldown）。冷卻檢查在前：v1→v2 migration 把 ineffective
// 洗回 false 後，大型 store 的 active 總數恆 ≥ ConsolidateThreshold，若無冷卻，每個
// feature 走到 pending-review/done 都會觸發一次 120 秒的 LLM consolidate 呼叫（F187 gap）。
// 以 AllActiveEntries 而非 ActiveEntries 計數：被 consolidate merge/remove 的正是那批 ineffective 條目，
// 用注入口徑計數會讓 store 塞滿 ineffective 時永遠觸發不了 consolidate。
func NeedConsolidate(ws *protocol.Workspace) bool {
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		return false
	}
	if !store.LastConsolidateAt.IsZero() && time.Since(store.LastConsolidateAt) < learning.ConsolidateCooldown {
		return false
	}
	return len(store.AllActiveEntries()) >= learning.ConsolidateThreshold
}

// PrepareConsolidateInput 將 active learnings（含 ineffective）寫入 .4x/consolidate-input.json，
// 供 consolidate runner 讀取。輸入集合與 NeedConsolidate 的判定集合一致（皆為 AllActiveEntries），
// 每筆額外帶 ineffective 布林欄位，讓 consolidator 優先處理那批條目。
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
		// Ineffective 不加 omitempty：templates/consolidator.md.tmpl 宣告每筆都帶此欄位，
		// 省略會讓 consolidator 把「缺席」讀成「未知」而非 false，影響 merge/remove 判斷。
		Ineffective bool `json:"ineffective"`
	}

	active := store.AllActiveEntries()
	entries := make([]inputEntry, len(active))
	for i, e := range active {
		entries[i] = inputEntry{
			ID:            e.ID,
			SourceFeature: e.SourceFeature,
			Category:      e.Category,
			Content:       e.Content,
			UsedCount:     e.UsedCount,
			Ineffective:   e.Ineffective,
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

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		return 0, 0, err
	}

	// LastConsolidateAt 必須在每一條 exit path 都蓋章存檔——包含下面兩個 0/0 早退路徑。
	// 若只在「真的合併/移除了什麼」的成功路徑蓋章，NeedConsolidate 的冷卻期形同虛設：
	// 一次 0/0 的 consolidate 呼叫完全不改 store，下一輪 active 數若仍 ≥ 門檻會立刻再觸發（F187 gap）。
	store.LastConsolidateAt = time.Now()

	if len(result.Actions) == 0 {
		slog.Info("consolidate produced no actions", "path", resultPath)
		if err := store.Save(storePath); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	}

	merged, removed := store.ApplyConsolidation(result.Actions)
	if merged+removed == 0 {
		slog.Info("consolidate actions matched no entries", "actions", len(result.Actions))
		if err := store.Save(storePath); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	}

	store.Prune()
	if err := store.Save(storePath); err != nil {
		return 0, 0, err
	}
	return merged, removed, nil
}

// GenerateLearningsContext 產生 .4x/learnings-context.md，按 category 分組列出 active learnings。
// 只輸出 active 且非 ineffective（ActiveEntries 已過濾），排除 candidate/stale/promoted。
// 先以與 LoadLearningsForRole 一致的 ranking（confidence 優先、recency 次之、ID tie-breaker）全域排序，
// 依 LearningsTokenBudget 保留高分/新鮮條目後再依 category 分組輸出，超出預算的低分條目不輸出。
// 無 active entries 時產生只含 header 的空檔。
func GenerateLearningsContext(ws *protocol.Workspace) error {
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		return fmt.Errorf("load learnings: %w", err)
	}

	active := store.ActiveEntries()
	rankLearnings(active)
	active = selectWithinBudget(active, LearningsTokenBudget)

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
