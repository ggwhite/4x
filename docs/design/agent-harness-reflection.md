# 4x Agent Harness Reflection — Multi-Agent Use, Context Boundaries, and Verification

## Purpose

本文件整理了對 4x 多角色開發流程的設計反思，
來源包括 Kelly Tsai 的 Agent Teams 影片分析、Codex 的結構化反思、以及實際使用經驗。
供後續 Claude session 在開發 4x 時參考。

核心結論：

> 4x 不該追求「更多 agent」。應該追求明確的 context boundary、
> durable artifact、deterministic gate，只在任務形狀值得時才升級到重量級流程。

4x 已經比 naive 的多 agent 聊天系統更接近穩健的 agent harness：

- Role 之間透過 `.4x/run/{feature}/` artifact 通訊，不是 fragile memory
- CLI 擁有 state transition、guardrail、verification、runner execution
- LLM role 被限定在需要判斷力的工作
- Template 要求 incremental write 和 resume behavior
- Batch mode 用 dependency DAG 排程，不是盲目平行化

剩餘的風險不是缺 agent role。風險是在小型、序列、context 密集的任務上過度使用完整 role pipeline。

## Background Principles

### Subagent vs Agent Team vs Dynamic Workflow

Use these terms precisely when designing 4x behavior:

| Mode | Meaning | Good fit | Bad fit |
|---|---|---|---|
| Subagent | One controller splits independent work into isolated workers that report back. Workers do not need to talk to each other. | Independent batch work, file-by-file review, per-package checks, parallel verify groups. | Tasks where worker A's output is required by worker B. |
| Agent Team | Multiple agents share a coordination surface and update each other while working. | Ambiguous cross-domain work where discoveries in one area change another area's next step. | Independent tasks where communication adds cost without improving correctness. |
| Dynamic Workflow | The controller chooses how many agents to spawn and when, based on task progress. | Large, decomposable research or refactor work with changing search direction. | Fixed workflow automation with known steps and deterministic checks. |

For 4x, most workflows should be closer to **artifact-backed subagents** than to an
open-ended agent team. Even when multiple agents are used, the coordination surface
should remain `.4x/` files, events, reports, and verify results.

### Split by Context, Not by Job Title

The most important rule for future 4x work:

> Split work by the context each worker must see, not by human job titles.

Bad split:

- Planner agent
- Coder agent
- Tester agent
- Reviewer agent

This can become a telephone game if every role needs the same large context but only
receives a lossy summary from the previous role.

Better split:

- `internal/state` transition semantics and tests
- `internal/protocol` artifact schemas and workspace IO
- `cmd/4x/run_*` Cobra flags and CLI compatibility
- `internal/runner` subprocess execution behavior
- `templates/*` role prompt contract changes
- `internal/guard` deterministic gates

Roles are still useful, but feature decomposition should start from context boundaries.

## What 4x Already Gets Right

### Artifact-Driven Handoff

The `.4x/run/{feature}/` protocol is the strongest part of the design.

Key artifacts:

- `task-brief.md`
- `acceptance-criteria.md`
- `test-strategy.yaml`
- `rounds/round-N/coder-report.md`
- `rounds/round-N/review-report.md`
- `rounds/round-N/test-report.md`
- `rounds/round-N/deep-review-report.md`
- `verify.json`
- `final-report.md`
- `retro-learnings.json`

This avoids the common multi-agent failure mode where each agent only sees a paraphrase
of what the previous agent thought. The durable artifact is the interface.

Design implication:

- Prefer adding or tightening artifacts over adding more free-form role discussion.
- If an agent needs to communicate state to another agent, ask whether that state should
  become a structured file or schema field.

### Deterministic CLI Gates

The CLI, not the model, owns:

- State transitions
- Scope checks
- Required-file checks
- Baseline checks
- Verify command execution
- Batch dependency scheduling
- Runner exit handling

This is correct. LLMs may produce reports, but they should not be trusted to enforce
the core process invariants.

Design implication:

- When a prompt instruction becomes important enough to block unsafe behavior, move it
  into `internal/guard`, `internal/verify`, `internal/state`, or schema validation.
- Treat prompt-only rules as guidance, not as enforcement.

### Resume and Incremental Writes

The role templates already instruct agents to:

- Check whether output files already exist.
- Resume incomplete sections.
- Write reports incrementally.
- Use git history and diffs to understand previous progress.

This directly addresses long-running agent failure modes. It keeps progress recoverable
after crash, timeout, context compaction, or model failure.

Design implication:

- Preserve this behavior when adding new roles or reports.
- Any new long-running role must have a resumable artifact format.

### Batch Mode Does Not Blindly Parallelize

Batch mode already uses:

- Dependency DAG
- Cycle detection
- Union-Find grouping
- Hub and leaf repo distinction
- Bridge detection
- Chain length limits

This is a good start because it recognizes that parallelism is only safe when work is
actually independent.

Design implication:

- Keep improving scheduling by context boundaries, not just by feature count.
- In a single Go repo, repo-level grouping is too coarse; package or subsystem grouping
  may become necessary.

## Main Risks

### Risk 1: Full Pipeline Cost for Small Tasks

The current full loop can include:

1. Designer
2. Design Reviewer
3. Coder
4. Reviewer
5. Tester
6. Deep Reviewer
7. Acceptor

This is valuable for high-risk changes. It is excessive for small, deterministic changes.

Symptoms:

- Simple CLI copy changes taking many role turns.
- Deep Review running all angles on low-risk diffs.
- Acceptor summarizing obvious outcomes.
- More time spent moving through phases than changing code.

Recommendation:

- Treat the full pipeline as the expensive path.
- Make profile selection first-class and risk-based.
- Let low-risk profiles skip heavy roles while keeping deterministic verification.

Suggested profile levels:

| Profile | Use case | Roles |
|---|---|---|
| `tiny` | Text/doc changes, typo fixes, one-file deterministic edits | Coder + verify |
| `unit` | Local package changes with clear tests | Coder + Reviewer + Tester |
| `normal` | Standard feature work | Designer + Coder + Reviewer + Tester |
| `risk` | State machine, protocol, guardrails, runner, merge, data loss risk | Designer + Design Reviewer + Coder + Reviewer + Tester + Deep Reviewer + Acceptor |
| `architecture` | Cross-package refactor, public protocol changes, workflow changes | Full pipeline plus explicit design doc |

### Risk 2: Role Split Masquerading as Context Split

4x role separation is useful, but future feature design can still accidentally split by
job title rather than context.

Example risk:

- Designer writes a broad plan for a run loop refactor.
- Coder touches many run files at once.
- Reviewer has to reconstruct all context from a huge diff.
- Tester only sees command pass/fail, not whether architectural intent survived.

This is not solved by adding more roles. It is solved by making features smaller and
context-bound.

Recommendation:

- Large refactors should be decomposed by subsystem before entering the role loop.
- Each feature should have a narrow context map: packages, files, invariants, tests.
- A feature that requires the Coder to hold too many unrelated contexts should be split.

### Risk 3: Verify Evidence May Be Too Coarse

The current guard checks that `verify.json` passed and has evidence entries. That is
necessary but not sufficient.

Potential anti-hack failure:

- `go test ./...` passes.
- `verify.json` has one evidence entry.
- `acceptance-criteria.md` has five ACs.
- No machine check proves each AC maps to evidence.

Recommendation:

- Strengthen the verify schema so each acceptance criterion has explicit evidence.
- A transition to accepting should fail if any AC lacks evidence.

Potential schema direction:

```json
{
  "passed": true,
  "criteria": [
    {
      "id": "AC-1",
      "passed": true,
      "evidence": [
        {
          "type": "command",
          "command": "go test ./internal/state",
          "summary": "state transition tests pass"
        }
      ]
    }
  ]
}
```

If changing `verify.json` is too large, enforce the mapping in `test-report.md` first:

- Every AC row must contain `PASS` or `FAIL`.
- Every `PASS` row must include a command, file, test name, or log reference.
- `SKIP` above threshold blocks acceptance.

### Risk 4: Deep Review Can Become a Fixed Tax

The deep reviewer template is strong. The 11 angles cover real defects:

- Line-by-line diff scan
- Removed-behavior audit
- Cross-file trace
- Concurrency and state
- Reuse
- Simplification
- Altitude
- Convention compliance
- Git history regression
- Code comment compliance
- Past review feedback

The risk is not quality. The risk is running all angles when only a subset matters.

Recommendation:

- Select deep review angles by diff type.
- Make the angle selection explicit in the run artifact.
- Let deterministic heuristics suggest angles.

Example mapping:

| Diff touches | Required angles |
|---|---|
| `internal/state`, `internal/protocol`, `internal/guard` | 1, 2, 3, 4, 8, 9, 10, 11 |
| `cmd/4x/run_*`, orchestration | 1, 2, 3, 4, 7, 8, 9, 11 |
| `templates/*` | 2, 6, 8, 10, 11 |
| docs only | 6, 8, 10 |
| tests only | 1, 2, 8, 11 |
| dashboard UI | 1, 3, 6, 8, plus screenshot/manual evidence |

### Risk 5: Learnings Can Become Prompt Bloat

The `selected-learnings.json` mechanism is good because it avoids injecting every past
learning into every prompt. Over time, however, even selected learnings can drift.

Recommendation:

- Periodically prune `.4x/learnings.json`.
- Merge duplicate learnings.
- Delete stale workaround learnings.
- Promote stable, enforceable learnings into deterministic checks.

Rule of thumb:

- If a learning says "always check X", consider a guard.
- If it says "remember to include Y", consider schema validation.
- If it says "prefer pattern Z", keep it as prompt guidance.

## F106 Retrospective: Extract Orchestrator

F106 已完成。`internal/orchestrator` package 包含：

- `orchestrator.go` — run loop 主迴圈
- `phase.go` — phase 切換邏輯
- `deep_review.go` — deep review 編排（self-heal、parallel review）
- `parallel.go` — parallel review+test
- `artifact.go` — artifact IO 和 report path
- `resume.go` — crash recovery / resume
- `worktree.go` — worktree 處理
- `hook.go` — phase hook

原本的反思建議拆成 6 個子 feature（F106a-f），實際上作為單一 feature 完成。
這驗證了重構類任務 context 高度連貫的判斷——拆太碎反而每個子 feature 都要重新建立相同 context。

教訓：**「按 context 拆」的粒度要看任務類型。** 重構是搬遷，不是新建；搬遷的 context 是連貫的，
過度拆分會增加 overhead 而不是降低風險。

## Proposed Future Features

### Feature: Risk-Based Pipeline Profiles

Problem:

- Full role pipeline is too expensive for small changes.
- Current profile usage exists but should become a more explicit risk gate.

Goal:

- Add a deterministic profile decision table.
- Make role skipping explicit and inspectable.
- Show selected profile and skipped roles in events/dashboard.

Acceptance ideas:

- Feature YAML can declare `profile`.
- Missing profile defaults to `normal`.
- `tiny` and `unit` profiles skip expensive roles as documented.
- `risk` and `architecture` profiles run heavier gates.
- `4x status` or run events show selected profile and skipped phases.

### Feature: Acceptance Criteria Evidence Mapping

Problem:

- `verify.json` evidence may prove commands ran, but not that every AC was verified.

Goal:

- Enforce AC-to-evidence mapping before accepting.

Acceptance ideas:

- Each AC has a stable ID.
- `test-report.md` or `verify.json` records evidence per AC.
- `4x check` blocks `testing -> accepting` if any AC lacks evidence.
- SKIP above threshold blocks acceptance.

### Feature: Context-Aware Deep Review Angle Selection

Problem:

- Deep review is powerful but can become a fixed cost.

Goal:

- Select deep review angles based on changed files, feature profile, and risk tags.

Acceptance ideas:

- Default mapping exists for state/protocol/guard/run/templates/docs/tests/dashboard.
- Selected angles are written to the run artifact.
- Users can override or force all angles.
- Tests cover mapping from file paths to angles.

### Feature: Package-Level Batch Context Groups

Problem:

- Batch scheduling currently reasons mostly about feature dependencies and repos.
- In a single Go repo, repo-level grouping is too coarse.

Goal:

- Add optional package/subsystem grouping for Go repos.

Acceptance ideas:

- Detect touched package groups from feature scope or design metadata.
- Avoid parallel execution when features share a package or shared subsystem.
- Allow parallel work for independent packages when dependencies permit.

### Feature: Learning Prune and Promotion Workflow

Problem:

- Learnings can accumulate and become prompt bloat.

Goal:

- Add a workflow to prune, merge, and promote learnings into deterministic checks.

Acceptance ideas:

- Command reports duplicate or stale learnings.
- Human can approve deletion or merge.
- Learnings marked `guard-candidate` are surfaced for future guard implementation.

## Guidance for Future Claude Sessions

Before making large 4x changes, Claude should classify the task:

1. Is the task independent and local?
   - Use a lightweight profile.
2. Does it touch state, protocol, guard, runner, orchestration, or merge behavior?
   - Use a risk profile.
3. Does it require multiple contexts?
   - Split by context before coding.
4. Is every acceptance criterion independently verifiable?
   - If not, fix the design before implementation.
5. Can a deterministic guard enforce the rule?
   - Prefer guard/schema/test over prompt instruction.

When in doubt, make the artifact contract stronger before adding another agent role.

## Additional Reflection: Role Isolation vs Context Overlap

以下是在 Codex 分析之上的補充觀點，聚焦在「角色拆分」和「context 拆分」之間的張力。

### 生成與驗證的隔離是 4x 的獨有價值

影片講的「按 context 拆」主要是在說平行化效率。但 4x 的 Coder → Reviewer → Tester
不只是分工，而是刻意讓**產出者和驗證者不是同一個 context**。

影片引用的研究結論「如果總算力一致，單 agent 常打平多 agent」並不適用於這裡——
因為 4x 的 review 不是為了加速，是為了**發現生成者的盲點**。
一個寫了 300 行 code 的 agent 再去 review 自己的 diff，效果遠不如一個冷啟動的 reviewer。

**結論**：生成與驗證的角色隔離不該丟掉。問題不是「要不要拆角色」，
而是「哪些角色的隔離真正有價值」。

### 哪些角色的 context boundary 其實是重疊的

| 角色對 | context 重疊度 | 隔離價值 |
|---|---|---|
| Designer → Coder | 高（兩者都要讀 codebase + feature spec） | 低——feature 不大時 Designer 只是多一層 paraphrase |
| Coder → Reviewer | 刻意不同（Reviewer 看 diff 不看開發過程） | 高——冷啟動 review 能抓到 coder 的盲點 |
| Reviewer → Tester | 不同（Tester 跑指令看結果，不讀 code） | 高——deterministic verification 不該由 reviewer 代勞 |
| Tester → Deep Reviewer | 部分重疊（都需要看 diff） | 中——Deep Review 的價值在多角度分析，但小 diff 時 overkill |
| Deep Reviewer → Acceptor | 高（Acceptor 主要是彙總） | 低——Acceptor 可以被自動化取代 |

`standard` profile 跳過 designing 直接 coding，其實就是承認 Designer 和 Coder 的 context
高度重疊。同理，`quick` profile 跳過 testing 和 deep-reviewing，也是承認小改動不需要那麼多隔離層。

### 什麼時候該用 4x、什麼時候不該

影片最值得帶回 4x 的不是「按 context 拆」，而是**「什麼時候不要開團隊」**。

決策 checklist：

```text
值得用 4x 的信號：
- 改動跨模組，影響面不確定
- 有可自動化的驗收條件（go test、CLI output）
- 改動涉及 state machine、protocol、guard 等核心模組
- 團隊成員需要看 review report 做最終決策

不值得用 4x 的信號：
- 改一個檔案，跑一次 test 就知道對不對
- 純文件 / config 調整
- 探索性工作（還不確定要做什麼）
- context 高度連貫，拆成多角色反而每個都要重新建立 context
```

Profile 的存在已經部分解決了這個問題。但 profile 選擇目前是手動的——
未來可以根據 feature scope（影響的 package 數、核心模組觸及度）自動推薦。

### F106 拆分粒度的事後驗證

Codex 建議拆成 F106a-f 六個子 feature。事後來看粒度過細。
F106 最終作為單一 feature 完成，驗證了重構的 context 連貫性判斷。

教訓：拆分粒度要看任務類型。
影片說的「需要嚴格順序的工作，多 agent 可能反而變慢」在重構場景完全成立。

### 影片的「可沉澱規則」對 4x 的適用性

影片總結的 multi-agent 前置檢查：

> 1. 任務是否能切成彼此獨立、可平行、可驗收的小塊？
> 2. 交接內容是否可被文件、測試或明確 artifact 固定？
> 3. 是否真的需要 agent 間互相溝通，還是 subagent 各自回報即可？
> 4. 成本與時間是否有上限？
> 5. 最後誰負責驗收？

4x 在第 2、4、5 點做得很好（artifact protocol、budget 機制、acceptor role）。
第 1 點靠 feature decomposition 和 subtask，但目前沒有工具化的「context overlap 檢測」。
第 3 點——4x 的角色之間完全不溝通（各自回報），這是正確的 subagent 模式。

## Summary

4x 應該維持 artifact-driven harness，不要變成 free-form agent swarm。

已有的好基礎：

- Durable protocol files
- Deterministic state machine
- Guardrails + verification
- Resume-aware role templates
- Dependency-aware batch scheduling
- 生成與驗證的角色隔離

下一步改善方向（按優先序）：

1. **Profile 自動推薦**——根據 feature scope 推薦 profile，減少人工判斷
2. **AC evidence mapping**——每個 AC 有對應 evidence，`4x check` 阻擋缺 evidence 的通過
3. **Selective deep review**——根據 diff 影響的模組選角度，不是每次跑全部 11 個
4. **Context-based feature decomposition**——大 feature 按 context boundary 拆，不按角色拆
5. **Learning 清理機制**——定期 prune、merge、promote 到 deterministic check

F106 已完成，驗證了重構類任務不需要過度拆分的判斷。
