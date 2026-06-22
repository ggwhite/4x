package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/learning"
)

// writeRoundFile 在 feature 的 round dir 寫入一個檔案，dir 不存在時自動建立。
func writeRoundFile(t *testing.T, ws *Workspace, featureID string, round int, name, content string) {
	t.Helper()
	dir := ws.RoundDir(featureID, round)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir round dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// writeEscalation 寫入一筆 escalation.json 到指定 round。
func writeEscalation(t *testing.T, ws *Workspace, featureID string, round int, esc Escalation) {
	t.Helper()
	data, err := json.Marshal(esc)
	if err != nil {
		t.Fatalf("marshal escalation: %v", err)
	}
	writeRoundFile(t, ws, featureID, round, EscalationFile, string(data))
}

// saveTestFeature 寫入一個最小可用的 feature YAML。
func saveTestFeature(t *testing.T, ws *Workspace, id, name string) {
	t.Helper()
	if err := ws.SaveFeature(feature.Feature{ID: id, Name: name, Status: feature.StatusInProgress}); err != nil {
		t.Fatalf("SaveFeature %s: %v", id, err)
	}
}

func failReport(verdict string, issues ...string) string {
	s := "# Review Report\n\n"
	for _, iss := range issues {
		s += "### [CRITICAL] " + iss + "\nsome detail line\n\n"
	}
	s += "## Verdict\n" + verdict + "\n"
	return s
}

func TestScanEscalations(t *testing.T) {
	ws := setupWorkspace(t)
	saveTestFeature(t, ws, "F001-a", "Feature A")
	saveTestFeature(t, ws, "F002-b", "Feature B")

	// 有效 escalation。
	writeEscalation(t, ws, "F001-a", 1, Escalation{Needed: true, Reason: "spec-mismatch", Detail: "spec contradicts itself"})
	// Needed=false → 不產 candidate。
	writeEscalation(t, ws, "F001-a", 2, Escalation{Needed: false, Reason: "blocker", Detail: "nope"})
	// 未知 reason → 跳過。
	writeEscalation(t, ws, "F002-b", 1, Escalation{Needed: true, Reason: "weird-reason", Detail: "x"})
	// 另一個有效 escalation。
	writeEscalation(t, ws, "F002-b", 2, Escalation{Needed: true, Reason: "blocker", Detail: "external dep down"})
	// 壞掉的 JSON → best-effort 跳過，不中斷。
	writeRoundFile(t, ws, "F002-b", 3, EscalationFile, "{not valid json")

	features, err := ws.ListFeatures()
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	cands := ScanEscalations(ws, features)
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(cands), cands)
	}
	if cands[0].Source != SourceEscalation {
		t.Errorf("source = %s, want %s", cands[0].Source, SourceEscalation)
	}
	if cands[0].Origin != "F001-a round-1 spec-mismatch" {
		t.Errorf("origin = %q", cands[0].Origin)
	}
	if cands[0].Description != "spec contradicts itself" {
		t.Errorf("description = %q", cands[0].Description)
	}
	if cands[1].Origin != "F002-b round-2 blocker" {
		t.Errorf("origin[1] = %q", cands[1].Origin)
	}
}

func TestScanEscalations_ListFeaturesMissing(t *testing.T) {
	// 空 feature 清單 → 掃描器回空 slice 不 panic。
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: root}
	if cands := ScanEscalations(ws, nil); len(cands) != 0 {
		t.Errorf("got %d candidates, want 0", len(cands))
	}
}

func TestScanStuckFeatures(t *testing.T) {
	ws := setupWorkspace(t)
	saveTestFeature(t, ws, "F010-stuck", "Stuck One")
	saveTestFeature(t, ws, "F011-ok", "Running OK")
	saveTestFeature(t, ws, "F012-fallback", "Fallback Reason")

	for _, id := range []string{"F010-stuck", "F011-ok", "F012-fallback"} {
		if err := ws.InitFeatureDir(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.WriteState("F010-stuck", State{
		FeatureID: "F010-stuck", Phase: PhaseNeedsAttention,
		StopReason: "guard-failed", StopMessage: "scope violation in repo X",
	}); err != nil {
		t.Fatal(err)
	}

	// 非 stuck phase → 不產 candidate。
	if err := ws.WriteState("F011-ok", State{FeatureID: "F011-ok", Phase: PhaseCoding}); err != nil {
		t.Fatal(err)
	}

	// StopReason/StopMessage 皆空 → 回退讀 escalation Detail。
	if err := ws.WriteState("F012-fallback", State{FeatureID: "F012-fallback", Phase: PhaseBlocked}); err != nil {
		t.Fatal(err)
	}
	writeEscalation(t, ws, "F012-fallback", 1, Escalation{Needed: true, Reason: "blocker", Detail: "waiting on upstream API"})

	features, err := ws.ListFeatures()
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	cands := ScanStuckFeatures(ws, features)
	if len(cands) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(cands), cands)
	}

	byOrigin := map[string]Candidate{}
	for _, c := range cands {
		byOrigin[c.Origin] = c
	}
	a, ok := byOrigin["F010-stuck needs-attention"]
	if !ok {
		t.Fatalf("missing F010-stuck candidate: %+v", cands)
	}
	if a.Source != SourceStuck {
		t.Errorf("source = %s", a.Source)
	}
	if a.Description != "guard-failed: scope violation in repo X" {
		t.Errorf("description = %q", a.Description)
	}
	b, ok := byOrigin["F012-fallback blocked"]
	if !ok {
		t.Fatalf("missing F012-fallback candidate")
	}
	if b.Description != "waiting on upstream API" {
		t.Errorf("fallback description = %q", b.Description)
	}
}

func TestScanFailPatterns_CrossFeatureCluster(t *testing.T) {
	ws := setupWorkspace(t)
	for _, id := range []string{"F020-a", "F021-b", "F022-c"} {
		saveTestFeature(t, ws, id, "F "+id)
		writeRoundFile(t, ws, id, 1, ReviewReport, failReport("FAIL", "Missing error handling"))
	}

	features, err := ws.ListFeatures()
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	cands, learnings := ScanFailPatterns(ws, features, 3)
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1: %+v", len(cands), cands)
	}
	if cands[0].Source != SourceFailPattern {
		t.Errorf("source = %s", cands[0].Source)
	}
	if cands[0].Origin != "3 features: F020-a, F021-b, F022-c" {
		t.Errorf("origin = %q", cands[0].Origin)
	}
	if len(learnings) != 1 || learnings[0].Category != learning.CategoryReview {
		t.Errorf("learnings = %+v", learnings)
	}
	if learnings[0].Source != SourceFailPattern {
		t.Errorf("learning source = %s", learnings[0].Source)
	}
}

func TestScanFailPatterns_SingleFeatureMultiRoundCountedOnce(t *testing.T) {
	ws := setupWorkspace(t)
	saveTestFeature(t, ws, "F030-x", "X")
	// 同一 feature 三輪都同樣 FAIL → 不同 feature 數仍為 1，不灌水。
	for r := 1; r <= 3; r++ {
		writeRoundFile(t, ws, "F030-x", r, ReviewReport, failReport("FAIL", "Missing error handling"))
	}
	features, err := ws.ListFeatures()
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	cands, _ := ScanFailPatterns(ws, features, 3)
	if len(cands) != 0 {
		t.Fatalf("got %d candidates, want 0 (single feature must not pass threshold): %+v", len(cands), cands)
	}
}

func TestScanFailPatterns_PassReportsIgnored(t *testing.T) {
	ws := setupWorkspace(t)
	for _, id := range []string{"F040-a", "F041-b", "F042-c"} {
		saveTestFeature(t, ws, id, id)
		// PASS verdict → issue 不蒐集。
		writeRoundFile(t, ws, id, 1, ReviewReport, failReport("PASS", "Missing error handling"))
	}
	features, err := ws.ListFeatures()
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	cands, _ := ScanFailPatterns(ws, features, 3)
	if len(cands) != 0 {
		t.Fatalf("got %d candidates, want 0 (PASS reports ignored)", len(cands))
	}
}

// TestScanFailPatterns_JaccardBoundary 驗證 AR-2：短標題下 IsSimilarFeature 聚類行為。
// 一組「短且語意相似」應歸同群（升級成 candidate），一組「短且不相似」不應歸同群。
func TestScanFailPatterns_JaccardBoundary(t *testing.T) {
	ws := setupWorkspace(t)

	// 相似群：三個 feature 各自的 deep-review 標題語意相近，應聚成一群。
	similar := map[string]string{
		"F050-a": "Missing error handling",
		"F051-b": "Missing error handling in parser",
		"F052-c": "Missing error handling here",
	}
	for id, title := range similar {
		saveTestFeature(t, ws, id, id)
		writeRoundFile(t, ws, id, 1, DeepReviewReport, failReport("FAIL", title))
	}

	// 不相似群：三個 feature 各自完全不同主題，不應聚成同一群。
	distinct := map[string]string{
		"F060-a": "Race condition in cache",
		"F061-b": "Hardcoded secret in config",
		"F062-c": "Slow database query",
	}
	for id, title := range distinct {
		saveTestFeature(t, ws, id, id)
		writeRoundFile(t, ws, id, 1, DeepReviewReport, failReport("FAIL", title))
	}

	features, err := ws.ListFeatures()
	if err != nil {
		t.Fatalf("ListFeatures: %v", err)
	}
	cands, _ := ScanFailPatterns(ws, features, 3)
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want exactly 1 (similar group clusters, distinct group does not): %+v", len(cands), cands)
	}
	if cands[0].Origin != "3 features: F050-a, F051-b, F052-c" {
		t.Errorf("clustered origin = %q, want the similar group", cands[0].Origin)
	}
}

func TestParseReportIssues(t *testing.T) {
	report := "# Report\n" +
		"### [CRITICAL] Missing error handling\n" +
		"detail\n" +
		"#### [WARNING] Inconsistent naming\n" +
		"[CRITICAL] Unchecked nil pointer\n" +
		"normal text [CRITICAL] not at line start should not count\n" +
		"## Verdict\n**FAIL**\n"
	passed, issues := ParseReportIssues(report)
	if passed {
		t.Error("expected FAIL verdict")
	}
	want := []string{"Missing error handling", "Inconsistent naming", "Unchecked nil pointer"}
	if len(issues) != len(want) {
		t.Fatalf("got issues %v, want %v", issues, want)
	}
	for i := range want {
		if issues[i] != want[i] {
			t.Errorf("issue[%d] = %q, want %q", i, issues[i], want[i])
		}
	}
}

func TestParseReportIssues_Pass(t *testing.T) {
	passed, _ := ParseReportIssues("## Verdict\nPASS\n")
	if !passed {
		t.Error("expected PASS")
	}
	passed, _ = ParseReportIssues("## Verdict\nCONDITIONAL PASS\n")
	if !passed {
		t.Error("expected CONDITIONAL PASS treated as passed")
	}
}

func TestDedupeCandidates(t *testing.T) {
	existingFeatures := []feature.Feature{
		{Name: "Add structured logging", Description: "introduce slog across modules"},
	}
	existingCands := []Candidate{
		{Title: "Recurring review issue", Description: "missing error handling cross feature"},
	}
	cands := []Candidate{
		// 與既有 feature 相似 → 丟。
		{Title: "Add structured logging", Description: "introduce slog across modules"},
		// 與既有 candidate 相似 → 丟。
		{Title: "Recurring review issue", Description: "missing error handling cross feature"},
		// 全新 → 保留。
		{Title: "Flaky integration tests", Description: "batch runner times out intermittently"},
		// 與上一筆保留者相似（batch 內去重）→ 丟。
		{Title: "Flaky integration tests", Description: "batch runner times out intermittently"},
		// 另一筆全新 → 保留。
		{Title: "Dashboard SSE reconnect", Description: "lost events after server restart"},
	}
	kept := DedupeCandidates(cands, existingFeatures, existingCands)
	if len(kept) != 2 {
		t.Fatalf("got %d kept, want 2: %+v", len(kept), kept)
	}
	if kept[0].Title != "Flaky integration tests" || kept[1].Title != "Dashboard SSE reconnect" {
		t.Errorf("kept order/content wrong: %+v", kept)
	}
}

func TestLoadCandidates_MissingFile(t *testing.T) {
	pool, err := LoadCandidates(filepath.Join(t.TempDir(), "nope", "candidates.json"))
	if err != nil {
		t.Fatalf("LoadCandidates on missing file: %v", err)
	}
	if pool.Version != 1 {
		t.Errorf("version = %d, want 1", pool.Version)
	}
	if len(pool.Candidates) != 0 {
		t.Errorf("expected empty candidates")
	}
}

func TestCandidatePool_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidates.json")
	want := CandidatePool{
		Version:     1,
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		Candidates: []Candidate{
			{Title: "T1", Description: "D1", Source: SourceEscalation, Origin: "F001 round-1 blocker"},
		},
		Learnings: []CandidateLearning{
			{Category: learning.CategoryReview, Content: "C1", Source: SourceFailPattern, Origin: "3 features: a, b, c"},
		},
	}
	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadCandidates(path)
	if err != nil {
		t.Fatalf("LoadCandidates: %v", err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Title != "T1" {
		t.Errorf("candidates round-trip mismatch: %+v", got.Candidates)
	}
	if len(got.Learnings) != 1 || got.Learnings[0].Category != learning.CategoryReview {
		t.Errorf("learnings round-trip mismatch: %+v", got.Learnings)
	}
	if !got.GeneratedAt.Equal(want.GeneratedAt) {
		t.Errorf("generatedAt = %v, want %v", got.GeneratedAt, want.GeneratedAt)
	}
}
