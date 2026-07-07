package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

// Task 6 — 真實 CLI --json 輸出測試。
//
// MCP handler 的 mock 測試（internal/mcp/tools_test.go）只能證明 args 組裝正確，
// 無法證明真實 CLI 在 --json 下吐出合法 JSON（L006 孤立測試陷阱）。本檔對每個新增
// --json 的指令以 run4x 子程序實際執行，斷言 stdout 可被 json.Unmarshal 解析且含預期欄位。
//
// 集中放一檔（而非散到各 *_test.go）沿用 cli_test.go 既有 --json E2E 測試（TestStatus_JSON
// 等）的分組慣例，避免為 done/reject/subtask 等無既有 test 檔的指令各建一個近乎空的檔案。

// initWorkspace 建一個 4x 專案並回傳 dir 與綁定的 in-process Workspace，供測試直接寫 fixture。
func initWorkspace(t *testing.T) (string, *protocol.Workspace) {
	t.Helper()
	dir := t.TempDir()
	if out, err := run4x(dir, "init"); err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	return dir, &protocol.Workspace{Root: dir}
}

// saveDraft 寫入一個 draft feature，供 approve/reject 測試使用。
func saveDraft(t *testing.T, ws *protocol.Workspace, id string) {
	t.Helper()
	if err := ws.SaveFeature(feat.Feature{ID: id, Name: "F099: Draft", Status: feat.StatusDraft}); err != nil {
		t.Fatalf("save draft feature: %v", err)
	}
}

func TestApprove_JSON(t *testing.T) {
	dir, ws := initWorkspace(t)
	saveDraft(t, ws, "F099-draft-approve")

	out, err := run4x(dir, "approve", "F099-draft-approve", "--json")
	if err != nil {
		t.Fatalf("approve --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.FeatureID != "F099-draft-approve" || result.Status != "not-started" {
		t.Errorf("got %+v, want featureId=F099-draft-approve status=not-started", result)
	}
}

func TestReject_JSON(t *testing.T) {
	dir, ws := initWorkspace(t)
	saveDraft(t, ws, "F099-draft-reject")

	out, err := run4x(dir, "reject", "F099-draft-reject", "--json")
	if err != nil {
		t.Fatalf("reject --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.Status != "abandoned" {
		t.Errorf("status = %q, want abandoned", result.Status)
	}
}

func TestApprove_JSON_Error(t *testing.T) {
	dir, ws := initWorkspace(t)
	// not-started（非 draft）→ approve 應回 error，--json 下走 jsonError 印 {"error":...} 並 exit 1。
	if err := ws.SaveFeature(feat.Feature{ID: "F099-not-draft", Name: "F099", Status: feat.StatusNotStarted}); err != nil {
		t.Fatal(err)
	}

	out, err := run4x(dir, "approve", "F099-not-draft", "--json")
	if err == nil {
		t.Fatal("expected non-zero exit for non-draft approve")
	}
	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON on error: %v\n%s", err, out)
	}
	if result.Error == "" {
		t.Error("error field is empty")
	}
}

func TestSubtask_JSON(t *testing.T) {
	dir, ws := initWorkspace(t)
	if err := ws.SaveFeature(feat.Feature{
		ID:     "F099-with-subtask",
		Name:   "F099: Subtask",
		Status: feat.StatusNotStarted,
		Subtasks: []feat.Subtask{
			{ID: "st1", Name: "first", Status: "not-started"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := run4x(dir, "subtask", "F099-with-subtask", "st1", "--status", "done", "--json")
	if err != nil {
		t.Fatalf("subtask --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		SubtaskID string `json:"subtaskId"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.SubtaskID != "st1" || result.Status != "done" {
		t.Errorf("got %+v, want subtaskId=st1 status=done", result)
	}
}

func TestDone_JSON(t *testing.T) {
	dir, ws := initWorkspace(t)
	featureID := "F099-done-json"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState(featureID, protocol.State{
		FeatureID: featureID,
		Phase:     protocol.PhasePendingReview,
		Round:     1,
		Active:    false,
		Runner:    "mock",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feat.Feature{ID: featureID, Name: "F099: Done", Status: feat.StatusReadyForReview}); err != nil {
		t.Fatal(err)
	}

	out, err := run4x(dir, "done", featureID, "--json")
	if err != nil {
		t.Fatalf("done --json failed: %v\n%s", err, out)
	}

	var result struct {
		FeatureID string `json:"featureId"`
		Phase     string `json:"phase"`
		Merged    bool   `json:"merged"`
		Conflict  bool   `json:"conflict"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.Phase != "done" || result.Conflict {
		t.Errorf("got phase=%q conflict=%v, want done/false", result.Phase, result.Conflict)
	}
}

// TestMerge_JSON_Error 驗證 merge 在 --json 的 error path 仍輸出合法 JSON。
// merge 的成功路徑需要真實 git worktree（gitops），setup 重且易 flaky；arg 組裝已由
// internal/mcp 的 TestMergeTool 覆蓋，故此處只驗證「無 worktree」error 仍是乾淨 JSON。
func TestMerge_JSON_Error(t *testing.T) {
	dir, ws := initWorkspace(t)
	featureID := "F099-merge-json"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState(featureID, protocol.State{
		FeatureID: featureID,
		Phase:     protocol.PhasePendingReview,
		Round:     1,
		Runner:    "mock",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feat.Feature{ID: featureID, Name: "F099: Merge", Status: feat.StatusReadyForReview}); err != nil {
		t.Fatal(err)
	}

	out, err := run4x(dir, "merge", featureID, "--json")
	if err == nil {
		t.Fatal("expected non-zero exit when no worktree exists")
	}
	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON on error: %v\n%s", err, out)
	}
	if result.Error == "" {
		t.Error("error field is empty")
	}
}

func TestClean_JSON(t *testing.T) {
	dir, _ := initWorkspace(t)
	// 無 done/abandoned feature → candidates 空；未帶 --force → dryRun=true。
	out, err := run4x(dir, "clean", "--json")
	if err != nil {
		t.Fatalf("clean --json failed: %v\n%s", err, out)
	}

	var result struct {
		Cleaned    []string `json:"cleaned"`
		FreedBytes int64    `json:"freedBytes"`
		DryRun     bool     `json:"dryRun"`
		Candidates []struct {
			FeatureID string `json:"featureId"`
			Size      int64  `json:"size"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !result.DryRun {
		t.Error("dryRun = false, want true (no --force)")
	}
}

func TestMine_JSON(t *testing.T) {
	dir, _ := initWorkspace(t)
	out, err := run4x(dir, "mine", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("mine --json failed: %v\n%s", err, out)
	}

	var result struct {
		Candidates int    `json:"candidates"`
		Output     string `json:"output"`
		DryRun     bool   `json:"dryRun"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !result.DryRun {
		t.Error("dryRun = false, want true")
	}
	if result.Output == "" {
		t.Error("output path is empty")
	}
}

func TestEvolve_JSON_DryRun(t *testing.T) {
	dir, _ := initWorkspace(t)
	out, err := run4x(dir, "evolve", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("evolve --json failed: %v\n%s", err, out)
	}

	var result struct {
		DryRun       bool `json:"dryRun"`
		Scanned      int  `json:"scanned"`
		New          int  `json:"new"`
		WouldEnqueue int  `json:"wouldEnqueue"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !result.DryRun {
		t.Error("dryRun = false, want true")
	}
}

func TestGate_JSON_Pre(t *testing.T) {
	dir, _ := initWorkspace(t)
	// candidates.json 缺檔時 LoadCandidates 回空 pool → PreVeto 產出 kept=0,dropped=0。
	out, err := run4x(dir, "gate", "--pre", "--json")
	if err != nil {
		t.Fatalf("gate --pre --json failed: %v\n%s", err, out)
	}

	var result struct {
		Phase   string `json:"phase"`
		Kept    int    `json:"kept"`
		Dropped int    `json:"dropped"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.Phase != "pre" {
		t.Errorf("phase = %q, want pre", result.Phase)
	}
}

func TestLearnList_JSON(t *testing.T) {
	dir, _ := initWorkspace(t)
	// learnings.json 缺檔 → LoadStore 回空 store → entries 空。
	out, err := run4x(dir, "learn", "list", "--json")
	if err != nil {
		t.Fatalf("learn list --json failed: %v\n%s", err, out)
	}

	var result struct {
		Entries []json.RawMessage `json:"entries"`
		Active  int               `json:"active"`
		Total   int               `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
}

func TestLearnPrune_JSON(t *testing.T) {
	dir, _ := initWorkspace(t)
	out, err := run4x(dir, "learn", "prune", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("learn prune --json failed: %v\n%s", err, out)
	}

	var result struct {
		Removed  int      `json:"removed"`
		DryRun   bool     `json:"dryRun"`
		StaleIDs []string `json:"staleIds"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !result.DryRun {
		t.Error("dryRun = false, want true")
	}
}

// writeLearnings 寫入一個含單一 active entry 的 learnings.json，供 promote/remove 測試使用。
func writeLearnings(t *testing.T, ws *protocol.Workspace, id string) {
	t.Helper()
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: id, Category: learning.CategoryDesign, Content: "x", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	if err := store.Save(storePath); err != nil {
		t.Fatalf("save learnings: %v", err)
	}
}

func TestLearnPromote_JSON(t *testing.T) {
	dir, ws := initWorkspace(t)
	writeLearnings(t, ws, "L001")

	out, err := run4x(dir, "learn", "promote", "L001", "--json")
	if err != nil {
		t.Fatalf("learn promote --json failed: %v\n%s", err, out)
	}

	var result struct {
		ID       string `json:"id"`
		Promoted bool   `json:"promoted"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if result.ID != "L001" || !result.Promoted {
		t.Errorf("got %+v, want id=L001 promoted=true", result)
	}
}

func TestLearnRemove_JSON(t *testing.T) {
	dir, ws := initWorkspace(t)
	writeLearnings(t, ws, "L001")

	out, err := run4x(dir, "learn", "remove", "L001", "--json")
	if err != nil {
		t.Fatalf("learn remove --json failed: %v\n%s", err, out)
	}

	var result struct {
		ID      string `json:"id"`
		Removed bool   `json:"removed"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if !result.Removed {
		t.Error("removed = false, want true")
	}
}

// config 指令讀寫 ~/.4x/settings.json（user config，與 workspace 無關），
// 故用 t.Setenv 把 HOME 指向暫存目錄隔離，避免污染真實使用者設定。

func TestConfigSetGetList_JSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	dir := t.TempDir()

	// set
	setOut, err := run4x(dir, "config", "set", "locale", "zh-TW", "--json")
	if err != nil {
		t.Fatalf("config set --json failed: %v\n%s", err, setOut)
	}
	var setResult struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal([]byte(setOut), &setResult); err != nil {
		t.Fatalf("invalid set JSON: %v\n%s", err, setOut)
	}
	if setResult.Key != "locale" || setResult.Value != "zh-TW" || setResult.Path == "" {
		t.Errorf("set got %+v, want key=locale value=zh-TW path!=''", setResult)
	}

	// get
	getOut, err := run4x(dir, "config", "get", "locale", "--json")
	if err != nil {
		t.Fatalf("config get --json failed: %v\n%s", err, getOut)
	}
	var getResult struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(getOut), &getResult); err != nil {
		t.Fatalf("invalid get JSON: %v\n%s", err, getOut)
	}
	if getResult.Value != "zh-TW" {
		t.Errorf("get value = %q, want zh-TW", getResult.Value)
	}

	// list（完整 UserConfig object）
	listOut, err := run4x(dir, "config", "list", "--json")
	if err != nil {
		t.Fatalf("config list --json failed: %v\n%s", err, listOut)
	}
	var listResult struct {
		Locale string `json:"locale"`
	}
	if err := json.Unmarshal([]byte(listOut), &listResult); err != nil {
		t.Fatalf("invalid list JSON: %v\n%s", err, listOut)
	}
	if listResult.Locale != "zh-TW" {
		t.Errorf("list locale = %q, want zh-TW", listResult.Locale)
	}
}
