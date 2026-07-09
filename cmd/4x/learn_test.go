package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestLearnPromote(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "test", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	loaded, _ := learning.LoadStore(storePath)
	if err := loaded.Promote("L001"); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Save(storePath); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := learning.LoadStore(storePath)
	if reloaded.Entries[0].Status != learning.StatusPromoted {
		t.Errorf("expected promoted, got %s", reloaded.Entries[0].Status)
	}
}

func TestLearnRemove(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "a", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "b", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	loaded, _ := learning.LoadStore(storePath)
	if err := loaded.Remove("L001"); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Save(storePath); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := learning.LoadStore(storePath)
	if len(reloaded.Entries) != 1 || reloaded.Entries[0].ID != "L002" {
		t.Errorf("expected only L002, got %+v", reloaded.Entries)
	}
}

func TestLearnPrune(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "old", Status: learning.StatusStale, CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "new", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	loaded, _ := learning.LoadStore(storePath)
	removed := loaded.Prune()
	if err := loaded.Save(storePath); err != nil {
		t.Fatal(err)
	}

	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	reloaded, _ := learning.LoadStore(storePath)
	if len(reloaded.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(reloaded.Entries))
	}
}

func ptrInt(n int) *int { return &n }

type prunePayload struct {
	Removed  int      `json:"removed"`
	DryRun   bool     `json:"dryRun"`
	StaleIDs []string `json:"staleIds"`
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// TestLearnPrune_AgesCandidate 驗證 candidate_max_idle_days 被 `4x learn prune` 消費：
// 閾值內的舊未用 candidate 會出現在 dry-run 的 staleIds，且 dry-run 不刪除（AC-8）。
func TestLearnPrune_AgesCandidate(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Evolution: &protocol.EvolutionConfig{CandidateMaxIdleDays: ptrInt(1)}}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "C001", Category: learning.CategoryDesign, Content: "old candidate", Status: learning.StatusCandidate, UsedCount: 0, CreatedAt: time.Now().Add(-48 * time.Hour)},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	out := captureStdout(t, func() {
		cmd := newLearnPruneCmd()
		cmd.SetArgs([]string{"--dry-run", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	var result prunePayload
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output %q: %v", out, err)
	}
	if !contains(result.StaleIDs, "C001") {
		t.Errorf("expected staleIds to contain C001, got %v", result.StaleIDs)
	}
	// dry-run 不得刪除：store 仍保有該 candidate。
	reloaded, _ := learning.LoadStore(storePath)
	if len(reloaded.Entries) != 1 {
		t.Errorf("dry-run should not delete; got %d entries", len(reloaded.Entries))
	}
}

// TestLearnPrune_AgingDisabled 驗證 candidate_max_idle_days=0（停用）時 prune 不老化 candidate（AC-9）。
func TestLearnPrune_AgingDisabled(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "test"}, Evolution: &protocol.EvolutionConfig{CandidateMaxIdleDays: ptrInt(0)}}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "C001", Category: learning.CategoryDesign, Content: "old candidate", Status: learning.StatusCandidate, UsedCount: 0, CreatedAt: time.Now().Add(-48 * time.Hour)},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	out := captureStdout(t, func() {
		cmd := newLearnPruneCmd()
		cmd.SetArgs([]string{"--dry-run", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	var result prunePayload
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output %q: %v", out, err)
	}
	if contains(result.StaleIDs, "C001") {
		t.Errorf("aging disabled (=0) but C001 was aged: %v", result.StaleIDs)
	}
}

// TestLearnList_StatusStale 驗證 `4x learn list --status stale` 能列出 stale 條目（被老化的 candidate；AC-11 / DR-3）。
func TestLearnList_StatusStale(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "S001", Category: learning.CategoryDesign, Content: "aged candidate", Status: learning.StatusStale, CreatedAt: time.Now()},
		{ID: "A001", Category: learning.CategoryDesign, Content: "active", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	out := captureStdout(t, func() {
		cmd := newLearnListCmd()
		cmd.SetArgs([]string{"--status", "stale", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	var result struct {
		Entries []learning.Entry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output %q: %v", out, err)
	}
	if len(result.Entries) != 1 || result.Entries[0].ID != "S001" {
		t.Errorf("expected only S001 stale entry, got %+v", result.Entries)
	}
}

func TestLearnAdd_Success(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	cmd := newLearnAddCmd()
	cmd.SetArgs([]string{"--category", "ops", "--content", "always set GOWORK=off in worktree"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	loaded, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}
	e := loaded.Entries[0]
	if e.SourceFeature != "manual" {
		t.Errorf("expected sourceFeature=manual, got %q", e.SourceFeature)
	}
	if e.SourceRole != "user" {
		t.Errorf("expected sourceRole=user, got %q", e.SourceRole)
	}
	if e.Category != learning.CategoryOps {
		t.Errorf("expected category=ops, got %q", e.Category)
	}
}

func TestLearnAdd_InvalidCategory(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	cmd := newLearnAddCmd()
	cmd.SetArgs([]string{"--category", "bogus", "--content", "test content"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid category")
	}
	if got := err.Error(); !strings.Contains(got, "invalid category") {
		t.Errorf("expected 'invalid category' in error, got %q", got)
	}
}

func TestLearnAdd_FuzzyDuplicate(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryOps, Content: "always set GOWORK=off in worktree", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	cmd := newLearnAddCmd()
	cmd.SetArgs([]string{"--category", "ops", "--content", "always set GOWORK=off in worktree"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
	if got := err.Error(); !strings.Contains(got, "similar learning already exists: L001") {
		t.Errorf("expected 'similar learning already exists: L001', got %q", got)
	}

	loaded, _ := learning.LoadStore(storePath)
	if len(loaded.Entries) != 1 {
		t.Errorf("expected store unchanged with 1 entry, got %d", len(loaded.Entries))
	}
}

func TestLearnAdd_JSON(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	out := captureStdout(t, func() {
		cmd := newLearnAddCmd()
		cmd.SetArgs([]string{"--category", "ops", "--content", "test json output", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	var result struct {
		ID    string `json:"id"`
		Added bool   `json:"added"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output %q: %v", out, err)
	}
	if !result.Added {
		t.Error("expected added=true")
	}
	if result.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestLearnContext_CLI(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "test learning", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	ws, err := protocol.Find(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := prompt.GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(root, protocol.DirName, protocol.LearningsContextFile)
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("learnings-context.md not created: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test learning") {
		t.Error("expected context file to contain the learning")
	}
}

func TestFindLearningsPath(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	path, err := findLearningsPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	if path != want {
		t.Errorf("expected %s, got %s", want, path)
	}
}

// demotePrunePayload 是 F159 後 `4x learn prune --json` 的完整輸出結構。
type demotePrunePayload struct {
	Removed    int      `json:"removed"`
	Demoted    int      `json:"demoted"`
	DryRun     bool     `json:"dryRun"`
	StaleIDs   []string `json:"staleIds"`
	DemotedIDs []string `json:"demotedIds"`
}

func runPruneJSON(t *testing.T, root string, args ...string) demotePrunePayload {
	t.Helper()
	t.Chdir(root)
	out := captureStdout(t, func() {
		cmd := newLearnPruneCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	var result demotePrunePayload
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("failed to parse JSON output %q: %v", out, err)
	}
	return result
}

// TestLearnPruneActiveDemoteUsesSettings 驗證 evolution.active_demote_days 被實際 command path 消費：
// 設為 5 天時，連 10 天未命中的 active 也被 demote（預設 90 不會），證明 resolved 值被讀取（AC-4）。
func TestLearnPruneActiveDemoteUsesSettings(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{
		Project:   protocol.ProjectConfig{Name: "test"},
		Evolution: &protocol.EvolutionConfig{ActiveDemoteDays: ptrInt(5)},
	}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "A_old", Category: learning.CategoryDesign, Content: "old", Status: learning.StatusActive, LastUsed: time.Now().Add(-100 * 24 * time.Hour)},
		{ID: "A_mid", Category: learning.CategoryDesign, Content: "mid", Status: learning.StatusActive, LastUsed: time.Now().Add(-10 * 24 * time.Hour)},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	result := runPruneJSON(t, root, "--dry-run", "--json")
	if !contains(result.DemotedIDs, "A_old") || !contains(result.DemotedIDs, "A_mid") {
		t.Errorf("active_demote_days=5 should demote both old and 10d-idle active, got %v", result.DemotedIDs)
	}
}

// TestLearnPruneDryRunActiveDemote 驗證 dry-run 預覽 demote 與 stale 但不寫回檔案（AC-5）。
func TestLearnPruneDryRunActiveDemote(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{
		Project:   protocol.ProjectConfig{Name: "test"},
		Evolution: &protocol.EvolutionConfig{ActiveDemoteDays: ptrInt(1)},
	}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "A001", Category: learning.CategoryDesign, Content: "old active", Status: learning.StatusActive, LastUsed: time.Now().Add(-100 * 24 * time.Hour)},
		{ID: "S001", Category: learning.CategoryDesign, Content: "stale", Status: learning.StatusStale, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	result := runPruneJSON(t, root, "--dry-run", "--json")
	if !contains(result.DemotedIDs, "A001") {
		t.Errorf("expected demotedIds to contain A001, got %v", result.DemotedIDs)
	}
	if !contains(result.StaleIDs, "S001") {
		t.Errorf("expected staleIds to contain S001, got %v", result.StaleIDs)
	}

	// dry-run 不寫回：A001 仍 active、S001 仍存在。
	reloaded, _ := learning.LoadStore(storePath)
	byID := map[string]learning.Entry{}
	for _, e := range reloaded.Entries {
		byID[e.ID] = e
	}
	if byID["A001"].Status != learning.StatusActive {
		t.Errorf("dry-run mutated A001 to %s", byID["A001"].Status)
	}
	if _, ok := byID["S001"]; !ok {
		t.Error("dry-run removed S001")
	}
}

// TestLearnPruneDemoteAndPrune 驗證非 dry-run 先 demote 再 prune：新 demote 的 active 存活為 candidate、
// 既有 stale 被移除，即使 demote 後條件符合 candidate 老化也不被直接刪除（AC-6）。
func TestLearnPruneDemoteAndPrune(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{
		Project:   protocol.ProjectConfig{Name: "test"},
		Evolution: &protocol.EvolutionConfig{ActiveDemoteDays: ptrInt(1), CandidateMaxIdleDays: ptrInt(1)},
	}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	old := time.Now().Add(-100 * 24 * time.Hour)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		// 久未命中且從未使用、CreatedAt 也很舊：demote 後若不保護會被 candidate 老化立刻 stale。
		{ID: "A001", Category: learning.CategoryDesign, Content: "old active", Status: learning.StatusActive, ActivatedAt: old, CreatedAt: old, UsedCount: 0},
		{ID: "S001", Category: learning.CategoryDesign, Content: "stale", Status: learning.StatusStale, CreatedAt: old},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	result := runPruneJSON(t, root, "--json")
	if result.DryRun {
		t.Error("expected non-dry-run")
	}
	if !contains(result.DemotedIDs, "A001") {
		t.Errorf("expected A001 demoted, got %v", result.DemotedIDs)
	}

	reloaded, _ := learning.LoadStore(storePath)
	byID := map[string]learning.Entry{}
	for _, e := range reloaded.Entries {
		byID[e.ID] = e
	}
	if e, ok := byID["A001"]; !ok || e.Status != learning.StatusCandidate {
		t.Errorf("demoted A001 should survive as candidate, got %+v (ok=%v)", e, ok)
	}
	if _, ok := byID["S001"]; ok {
		t.Error("pre-existing stale S001 should be removed")
	}
}

// TestLearnPrunePromotedNotDemoted 驗證 promoted learning 不會被 active demote 也不出現在預覽（AC-7）。
func TestLearnPrunePromotedNotDemoted(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{
		Project:   protocol.ProjectConfig{Name: "test"},
		Evolution: &protocol.EvolutionConfig{ActiveDemoteDays: ptrInt(1)},
	}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	old := time.Now().Add(-100 * 24 * time.Hour)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "P001", Category: learning.CategoryDesign, Content: "promoted", Status: learning.StatusPromoted, CreatedAt: old, LastUsed: old},
		{ID: "A001", Category: learning.CategoryDesign, Content: "old active", Status: learning.StatusActive, LastUsed: old},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	result := runPruneJSON(t, root, "--dry-run", "--json")
	if contains(result.DemotedIDs, "P001") {
		t.Errorf("promoted P001 must not be demoted, got %v", result.DemotedIDs)
	}
	if contains(result.StaleIDs, "P001") {
		t.Errorf("promoted P001 must not be in stale preview, got %v", result.StaleIDs)
	}
	if !contains(result.DemotedIDs, "A001") {
		t.Errorf("control active A001 should be demoted, got %v", result.DemotedIDs)
	}
}

// TestLearnPruneDryRunTextSeparatesDemoteAndRemove 驗證非 JSON dry-run stdout 分開預覽
// demote 與 stale remove，且 demote 文字能與 delete 區分（AC-14）。
func TestLearnPruneDryRunTextSeparatesDemoteAndRemove(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{
		Project:   protocol.ProjectConfig{Name: "test"},
		Evolution: &protocol.EvolutionConfig{ActiveDemoteDays: ptrInt(1), CandidateMaxIdleDays: ptrInt(1)},
	}); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	old := time.Now().Add(-100 * 24 * time.Hour)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "A001", Category: learning.CategoryDesign, Content: "old active", Status: learning.StatusActive, LastUsed: old, CreatedAt: time.Now()},
		{ID: "C001", Category: learning.CategoryDesign, Content: "idle candidate", Status: learning.StatusCandidate, UsedCount: 0, CreatedAt: old},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	out := captureStdout(t, func() {
		cmd := newLearnPruneCmd()
		cmd.SetArgs([]string{"--dry-run"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "would be demoted") {
		t.Errorf("stdout missing demote section: %q", out)
	}
	if !strings.Contains(out, "not deleted") {
		t.Errorf("demote text should clarify it is not a delete: %q", out)
	}
	if !strings.Contains(out, "stale entries would be removed") {
		t.Errorf("stdout missing stale removal section: %q", out)
	}
	if !strings.Contains(out, "A001") || !strings.Contains(out, "C001") {
		t.Errorf("stdout should mention both A001 (demote) and C001 (stale): %q", out)
	}
	// demote 段落須在 stale 段落之前，且各自標示不同 ID。
	demoteIdx := strings.Index(out, "would be demoted")
	staleIdx := strings.Index(out, "stale entries would be removed")
	if demoteIdx > staleIdx {
		t.Errorf("demote preview should precede stale preview: demote=%d stale=%d", demoteIdx, staleIdx)
	}
}
