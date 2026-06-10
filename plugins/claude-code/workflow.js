export const meta = {
  name: '4x-loop',
  description: 'Designer-Coder-Reviewer-Tester multi-role development loop',
  phases: [
    { title: 'Design', detail: 'Analyze requirements, produce spec + acceptance criteria' },
    { title: 'Code', detail: 'Implement per task-brief' },
    { title: 'Review', detail: 'Checklist + adversarial code review' },
    { title: 'Test', detail: 'Validate against acceptance criteria' },
    { title: 'Amend', detail: 'Designer adjusts spec (on escalation)' },
    { title: 'Accept', detail: 'Final acceptance + commit plan' }
  ]
}

// ── Schemas ──

const DESIGN_SCHEMA = {
  type: 'object',
  properties: {
    taskBriefPath: { type: 'string' },
    acceptanceCriteriaPath: { type: 'string' },
    testStrategy: {
      type: 'object',
      properties: {
        web: { type: 'boolean' },
        api: { type: 'boolean' },
        coder_only: { type: 'boolean' },
        verify_commands: { type: 'array', items: { type: 'string' } }
      },
      required: ['web', 'api', 'coder_only']
    },
    involvedRepos: { type: 'array', items: { type: 'string' } },
    summary: { type: 'string' }
  },
  required: ['taskBriefPath', 'acceptanceCriteriaPath', 'testStrategy', 'involvedRepos']
}

const CODER_SCHEMA = {
  type: 'object',
  properties: {
    changedFiles: { type: 'array', items: { type: 'string' } },
    buildPass: { type: 'boolean' },
    testPass: { type: 'boolean' },
    coderReportPath: { type: 'string' },
    summary: { type: 'string' },
    subtaskUpdates: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          id: { type: 'string' },
          status: { type: 'string', enum: ['done', 'in-progress', 'blocked', 'not-started'] }
        },
        required: ['id', 'status']
      }
    },
    escalation: {
      type: 'object',
      properties: {
        needed: { type: 'boolean' },
        reason: { type: 'string', enum: ['spec-mismatch', 'criteria-wrong', 'blocker', 'scope-change'] },
        detail: { type: 'string' }
      }
    }
  },
  required: ['changedFiles', 'buildPass', 'coderReportPath', 'summary']
}

const REVIEW_SCHEMA = {
  type: 'object',
  properties: {
    pass: { type: 'boolean' },
    criticalCount: { type: 'integer' },
    warningCount: { type: 'integer' },
    issues: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          severity: { type: 'string', enum: ['critical', 'warning', 'low'] },
          rule: { type: 'string' },
          file: { type: 'string' },
          detail: { type: 'string' }
        },
        required: ['severity', 'rule', 'file', 'detail']
      }
    },
    reviewReportPath: { type: 'string' },
    summary: { type: 'string' }
  },
  required: ['pass', 'criticalCount', 'warningCount', 'issues', 'reviewReportPath']
}

const TEST_SCHEMA = {
  type: 'object',
  properties: {
    passCount: { type: 'number' },
    failCount: { type: 'number' },
    skipCount: { type: 'number' },
    results: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          criteria: { type: 'string' },
          status: { type: 'string', enum: ['pass', 'fail', 'skip'] },
          evidence: { type: 'string' },
          detail: { type: 'string' }
        },
        required: ['criteria', 'status']
      }
    },
    testReportPath: { type: 'string' },
    escalation: {
      type: 'object',
      properties: {
        needed: { type: 'boolean' },
        reason: { type: 'string', enum: ['spec-mismatch', 'criteria-wrong', 'blocker', 'scope-change'] },
        detail: { type: 'string' }
      }
    }
  },
  required: ['passCount', 'failCount', 'skipCount', 'results', 'testReportPath']
}

// ── Parse args ──

const parsed = (typeof args === 'string') ? JSON.parse(args) : args
const featureId = parsed.featureId
const maxRounds = parsed.maxRounds || 5
const dotDir = parsed.dotDir || '.4x'
const onlyPhase = parsed.only || null
const featureDir = `${dotDir}/${featureId}`

// ── Helper: guardrail check via CLI ──

function guardrailPrompt() {
  return `
After completing your work, run the guardrail check:
  4x check ${featureId}
If it fails, fix the issues before reporting.
`
}

function stateUpdate(role, phaseLabel, round) {
  return `
echo '{"active":true,"role":"${role}","phase":"${phaseLabel}","round":${round},"label":"Round ${round} ${role}"}' > ${featureDir}/state.json
echo '{"phase":"${phaseLabel}","type":"phase-start","role":"${role}","round":${round}}' >> ${featureDir}/events.jsonl
`
}

function stateEnd(role, phaseLabel, round) {
  return `echo '{"phase":"${phaseLabel}","type":"phase-end","role":"${role}","round":${round}}' >> ${featureDir}/events.jsonl`
}

// ── Read feature info ──

phase('Design')
log(`${featureId}: Starting design phase`)

const featureYaml = `${dotDir}/features/${featureId}.yaml`

const designResult = await agent(`You are the Designer.

Read the feature definition: ${featureYaml}
Read any existing docs, specs, or analysis related to this feature.

${stateUpdate('designer', 'design', 0)}

Your job:
1. Analyze the feature requirements
2. Explore the current codebase to understand what exists
3. Write a task-brief.md with an actionable task list for the Coder
4. Write acceptance-criteria.md with testable criteria for the Tester
5. Write test-strategy.yaml specifying which test types to run
6. Save all outputs to ${featureDir}/

Be specific: name files, functions, endpoints, schemas.
You may NOT modify any source code.

${stateEnd('designer', 'design', 0)}
`, {
  label: `designer:${featureId}`,
  phase: 'Design',
  model: 'opus',
  schema: DESIGN_SCHEMA
})

if (!designResult) {
  log(`${featureId}: Design phase failed`)
  return { status: 'blocked', reason: 'design failed' }
}

log(`${featureId}: Design complete — ${designResult.involvedRepos?.length || 0} repos involved`)

// ── Code-Review-Test Loop ──

let round = 0
let lastTestResult = null
let consecutiveNoProgress = 0

while (round < maxRounds) {
  round++

  // ── Code ──
  phase('Code')
  log(`${featureId}: Round ${round} — Coder`)

  const prevTestReport = round > 1 ? `${featureDir}/rounds/round-${round - 1}/test-report.md` : null

  const codeResult = await agent(`You are the Coder for round ${round}.

${stateUpdate('coder', 'code', round)}

Read your task brief: ${featureDir}/task-brief.md
${prevTestReport ? `Read the previous test report: ${prevTestReport}` : ''}

Implement the required changes. After each change:
1. Run verify commands (build, test, lint)
2. Report what you changed and the results

Write your report to: ${featureDir}/rounds/round-${round}/coder-report.md

${guardrailPrompt()}
${stateEnd('coder', 'code', round)}
`, {
    label: `coder:${featureId}:r${round}`,
    phase: 'Code',
    model: 'sonnet',
    schema: CODER_SCHEMA
  })

  if (!codeResult) {
    log(`${featureId}: Coder failed in round ${round}`)
    break
  }

  // Handle escalation
  if (codeResult.escalation?.needed) {
    phase('Amend')
    log(`${featureId}: Escalation — ${codeResult.escalation.reason}`)

    await agent(`You are the Designer. The Coder has escalated an issue.

Reason: ${codeResult.escalation.reason}
Detail: ${codeResult.escalation.detail}

Read:
- ${featureDir}/task-brief.md
- ${featureDir}/rounds/round-${round}/coder-report.md

Amend the task-brief and/or acceptance-criteria as needed.
Write changes to the same files. Document what changed in ${featureDir}/spec-amendments.md.
`, {
      label: `amend:${featureId}:r${round}`,
      phase: 'Amend',
      model: 'opus'
    })

    continue
  }

  // ── Review ──
  phase('Review')
  log(`${featureId}: Round ${round} — Reviewer`)

  let reviewPass = false
  const review = await agent(`You are the Reviewer for round ${round}.

${stateUpdate('reviewer', 'review', round)}

Review the Coder's changes.

Read:
1. The git diff of changed files
2. ${featureDir}/task-brief.md
3. ${featureDir}/rounds/round-${round}/coder-report.md

Check against any project rules defined in the feature YAML or config.yaml.

Severity:
- critical: violates project rules, causes bugs or security issues
- warning: code quality issue that should be fixed
- low: style preference only

Write review report to: ${featureDir}/rounds/round-${round}/review-report.md

${stateEnd('reviewer', 'review', round)}
`, {
    label: `reviewer:${featureId}:r${round}`,
    phase: 'Review',
    model: 'sonnet',
    schema: REVIEW_SCHEMA
  })

  if (review && (review.criticalCount > 0 || review.warningCount > 0)) {
    log(`${featureId}: Review found ${review.criticalCount} critical, ${review.warningCount} warning`)

    // Coder fixes review issues
    phase('Code')
    await agent(`You are the Coder. Fix these review issues:

${review.issues.filter(i => i.severity !== 'low').map(i =>
  `[${i.severity.toUpperCase()}] ${i.rule} — ${i.file}: ${i.detail}`
).join('\n')}

Full report: ${review.reviewReportPath}
Run verify commands after fixes.
`, {
      label: `coder-fix:${featureId}:r${round}`,
      phase: 'Code',
      model: 'sonnet',
      schema: CODER_SCHEMA
    })
  } else {
    reviewPass = true
    log(`${featureId}: Review passed`)
  }

  // ── Deep Review (if checklist passed) ──
  if (reviewPass) {
    const deepReview = await agent(`You are the Adversarial Reviewer (Deep Review).

${stateUpdate('deep-reviewer', 'deep-review', round)}

The checklist review already passed. Your job is to find what it missed:
1. Business logic bugs (wrong flow, missing edge cases, state machine gaps)
2. Spec vs implementation mismatches
3. Concurrency edge cases (not just "has a lock" but "right granularity")
4. Security beyond RBAC (auth bypass, privilege escalation, injection, timing)
5. Performance (N+1 queries, unbounded results, goroutine leaks)

Read the diff and coder-report. Try to PROVE the code is broken.
If you can't, it passes.

Write report: ${featureDir}/rounds/round-${round}/deep-review-report.md

${stateEnd('deep-reviewer', 'deep-review', round)}
`, {
      label: `deep-review:${featureId}:r${round}`,
      phase: 'Review',
      model: 'opus',
      schema: REVIEW_SCHEMA
    })

    if (deepReview && (deepReview.criticalCount > 0 || deepReview.warningCount > 0)) {
      log(`${featureId}: Deep review found ${deepReview.criticalCount} critical`)
      phase('Code')
      await agent(`Fix these deep review issues:

${deepReview.issues.filter(i => i.severity !== 'low').map(i =>
  `[${i.severity.toUpperCase()}] ${i.rule} — ${i.file}: ${i.detail}`
).join('\n')}
`, {
        label: `coder-deep-fix:${featureId}:r${round}`,
        phase: 'Code',
        model: 'sonnet',
        schema: CODER_SCHEMA
      })
    }
  }

  // ── Test ──
  phase('Test')
  log(`${featureId}: Round ${round} — Tester`)

  const testResult = await agent(`You are the Tester for round ${round}.

${stateUpdate('tester', 'test', round)}

Read:
1. ${featureDir}/acceptance-criteria.md
2. ${featureDir}/rounds/round-${round}/coder-report.md
3. ${featureDir}/test-strategy.yaml

Workflow:
1. Read acceptance criteria
2. Write test scripts FIRST (every AC item gets at least one test)
3. Run the test scripts
4. Compile test report from actual results

Every AC item must be pass/fail/skip with evidence.
SKIP > 30% blocks acceptance.
Do NOT modify source code.

Write report: ${featureDir}/rounds/round-${round}/test-report.md

${stateEnd('tester', 'test', round)}
`, {
    label: `tester:${featureId}:r${round}`,
    phase: 'Test',
    model: 'sonnet',
    schema: TEST_SCHEMA
  })

  if (!testResult) {
    log(`${featureId}: Tester failed`)
    break
  }

  // Handle test escalation
  if (testResult.escalation?.needed) {
    phase('Amend')
    log(`${featureId}: Tester escalation — ${testResult.escalation.reason}`)
    await agent(`You are the Designer. The Tester escalated: ${testResult.escalation.detail}
Amend task-brief/criteria in ${featureDir}/.`, {
      label: `amend-test:${featureId}:r${round}`,
      phase: 'Amend',
      model: 'opus'
    })
    continue
  }

  // Check progress
  if (testResult.failCount === 0) {
    log(`${featureId}: All tests passed in round ${round}!`)
    lastTestResult = testResult
    break
  }

  if (lastTestResult && testResult.failCount >= lastTestResult.failCount) {
    consecutiveNoProgress++
  } else {
    consecutiveNoProgress = 0
  }

  lastTestResult = testResult
  log(`${featureId}: Round ${round} — ${testResult.passCount} pass, ${testResult.failCount} fail`)

  if (consecutiveNoProgress >= 3) {
    log(`${featureId}: 3 rounds with no progress, stopping`)
    break
  }
}

// ── Accept ──
phase('Accept')
log(`${featureId}: Acceptance phase`)

const allPassed = lastTestResult && lastTestResult.failCount === 0
const finalStatus = allPassed ? 'ready-for-review' :
  consecutiveNoProgress >= 3 ? 'blocked' : 'needs-attention'

await agent(`You are the Acceptor (Designer role).

${stateUpdate('acceptor', 'accept', round)}

Evaluate the overall results:
- Rounds completed: ${round}/${maxRounds}
- Final status: ${finalStatus}
- Test results: ${lastTestResult ? `${lastTestResult.passCount} pass, ${lastTestResult.failCount} fail, ${lastTestResult.skipCount} skip` : 'no test results'}

Read all round reports in ${featureDir}/rounds/.

Write:
1. ${featureDir}/final-report.md — summary of what was done, status, remaining issues
2. ${featureDir}/commit-plan.md — suggested commits (do NOT commit, just plan)

${stateEnd('acceptor', 'accept', round)}
echo '{"active":false,"phase":"${finalStatus}"}' > ${featureDir}/state.json
`, {
  label: `acceptor:${featureId}`,
  phase: 'Accept',
  model: 'opus'
})

log(`${featureId}: Complete — ${finalStatus}`)

return {
  featureId,
  status: finalStatus,
  rounds: round,
  testResults: lastTestResult
}
