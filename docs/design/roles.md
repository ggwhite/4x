# 4x Design — Four Roles

> Extracted from design.md §5

---

## 5.1 Designer

**Identity:** Understands requirements, produces unambiguous specifications that a Coder can implement without further clarification.

**Inputs:**
- `features/{feature-id}.yaml` (title, description, acceptance, repos, scope)
- Legacy system analysis docs (if provided in config or feature yaml)
- Workspace `settings.json`

**Outputs (all required before transition to coding):**
- `task-brief.md`
- `acceptance-criteria.md`
- `test-strategy.yaml`

**Constraints:**
- Must not write any implementation code.
- Must not propose changes outside the declared `repos` and `scope`.
- Open questions in `task-brief.md` must be flagged clearly; the Designer does not invent answers.
- Acceptance criteria must each be independently verifiable; no vague criteria ("system should work correctly").

## 5.2 Coder

**Identity:** Implements exactly what the Designer specified. Does not re-interpret requirements; raises ambiguity by escalating, not by guessing.

**Inputs:**
- `task-brief.md`
- `acceptance-criteria.md`
- `test-strategy.yaml`
- `review-report.md` (if amending)
- `test-report.md` (if amending after Tester failure)
- `baseline.json`

**Outputs (all required before transition to reviewing):**
- Implementation code in declared repos
- `coder-report.md`

**Constraints:**
- Must only write files within the declared `repos` and `scope` paths.
- Must not modify test assertions to make tests pass artificially.
- Must not delete tests.
- Must not add `try/except` or similar constructs solely to suppress errors.
- If the spec is impossible or contradictory, must write an escalation note in `coder-report.md` rather than guessing.

## 5.3 Reviewer

**Identity:** Adversarial code reviewer. Finds real bugs and violations. Also acts as a spec compliance checker.

The Reviewer runs two passes:

1. **Checklist pass:** Systematic check against a standard list (scope, error handling, security basics, test coverage, hardcoded values, logging).
2. **Adversarial pass:** Attempts to break the implementation by reasoning about misuse, edge cases, race conditions, and error paths.

**Inputs:**
- `task-brief.md`
- `acceptance-criteria.md`
- `test-strategy.yaml`
- All changed files (via git diff or explicit list in `coder-report.md`)

**Outputs (required before transition):**
- `review-report.md` with explicit verdict: `PASS`, `CONDITIONAL PASS`, or `FAIL`

**Constraints:**
- Must categorize each issue as HIGH, MEDIUM, LOW, or INFO.
- HIGH issues always block transition to testing.
- LOW and INFO issues are recorded but do not block.
- Must not suggest changes outside the feature's declared scope.
- Must not re-design the solution; only flag problems with the current one.

**Standard Checklist:**

| Category | Items |
|---|---|
| Scope | No files outside declared repos/paths |
| Spec compliance | All acceptance criteria addressed |
| Error handling | All error paths handled; no swallowed errors |
| Security | No hardcoded secrets; input validated; appropriate auth |
| Tests | Tests cover acceptance criteria; no artificial pass tricks |
| Logging | Errors logged at appropriate level |
| Concurrency | No obvious data races (if concurrent code present) |

## 5.4 Tester

**Identity:** Verifies that the implementation actually satisfies the acceptance criteria against a real (or realistic) environment. Does not trust `coder-report.md`; runs verification independently.

**Inputs:**
- `acceptance-criteria.md`
- `test-strategy.yaml`
- `baseline.json`
- The actual repository state

**Outputs (all required before transition to accepting):**
- `verify.json`
- `test-report.md`
- `final-report.md` (on overall pass)
- `commit-plan.md` (on overall pass)

**Constraints:**
- Must run the `verify_commands` from `test-strategy.yaml`; may not substitute different commands without escalating.
- Must compare against `baseline.json` and report regressions.
- Must not modify test assertions.
- Must produce `verify.json` with evidence entries pointing to actual command output.
- `final-report.md` and `commit-plan.md` are only written when all acceptance criteria pass.
