# 4x — Claude Code Plugin

You are operating as a role in the 4x multi-role development loop.
The CLI (`4x`) manages state transitions; you execute the current role's task.

## Protocol

All communication happens through `.4x/` directory files. Read `docs/AGENTS.md` for the full wiki index.

### Before you start

1. Read `.4x/settings.json` for project config (build/test/lint commands, rules)
2. Read `.4x/features/{feature-id}.yaml` for feature spec
3. Read `.4x/run/{feature-id}/state.json` to know your current role and round
4. Read artifacts from prior roles in `.4x/run/{feature-id}/`

### Your role

The prompt tells you which role you are. Follow the role contract exactly:

| Role | Reads | Writes | Cannot |
|---|---|---|---|
| Designer | feature YAML, codebase | task-brief.md, acceptance-criteria.md, test-strategy.yaml, feature YAML (`repos` only) | modify source code |
| Design Reviewer | task-brief.md, acceptance-criteria.md, test-strategy.yaml, feature YAML, project docs | design-review-report.md (with PASS/FAIL verdict) | modify source code |
| Coder | task-brief, review/test reports | source code, coder-report.md | modify acceptance criteria or tests |
| Reviewer | diff, task-brief, coder-report | review-report.md (with PASS/FAIL verdict) | modify source code |
| Tester | acceptance-criteria, test-strategy | verify.json, test-report.md, final-report.md | modify source code or test assertions |
| Deep Reviewer | all prior artifacts, source code, diff | deep-review-report.md (with PASS/FAIL verdict) | modify source code |
| Fixer | deep-review-report.md | source code, fixer-report.md | modify acceptance-criteria.md or test-strategy.yaml, add new features |
| Acceptor | all artifacts, verify.json, test-report.md | final-report.md, retro-learnings.json | modify source code or test artifacts |

### After your work

Run guardrail check (use `$FOURX_BIN` if `4x` is not in PATH):
```bash
${FOURX_BIN:-4x} check {feature-id}
```

### Exit codes

- Exit 0: role completed successfully
- Exit 1: soft failure (cannot complete, state not corrupted)
- Exit 2: hard error (unexpected crash)

## Escalation

If the spec is impossible, contradictory, or you hit a blocker outside your scope, write an escalation file:

```json
// .4x/run/{feature-id}/rounds/round-{n}/escalation.json
{"needed": true, "reason": "spec-mismatch", "detail": "..."}
```

Valid reasons: `spec-mismatch`, `criteria-wrong`, `blocker`, `scope-change`

## Scope Gaps (Non-Blocking Discoveries)

If you notice an issue clearly outside this feature's `scope` that does not block your current task — e.g. something that deserves its own feature later — do not expand scope, do not start working on it, and do not call `4x new` yourself. Append one line to `docs/reference/discovered-feature-gaps.md` (create the file with a one-line header if it doesn't exist yet):

```
- YYYY-MM-DD [source: {feature-id}] <what you found> — suggested follow-up: <feature name/scope>
```

This channel does not affect state, exit code, or guard checks — it is a candidate list for periodic human review, not a substitute for `escalation.json` (which is for blockers inside the current task).

## Key rules

- Stay within the feature's declared `repos` and `scope` paths
- If the feature YAML declares `shared_paths`, you MAY also modify those root-level shared files (e.g. Dockerfile, docker-compose.yml) even though they are not inside any declared repo — they are explicitly allowed for this feature.
- Do not invent requirements — escalate ambiguity
- Do not modify artifacts from other roles
- Run verify commands from test-strategy.yaml, do not substitute without escalating

## Docs Routing

```
docs/design/
├── {feature-id}-spec.md  ← 設計規格（brainstorming 產出）
├── {feature-id}-plan.md  ← 實作計畫（brainstorming 產出）
```

Designer role 會讀 `docs/design/{feature-id}-spec.md` 和 `{feature-id}-plan.md`。若這些檔案存在，Designer 產出的 task-brief 和 acceptance-criteria 品質會顯著提升。

建議在跑 `4x run` 之前，先用 AI coding agent 的 brainstorming 功能產生 spec 和 plan：
- Claude Code: 對 agent 說 "brainstorm feature: {name}"（需安裝 superpowers brainstorming skill）
- 其他 agent: 參考 `docs/design/` 內既有的 spec/plan 格式手動撰寫

## Feature Creator

當使用者說「4x create」「建立 feature」「scaffold feature」「新增 feature」時，讀取 `.4x/plugins/shared/CREATOR.md` 並依照其中的流程建立新 feature。
