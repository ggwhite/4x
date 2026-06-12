# Usage Tips & Best Practices

## Token Usage Warning

4x consumes **significantly more tokens than single-agent approaches**. Each feature goes through at least 4 roles (Designer → Coder → Reviewer → Tester), each as a separate LLM call. If Review or Test fails, the token cost doubles per retry round.

Rough estimate per feature:

| Scenario | ~LLM Calls | Notes |
|---|---|---|
| Pass on first try (best case) | 5 | Designer + Coder + Reviewer (2 pass) + Tester |
| Review rejects once | 8 | Extra round of Coder + Reviewer + Tester |
| Full 5 rounds | ~20 | Each round = Coder + Reviewer + Tester |

**Tips to reduce token cost:**
- Lower `--max-rounds` for simple tasks (`--max-rounds 2`)
- Use sonnet-tier models for everything on simple tasks (5-10x cheaper)
- Use `--dry-run` to verify prompt quality before committing to a real run
- Write clear feature descriptions to reduce escalation and retries
- The loop auto-stops after 3 consecutive rounds with no progress — it won't burn through max-rounds unnecessarily

---

## End-to-End Workflow

The complete flow from creating a task to shipping it. 4x handles the AI development; you handle the final review and merge.

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
# Mark as done
4x done F001

# Merge into main branch
git merge 4x/F001-add-redis-cache-for-or

# Clean up worktree and branch
git worktree remove .worktrees/4x/F001-add-redis-cache-for-or
git branch -d 4x/F001-add-redis-cache-for-or
```

### Workflow Overview

```
4x new "..."                     # create task
    ↓
4x run F001 --runner claude      # AI runs Design→Code→Review→Test
    ↓
pending-review                   # waiting for your review
    ↓
review final-report / diff       # you inspect the results
    ↓
4x done F001                     # mark as done
    ↓
git merge + cleanup              # merge, clean up worktree/branch
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

This prints the full prompt for all four roles without calling any LLM. Verify:
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

Configure in settings:

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
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
- Missing external dependency (`blocker`)

Escalations are recorded in `.4x/{feature-id}/rounds/round-{N}/escalation.json`. The Designer receives the escalation details and re-designs the spec.

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
# After completion, merge and clean up
git merge 4x/F001-user-auth
git worktree remove .worktrees/4x/F001-user-auth
git branch -d 4x/F001-user-auth
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
