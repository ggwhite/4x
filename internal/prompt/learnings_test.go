package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

// entryIDSet 把 entries 轉成 ID 集合，方便測試斷言「包含/不包含」某些 ID。
func entryIDSet(entries []learning.Entry) map[string]bool {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[e.ID] = true
	}
	return set
}

func saveLearningsStore(t *testing.T, ws *protocol.Workspace, entries []learning.Entry) string {
	t.Helper()
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: entries}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}
	return storePath
}

func newTestWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	return &protocol.Workspace{Root: root}
}

// TestLoadLearningsForRole_FiltersByCategory 驗證每角色只回傳自己 CategoriesForRole
// 內的條目，且明確不含其他 category（正反兩面），涵蓋 designer{design,process} 與
// reviewer{code-quality,review} 這兩個彼此不相交的角色（AC-2）。
func TestLoadLearningsForRole_FiltersByCategory(t *testing.T) {
	ws := newTestWorkspace(t)
	base := time.Now()
	saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "design learning", Status: learning.StatusActive, CreatedAt: base},
		{ID: "L002", Category: learning.CategoryProcess, Content: "process learning", Status: learning.StatusActive, CreatedAt: base},
		{ID: "L003", Category: learning.CategoryCodeQuality, Content: "code quality learning", Status: learning.StatusActive, CreatedAt: base},
		{ID: "L004", Category: learning.CategoryReview, Content: "review learning", Status: learning.StatusActive, CreatedAt: base},
	})

	designerIDs := entryIDSet(LoadLearningsForRole(ws.DotDir(), protocol.RoleDesigner))
	if !designerIDs["L001"] || !designerIDs["L002"] {
		t.Errorf("designer should see design/process entries, got %v", designerIDs)
	}
	if designerIDs["L003"] || designerIDs["L004"] {
		t.Errorf("designer should NOT see code-quality/review entries, got %v", designerIDs)
	}

	reviewerIDs := entryIDSet(LoadLearningsForRole(ws.DotDir(), protocol.RoleReviewer))
	if !reviewerIDs["L003"] || !reviewerIDs["L004"] {
		t.Errorf("reviewer should see code-quality/review entries, got %v", reviewerIDs)
	}
	if reviewerIDs["L001"] || reviewerIDs["L002"] {
		t.Errorf("reviewer should NOT see design/process entries, got %v", reviewerIDs)
	}
}

// TestLoadLearningsForRole_UnknownRoleReturnsNil 驗證無對應 category 的角色（如未在
// roleCategoryMap 定義的 synthesizer）回傳 nil。
func TestLoadLearningsForRole_UnknownRoleReturnsNil(t *testing.T) {
	ws := newTestWorkspace(t)
	saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "x", Status: learning.StatusActive, CreatedAt: time.Now()},
	})

	result := LoadLearningsForRole(ws.DotDir(), protocol.Role("synthesizer"))
	if result != nil {
		t.Errorf("expected nil for role without category mapping, got %v", result)
	}
}

// TestLoadLearningsForRole_CandidateQuotaNotSquashed 驗證即使 active 遠超過配額，
// candidate 桶仍能取滿 CandidateLearningsQuota 筆保底名額（AC-3）。
func TestLoadLearningsForRole_CandidateQuotaNotSquashed(t *testing.T) {
	ws := newTestWorkspace(t)
	base := time.Now()

	var entries []learning.Entry
	for i := 0; i < 45; i++ {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("A%03d", i), Category: learning.CategoryCodeQuality, Content: "active learning",
			Status: learning.StatusActive, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	for i := 0; i < 15; i++ {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("C%03d", i), Category: learning.CategoryCodeQuality, Content: "candidate learning",
			Status: learning.StatusCandidate, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	saveLearningsStore(t, ws, entries)

	result := LoadLearningsForRole(ws.DotDir(), protocol.RoleReviewer)
	activeCount, candidateCount := 0, 0
	for _, e := range result {
		switch e.Status {
		case learning.StatusActive:
			activeCount++
		case learning.StatusCandidate:
			candidateCount++
		}
	}
	if candidateCount != CandidateLearningsQuota {
		t.Errorf("candidate count = %d, want %d (must not be squeezed by active surplus)", candidateCount, CandidateLearningsQuota)
	}
	if activeCount != ActiveLearningsQuota {
		t.Errorf("active count = %d, want %d", activeCount, ActiveLearningsQuota)
	}
}

// TestLoadLearningsForRole_Interleaved 驗證兩桶皆非空時，回傳清單並非「所有 candidate
// 都排在所有 active 之後」——round-robin 下首筆 candidate 必出現在 index <= 1（AC-4）。
func TestLoadLearningsForRole_Interleaved(t *testing.T) {
	ws := newTestWorkspace(t)
	base := time.Now()

	var entries []learning.Entry
	for i := 0; i < 5; i++ {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("A%d", i), Category: learning.CategoryTesting, Content: "active",
			Status: learning.StatusActive, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	for i := 0; i < 5; i++ {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("C%d", i), Category: learning.CategoryTesting, Content: "candidate",
			Status: learning.StatusCandidate, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	saveLearningsStore(t, ws, entries)

	result := LoadLearningsForRole(ws.DotDir(), protocol.RoleTester)
	if len(result) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(result))
	}
	foundCandidateEarly := false
	for i := 0; i < 2; i++ {
		if result[i].Status == learning.StatusCandidate {
			foundCandidateEarly = true
		}
	}
	if !foundCandidateEarly {
		t.Errorf("expected a candidate entry within first 2 results (round-robin interleave), got %s, %s",
			result[0].ID, result[1].ID)
	}
}

// TestLoadLearningsForRole_CapAt40 驗證 pool 遠超上限時，回傳清單總數恰好截斷在
// ActiveLearningsQuota+CandidateLearningsQuota=40 筆（AC-5）。
func TestLoadLearningsForRole_CapAt40(t *testing.T) {
	ws := newTestWorkspace(t)
	base := time.Now()

	var entries []learning.Entry
	for i := 0; i < 100; i++ {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("A%03d", i), Category: learning.CategoryOps, Content: "active",
			Status: learning.StatusActive, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	for i := 0; i < 100; i++ {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("C%03d", i), Category: learning.CategoryOps, Content: "candidate",
			Status: learning.StatusCandidate, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	saveLearningsStore(t, ws, entries)

	result := LoadLearningsForRole(ws.DotDir(), protocol.RoleTester)
	want := ActiveLearningsQuota + CandidateLearningsQuota
	if len(result) != want {
		t.Fatalf("len(result) = %d, want %d", len(result), want)
	}
	activeCount, candidateCount := 0, 0
	for _, e := range result {
		switch e.Status {
		case learning.StatusActive:
			activeCount++
		case learning.StatusCandidate:
			candidateCount++
		}
	}
	if activeCount != ActiveLearningsQuota || candidateCount != CandidateLearningsQuota {
		t.Errorf("active=%d candidate=%d, want active=%d candidate=%d", activeCount, candidateCount, ActiveLearningsQuota, CandidateLearningsQuota)
	}
}

// TestLoadLearningsForRole_Deterministic 驗證重跑結果穩定，避免依賴 map iteration
// order 之類的非決定性來源（見 L004）。
func TestLoadLearningsForRole_Deterministic(t *testing.T) {
	ws := newTestWorkspace(t)
	base := time.Now()

	var entries []learning.Entry
	for i := 0; i < 20; i++ {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("L%03d", i), Category: learning.CategoryProcess, Content: "x",
			Status: learning.StatusActive, CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	saveLearningsStore(t, ws, entries)

	first := LoadLearningsForRole(ws.DotDir(), protocol.RoleAcceptor)
	second := LoadLearningsForRole(ws.DotDir(), protocol.RoleAcceptor)
	if len(first) != len(second) {
		t.Fatalf("non-deterministic length: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("non-deterministic order at index %d: %s vs %s", i, first[i].ID, second[i].ID)
		}
	}
}

// TestLoadLearningsForRole_NoSideEffects 驗證 LoadLearningsForRole 純讀取、不寫入
// store：呼叫前後每個 entry 的 LastUsed/UsedCount/Status 完全未變（AC-8 核心）。
func TestLoadLearningsForRole_NoSideEffects(t *testing.T) {
	ws := newTestWorkspace(t)
	created := time.Now().Add(-24 * time.Hour)
	lastUsed := time.Now().Add(-1 * time.Hour)
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryProcess, Content: "a", Status: learning.StatusActive, CreatedAt: created, LastUsed: lastUsed, UsedCount: 3},
		{ID: "L002", Category: learning.CategoryProcess, Content: "b", Status: learning.StatusCandidate, CreatedAt: created},
	})

	_ = LoadLearningsForRole(ws.DotDir(), protocol.RoleAcceptor)

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range reloaded.Entries {
		switch e.ID {
		case "L001":
			if !e.LastUsed.Equal(lastUsed) || e.UsedCount != 3 || e.Status != learning.StatusActive {
				t.Errorf("L001 mutated by LoadLearningsForRole: LastUsed=%v UsedCount=%d Status=%s", e.LastUsed, e.UsedCount, e.Status)
			}
		case "L002":
			if !e.LastUsed.IsZero() || e.UsedCount != 0 || e.Status != learning.StatusCandidate {
				t.Errorf("L002 mutated by LoadLearningsForRole: LastUsed=%v UsedCount=%d Status=%s", e.LastUsed, e.UsedCount, e.Status)
			}
		}
	}
}

// TestGenerate_LearningsNoSideEffects 驗證 prompt.Generate 對 learnings.json 無寫入
// 副作用：呼叫 Generate 後重新載入 store，LastUsed/UsedCount 皆未變（AC-8，裁決 7）。
func TestGenerate_LearningsNoSideEffects(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F999-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	created := time.Now().Add(-24 * time.Hour)
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryCodeQuality, Content: "always wrap errors", Status: learning.StatusActive, CreatedAt: created},
	})

	pctx := &Context{Ws: ws, RunnerWs: ws, Feature: feat.Feature{ID: featureID, Name: "Test"}, Cfg: protocol.Config{}}
	if _, err := Generate(pctx, protocol.RoleReviewer, 1, 0, ""); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Entries[0].LastUsed.IsZero() || reloaded.Entries[0].UsedCount != 0 {
		t.Errorf("Generate mutated learnings store: LastUsed=%v UsedCount=%d", reloaded.Entries[0].LastUsed, reloaded.Entries[0].UsedCount)
	}
}

// TestMarkLearningsUsed_UpdatesUsage 驗證 MarkLearningsUsed 對傳入的每個 ID 更新
// LastUsed/UsedCount，未傳入的 ID 不受影響，且 candidate 的 Status 維持 candidate
// （不呼叫 PromoteCandidates，見裁決 1）（AC-7）。
func TestMarkLearningsUsed_UpdatesUsage(t *testing.T) {
	ws := newTestWorkspace(t)
	created := time.Now().Add(-24 * time.Hour)
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryProcess, Content: "a", Status: learning.StatusActive, CreatedAt: created},
		{ID: "L002", Category: learning.CategoryProcess, Content: "b", Status: learning.StatusCandidate, CreatedAt: created},
		{ID: "L003", Category: learning.CategoryProcess, Content: "c", Status: learning.StatusActive, CreatedAt: created},
	})

	MarkLearningsUsed(ws.DotDir(), []learning.Entry{{ID: "L001"}, {ID: "L002"}})

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]learning.Entry, len(reloaded.Entries))
	for _, e := range reloaded.Entries {
		byID[e.ID] = e
	}

	if byID["L001"].LastUsed.IsZero() || byID["L001"].UsedCount != 1 {
		t.Errorf("L001 not marked used: %+v", byID["L001"])
	}
	if byID["L002"].LastUsed.IsZero() || byID["L002"].UsedCount != 1 {
		t.Errorf("L002 not marked used: %+v", byID["L002"])
	}
	if byID["L002"].Status != learning.StatusCandidate {
		t.Errorf("L002 status should remain candidate (MarkLearningsUsed must not call PromoteCandidates), got %s", byID["L002"].Status)
	}
	if !byID["L003"].LastUsed.IsZero() || byID["L003"].UsedCount != 0 {
		t.Errorf("L003 should be untouched, got %+v", byID["L003"])
	}
}

// TestMarkLearningsUsed_EmptyEntriesNoOp 驗證空 entries 為 no-op，不做任何 I/O、store 檔案不變。
func TestMarkLearningsUsed_EmptyEntriesNoOp(t *testing.T) {
	ws := newTestWorkspace(t)
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryProcess, Content: "a", Status: learning.StatusActive, CreatedAt: time.Now().Add(-24 * time.Hour)},
	})
	origInfo, err := os.Stat(storePath)
	if err != nil {
		t.Fatal(err)
	}

	MarkLearningsUsed(ws.DotDir(), nil)

	newInfo, err := os.Stat(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !newInfo.ModTime().Equal(origInfo.ModTime()) {
		t.Error("MarkLearningsUsed with empty entries should not touch the store file")
	}
}

func TestGenerateLearningsContext_GroupsByCategory(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryOps, Content: "set GOWORK=off in worktree", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "reuse existing structures", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L003", Category: learning.CategoryOps, Content: "check docker daemon first", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L004", Category: learning.CategoryTesting, Content: "test edge cases", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "## design") {
		t.Error("missing design section")
	}
	if !strings.Contains(content, "## ops") {
		t.Error("missing ops section")
	}
	if !strings.Contains(content, "## testing") {
		t.Error("missing testing section")
	}

	designIdx := strings.Index(content, "## design")
	opsIdx := strings.Index(content, "## ops")
	testingIdx := strings.Index(content, "## testing")
	if designIdx > opsIdx || opsIdx > testingIdx {
		t.Errorf("categories not sorted: design=%d ops=%d testing=%d", designIdx, opsIdx, testingIdx)
	}

	opsSection := content[opsIdx:]
	if !strings.Contains(opsSection, "- set GOWORK=off in worktree") {
		t.Error("missing ops entry 1")
	}
	if !strings.Contains(opsSection, "- check docker daemon first") {
		t.Error("missing ops entry 2")
	}
}

func TestGenerateLearningsContext_OnlyActive(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "active entry", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "stale entry", Status: learning.StatusStale, CreatedAt: time.Now()},
		{ID: "L003", Category: learning.CategoryDesign, Content: "promoted entry", Status: learning.StatusPromoted, CreatedAt: time.Now()},
	}}
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "active entry") {
		t.Error("active entry should be included")
	}
	if strings.Contains(content, "stale entry") {
		t.Error("stale entry should not be included")
	}
	if strings.Contains(content, "promoted entry") {
		t.Error("promoted entry should not be included")
	}
}

func TestGenerateLearningsContext_EmptyStore(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "# Learnings") {
		t.Error("missing header")
	}
	if !strings.Contains(content, "No active learnings.") {
		t.Error("missing empty message")
	}
}

func intPtrTest(n int) *int { return &n }

// TestLoadLearningsForRoleConfidenceRankingAndTokenBudget 驗證注入排序以 confidence 優先，
// 且超過 token budget 時截斷低分條目；純讀取無副作用（AC-8）。
func TestLoadLearningsForRoleConfidenceRankingAndTokenBudget(t *testing.T) {
	ws := newTestWorkspace(t)
	pad := strings.Repeat("a", 2000) // 每筆約 508 token，budget 1800 → 只容納 3 筆
	confs := []float64{0.9, 0.7, 0.5, 0.3, 0.1}
	var entries []learning.Entry
	for i, c := range confs {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("L%03d", i), Category: learning.CategoryCodeQuality,
			Content: fmt.Sprintf("E%d %s", i, pad), Status: learning.StatusActive,
			Confidence: c, CreatedAt: time.Now(),
		})
	}
	storePath := saveLearningsStore(t, ws, entries)

	result := LoadLearningsForRole(ws.DotDir(), protocol.RoleReviewer)
	if len(result) != 3 {
		t.Fatalf("token budget should keep 3 entries, got %d", len(result))
	}
	if result[0].Confidence != 0.9 || result[1].Confidence != 0.7 || result[2].Confidence != 0.5 {
		t.Errorf("not ranked by confidence desc: %v %v %v", result[0].Confidence, result[1].Confidence, result[2].Confidence)
	}
	got := entryIDSet(result)
	if got["L003"] || got["L004"] {
		t.Errorf("low-confidence entries should be truncated by budget, got %v", got)
	}

	// 無副作用：confidence 未被寫回。
	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range reloaded.Entries {
		if e.LastUsed.IsZero() == false || e.UsedCount != 0 {
			t.Errorf("%s mutated by read-only load: %+v", e.ID, e)
		}
	}
}

// TestLoadLearningsForRole_MixedBudgetDropsGlobalLowScore 驗證 active/candidate 交錯後，
// budget 截斷仍以全域 confidence 高分優先存活：高分 active 不會被排在交錯序前段的低分 candidate 擠出
// 預算（前一輪 review 反例——交錯序 A(0.9),C(0.1),A(0.8) 在 budget 只容 2 筆時應保留兩筆 active、
// 淘汰低分 candidate，而非保留交錯前綴 A,C）。
func TestLoadLearningsForRole_MixedBudgetDropsGlobalLowScore(t *testing.T) {
	ws := newTestWorkspace(t)
	pad := strings.Repeat("a", 3000) // 每筆約 758 token，budget 1800 → 只容納 2 筆
	base := time.Now()
	// 兩筆高分 active + 一筆低分 candidate。交錯序會把低分 candidate 排到 index 1，
	// 若 budget 只保留交錯前綴會誤留 candidate、截掉高分 active。
	entries := []learning.Entry{
		{ID: "A001", Category: learning.CategoryCodeQuality, Content: "HIGH1 " + pad, Status: learning.StatusActive, Confidence: 0.9, CreatedAt: base},
		{ID: "A002", Category: learning.CategoryCodeQuality, Content: "HIGH2 " + pad, Status: learning.StatusActive, Confidence: 0.8, CreatedAt: base},
		{ID: "C001", Category: learning.CategoryCodeQuality, Content: "LOW " + pad, Status: learning.StatusCandidate, Confidence: 0.1, CreatedAt: base},
	}
	saveLearningsStore(t, ws, entries)

	result := LoadLearningsForRole(ws.DotDir(), protocol.RoleReviewer)
	got := entryIDSet(result)
	if !got["A001"] || !got["A002"] {
		t.Errorf("high-confidence active entries must survive budget, got %v", got)
	}
	if got["C001"] {
		t.Errorf("low-confidence candidate must be truncated first by global budget, got %v", got)
	}
}

// TestLoadLearningsForRole_SingleOversizedEntryNotReturned 驗證單筆估算已超過 LearningsTokenBudget 時，
// budget 為硬上限——不破例保留該單筆（前一輪 review 反例）。
func TestLoadLearningsForRole_SingleOversizedEntryNotReturned(t *testing.T) {
	ws := newTestWorkspace(t)
	// 單筆 pad 讓 EstimateLearningTokens 遠超 budget（budget 1800，此筆約 2508 token）。
	pad := strings.Repeat("a", (LearningsTokenBudget+700)*4)
	entries := []learning.Entry{
		{ID: "BIG1", Category: learning.CategoryCodeQuality, Content: "OVERSIZE " + pad, Status: learning.StatusActive, Confidence: 0.9, CreatedAt: time.Now()},
	}
	saveLearningsStore(t, ws, entries)

	if got := LoadLearningsForRole(ws.DotDir(), protocol.RoleReviewer); len(got) != 0 {
		t.Errorf("oversized single entry must not break budget, got %d entries", len(got))
	}
}

// TestGenerateLearningsContext_SingleOversizedEntryNotOutput 驗證 context 輸出同樣不破例保留單筆超限條目。
func TestGenerateLearningsContext_SingleOversizedEntryNotOutput(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	pad := strings.Repeat("b", (LearningsTokenBudget+700)*4)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "BIG1", Category: learning.CategoryDesign, Content: "OVERSIZE " + pad, Status: learning.StatusActive, Confidence: 0.9, CreatedAt: time.Now()},
	}}
	if err := store.Save(filepath.Join(ws.DotDir(), protocol.LearningsFile)); err != nil {
		t.Fatal(err)
	}
	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(ws.DotDir(), protocol.LearningsContextFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "OVERSIZE") {
		t.Error("oversized single entry must not be output beyond budget")
	}
}

// TestGenerateLearningsContextRanksAndTruncatesActive 驗證 learnings-context.md 依 ranking
// 高分優先輸出，並在超出 budget 時不輸出低分條目（AC-9）。
func TestGenerateLearningsContextRanksAndTruncatesActive(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	pad := strings.Repeat("b", 2000)
	confs := []float64{0.9, 0.7, 0.5, 0.3, 0.1}
	var entries []learning.Entry
	for i, c := range confs {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("L%03d", i), Category: learning.CategoryDesign,
			Content: fmt.Sprintf("MARK%d %s", i, pad), Status: learning.StatusActive,
			Confidence: c, CreatedAt: time.Now(),
		})
	}
	store := learning.Store{Version: 1, Entries: entries}
	if err := store.Save(filepath.Join(ws.DotDir(), protocol.LearningsFile)); err != nil {
		t.Fatal(err)
	}

	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(ws.DotDir(), protocol.LearningsContextFile))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	i0 := strings.Index(content, "MARK0")
	i1 := strings.Index(content, "MARK1")
	i2 := strings.Index(content, "MARK2")
	if i0 < 0 || i1 < 0 || i2 < 0 {
		t.Fatalf("top-3 confidence entries should be present: MARK0=%d MARK1=%d MARK2=%d", i0, i1, i2)
	}
	if !(i0 < i1 && i1 < i2) {
		t.Errorf("entries not ordered by confidence desc: MARK0=%d MARK1=%d MARK2=%d", i0, i1, i2)
	}
	if strings.Contains(content, "MARK3") || strings.Contains(content, "MARK4") {
		t.Error("low-confidence entries should be truncated by budget")
	}
}

// TestHarvestLearningsDemotesActiveNotStale 驗證 harvest 對久未命中 active 執行 demote 回 candidate，
// 而非標成 stale（AC-10）。
func TestHarvestLearningsDemotesActiveNotStale(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{
		Project:   protocol.ProjectConfig{Name: "test"},
		Evolution: &protocol.EvolutionConfig{ActiveDemoteDays: intPtrTest(1)},
	}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryCodeQuality, Content: "stale old active", Status: learning.StatusActive, LastUsed: time.Now().Add(-100 * 24 * time.Hour)},
	})

	HarvestLearnings(ws, "F999-test")

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Entries[0].Status != learning.StatusCandidate {
		t.Errorf("old active should be demoted to candidate, got %s", reloaded.Entries[0].Status)
	}
}

// TestHarvestLearningsDemoteConfigFailureSkips 驗證 merged config 載入失敗時只 skip demote，
// 不阻塞 harvest，且 active 不被改動（AC-10）。
func TestHarvestLearningsDemoteConfigFailureSkips(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "test"}}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryCodeQuality, Content: "old active", Status: learning.StatusActive, LastUsed: time.Now().Add(-100 * 24 * time.Hour)},
	})

	// 破壞 settings.json 讓 LoadMergedConfig 失敗。
	if err := os.WriteFile(filepath.Join(ws.DotDir(), protocol.ConfigFile), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	HarvestLearnings(ws, "F999-test") // 不應 panic

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Entries[0].Status != learning.StatusActive {
		t.Errorf("config failure should skip demote; active must stay active, got %s", reloaded.Entries[0].Status)
	}
}

// TestLoadLearningsForRoleLegacyConfidenceReadOnly 驗證舊 JSON 中 confidence==0 的 entry
// 只透過 fallback score 排序，不因 read-only prompt 載入被隱式寫回（AC-13）。
func TestLoadLearningsForRoleLegacyConfidenceReadOnly(t *testing.T) {
	ws := newTestWorkspace(t)
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", Category: learning.CategoryCodeQuality, Content: "legacy zero-confidence", Status: learning.StatusActive, UsedCount: 2, CreatedAt: time.Now()},
	})

	_ = LoadLearningsForRole(ws.DotDir(), protocol.RoleReviewer)

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Entries[0].Confidence != 0 {
		t.Errorf("legacy confidence should remain 0 after read-only load, got %v", reloaded.Entries[0].Confidence)
	}
}

// saveLearningsStoreV2 以 version 2 寫入 learnings store：consolidate 相關測試必須避開
// LoadStore 的 v1→v2 migration，否則 ineffective 會全被洗成 false，斷言退化成假陽性。
func saveLearningsStoreV2(t *testing.T, ws *protocol.Workspace, entries []learning.Entry) string {
	t.Helper()
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store := learning.Store{Version: learning.StoreVersion, Entries: entries}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}
	return storePath
}

// captureSlog 以 TextHandler 攔截 slog 預設 logger 的輸出，測完還原。
func captureSlog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

// mostlyIneffectiveActives 產生 total 條 active 條目，其中前 ineffectiveCount 條為 ineffective。
func mostlyIneffectiveActives(total, ineffectiveCount int) []learning.Entry {
	entries := make([]learning.Entry, 0, total)
	for i := 0; i < total; i++ {
		entries = append(entries, learning.Entry{
			ID:            fmt.Sprintf("L%03d", i+1),
			SourceFeature: fmt.Sprintf("F%03d", i+1),
			Category:      learning.CategoryCodeQuality,
			Content:       fmt.Sprintf("consolidate fixture entry number %d", i),
			Status:        learning.StatusActive,
			UsedCount:     3,
			CreatedAt:     time.Now(),
			Ineffective:   i < ineffectiveCount,
		})
	}
	return entries
}

// TestNeedConsolidate_CountsIneffective 驗證 consolidate 門檻以 AllActiveEntries（含 ineffective）
// 計數：ActiveEntries 只剩 2 條時仍應觸發，否則 store 塞滿 ineffective 就永遠餓死 consolidate（AC-9）。
func TestNeedConsolidate_CountsIneffective(t *testing.T) {
	ws := newTestWorkspace(t)
	storePath := saveLearningsStoreV2(t, ws, mostlyIneffectiveActives(30, 28))

	if !NeedConsolidate(ws) {
		t.Error("NeedConsolidate = false, want true (30 active entries including ineffective)")
	}

	store, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(store.ActiveEntries()); got != 2 {
		t.Errorf("ActiveEntries() = %d, want 2", got)
	}
	if got := len(store.AllActiveEntries()); got != 30 {
		t.Errorf("AllActiveEntries() = %d, want 30", got)
	}
}

// TestPrepareConsolidateInput_IncludesIneffective 驗證 consolidate 輸入包含 ineffective 條目，
// 且該筆帶 ineffective: true 欄位——被 merge/remove 的正是那批條目（AC-10）。
func TestPrepareConsolidateInput_IncludesIneffective(t *testing.T) {
	ws := newTestWorkspace(t)
	saveLearningsStoreV2(t, ws, mostlyIneffectiveActives(30, 28))

	if err := PrepareConsolidateInput(ws); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(ws.DotDir(), protocol.ConsolidateInputFile))
	if err != nil {
		t.Fatal(err)
	}
	var parsed []struct {
		ID          string `json:"id"`
		Ineffective bool   `json:"ineffective"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse consolidate-input.json: %v", err)
	}
	if len(parsed) != 30 {
		t.Fatalf("consolidate input entries = %d, want 30", len(parsed))
	}
	n := 0
	for _, e := range parsed {
		if e.Ineffective {
			n++
		}
	}
	if n != 28 {
		t.Errorf("ineffective entries in consolidate input = %d, want 28", n)
	}
}

// TestApplyConsolidateResult_LogsNoOp 驗證兩條回 (0, 0, nil) 的 no-op 路徑各輸出可辨識的
// slog.Info 訊息，與 runner 失敗的 slog.Warn 可區分（AC-11）。
func TestApplyConsolidateResult_LogsNoOp(t *testing.T) {
	t.Run("empty actions", func(t *testing.T) {
		ws := newTestWorkspace(t)
		resultPath := filepath.Join(ws.DotDir(), protocol.ConsolidateResultFile)
		if err := os.WriteFile(resultPath, []byte(`{"actions":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		var merged, removed int
		var err error
		out := captureSlog(t, func() {
			merged, removed, err = ApplyConsolidateResult(ws)
		})
		if merged != 0 || removed != 0 || err != nil {
			t.Errorf("got (%d, %d, %v), want (0, 0, nil)", merged, removed, err)
		}
		if !strings.Contains(out, "consolidate produced no actions") {
			t.Errorf("missing no-actions log, got %q", out)
		}
	})

	t.Run("actions match no entries", func(t *testing.T) {
		ws := newTestWorkspace(t)
		saveLearningsStoreV2(t, ws, []learning.Entry{
			{ID: "L001", Category: learning.CategoryOps, Content: "present entry", Status: learning.StatusActive, CreatedAt: time.Now()},
		})
		resultPath := filepath.Join(ws.DotDir(), protocol.ConsolidateResultFile)
		body := `{"actions":[{"id":"L900","action":"remove","reason":"missing"},{"id":"L901","action":"merge","merge_into":"L902","reason":"missing"}]}`
		if err := os.WriteFile(resultPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		var merged, removed int
		var err error
		out := captureSlog(t, func() {
			merged, removed, err = ApplyConsolidateResult(ws)
		})
		if merged != 0 || removed != 0 || err != nil {
			t.Errorf("got (%d, %d, %v), want (0, 0, nil)", merged, removed, err)
		}
		if !strings.Contains(out, "consolidate actions matched no entries") {
			t.Errorf("missing unmatched-actions log, got %q", out)
		}
	})
}

// TestHarvestLearnings_PersistsMigration 驗證即使沒有新增、沒有 mark/reset、沒有 demote，
// 只要 LoadStore 套用過 v1→v2 migration 就必須存檔，否則一次性重設永遠不落地（AC-14）。
func TestHarvestLearnings_PersistsMigration(t *testing.T) {
	ws := newTestWorkspace(t)
	dayAgo := time.Now().Add(-24 * time.Hour)
	storePath := saveLearningsStore(t, ws, []learning.Entry{
		{ID: "L001", SourceFeature: "F001", Category: learning.CategoryCodeQuality, Content: "first legacy ineffective",
			Status: learning.StatusActive, Ineffective: true, UsedCount: 0,
			CreatedAt: dayAgo, ActivatedAt: dayAgo, LastUsed: dayAgo},
		{ID: "L002", SourceFeature: "F002", Category: learning.CategoryCodeQuality, Content: "second legacy ineffective",
			Status: learning.StatusActive, Ineffective: true, UsedCount: 0,
			CreatedAt: dayAgo, ActivatedAt: dayAgo, LastUsed: dayAgo},
	})
	featureID := "F900-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	HarvestLearnings(ws, featureID)

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != learning.StoreVersion {
		t.Errorf("persisted version = %d, want %d", reloaded.Version, learning.StoreVersion)
	}
	for _, e := range reloaded.Entries {
		if e.Ineffective {
			t.Errorf("%s should be persisted with ineffective=false", e.ID)
		}
	}
	got := map[string]bool{}
	for _, id := range reloaded.IneffectiveResetIDs {
		got[id] = true
	}
	if !got["L001"] || !got["L002"] {
		t.Errorf("ineffective_reset_ids = %v, want both L001 and L002", reloaded.IneffectiveResetIDs)
	}
}

// TestHarvestLearnings_CountsSkippedOverQuota 驗證整輪 learnings 全被桶上限擋掉（added==0）時
// totalSkipped 仍正確累加並輸出 log——累加若被 `if added > 0` guard 吞掉，這個最需要被觀測的
// 情境會回報 skipped == 0 且一行 log 都不印（AC-23）。
func TestHarvestLearnings_CountsSkippedOverQuota(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F300"
	existing := []string{
		"always run gofmt before committing go code",
		"prefer table driven tests for enum coverage",
		"wrap errors with context at every boundary",
	}
	entries := make([]learning.Entry, 0, len(existing))
	for i, c := range existing {
		entries = append(entries, learning.Entry{
			ID: fmt.Sprintf("L%03d", i+1), SourceFeature: featureID, Category: learning.CategoryReview,
			Content: c, Status: learning.StatusCandidate, CreatedAt: time.Now(),
		})
	}
	storePath := saveLearningsStoreV2(t, ws, entries)

	roundDir := ws.RoundDir(featureID, 0)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	incoming := []string{
		"avoid global mutable state inside packages",
		"document exported symbols with godoc comments",
		"check goroutine leaks in server integration suites",
		"pin dependency versions in the module file",
		"measure allocations before optimizing hot loops",
		"validate cli flags at parse time not later",
		"keep migration scripts idempotent across reruns",
		"log structured fields rather than formatted strings",
	}
	rf := learning.RoleLearningsFile{Role: "coder"}
	for _, c := range incoming {
		rf.Learnings = append(rf.Learnings, learning.RetroLearning{Category: learning.CategoryReview, Content: c})
	}
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roundDir, "coder-learnings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureSlog(t, func() { HarvestLearnings(ws, featureID) })
	if !strings.Contains(out, "skipped=8") {
		t.Errorf("expected skipped=8 in log output, got %q", out)
	}

	reloaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range reloaded.Entries {
		if e.SourceFeature == featureID && e.Category == learning.CategoryReview {
			n++
		}
	}
	if n != learning.MaxPerFeatureCategory {
		t.Errorf("(F300, review) bucket = %d, want %d", n, learning.MaxPerFeatureCategory)
	}
}

// TestHarvestLearnings_LogsReevaluated 驗證 ReevaluateIneffective 有動到旗標時輸出一行
// slog.Info，讓「本輪撤銷了幾條 ineffective」可觀測（AC-25）。
func TestHarvestLearnings_LogsReevaluated(t *testing.T) {
	ws := newTestWorkspace(t)
	dayAgo := time.Now().Add(-24 * time.Hour)
	saveLearningsStoreV2(t, ws, []learning.Entry{
		{ID: "L001", SourceFeature: "F001", Category: learning.CategoryCodeQuality, Content: "stale ineffective flag",
			Status: learning.StatusActive, Ineffective: true, UsedCount: 0,
			CreatedAt: dayAgo, ActivatedAt: dayAgo, LastUsed: dayAgo},
	})
	featureID := "F901-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	out := captureSlog(t, func() { HarvestLearnings(ws, featureID) })
	for _, want := range []string{"reevaluated ineffective learnings", "marked=0", "reset=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in log output, got %q", want, out)
		}
	}
}
