# AC Verify Type Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Designer 標記每條 AC 的驗證類型，Guard 依標記 enforce Tester 的 evidence 品質。

**Architecture:** 在 TestStrategy 加 `ACVerifyMap`，ACEvidence 加 `VerifyType`。Guard 的 `checkACEvidence` 讀 map 後對 execution 類 AC 做正則檢查。Templates 更新讓 Designer 產出標記、Tester 依標記行動。

**Tech Stack:** Go 1.26+, Cobra CLI, YAML, Go templates

## Global Constraints

- CLI 層不呼叫 LLM
- 遵循 gofmt
- 測試用 Go 標準 testing package
- 每次改動後跑 `make build && make test && make lint`
- 向後相容：舊 feature 無 ac_verify_map → 全部預設 execution

---

### Task 1: ACEvidence 加 VerifyType 欄位 + TestStrategy 加 ACVerifyMap

**Files:**
- Modify: `internal/protocol/types.go:185-189` (ACEvidence struct)
- Modify: `internal/protocol/types.go:232-246` (TestStrategy struct)
- Test: `internal/guard/check_test.go` (existing tests must still pass)

**Interfaces:**
- Produces: `ACEvidence.VerifyType string` field, `TestStrategy.ACVerifyMap map[string]string` field

- [ ] **Step 1: Add VerifyType to ACEvidence**

In `internal/protocol/types.go`, change:

```go
// ACEvidence 是單一 acceptance criterion 的驗證結果
type ACEvidence struct {
	ID       string   `json:"id"`
	Passed   bool     `json:"passed"`
	Evidence []string `json:"evidence"`
}
```

to:

```go
// ACEvidence 是單一 acceptance criterion 的驗證結果
type ACEvidence struct {
	ID         string   `json:"id"`
	Passed     bool     `json:"passed"`
	Evidence   []string `json:"evidence"`
	VerifyType string   `json:"verify_type,omitempty"`
}
```

- [ ] **Step 2: Add ACVerifyMap to TestStrategy**

In `internal/protocol/types.go`, add field after `ManualChecks`:

```go
// TestStrategy 是 test-strategy.yaml 的結構
type TestStrategy struct {
	Web          bool                `yaml:"web" json:"web"`
	API          bool                `yaml:"api" json:"api"`
	Gate         bool                `yaml:"gate" json:"gate"`
	CoderOnly    bool                `yaml:"coder_only" json:"coder_only"`
	Verify       []string            `yaml:"verify_commands" json:"verify_commands"`
	HealthCheck  *HealthCheck        `yaml:"health_check,omitempty" json:"health_check,omitempty"`
	VerifyGroups map[string][]string `yaml:"verify_groups,omitempty" json:"verify_groups,omitempty"`
	Profiles     []string            `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	ManualChecks []ManualCheck       `yaml:"manual_checks,omitempty" json:"manual_checks,omitempty"`
	ACVerifyMap  map[string]string   `yaml:"ac_verify_map,omitempty" json:"ac_verify_map,omitempty"`
}
```

- [ ] **Step 3: Run tests to verify no breakage**

Run: `make build && make test && make lint`
Expected: all pass — new fields are omitempty, zero-value compatible

- [ ] **Step 4: Commit**

```bash
git add internal/protocol/types.go
git commit -m "feat: add VerifyType to ACEvidence and ACVerifyMap to TestStrategy"
```

---

### Task 2: Guard evidence 品質檢查

**Files:**
- Modify: `internal/guard/check.go:236,241-262` (checkACEvidence)
- Test: `internal/guard/check_test.go` (add new tests)

**Interfaces:**
- Consumes: `protocol.ACEvidence.VerifyType`, `protocol.TestStrategy.ACVerifyMap`, `protocol.Workspace.ReadTestStrategy()`
- Produces: guard errors when execution-type AC evidence lacks command output

- [ ] **Step 1: Write failing tests**

Add to `internal/guard/check_test.go`:

```go
func TestCheckTestingToAccepting_ExecutionEvidenceLacksOutput(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	// Write test-strategy with ac_verify_map
	tsData := []byte("verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n")
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile), string(tsData))

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"code looks correct at main.go:42"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("execution-type AC with code-only evidence should fail")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "AC-1") && strings.Contains(e, "no execution output") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error about AC-1 lacking execution output, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_InspectionEvidencePass(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	tsData := []byte("verify_commands:\n  - make test\nac_verify_map:\n  AC-1: inspection\n")
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile), string(tsData))

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"git diff shows no API signature changes"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("inspection AC with non-empty evidence should pass, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_SkipVerifyType(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	tsData := []byte("verify_commands:\n  - make test\nac_verify_map:\n  AC-1: skip\n")
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile), string(tsData))

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("skip AC should pass without evidence, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_NoMapDefaultsExecution(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	// No test-strategy.yaml at all — defaults to execution
	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"code looks correct at main.go:42"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("no ac_verify_map should default to execution and fail on code-only evidence")
	}
}

func TestCheckTestingToAccepting_ExecutionEvidenceWithOutput(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	tsData := []byte("verify_commands:\n  - make test\nac_verify_map:\n  AC-1: unit-test\n")
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile), string(tsData))

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"$ go test -run TestFoo → PASS (0.02s)"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if !result.Pass {
		t.Fatalf("execution AC with real output should pass, got: %v", result.Errors)
	}
}

func TestCheckTestingToAccepting_InvalidVerifyType(t *testing.T) {
	ws := setupGuardWorkspace(t, "feat-1")
	roundDir := ws.RoundDir("feat-1", 1)
	featureDir := ws.FeatureDir("feat-1")

	tsData := []byte("verify_commands:\n  - make test\nac_verify_map:\n  AC-1: bogus\n")
	writeFile(t, filepath.Join(ws.FeatureDir("feat-1"), protocol.TestStratFile), string(tsData))

	data, _ := json.Marshal(protocol.VerifyEvidence{Passed: true, Round: 1,
		ACResults: []protocol.ACEvidence{
			{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS"}},
		}})
	writeFile(t, filepath.Join(roundDir, protocol.VerifyFile), string(data))
	writeFile(t, filepath.Join(roundDir, protocol.TestReport), "# Test")
	writeFile(t, filepath.Join(featureDir, protocol.FinalReport), "# Final")

	result := CheckTestingToAccepting(ws, "feat-1", 1)
	if result.Pass {
		t.Fatal("invalid verify_type should fail")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestCheckTestingToAccepting_(ExecutionEvidenceLacks|InspectionEvidence|SkipVerify|NoMapDefaults|ExecutionEvidenceWith|InvalidVerify)" ./internal/guard/ -v`
Expected: all 6 new tests FAIL (no verify_type logic yet)

- [ ] **Step 3: Implement checkACEvidence changes**

Replace `checkACEvidence` in `internal/guard/check.go`:

```go
// validACVerifyTypes 是合法的 AC 驗證類型。
var validACVerifyTypes = map[string]bool{
	"unit-test":    true,
	"integration":  true,
	"inspection":   true,
	"skip":         true,
	"execution":    true,
}

// executionPattern 匹配命令輸出的特徵模式。
var executionPattern = regexp.MustCompile(`(\$\s|PASS|FAIL|^ok\s|--- |exit code|→|stdout|stderr|\d+\.\d+s)`)

// isExecutionType 判斷 verify_type 是否需要執行輸出作為 evidence。
func isExecutionType(vt string) bool {
	return vt == "unit-test" || vt == "integration" || vt == "execution"
}

// checkACEvidence 檢查 verify.json 的 per-AC evidence mapping：每個 AC 都必須 passed 且有 evidence。
// ac_results 為空時阻擋（舊格式 verify.json 不通過此檢查）。
// 若 test-strategy.yaml 有 ac_verify_map，依 verify_type 檢查 evidence 品質。
func checkACEvidence(ws *protocol.Workspace, featureID string, evidence protocol.VerifyEvidence, r *CheckResult) {
	if len(evidence.ACResults) == 0 {
		r.Pass = false
		r.Errors = append(r.Errors, "verify.json missing ac_results: every acceptance criterion must have evidence")
		r.RetryableErrors++
		return
	}

	ts, _ := ws.ReadTestStrategy(featureID)
	verifyMap := ts.ACVerifyMap // nil if not set

	for _, ac := range evidence.ACResults {
		vt := "execution" // default
		if verifyMap != nil {
			if v, ok := verifyMap[ac.ID]; ok {
				vt = v
			}
		}

		if !validACVerifyTypes[vt] {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("AC %s has invalid verify_type %q", ac.ID, vt))
			r.RetryableErrors++
			continue
		}

		if vt == "skip" {
			continue
		}

		if !ac.Passed {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("AC %s failed", ac.ID))
			r.RetryableErrors++
		}

		if len(ac.Evidence) == 0 {
			r.Pass = false
			r.Errors = append(r.Errors, fmt.Sprintf("AC %s has no evidence", ac.ID))
			r.RetryableErrors++
			continue
		}

		if isExecutionType(vt) {
			hasExecEvidence := false
			for _, e := range ac.Evidence {
				if executionPattern.MatchString(e) {
					hasExecEvidence = true
					break
				}
			}
			if !hasExecEvidence {
				r.Pass = false
				r.Errors = append(r.Errors, fmt.Sprintf(
					"AC %s: verify_type=%s but evidence has no execution output (need command results, not code references)", ac.ID, vt))
				r.RetryableErrors++
			}
		}
	}
}
```

- [ ] **Step 4: Update checkACEvidence call site**

In `checkTestingToAccepting`, change line 236 from:

```go
checkACEvidence(evidence, r)
```

to:

```go
checkACEvidence(ws, featureID, evidence, r)
```

- [ ] **Step 5: Add regexp import**

Add `"regexp"` to the import block at the top of `check.go`.

- [ ] **Step 6: Fix existing test evidence**

The existing `TestCheckTestingToAccepting_AllArtifactsPresent` test uses `Evidence: []string{"ok"}` which won't match the execution pattern. Since it has no test-strategy.yaml, it defaults to execution. Update:

In `internal/guard/check_test.go`, change:

```go
ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"ok"}}}})
```

to:

```go
ACResults: []protocol.ACEvidence{{ID: "AC-1", Passed: true, Evidence: []string{"$ make test → PASS (1.23s)"}}}})
```

Check all other existing tests in `check_test.go` that create `ACEvidence` with plain text evidence — update them the same way.

- [ ] **Step 7: Run tests**

Run: `make build && make test && make lint`
Expected: all pass, including 6 new tests

- [ ] **Step 8: Commit**

```bash
git add internal/guard/check.go internal/guard/check_test.go
git commit -m "feat: guard enforces AC evidence quality by verify_type"
```

---

### Task 3: Designer template 更新

**Files:**
- Modify: `templates/designer.md.tmpl:95-132`

**Interfaces:**
- Produces: Designer 產出的 acceptance-criteria.md 含 Verify 欄、test-strategy.yaml 含 ac_verify_map

- [ ] **Step 1: Update acceptance-criteria.md format**

In `templates/designer.md.tmpl`, change lines 95-99 from:

```
== acceptance-criteria.md format ==
# Acceptance Criteria
| # | Criterion | Verification Method |
|---|---|---|
| AC-1 | ... | ... |
```

to:

```
== acceptance-criteria.md format ==
# Acceptance Criteria
| # | Criterion | Verification Method | Verify |
|---|---|---|---|
| AC-1 | ... | ... | unit-test |

The Verify column classifies how each AC should be verified:
- unit-test: verifiable by running a unit test — Tester must show test command + output
- integration: needs a running service or cross-component check — Tester must show command + output
- inspection: verifiable by reading code or diff — Tester may use static evidence
- skip: already verified by Reviewer or not applicable — Tester skips this AC
Default to unit-test when uncertain. The Guard rejects execution-type ACs without command output.
```

- [ ] **Step 2: Add ac_verify_map to test-strategy.yaml format**

In `templates/designer.md.tmpl`, after the `manual_checks:` section (around line 132), add:

```
ac_verify_map maps each AC to its verify type. Must match the Verify column in acceptance-criteria.md.
ac_verify_map:
  AC-1: unit-test
  AC-2: integration
  AC-3: inspection
```

- [ ] **Step 3: Verify template renders**

Run: `make build && make test`
Expected: pass (templates are embedded at build time)

- [ ] **Step 4: Commit**

```bash
git add templates/designer.md.tmpl
git commit -m "feat: designer template adds Verify column and ac_verify_map"
```

---

### Task 4: Design Reviewer template 更新

**Files:**
- Modify: `templates/design-reviewer.md.tmpl:70-75`

**Interfaces:**
- Consumes: Designer 產出的 acceptance-criteria.md Verify 欄

- [ ] **Step 1: Add checklist item to Review Scope**

In `templates/design-reviewer.md.tmpl`, change lines 70-74 from:

```
== Review Scope ==
Assess the Designer's deliverables before coding starts:
- Architecture risks: coupling, state transitions, protocol changes, migration risk, and missing integration points.
- Overengineering: unnecessary abstractions, premature extensibility, new dependencies, or work beyond the feature.
- Missing requirements: acceptance criteria gaps, test strategy gaps, ambiguous source of truth, and docs/plugin context that must be updated.
```

to:

```
== Review Scope ==
Assess the Designer's deliverables before coding starts:
- Architecture risks: coupling, state transitions, protocol changes, migration risk, and missing integration points.
- Overengineering: unnecessary abstractions, premature extensibility, new dependencies, or work beyond the feature.
- Missing requirements: acceptance criteria gaps, test strategy gaps, ambiguous source of truth, and docs/plugin context that must be updated.
- AC verify types: every AC row must have a Verify column value (unit-test / integration / inspection / skip). The ac_verify_map in test-strategy.yaml must match. FAIL if any AC is missing its verify type.
```

- [ ] **Step 2: Verify template renders**

Run: `make build && make test`
Expected: pass

- [ ] **Step 3: Commit**

```bash
git add templates/design-reviewer.md.tmpl
git commit -m "feat: design reviewer checks AC verify type completeness"
```

---

### Task 5: Tester template 更新

**Files:**
- Modify: `templates/tester.md.tmpl:109-132,205-220`

**Interfaces:**
- Consumes: `test-strategy.yaml` 的 `ac_verify_map`

- [ ] **Step 1: Update Per-AC Evidence Mapping section**

In `templates/tester.md.tmpl`, replace lines 109-132 with:

```
== Per-AC Evidence Mapping (REQUIRED) ==
After running `4x verify`, you MUST update verify.json to include `ac_results`.
Read the existing verify.json, add the `ac_results` field, and write it back.

Read `ac_verify_map` from test-strategy.yaml to know each AC's verify type.
If test-strategy.yaml has no `ac_verify_map`, treat ALL ACs as execution type.

Every AC item from acceptance-criteria.md must appear in `ac_results` with:
- `id`: the AC identifier (e.g. "AC-1", "AC-2")
- `passed`: true/false
- `verify_type`: the type from ac_verify_map (or "execution" if not mapped)
- `evidence`: array of strings — content depends on verify_type:

| verify_type | Required evidence |
|---|---|
| unit-test | Command + test output (e.g. "$ go test -run TestFoo → PASS (0.02s)") |
| integration | Command + runtime output (e.g. "$ curl localhost:4567/api → 200 OK") |
| inspection | Non-empty description (code ref or diff check is acceptable) |
| skip | Empty array — this AC is skipped |

Example ac_results entry in verify.json:
```json
{
  "passed": true,
  "round": 1,
  "role": "tester",
  "commands": [...],
  "ac_results": [
    {"id": "AC-1", "verify_type": "unit-test", "passed": true, "evidence": ["$ go test -run TestFindSimilar → PASS (0.03s)"]},
    {"id": "AC-2", "verify_type": "inspection", "passed": true, "evidence": ["git diff shows no API signature changes"]},
    {"id": "AC-3", "verify_type": "skip", "passed": true, "evidence": []}
  ]
}
```

The guard will block testing → accepting if:
- Any execution-type AC evidence lacks command output patterns
- Any non-skip AC has empty evidence
```

- [ ] **Step 2: Strengthen "You are NOT a Reviewer" section**

In `templates/tester.md.tmpl`, replace lines 205-220 with:

```
== You are NOT a Reviewer — do NOT repeat their work ==
The Reviewer has ALREADY done these things. Repeating them wastes a full round of tokens:
- Reading source code to check correctness → Reviewer did this
- Grepping for patterns or naming conventions → Reviewer did this
- Static analysis of code structure → Reviewer did this

Your UNIQUE value is verifying RUNTIME BEHAVIOR:
- Execute commands and observe actual output
- Start services and send real requests
- Run the feature end-to-end and confirm it works
- Compare actual output against expected output

Evidence rules by verify_type:
- unit-test / integration: you MUST include actual command output. "I read the code and it looks correct" will be REJECTED by the guard. Include the $ command and its output.
- inspection: static evidence is acceptable (diff output, grep results, code reference). Only use this for ACs explicitly marked as inspection in ac_verify_map.
- skip: no evidence needed. Only use for ACs marked skip.

If ac_verify_map is not present in test-strategy.yaml, ALL ACs default to execution — you must provide command output for every single AC.
```

- [ ] **Step 3: Verify template renders**

Run: `make build && make test`
Expected: pass

- [ ] **Step 4: Commit**

```bash
git add templates/tester.md.tmpl
git commit -m "feat: tester template reads ac_verify_map and enforces evidence rules"
```

---

### Task 6: 驗證全流程

**Files:**
- Run: full test suite

**Interfaces:**
- Consumes: all changes from Tasks 1-5

- [ ] **Step 1: Run full verification**

Run: `make build && make test && make lint`
Expected: all pass

- [ ] **Step 2: Verify existing features still work**

Run: `bin/4x check <any-completed-feature-id>` on a feature that already passed testing.
Expected: should still pass (no ac_verify_map → defaults to execution, but guard only runs on testing→accepting transition, not retroactively)
