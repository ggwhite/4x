# Usage Tips & Best Practices

## Token Usage Warning

4x consumes **significantly more tokens than single-agent approaches**. Each feature goes through at least 6 roles (Designer → Coder → Reviewer → Tester → Deep-Reviewer → Acceptor), each as a separate LLM call. If Review or Test fails, the token cost increases significantly per retry round.

Rough estimate per feature:

| Scenario | ~LLM Calls | Notes |
|---|---|---|
| Pass on first try (best case) | 7 | Designer + Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |
| Best case (no deep_model configured) | 5 | Designer + Coder + Reviewer + Tester + Acceptor (Deep-Review skipped) |
| Review rejects once | 12 | Extra round of Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |
| Full 5 rounds | ~27 | Each round = Coder + Reviewer + Tester + Deep-Reviewer + Acceptor |

**Tips to reduce token cost:**
- Lower `--max-rounds` for simple tasks (`--max-rounds 2`)
- Use sonnet-tier models for everything on simple tasks (5-10x cheaper)
- Use `--dry-run` to verify prompt quality before committing to a real run
- Write clear feature descriptions to reduce escalation and retries
- The loop auto-stops after 3 consecutive rounds with no progress — it won't burn through max-rounds unnecessarily

---

## Real-world Workflow (with AI Agent)

This is how the author actually uses 4x day-to-day — not raw CLI commands, but an AI-assisted loop where you stay in the same conversation throughout.

### 1. Create Feature

Ask the AI agent to create a feature for you:

```
> 4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

### 2. Brainstorm — Spec & Plan

Before running the loop, ask the agent to brainstorm the design:

```
> brainstorm F001
```

The agent uses the brainstorming skill to explore requirements, trade-offs, and edge cases with you. Once aligned, it produces two artifacts:

- `docs/design/F001-add-redis-cache-for-or-spec.md` — design spec
- `docs/design/F001-add-redis-cache-for-or-plan.md` — implementation plan

These files follow the naming convention declared in `CLAUDE.md` under **Docs Routing**: `docs/design/{feature-id}-spec.md` and `docs/design/{feature-id}-plan.md`.

The spec becomes the Designer's reference input — a well-brainstormed spec means the Designer produces better task briefs, which means fewer review rejections and retry rounds.

### 3. Run the Loop

```bash
4x run F001 --runner claude
```

Open the dashboard in another terminal to watch progress:

```bash
4x live -w
```

### 4. AI Code Review

When the loop finishes (`pending-review`), ask your AI agent to review the diff:

```
> help me review the diff on branch 4x/F001-add-redis-cache-for-or
```

The agent reads `final-report.md`, diffs the branch against main, and points out issues. Fix what needs fixing — either manually or by asking the agent.

### 5. Merge & Cleanup

Once you're satisfied, ask the agent to merge and clean up:

```
> merge it and clean up the worktree
```

The agent runs:
```bash
4x done F001
```

`4x done` automatically merges the branch, removes the worktree, and deletes the branch. If there are merge conflicts, you'll be prompted to resolve them manually and then run `4x merge F001`.

### 6. Mark Done in Dashboard

Open the dashboard (`4x live -w`) and click **Mark Done** on the feature card. This is intentionally a human action — the AI loop never auto-completes a feature.

### Why This Works

- **Brainstorming before coding** — the spec grounds the entire loop; ambiguity is resolved upfront, not mid-implementation
- **You stay in one conversation** — no context-switching between terminals and tools
- **The AI agent already has full context** from brainstorming and running the feature, so its review is informed
- **Mark Done is manual** — you're the final gatekeeper, not the AI

### What 4x Is (and Isn't)

4x is a **workflow orchestrator** — it runs Designer, Coder, Reviewer, and Tester roles in sequence and manages the state machine between them. It does not replace your judgment.

In practice, the loop handles the happy path well: straightforward features with clear specs usually pass in 1-2 rounds. But real-world development is messy:

- **The Coder might misunderstand the spec** — the Reviewer catches it, but the fix in the next round may still miss the point. After 2-3 failed rounds, it's faster to intervene yourself or ask your AI agent to fix the specific issue directly.
- **Test failures may be environment-specific** — the Tester writes tests based on the spec, but if your project has quirks (custom test setup, flaky CI, legacy constraints), the tests may fail for reasons the AI can't diagnose. You'll need to debug these yourself.
- **Edge cases surface after the loop** — 4x covers what the spec describes. Business logic edge cases, race conditions, or integration issues often only appear during manual review or production use.
- **Complex refactors may need human guidance** — when a feature touches many files or requires understanding of implicit conventions, the Coder may produce correct but suboptimal code. A quick human nudge ("use the existing helper in `pkg/util`") saves multiple retry rounds.

**The right mental model**: 4x gives you a solid first draft with test coverage and review feedback. Think of it as a capable junior developer that follows instructions precisely but sometimes needs steering. The time savings come from not writing the initial implementation yourself — not from removing yourself from the process entirely.

### Customize Roles per Project

4x only handles state transitions and role switching — it doesn't know how your project should be built, tested, or reviewed. That knowledge lives in your project settings.

Each role reads from the project's `.4x/settings.json` to understand what to do. The more context you give, the better the output:

```json
{
  "project": {
    "name": "my-api",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["golangci-lint run"],
    "rules": ["all exported functions must have GoDoc comments"]
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": {
      "model": "sonnet",
      "instructions": ["always use dependency injection via constructors"]
    },
    "reviewer": {
      "model": "sonnet",
      "deep_model": "opus",
      "instructions": ["check for SQL injection in all query builders"]
    },
    "tester": {
      "model": "sonnet",
      "instructions": ["use testcontainers for integration tests, not mocks"]
    }
  }
}
```

Key fields:

| Field | Effect |
|---|---|
| `project.build/test/lint` | Coder runs these after changes; Tester uses `test` for verification |
| `project.rules` | Injected into every role as hard constraints |
| `roles.*.instructions` | Role-specific guidance — what to focus on, what to avoid |
| `roles.*.includes` | Extra files to read (e.g., `["docs/api-conventions.md"]`) |

Without these, roles fall back to generic behavior. With them, the Designer writes specs that match your architecture, the Coder follows your conventions, the Reviewer catches your project's specific pitfalls, and the Tester writes tests that actually run in your environment.

See [Configuration](configuration.md) for the full reference.

---

## End-to-End Workflow (CLI Only)

The same flow as above, but using CLI commands directly — useful when you're not in an AI agent session.

### Step 1: Create a Task

```bash
4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

Optionally edit `.4x/features/F001-add-redis-cache-for-or.yaml` to fill in description, priority, depends, repos, etc.

### Step 2: Run the Loop

```bash
# Recommended: dry run first to check prompts
4x run F001 --dry-run

# Run for real
4x run F001 --runner claude
```

Open the dashboard in another terminal for real-time monitoring:

```bash
4x live -w
```

### Step 3: Loop Completes → pending-review

When the loop finishes, the feature lands in `pending-review` — this is intentional. The AI is done, but it needs your sign-off.

```bash
4x status F001
# Phase: pending-review
```

### Step 4: Human Review

Inspect the AI's output:

```bash
# Read the final report
cat .4x/F001/final-report.md

# Read the commit plan
cat .4x/F001/commit-plan.md

# Review the code diff
git diff                          # non-worktree mode
git diff main...4x/F001-add-redis  # worktree mode
```

Not satisfied? You can send it back:

```bash
# Re-run review + test after manual edits
4x transition F001 --to reviewing
4x run F001

# Or start over from design
4x transition F001 --to designing
4x run F001
```

### Step 5: Merge & Cleanup

**Non-worktree mode** (changes are in the working tree):

```bash
# Mark as done
4x done F001

# Commit following the commit plan
git add -A
git commit -m "feat: add Redis cache for order query API"
```

**Worktree mode** (changes are on an isolated branch):

```bash
# Mark as done — automatically merges, removes worktree, and deletes branch
4x done F001
```

> If there are merge conflicts, `4x done` will print a message asking you to resolve them manually, then run `4x merge F001` to complete the merge and cleanup.

### Workflow Overview

```
4x new "..."                     # create task
    ↓
4x run F001 --runner claude      # AI runs Design→Code→Review→Test→Deep-Review→Accept
    ↓
pending-review                   # waiting for your review
    ↓
review final-report / diff       # you inspect the results
    ↓
4x done F001                     # mark as done + auto merge/cleanup
```

---

## Writing Good Feature Descriptions

The feature description is the Designer's only input — the clearer you write it, the better the spec.

```bash
# Bad: too vague, Designer will fill in the blanks with guesses
4x new "improve performance"

# Good: specific goal, boundaries, acceptance criteria
4x new "optimize order query API — add Redis cache, target p99 < 200ms, cache TTL 5min"
```

Include in your description:
- **What to do** (specific feature or change)
- **Why** (business motivation or problem statement)
- **Boundaries** (what NOT to touch, known constraints)
- **Acceptance criteria** (quantifiable definition of success)

## Feature Granularity

One feature = one independently deliverable change. Too large and the Coder gets lost, the Reviewer misses issues, and testing becomes unreliable.

| Granularity | Good fit | Bad fit |
|---|---|---|
| One API endpoint | OK | — |
| One refactor (rename, extract interface) | OK | — |
| One bug fix | OK | — |
| Entire module from scratch | — | Split into multiple features + depends |
| Large feature spanning 3 repos | — | One feature per repo, linked with depends |

Use `depends` to decompose large tasks:

```bash
4x new "Add user model and migrations"           # F001
4x new "Add user registration API"               # F002, depends: [F001]
4x new "Add OAuth2 login flow"                    # F003, depends: [F002]
```

## Dry Run First

After creating a new feature or modifying settings, use `--dry-run` to check that prompts look right:

```bash
4x run F001 --dry-run
```

This prints the full prompt for all roles without calling any LLM. Verify:
- Does the Designer have enough context?
- Are your project rules injected correctly?
- Is the locale correct?

## Model Selection

| Role | Recommendation | Reason |
|---|---|---|
| Designer | opus or equivalent | Needs deep understanding to analyze requirements and design architecture |
| Coder | sonnet or equivalent | High output volume, doesn't need strongest reasoning |
| Reviewer (checklist) | sonnet | Rule-based checking, speed matters |
| Reviewer (adversarial) | opus | Needs deep reasoning to find hidden bugs |
| Tester | sonnet | Writing and running tests, doesn't need strongest reasoning |
| Acceptor | sonnet | Final verification against spec, similar to reviewer tier |

Configure in settings:

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" },
    "acceptor": { "model": "sonnet" }
  }
}
```

For simple projects (small bug fixes, minor refactors), using sonnet for everything is fine and much cheaper.

## Tuning Max Rounds

The default of 5 rounds works for most cases. Adjust based on feature complexity:

| Scenario | Recommended rounds |
|---|---|
| Simple bug fix, small change | 2-3 |
| Typical feature development | 5 (default) |
| Complex cross-module feature | 7-10 |

```bash
4x run F001 --max-rounds 3   # simple task
4x run F001 --max-rounds 8   # complex task
```

Note: the loop auto-stops after 3 consecutive rounds with no progress (it won't run all the way to max-rounds).

## Handling Review Failures

Review failures (verdict FAIL or CRITICAL findings) automatically send code back to the Coder — no manual intervention needed. But if it keeps failing:

1. **Read review-report.md** — at `.4x/{feature-id}/rounds/round-{N}/review-report.md`
2. **Read coder-report.md** — did the Coder understand the issue?
3. **Consider adjusting**:
   - Feature description too vague → rewrite it, re-run from Designer
   - Reviewer too strict → relax specific rules in `roles.reviewer.instructions`
   - Genuinely hard problem → intervene manually, then use `4x transition` to push forward

## Handling Escalation

When the Coder or Tester finds that the spec doesn't match reality, they automatically escalate back to the Designer. Common scenarios:

- DB schema doesn't match the spec (`spec-mismatch`)
- Acceptance criteria are unreasonable (`criteria-wrong`)
- Feature scope needs adjustment (`scope-change`)

These escalations are sent back to the Designer, who re-designs the spec. Escalations are recorded in `.4x/{feature-id}/rounds/round-{N}/escalation.json`.

Note: `blocker` escalations (e.g., missing external dependency) go directly to `needs-attention` and require manual intervention — they are not sent back to the Designer.

If the Designer can't resolve it either (usually due to missing context), the loop stops at `needs-attention`. Manual intervention is needed:

```bash
# Check status
4x status F001

# Manually fix the spec or codebase
vim .4x/F001/task-brief.md

# Push back to coding
4x transition F001 --to coding
```

## Resuming an Interrupted Feature

4x is file-based — if the session dies or the machine reboots, all state is in `.4x/`. Just re-run:

```bash
4x run F001 --runner claude
```

It picks up from the last phase and round, not from the beginning.

## Worktree Isolation

To run multiple features simultaneously or isolate AI changes from your working tree, enable worktree mode:

```json
// .4x/settings.json
{
  "isolation": "worktree"
}
```

What happens:
- Each feature runs in `.worktrees/4x/{feature-id}/` with its own working directory
- A branch `4x/{feature-id}` is created automatically
- After completion, the CLI prints merge instructions

```bash
# After completion, merge and clean up automatically
4x done F001
# On merge conflict, resolve manually then run: 4x merge F001
```

## When to Use Batch Mode

| Scenario | Use `4x run` | Use `4x batch run` |
|---|---|---|
| Single feature | OK | — |
| Multiple features with dependencies | Must order manually | Handles dependency order automatically |
| Overnight backlog processing | — | OK, use `batch stop` to halt anytime |

Batch mode uses commit strategy `"never"` — all changes stay in the working tree for human review before committing.

## Dashboard Usage

```bash
# Run a feature while watching the dashboard
4x live -w &
4x run F001 --runner claude

# Start features directly from the dashboard UI
# POST /api/run via the web interface

# Monitor multiple projects
4x live /path/to/project-a /path/to/project-b -w
```

## Locale Setting

Set the language for AI responses:

```bash
4x config set locale zh-TW
```

If not set, it's automatically inferred from the `LANG` environment variable.

## Troubleshooting

### Feature stuck in needs-attention

A required artifact is missing for the current phase (e.g., Designer didn't produce task-brief.md).

```bash
4x status F001          # see what's missing
4x check F001           # run full check
```

Manually fix or re-run the phase:

```bash
4x transition F001 --to designing
4x run F001
```

### Feature stuck in blocked

Usually caused by runner exit code 1 (soft failure). Check the logs:

```bash
ls .4x/F001/logs/
cat .4x/F001/logs/round-1-coder.log
```

After fixing, push back:

```bash
4x transition F001 --to coding
4x run F001
```

### Blocked by dependency gate

```
blocked: F001-user-model is not done (status: coding)
```

Complete the dependency first, or manually mark it done:

```bash
4x done F001
4x run F002
```
