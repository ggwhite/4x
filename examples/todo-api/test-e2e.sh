#!/usr/bin/env bash
set -euo pipefail

CLI="../../bin/4x"
FEAT="rest-api-for-todo-items"
DOT=".4x"

echo "=== 4x e2e integration test ==="

# 1. Status
echo "--- status ---"
$CLI status
$CLI status $FEAT

# 2. Check (should pass for init state)
echo "--- check (expect: no state.json yet → fail) ---"
$CLI check $FEAT 2>&1 && echo "UNEXPECTED: check passed" || echo "OK: check failed as expected (no state.json)"

# 3. Simulate Designer output
echo "--- simulate Designer phase ---"
FDIR="${DOT}/${FEAT}"
mkdir -p "${FDIR}/rounds/round-1"

cat > "${FDIR}/state.json" <<'STATE'
{"featureId":"rest-api-for-todo-items","phase":"designing","role":"designer","round":0,"maxRounds":5,"active":true,"runner":"claude"}
STATE

cat > "${FDIR}/task-brief.md" <<'TB'
# Task Brief — REST API for todo items

## Goal
Implement CRUD endpoints for todo items with PostgreSQL storage.

## Scope
- backend/internal/todo/
- backend/api/v1/
TB

cat > "${FDIR}/acceptance-criteria.md" <<'AC'
# Acceptance Criteria

| # | Criterion | Method |
|---|---|---|
| AC-1 | GET /v1/todos returns 200 | Integration test |
| AC-2 | POST missing title returns 400 | Integration test |
| AC-3 | Non-existent ID returns 404 | Integration test |
AC

cat > "${FDIR}/test-strategy.yaml" <<'TS'
web: false
api: true
gate: false
coder_only: false
verify_commands:
  - go test ./...
TS

echo '{"ts":"2026-06-11T10:00:00Z","type":"phase-start","phase":"designing","role":"designer","round":0}' > "${FDIR}/events.jsonl"

# 4. Transition to coding
echo "--- transition: designing → coding ---"
$CLI transition $FEAT --to coding

# 5. Check (should pass now — coding phase, task-brief + criteria exist)
echo "--- check (expect: pass) ---"
$CLI check $FEAT
echo "OK: check passed"

# 6. Status after transition
echo "--- status after transition ---"
$CLI status $FEAT

# 7. Simulate Coder output
echo "--- simulate Coder output ---"
cat > "${FDIR}/rounds/round-1/coder-report.md" <<'CR'
# Coder Report — Round 1
## What Was Done
Implemented todo CRUD handlers and store.
## Files Changed
- backend/internal/todo/handler.go
- backend/internal/todo/store.go
CR

# 8. Transition to reviewing
echo "--- transition: coding → reviewing ---"
$CLI transition $FEAT --to reviewing

# 9. Transition to testing (reviewer passed)
echo "--- transition: reviewing → testing ---"
$CLI transition $FEAT --to testing

# 10. Transition to accepting
echo "--- transition: testing → accepting ---"
$CLI transition $FEAT --to accepting

# 11. Transition to done
echo "--- transition: accepting → done ---"
$CLI transition $FEAT --to done

# 12. Final status
echo "--- final status ---"
$CLI status $FEAT

# 13. Event log
echo "--- event log ($(wc -l < "${FDIR}/events.jsonl" | tr -d ' ') events) ---"

# 14. Batch plan (no more pending features)
echo "--- batch plan ---"
$CLI batch plan --dry-run 2>&1 || true

echo "=== All e2e checks passed ==="
