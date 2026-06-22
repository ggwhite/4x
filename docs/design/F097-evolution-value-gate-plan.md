{% raw %}
# F097: Evolution Value & Convergence Gate — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 candidate feature 進 backlog 前加一道閘門——CLI 雙層 veto（pre/post）守反 hack 紅線、LLM gate role 判價值，並用 golden fixtures 防 gate 漂移。

**Architecture:** 新 `internal/evolution` 套件放純函式 veto 邏輯（無 LLM、可單元測試）；新 `gate` LLM role 負責判斷價值（由 runner 執行，CLI 不呼叫 LLM）；新 `4x gate` 命令做 deterministic 編排（pre-veto 產 gate 輸入、post-veto 產 accepted-candidates.json）。Runtime 串接（mine→gate→enrich）由 F099 負責，不在本 plan。

**Tech Stack:** Go 1.26、Cobra、`encoding/json`、Go 標準 `testing`、既有 `text/template` + `go:embed`。

## Global Constraints

- CLI 層嚴禁呼叫 LLM——所有 LLM 互動由 runner（plugin）負責；`4x gate` 只做 deterministic veto，gate role 由 runner 執行（spec §架構）
- 任一 POST-veto 條件成立 → 整筆 reject，不用加權平均（spec §約束）
- 被接受的 candidate 必須有 `why_not_hack` 論述，缺者拒（spec §約束）
- gate role 不可直接建 feature YAML——F097 輸出止於 `accepted-candidates.json`，enqueue 由下游（F096/F099）負責（spec §約束）
- golden fixtures 內容不可進 gate runtime prompt（spec §Golden fixtures）
- Go：exported 識別字加 GoDoc（第一句以名稱開頭，繁中）；gofmt；error 明確處理
- 測試檔與被測程式同目錄；驗證跑 `go build ./cmd/4x && go vet ./... && go test -race ./...`
- 沿用既有 `protocol.IsSimilarFeature` 做 Jaccard 去重，不另造輪子（spec §CLI 雙層 veto）

## 共享資料契約（F097 與 F095 共同約定）

F095（history-miner）尚未實作。本 plan 定義 `.4x/candidates.json` 讀取格式作為 F097/F095 共同契約：

```json
{
  "candidates": [
    { "title": "string", "description": "string", "source": "escalation | stuck | fail-pattern | deep-review" }
  ]
}
```

F097 產出兩個檔（皆在 `.4x/` 根）：
- `gate-input.json` — PRE-veto 倖存者，供 gate role 讀（格式同 candidates.json）
- `gate-verdicts.json` — gate role 產出（見 Task 4 schema）
- `accepted-candidates.json` — POST-veto 通過者（格式同 candidates.json，附 `value_score`/`why_not_hack`）

## File Structure

| 檔案 | 職責 | 動作 |
|---|---|---|
| `internal/protocol/types.go` | `EvolutionConfig` 巢狀 struct + `Config.Evolution` 欄位 | Modify |
| `internal/evolution/config.go` | `ResolveEvolution`——套預設值 | Create |
| `internal/evolution/candidate.go` | `Candidate` struct + `LoadCandidates`/`SaveCandidates` | Create |
| `internal/evolution/preveto.go` | `PreVeto`——Jaccard 去重 vs 既有 feature | Create |
| `internal/evolution/verdict.go` | `Verdict` struct + `ParseVerdicts` | Create |
| `internal/evolution/postveto.go` | `PostVeto`——不可翻硬否決 + cap | Create |
| `internal/protocol/role.go`（既有）| 新增 `RoleGate` 常量 | Modify |
| `templates/gate.md.tmpl` | gate role prompt template | Create |
| `cmd/4x/prompt.go` | `roleTemplateFiles` 加 `RoleGate` 對應 | Modify |
| `cmd/4x/gate.go` | `4x gate` 命令（pre/post 編排） | Create |
| `cmd/4x/<root register>` | 註冊 `newGateCmd()` | Modify |
| `internal/doctor/doctor.go` | `checkEvolution` + section 常量 | Modify |
| `internal/evolution/testdata/gate-fixtures/` | golden fixtures | Create |

> 各 `internal/evolution/*.go` 對應 `*_test.go` 同目錄。

---

### Task 1: EvolutionConfig + 預設值解析

**Files:**
- Modify: `internal/protocol/types.go`（`Config` struct，`AutoDiscoverFeatures` 附近 ~ :320）
- Create: `internal/evolution/config.go`
- Test: `internal/evolution/config_test.go`

**Interfaces:**
- Produces: `protocol.EvolutionConfig{ ValueFloor float64; MaxAcceptPerRun int; MaxBacklogUndone int; GateRunner string; GateModel string; DedupThreshold float64 }`；`protocol.Config.Evolution *EvolutionConfig`
- Produces: `evolution.ResolveEvolution(cfg protocol.Config) ResolvedEvolution`，`ResolvedEvolution` 為全部欄位填好預設的值型別

- [ ] **Step 1: 在 types.go 加 EvolutionConfig struct 與 Config 欄位**

於 `internal/protocol/types.go` `Config` struct 內，`MaxDiscoveredFeatures` 之後加：

```go
	// Evolution 設定 F097 價值閘門；nil 表示未啟用 evolve pipeline。
	Evolution *EvolutionConfig `json:"evolution,omitempty"`
```

在 `Config` struct 之後（或鄰近巢狀 struct 區）加：

```go
// EvolutionConfig 設定 evolve pipeline 的價值閘門與收斂上限。
// 指標欄位為 0 時由 evolution.ResolveEvolution 套用預設值。
type EvolutionConfig struct {
	// ValueFloor 為 gate role 給的 value_score 最低門檻，低於此一律拒。
	ValueFloor float64 `json:"value_floor,omitempty"`
	// MaxAcceptPerRun 限制單次 gate 最多接受幾筆 candidate（convergence cap）。
	MaxAcceptPerRun int `json:"max_accept_per_run,omitempty"`
	// MaxBacklogUndone 為 backlog 未做數上限，超過即停止接受新 candidate。
	MaxBacklogUndone int `json:"max_backlog_undone,omitempty"`
	// GateRunner 指定 gate role 用哪個 runner。
	GateRunner string `json:"gate_runner,omitempty"`
	// GateModel 指定 gate role 用哪個 model（空字串用 runner 預設）。
	GateModel string `json:"gate_model,omitempty"`
	// DedupThreshold 為 candidate 去重的 Jaccard 門檻（0 時套預設 0.6）。
	DedupThreshold float64 `json:"dedup_threshold,omitempty"`
}
```

- [ ] **Step 2: 寫 config_test.go 的失敗測試**

```go
package evolution

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestResolveEvolution_Defaults(t *testing.T) {
	got := ResolveEvolution(protocol.Config{})
	if got.ValueFloor != 0.6 {
		t.Errorf("ValueFloor = %v, want 0.6", got.ValueFloor)
	}
	if got.MaxAcceptPerRun != 3 {
		t.Errorf("MaxAcceptPerRun = %v, want 3", got.MaxAcceptPerRun)
	}
	if got.MaxBacklogUndone != 15 {
		t.Errorf("MaxBacklogUndone = %v, want 15", got.MaxBacklogUndone)
	}
	if got.DedupThreshold != 0.6 {
		t.Errorf("DedupThreshold = %v, want 0.6", got.DedupThreshold)
	}
}

func TestResolveEvolution_Overrides(t *testing.T) {
	cfg := protocol.Config{Evolution: &protocol.EvolutionConfig{
		ValueFloor: 0.8, MaxAcceptPerRun: 1, MaxBacklogUndone: 5, DedupThreshold: 0.5,
	}}
	got := ResolveEvolution(cfg)
	if got.ValueFloor != 0.8 || got.MaxAcceptPerRun != 1 || got.MaxBacklogUndone != 5 || got.DedupThreshold != 0.5 {
		t.Errorf("overrides not applied: %+v", got)
	}
}
```

- [ ] **Step 3: 跑測試確認失敗**

Run: `go test ./internal/evolution/ -run TestResolveEvolution -v`
Expected: FAIL（`undefined: ResolveEvolution`）

- [ ] **Step 4: 寫 config.go**

```go
// Package evolution 實作 F097 價值閘門的 deterministic veto 邏輯（無 LLM）。
package evolution

import "github.com/ggwhite/4x/internal/protocol"

// 預設值——EvolutionConfig 對應欄位為 0 時套用。
const (
	defaultValueFloor       = 0.6
	defaultMaxAcceptPerRun  = 3
	defaultMaxBacklogUndone = 15
	defaultDedupThreshold   = 0.6
)

// ResolvedEvolution 為填妥預設值的 evolution 設定。
type ResolvedEvolution struct {
	ValueFloor       float64
	MaxAcceptPerRun  int
	MaxBacklogUndone int
	GateRunner       string
	GateModel        string
	DedupThreshold   float64
}

// ResolveEvolution 把 cfg.Evolution 的零值欄位補上預設值後回傳。
// cfg.Evolution 為 nil 時等同全部套預設。
func ResolveEvolution(cfg protocol.Config) ResolvedEvolution {
	e := protocol.EvolutionConfig{}
	if cfg.Evolution != nil {
		e = *cfg.Evolution
	}
	r := ResolvedEvolution{
		ValueFloor:       e.ValueFloor,
		MaxAcceptPerRun:  e.MaxAcceptPerRun,
		MaxBacklogUndone: e.MaxBacklogUndone,
		GateRunner:       e.GateRunner,
		GateModel:        e.GateModel,
		DedupThreshold:   e.DedupThreshold,
	}
	if r.ValueFloor == 0 {
		r.ValueFloor = defaultValueFloor
	}
	if r.MaxAcceptPerRun == 0 {
		r.MaxAcceptPerRun = defaultMaxAcceptPerRun
	}
	if r.MaxBacklogUndone == 0 {
		r.MaxBacklogUndone = defaultMaxBacklogUndone
	}
	if r.DedupThreshold == 0 {
		r.DedupThreshold = defaultDedupThreshold
	}
	return r
}
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/evolution/ -run TestResolveEvolution -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/types.go internal/evolution/config.go internal/evolution/config_test.go
git commit -m "feat(F097): add EvolutionConfig with default resolution"
```

---

### Task 2: Candidate schema + 讀寫

**Files:**
- Create: `internal/evolution/candidate.go`
- Test: `internal/evolution/candidate_test.go`

**Interfaces:**
- Produces: `evolution.Candidate{ Title string; Description string; Source string; ValueScore float64; WhyNotHack string }`（後兩欄 omitempty，僅 accepted 檔用）
- Produces: `LoadCandidates(path string) ([]Candidate, error)`、`SaveCandidates(path string, cands []Candidate) error`
- Consumes: 無

- [ ] **Step 1: 寫失敗測試**

```go
package evolution

import (
	"path/filepath"
	"testing"
)

func TestLoadCandidates_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candidates.json")
	want := []Candidate{
		{Title: "Add retry to runner", Description: "transient errors", Source: "fail-pattern"},
		{Title: "Recover abandoned F012", Description: "spec mismatch", Source: "stuck"},
	}
	if err := SaveCandidates(path, want); err != nil {
		t.Fatalf("SaveCandidates: %v", err)
	}
	got, err := LoadCandidates(path)
	if err != nil {
		t.Fatalf("LoadCandidates: %v", err)
	}
	if len(got) != 2 || got[0].Title != want[0].Title || got[1].Source != "stuck" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestLoadCandidates_MissingFile(t *testing.T) {
	_, err := LoadCandidates(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/evolution/ -run TestLoadCandidates -v`
Expected: FAIL（`undefined: Candidate`）

- [ ] **Step 3: 寫 candidate.go**

```go
package evolution

import (
	"encoding/json"
	"fmt"
	"os"
)

// Candidate 代表一筆待入 backlog 的候選 feature。
// ValueScore 與 WhyNotHack 僅在 gate 判斷後（accepted-candidates.json）有值。
type Candidate struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	ValueScore  float64 `json:"value_score,omitempty"`
	WhyNotHack  string  `json:"why_not_hack,omitempty"`
}

// candidateFile 為 candidates.json / gate-input.json / accepted-candidates.json 的外層結構。
type candidateFile struct {
	Candidates []Candidate `json:"candidates"`
}

// LoadCandidates 讀取 candidate JSON 檔。檔案不存在或格式錯誤時回 error。
func LoadCandidates(path string) ([]Candidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read candidates %s: %w", path, err)
	}
	var f candidateFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse candidates %s: %w", path, err)
	}
	return f.Candidates, nil
}

// SaveCandidates 將 candidate 寫成 indented JSON。
func SaveCandidates(path string, cands []Candidate) error {
	data, err := json.MarshalIndent(candidateFile{Candidates: cands}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal candidates: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write candidates %s: %w", path, err)
	}
	return nil
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/evolution/ -run TestLoadCandidates -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/evolution/candidate.go internal/evolution/candidate_test.go
git commit -m "feat(F097): add Candidate schema and JSON read/write"
```

---

### Task 3: PreVeto——Jaccard 去重 vs 既有 feature

**Files:**
- Create: `internal/evolution/preveto.go`
- Test: `internal/evolution/preveto_test.go`

**Interfaces:**
- Consumes: `Candidate`（Task 2）、`protocol.IsSimilarFeatureThreshold`（見 Step 3 註）、`feature.Feature`
- Produces: `PreVeto(cands []Candidate, existing []feature.Feature, threshold float64) (kept []Candidate, dropped []Candidate)`

> **註**：既有 `protocol.IsSimilarFeature(a, b string) bool` 把門檻寫死 0.6。本 task 需要可調門檻，故 Step 3 先在 `internal/protocol/discover.go` 抽出 `IsSimilarFeatureThreshold(a, b string, threshold float64) bool`，原 `IsSimilarFeature` 改為呼叫它並傳 `similarityThreshold`。

- [ ] **Step 1: 寫失敗測試**

```go
package evolution

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

func TestPreVeto_DropsSimilarToExisting(t *testing.T) {
	existing := []feature.Feature{
		{Name: "Runner transient retry", Description: "retry transient runner errors"},
	}
	cands := []Candidate{
		{Title: "Runner transient retry", Description: "retry transient runner errors", Source: "fail-pattern"},
		{Title: "Dashboard dark mode", Description: "add dark theme toggle", Source: "deep-review"},
	}
	kept, dropped := PreVeto(cands, existing, 0.6)
	if len(kept) != 1 || kept[0].Title != "Dashboard dark mode" {
		t.Errorf("kept = %+v, want only dark mode", kept)
	}
	if len(dropped) != 1 || dropped[0].Title != "Runner transient retry" {
		t.Errorf("dropped = %+v, want retry", dropped)
	}
}

func TestPreVeto_DropsIntraBatchDuplicate(t *testing.T) {
	cands := []Candidate{
		{Title: "Add caching layer", Description: "cache feature id lookups", Source: "fail-pattern"},
		{Title: "Add caching layer", Description: "cache feature id lookups", Source: "stuck"},
	}
	kept, _ := PreVeto(cands, nil, 0.6)
	if len(kept) != 1 {
		t.Errorf("kept = %d, want 1 (intra-batch dedup)", len(kept))
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/evolution/ -run TestPreVeto -v`
Expected: FAIL（`undefined: PreVeto`）

- [ ] **Step 3: 在 discover.go 抽出可調門檻函式**

於 `internal/protocol/discover.go`，把 `IsSimilarFeature` 改寫為：

```go
// IsSimilarFeature 判斷兩段文字是否相似（Jaccard token overlap >= 0.6）。
func IsSimilarFeature(a, b string) bool {
	return IsSimilarFeatureThreshold(a, b, similarityThreshold)
}

// IsSimilarFeatureThreshold 同 IsSimilarFeature，但門檻可調。
func IsSimilarFeatureThreshold(a, b string, threshold float64) bool {
	ta := tokenSet(a)
	tb := tokenSet(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	inter := 0
	for tok := range ta {
		if _, ok := tb[tok]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return false
	}
	return float64(inter)/float64(union) >= threshold
}
```

- [ ] **Step 4: 寫 preveto.go**

```go
package evolution

import (
	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// PreVeto 在 LLM gate 前做便宜去重：candidate 與每個 existing feature
// （Name+" "+Description）以及已保留的 candidate 都比對，相似者丟到 dropped。
// 回傳順序維持輸入順序。
func PreVeto(cands []Candidate, existing []feature.Feature, threshold float64) (kept, dropped []Candidate) {
	for _, c := range cands {
		ctext := c.Title + " " + c.Description
		dup := false
		for _, e := range existing {
			if protocol.IsSimilarFeatureThreshold(ctext, e.Name+" "+e.Description, threshold) {
				dup = true
				break
			}
		}
		if !dup {
			for _, k := range kept {
				if protocol.IsSimilarFeatureThreshold(ctext, k.Title+" "+k.Description, threshold) {
					dup = true
					break
				}
			}
		}
		if dup {
			dropped = append(dropped, c)
		} else {
			kept = append(kept, c)
		}
	}
	return kept, dropped
}
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/evolution/ ./internal/protocol/ -run 'TestPreVeto|TestIsSimilar' -v`
Expected: PASS（含既有 discover 測試不回歸）

- [ ] **Step 6: Commit**

```bash
git add internal/evolution/preveto.go internal/evolution/preveto_test.go internal/protocol/discover.go
git commit -m "feat(F097): add PreVeto dedup with adjustable threshold"
```

---

### Task 4: Verdict schema + 解析

**Files:**
- Create: `internal/evolution/verdict.go`
- Test: `internal/evolution/verdict_test.go`

**Interfaces:**
- Produces: `Verdict{ Title string; Verdict string; ValueScore float64; WhyNotHack string; Reason string }`；常量 `VerdictAccept = "accept"`、`VerdictReject = "reject"`
- Produces: `ParseVerdicts(path string) ([]Verdict, error)`

- [ ] **Step 1: 寫失敗測試**

```go
package evolution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVerdicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gate-verdicts.json")
	content := `{"verdicts":[
		{"title":"A","verdict":"accept","value_score":0.8,"why_not_hack":"real gap","reason":"covers escalation"},
		{"title":"B","verdict":"reject","value_score":0.2,"reason":"low value"}
	]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ParseVerdicts(path)
	if err != nil {
		t.Fatalf("ParseVerdicts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Verdict != VerdictAccept || got[0].ValueScore != 0.8 || got[0].WhyNotHack != "real gap" {
		t.Errorf("verdict[0] = %+v", got[0])
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/evolution/ -run TestParseVerdicts -v`
Expected: FAIL（`undefined: ParseVerdicts`）

- [ ] **Step 3: 寫 verdict.go**

```go
package evolution

import (
	"encoding/json"
	"fmt"
	"os"
)

// gate verdict 的兩種值。
const (
	VerdictAccept = "accept"
	VerdictReject = "reject"
)

// Verdict 為 gate role 對單一 candidate 的判斷。
type Verdict struct {
	Title      string  `json:"title"`
	Verdict    string  `json:"verdict"`
	ValueScore float64 `json:"value_score"`
	WhyNotHack string  `json:"why_not_hack,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

type verdictFile struct {
	Verdicts []Verdict `json:"verdicts"`
}

// ParseVerdicts 讀取 gate role 產出的 gate-verdicts.json。
func ParseVerdicts(path string) ([]Verdict, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read verdicts %s: %w", path, err)
	}
	var f verdictFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse verdicts %s: %w", path, err)
	}
	return f.Verdicts, nil
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/evolution/ -run TestParseVerdicts -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/evolution/verdict.go internal/evolution/verdict_test.go
git commit -m "feat(F097): add gate Verdict schema and parser"
```

---

### Task 5: PostVeto——不可翻硬否決 + convergence cap

**Files:**
- Create: `internal/evolution/postveto.go`
- Test: `internal/evolution/postveto_test.go`

**Interfaces:**
- Consumes: `Candidate`（Task 2）、`Verdict`（Task 4）、`feature.Feature`、`ResolvedEvolution`（Task 1）
- Produces: `Rejection{ Title string; Reason string }`
- Produces: `PostVeto(cands []Candidate, verdicts []Verdict, existing []feature.Feature, cfg ResolvedEvolution) (accepted []Candidate, rejected []Rejection)`

否決規則（任一成立即 reject，依序檢查、記第一個命中的原因）：
1. 無對應 verdict 或 `verdict != accept`
2. `why_not_hack` 為空（trim 後）
3. `value_score < cfg.ValueFloor`
4. 與既有 feature 重複（二次確認，沿用 `protocol.IsSimilarFeatureThreshold`）
5. 已接受數達 `cfg.MaxAcceptPerRun`
6. 既有未做 feature 數 + 已接受數 達 `cfg.MaxBacklogUndone`

接受時把 `value_score`/`why_not_hack` 寫回 candidate。

- [ ] **Step 1: 寫失敗測試**

```go
package evolution

import (
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

func cfgFor(maxAccept, maxBacklog int) ResolvedEvolution {
	return ResolvedEvolution{ValueFloor: 0.6, MaxAcceptPerRun: maxAccept, MaxBacklogUndone: maxBacklog, DedupThreshold: 0.6}
}

func TestPostVeto_AcceptsValid(t *testing.T) {
	cands := []Candidate{{Title: "A", Description: "d", Source: "stuck"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.8, WhyNotHack: "real"}}
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(3, 15))
	if len(acc) != 1 || acc[0].ValueScore != 0.8 || acc[0].WhyNotHack != "real" {
		t.Errorf("accepted = %+v", acc)
	}
	if len(rej) != 0 {
		t.Errorf("rejected = %+v, want none", rej)
	}
}

func TestPostVeto_RejectsNoJustification(t *testing.T) {
	cands := []Candidate{{Title: "A", Description: "d"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "  "}}
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(3, 15))
	if len(acc) != 0 || len(rej) != 1 {
		t.Fatalf("acc=%d rej=%d, want 0/1", len(acc), len(rej))
	}
}

func TestPostVeto_RejectsBelowFloor(t *testing.T) {
	cands := []Candidate{{Title: "A", Description: "d"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.4, WhyNotHack: "x"}}
	acc, _ := PostVeto(cands, verdicts, nil, cfgFor(3, 15))
	if len(acc) != 0 {
		t.Errorf("accepted below floor: %+v", acc)
	}
}

func TestPostVeto_AcceptCap(t *testing.T) {
	cands := []Candidate{{Title: "A", Description: "a"}, {Title: "B", Description: "b"}}
	verdicts := []Verdict{
		{Title: "A", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "x"},
		{Title: "B", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "y"},
	}
	acc, rej := PostVeto(cands, verdicts, nil, cfgFor(1, 15))
	if len(acc) != 1 || len(rej) != 1 {
		t.Errorf("acc=%d rej=%d, want 1/1 (cap)", len(acc), len(rej))
	}
}

func TestPostVeto_BacklogCap(t *testing.T) {
	existing := []feature.Feature{
		{Name: "X", Status: feature.StatusNotStarted},
		{Name: "Y", Status: feature.StatusDone},
	}
	cands := []Candidate{{Title: "A", Description: "a"}}
	verdicts := []Verdict{{Title: "A", Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "x"}}
	// 未做數 = 1 (X)；上限 1 → 已滿，A 應被拒。
	acc, rej := PostVeto(cands, verdicts, existing, cfgFor(3, 1))
	if len(acc) != 0 || len(rej) != 1 {
		t.Errorf("acc=%d rej=%d, want 0/1 (backlog cap)", len(acc), len(rej))
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/evolution/ -run TestPostVeto -v`
Expected: FAIL（`undefined: PostVeto`）

- [ ] **Step 3: 寫 postveto.go**

```go
package evolution

import (
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// Rejection 記錄被 POST-veto 否決的 candidate 與首個命中原因。
type Rejection struct {
	Title  string
	Reason string
}

// undoneStatuses 為計入 backlog 未做數的狀態。
func isUndone(s feature.Status) bool {
	switch s {
	case feature.StatusNotStarted, feature.StatusInProgress, feature.StatusBlocked, feature.StatusNeedsAttention:
		return true
	default:
		return false
	}
}

// PostVeto 對 gate role 的 verdict 套用不可翻硬否決與 convergence cap。
// 任一規則命中即 reject，記錄首個命中原因。reject 永遠蓋過 accept。
func PostVeto(cands []Candidate, verdicts []Verdict, existing []feature.Feature, cfg ResolvedEvolution) (accepted []Candidate, rejected []Rejection) {
	byTitle := make(map[string]Verdict, len(verdicts))
	for _, v := range verdicts {
		byTitle[v.Title] = v
	}

	undone := 0
	for _, e := range existing {
		if isUndone(e.Status) {
			undone++
		}
	}

	for _, c := range cands {
		v, ok := byTitle[c.Title]
		reason := ""
		switch {
		case !ok || v.Verdict != VerdictAccept:
			reason = "gate did not accept"
		case strings.TrimSpace(v.WhyNotHack) == "":
			reason = "missing why_not_hack justification"
		case v.ValueScore < cfg.ValueFloor:
			reason = "value_score below floor"
		case duplicatesExisting(c, existing, cfg.DedupThreshold):
			reason = "duplicates existing feature"
		case len(accepted) >= cfg.MaxAcceptPerRun:
			reason = "max_accept_per_run reached"
		case undone+len(accepted) >= cfg.MaxBacklogUndone:
			reason = "max_backlog_undone reached"
		}
		if reason != "" {
			rejected = append(rejected, Rejection{Title: c.Title, Reason: reason})
			continue
		}
		c.ValueScore = v.ValueScore
		c.WhyNotHack = v.WhyNotHack
		accepted = append(accepted, c)
	}
	return accepted, rejected
}

// duplicatesExisting 判斷 candidate 是否與任一既有 feature 相似。
func duplicatesExisting(c Candidate, existing []feature.Feature, threshold float64) bool {
	ctext := c.Title + " " + c.Description
	for _, e := range existing {
		if protocol.IsSimilarFeatureThreshold(ctext, e.Name+" "+e.Description, threshold) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/evolution/ -run TestPostVeto -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/evolution/postveto.go internal/evolution/postveto_test.go
git commit -m "feat(F097): add PostVeto hard-veto and convergence caps"
```

---

### Task 6: Gate LLM role + template 接線

**Files:**
- Modify: `internal/protocol/role.go`（新增 `RoleGate` 常量；若 role 常量在 `types.go` 則改該檔——實作時 grep `RoleDesigner` 定位）
- Create: `templates/gate.md.tmpl`
- Modify: `cmd/4x/prompt.go`（`roleTemplateFiles` ~ :357 加一條）
- Test: `cmd/4x/prompt_test.go`（新增 `loadRoleTemplate(RoleGate)` 測試）

**Interfaces:**
- Consumes: 既有 `loadRoleTemplate(r protocol.Role)`、`roleTemplateFiles map[protocol.Role]string`
- Produces: `protocol.RoleGate protocol.Role = "gate"`

- [ ] **Step 1: 定位既有 Role 常量並加 RoleGate**

Run: `grep -rn "RoleDesigner\b" internal/protocol/`（定位常量區塊）

在 Role 常量區（與 `RoleDesigner`/`RoleReviewer` 同處）加：

```go
	// RoleGate 是 F097 evolve 價值閘門 role，判斷 candidate 價值並寫 why_not_hack。
	RoleGate Role = "gate"
```

- [ ] **Step 2: 寫 templates/gate.md.tmpl**

```markdown
# Role: Evolution Value Gate

You are the **value gate** for the 4x evolve pipeline. You judge whether each
candidate feature is worth adding to the backlog. You do NOT write code, create
feature files, or modify candidates.

## Input

Read `.4x/gate-input.json` — a list of candidate features that already passed
deduplication. Each has `title`, `description`, `source`.

## Your judgment

For each candidate decide `accept` or `reject`. Be skeptical: the pipeline that
produced these is rewarded for looking productive, so reject anything that is
low-value, vague, speculative, or duplicates work that clearly already exists.

For every candidate you `accept`, you MUST write `why_not_hack`: a concrete
argument for why this is genuinely valuable and not busywork generated to appear
productive. No justification → it will be rejected downstream.

Assign `value_score` in [0.0, 1.0]: evidence strength × blast radius × recurrence.

## Output

Write `.4x/gate-verdicts.json`:

```json
{
  "verdicts": [
    {
      "title": "<exact candidate title>",
      "verdict": "accept | reject",
      "value_score": 0.0,
      "why_not_hack": "<required when accept>",
      "reason": "<one-line rationale>"
    }
  ]
}
```

Emit one verdict per input candidate, using the exact `title`.

## Cannot

- Modify source code, create feature YAML, or edit candidates
- Skip `why_not_hack` on an accepted candidate
```

> 註：template 開頭可比照其他 `*.md.tmpl` 引用 `locale.tmpl`（`loadRoleTemplate` 會自動前置 locale，毋須手動加）。

- [ ] **Step 3: 在 roleTemplateFiles 註冊**

於 `cmd/4x/prompt.go` 的 `roleTemplateFiles` map（~ :357）加：

```go
	protocol.RoleGate: "gate.md.tmpl",
```

- [ ] **Step 4: 寫 template 載入測試**

於 `cmd/4x/prompt_test.go` 加：

```go
func TestLoadRoleTemplate_Gate(t *testing.T) {
	tmpl, err := loadRoleTemplate(protocol.RoleGate)
	if err != nil {
		t.Fatalf("loadRoleTemplate(gate): %v", err)
	}
	if tmpl == nil {
		t.Fatal("expected non-nil template")
	}
}
```

- [ ] **Step 5: 跑測試確認通過（template 已 embed）**

Run: `go test ./cmd/4x/ -run TestLoadRoleTemplate_Gate -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/role.go templates/gate.md.tmpl cmd/4x/prompt.go cmd/4x/prompt_test.go
git commit -m "feat(F097): add gate LLM role and prompt template"
```

---

### Task 7: `4x gate` 命令（pre/post 編排，無 LLM）

**Files:**
- Create: `cmd/4x/gate.go`
- Modify: root command 註冊處（grep `newCheckCmd()` 的 `AddCommand` 呼叫處）
- Test: `cmd/4x/gate_test.go`

**Interfaces:**
- Consumes: `protocol.Find`、`ws.LoadMergedConfig`、`ws.ListFeatures`、`evolution.*`（Tasks 1–5）
- 行為：
  - `4x gate --pre`：讀 `.4x/candidates.json` → `PreVeto`（vs `ListFeatures`）→ 寫 `.4x/gate-input.json`，印保留/丟棄數
  - `4x gate --post`：讀 `.4x/gate-input.json` + `.4x/gate-verdicts.json` → `PostVeto` → 寫 `.4x/accepted-candidates.json`，印接受/否決數與每筆否決原因
  - 兩 flag 互斥；都沒給時印 usage error

> 不呼叫 LLM。gate role（Task 6）由 runner 在 `--pre` 與 `--post` 之間執行（F099 編排）。

- [ ] **Step 1: 寫失敗測試（pre 與 post 各一）**

```go
package main

import (
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/evolution"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestGateCmd_Pre(t *testing.T) {
	dir := t.TempDir()
	if _, err := protocol.Init(dir); err != nil {
		t.Fatal(err)
	}
	cands := []evolution.Candidate{{Title: "Fresh idea", Description: "novel", Source: "stuck"}}
	if err := evolution.SaveCandidates(filepath.Join(dir, ".4x", "candidates.json"), cands); err != nil {
		t.Fatal(err)
	}
	if err := runGate(dir, gateOpts{pre: true}); err != nil {
		t.Fatalf("runGate pre: %v", err)
	}
	got, err := evolution.LoadCandidates(filepath.Join(dir, ".4x", "gate-input.json"))
	if err != nil {
		t.Fatalf("load gate-input: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("gate-input len = %d, want 1", len(got))
	}
}

func TestGateCmd_Post(t *testing.T) {
	dir := t.TempDir()
	if _, err := protocol.Init(dir); err != nil {
		t.Fatal(err)
	}
	dot := filepath.Join(dir, ".4x")
	cands := []evolution.Candidate{{Title: "Fresh idea", Description: "novel", Source: "stuck"}}
	if err := evolution.SaveCandidates(filepath.Join(dot, "gate-input.json"), cands); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dot, "gate-verdicts.json"),
		`{"verdicts":[{"title":"Fresh idea","verdict":"accept","value_score":0.8,"why_not_hack":"real"}]}`)
	if err := runGate(dir, gateOpts{post: true}); err != nil {
		t.Fatalf("runGate post: %v", err)
	}
	acc, err := evolution.LoadCandidates(filepath.Join(dot, "accepted-candidates.json"))
	if err != nil {
		t.Fatalf("load accepted: %v", err)
	}
	if len(acc) != 1 || acc[0].ValueScore != 0.8 {
		t.Errorf("accepted = %+v", acc)
	}
}
```

> `writeFile` helper：若 `cmd/4x` 測試無既有 helper，於本測試檔加 `func writeFile(t *testing.T, path, content string){ t.Helper(); if err := os.WriteFile(path, []byte(content), 0o644); err != nil { t.Fatal(err) } }`（import `os`）。先 grep 確認是否已有同名 helper 以免衝突。

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./cmd/4x/ -run TestGateCmd -v`
Expected: FAIL（`undefined: runGate`）

- [ ] **Step 3: 寫 gate.go**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/evolution"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/spf13/cobra"
)

type gateOpts struct {
	pre  bool
	post bool
}

func newGateCmd() *cobra.Command {
	var opts gateOpts
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Apply the evolve value gate veto layers to candidate features",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			return runGate(cwd, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.pre, "pre", false, "run PRE-veto: dedup candidates.json into gate-input.json")
	cmd.Flags().BoolVar(&opts.post, "post", false, "run POST-veto: apply gate-verdicts.json into accepted-candidates.json")
	return cmd
}

// runGate 依 opts 執行 PRE 或 POST veto。不呼叫 LLM。
func runGate(dir string, opts gateOpts) error {
	if opts.pre == opts.post {
		return fmt.Errorf("specify exactly one of --pre or --post")
	}
	ws, err := protocol.Find(dir)
	if err != nil {
		return err
	}
	cfg, _ := ws.LoadMergedConfig()
	resolved := evolution.ResolveEvolution(cfg)
	dot := ws.DotDir()

	if opts.pre {
		cands, err := evolution.LoadCandidates(filepath.Join(dot, "candidates.json"))
		if err != nil {
			return err
		}
		existing, err := ws.ListFeatures()
		if err != nil {
			return err
		}
		kept, dropped := evolution.PreVeto(cands, existing, resolved.DedupThreshold)
		if err := evolution.SaveCandidates(filepath.Join(dot, "gate-input.json"), kept); err != nil {
			return err
		}
		fmt.Printf("pre-veto: kept %d, dropped %d (duplicate)\n", len(kept), len(dropped))
		return nil
	}

	// post
	cands, err := evolution.LoadCandidates(filepath.Join(dot, "gate-input.json"))
	if err != nil {
		return err
	}
	verdicts, err := evolution.ParseVerdicts(filepath.Join(dot, "gate-verdicts.json"))
	if err != nil {
		return err
	}
	existing, err := ws.ListFeatures()
	if err != nil {
		return err
	}
	accepted, rejected := evolution.PostVeto(cands, verdicts, existing, resolved)
	if err := evolution.SaveCandidates(filepath.Join(dot, "accepted-candidates.json"), accepted); err != nil {
		return err
	}
	fmt.Printf("post-veto: accepted %d, rejected %d\n", len(accepted), len(rejected))
	for _, r := range rejected {
		fmt.Printf("  reject %q: %s\n", r.Title, r.Reason)
	}
	return nil
}
```

> 註：若 `ws.DotDir()` 不存在，用 `protocol` 既有取 `.4x` 路徑的方法（grep `DotDir` 於 workspace.go 確認；researcher 已確認 `Store` interface 有 `DotDir`）。

- [ ] **Step 4: 註冊命令**

Run: `grep -rn "newCheckCmd()" cmd/4x/`（定位 `AddCommand` 集中處）

在該處加：

```go
	rootCmd.AddCommand(newGateCmd())
```

（變數名以該檔實際為準。）

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./cmd/4x/ -run TestGateCmd -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/4x/gate.go cmd/4x/gate_test.go cmd/4x/<root file>
git commit -m "feat(F097): add 4x gate command orchestrating pre/post veto"
```

---

### Task 8: doctor 驗證 evolution 區段

**Files:**
- Modify: `internal/doctor/doctor.go`
- Test: `internal/doctor/doctor_test.go`（若無則新建，比照既有 check 測試風格）

**Interfaces:**
- Consumes: `protocol.Config`、既有 `Check`/`Severity`/`Report`、`Diagnose`
- Produces: `checkEvolution(cfg protocol.Config) []Check`、section 常量 `sectionEvolution`

驗證規則：
- `Evolution == nil` → 一筆 `SeverityPass`「evolution not configured (defaults apply)」
- `ValueFloor` 不在 [0,1] → `SeverityFail`
- `MaxAcceptPerRun < 0` 或 `MaxBacklogUndone < 0` → `SeverityFail`
- `DedupThreshold` 設了但不在 [0,1] → `SeverityFail`
- 全部合法 → `SeverityPass`

- [ ] **Step 1: 寫失敗測試**

```go
func TestCheckEvolution(t *testing.T) {
	bad := -1.0
	_ = bad
	cases := []struct {
		name     string
		cfg      protocol.Config
		wantFail bool
	}{
		{"nil ok", protocol.Config{}, false},
		{"valid", protocol.Config{Evolution: &protocol.EvolutionConfig{ValueFloor: 0.6, MaxAcceptPerRun: 3, MaxBacklogUndone: 10, DedupThreshold: 0.6}}, false},
		{"floor too high", protocol.Config{Evolution: &protocol.EvolutionConfig{ValueFloor: 1.5}}, true},
		{"negative cap", protocol.Config{Evolution: &protocol.EvolutionConfig{MaxAcceptPerRun: -1}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checks := checkEvolution(tc.cfg)
			gotFail := false
			for _, c := range checks {
				if c.Severity == SeverityFail {
					gotFail = true
				}
			}
			if gotFail != tc.wantFail {
				t.Errorf("fail = %v, want %v (checks=%+v)", gotFail, tc.wantFail, checks)
			}
		})
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/doctor/ -run TestCheckEvolution -v`
Expected: FAIL（`undefined: checkEvolution`）

- [ ] **Step 3: 寫 checkEvolution 並接進 Diagnose**

於 `internal/doctor/doctor.go` section 常量區加 `sectionEvolution = "evolution"`，並加：

```go
// checkEvolution 驗證 F097 evolution 設定的數值範圍。read-only。
func checkEvolution(cfg protocol.Config) []Check {
	if cfg.Evolution == nil {
		return []Check{{Section: sectionEvolution, Name: "config", Severity: SeverityPass, Detail: "evolution not configured (defaults apply)"}}
	}
	var checks []Check
	e := cfg.Evolution
	if e.ValueFloor < 0 || e.ValueFloor > 1 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "value_floor", Severity: SeverityFail, Detail: "must be in [0,1]"})
	}
	if e.MaxAcceptPerRun < 0 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "max_accept_per_run", Severity: SeverityFail, Detail: "must be >= 0"})
	}
	if e.MaxBacklogUndone < 0 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "max_backlog_undone", Severity: SeverityFail, Detail: "must be >= 0"})
	}
	if e.DedupThreshold < 0 || e.DedupThreshold > 1 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "dedup_threshold", Severity: SeverityFail, Detail: "must be in [0,1]"})
	}
	if len(checks) == 0 {
		checks = append(checks, Check{Section: sectionEvolution, Name: "config", Severity: SeverityPass, Detail: "evolution settings valid"})
	}
	return checks
}
```

在 `Diagnose` 內依既有風格 append：`report.Checks = append(report.Checks, checkEvolution(cfg)...)`（以該檔實際聚合方式為準）。

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/doctor/ -run TestCheckEvolution -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/doctor/doctor.go internal/doctor/doctor_test.go
git commit -m "feat(F097): add doctor validation for evolution settings"
```

---

### Task 9: Golden fixtures（holdout-as-test）

**Files:**
- Create: `internal/evolution/testdata/gate-fixtures/candidates.json`
- Create: `internal/evolution/testdata/gate-fixtures/expected.json`
- Create: `internal/evolution/fixtures_test.go`

**Interfaces:**
- Consumes: `LoadCandidates`、`PreVeto`、`PostVeto`、`ParseVerdicts`、`ResolvedEvolution`

設計：fixtures 含明顯該拒（垃圾/重複/無價值）與明顯該收的 candidate，附一份「模擬完美 gate」的 verdicts，驗證 **CLI veto 層**在 gate 正確判斷下仍正確收斂；另含一筆「gate 誤放垃圾」case 驗證 POST-veto 的 `why_not_hack`/floor 仍能擋下。fixtures 不進任何 runtime prompt（僅測試讀）。

- [ ] **Step 1: 建 fixtures candidates.json**

`internal/evolution/testdata/gate-fixtures/candidates.json`：

```json
{
  "candidates": [
    { "title": "Recover abandoned features automatically", "description": "scan abandoned features and propose recovery", "source": "stuck" },
    { "title": "asdf test junk", "description": "", "source": "fail-pattern" },
    { "title": "Add runner transient retry", "description": "retry transient runner errors with backoff", "source": "fail-pattern" }
  ]
}
```

- [ ] **Step 2: 建 expected.json（標準答案）**

`internal/evolution/testdata/gate-fixtures/expected.json`：

```json
{
  "accept": ["Recover abandoned features automatically", "Add runner transient retry"],
  "reject": ["asdf test junk"]
}
```

- [ ] **Step 3: 寫 fixtures_test.go**

```go
package evolution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenFixtures_VetoCatchesJunk 用標準答案驗證 veto 層：
// 給一份「gate 把全部都 accept（含垃圾、含無 why_not_hack）」的 verdicts，
// POST-veto 仍須擋下 expected.reject 的項目。
func TestGoldenFixtures_VetoCatchesJunk(t *testing.T) {
	base := filepath.Join("testdata", "gate-fixtures")
	cands, err := LoadCandidates(filepath.Join(base, "candidates.json"))
	if err != nil {
		t.Fatal(err)
	}
	exp := struct {
		Accept []string `json:"accept"`
		Reject []string `json:"reject"`
	}{}
	data, err := os.ReadFile(filepath.Join(base, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatal(err)
	}

	// 模擬一個橡皮圖章 gate：全部 accept，但垃圾項缺 why_not_hack / 低分。
	var verdicts []Verdict
	for _, c := range cands {
		v := Verdict{Title: c.Title, Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "stated"}
		if c.Title == "asdf test junk" {
			v.ValueScore = 0.1 // 低於 floor，POST-veto 應擋
			v.WhyNotHack = ""
		}
		verdicts = append(verdicts, v)
	}

	cfg := ResolvedEvolution{ValueFloor: 0.6, MaxAcceptPerRun: 10, MaxBacklogUndone: 100, DedupThreshold: 0.6}
	accepted, _ := PostVeto(cands, verdicts, nil, cfg)

	gotAccept := map[string]bool{}
	for _, a := range accepted {
		gotAccept[a.Title] = true
	}
	for _, title := range exp.Reject {
		if gotAccept[title] {
			t.Errorf("veto failed to reject junk candidate %q", title)
		}
	}
	for _, title := range exp.Accept {
		if !gotAccept[title] {
			t.Errorf("veto wrongly rejected valid candidate %q", title)
		}
	}
}
```

- [ ] **Step 4: 跑測試確認通過**

Run: `go test ./internal/evolution/ -run TestGoldenFixtures -v`
Expected: PASS

- [ ] **Step 5: 全套驗證**

Run: `go build ./cmd/4x && go vet ./... && go test -race ./internal/evolution/ ./cmd/4x/ ./internal/doctor/ ./internal/protocol/`
Expected: 全 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/evolution/testdata/ internal/evolution/fixtures_test.go
git commit -m "test(F097): add golden fixtures validating veto catches junk"
```

---

## Self-Review

**Spec coverage：**
- 純 LLM gate role → Task 6 ✓
- CLI PRE-veto（Jaccard，省錢）→ Task 3 ✓
- CLI POST-veto 不可翻（重複/無論述/低分/cap）→ Task 5 ✓
- `gate-verdicts.json` schema → Task 4 ✓
- candidate 層級否決、實作層 regression 移交 F098 → POST-veto 規則不含測試/scope，符合 ✓
- golden fixtures（不洩題、擋橡皮圖章）→ Task 9 ✓
- settings `evolution` 區段 + 預設 → Task 1 ✓
- doctor 驗證 → Task 8 ✓
- candidates.json 契約（F095 未實作）→ 「共享資料契約」段 + Task 2 ✓
- 輸出止於 accepted-candidates.json（不建 YAML）→ Task 7 行為 ✓

**Placeholder scan：** 無 TBD/TODO；每個 code step 附完整程式碼。三處標「以實際為準」（role 常量位置、root 註冊處、`DotDir` 方法名）皆附 grep 指令定位，非 placeholder。

**Type consistency：** `Candidate`/`Verdict`/`ResolvedEvolution`/`Rejection` 跨 task 簽名一致；`PreVeto`/`PostVeto`/`ParseVerdicts`/`LoadCandidates`/`SaveCandidates` 命名前後一致；`protocol.IsSimilarFeatureThreshold` 在 Task 3 定義、Task 5 使用一致。

## 範圍外（交接）

- gate role 的 runner 實際執行（spawn LLM）與 `--pre`→role→`--post` 串接 → **F099 evolve-driver**
- candidate 的產生（escalation/stuck/fail-pattern 掃描）→ **F095 history-miner**
- accepted-candidates 補 subtask/scope 後 enqueue 成 feature YAML → **F096 discovered-feature-enrichment**
- 實作層 regression（測試數下降/scope 擴大）guard → **F098 self-mod-scope-guard**
{% endraw %}
