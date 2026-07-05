package prompt

import (
	"fmt"
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
