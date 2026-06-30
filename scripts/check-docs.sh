#!/usr/bin/env bash
set -euo pipefail

# Extract subcommand names registered in cmd/4x/main.go
# Pattern: newXxxCmd() calls inside rootCmd.AddCommand(...)
COMMANDS=$(grep -oE 'new[A-Z][a-zA-Z]*Cmd' cmd/4x/main.go | sed 's/^new//;s/Cmd$//' | tr '[:upper:]' '[:lower:]')

CLI_DOC="docs/guide/cli.md"

if [ ! -f "$CLI_DOC" ]; then
  echo "FAIL: $CLI_DOC not found"
  exit 1
fi

cli_content=$(tr '[:upper:]' '[:lower:]' < "$CLI_DOC")

missing=()
for cmd in $COMMANDS; do
  # batch is a parent command — check for "4x batch" pattern
  # Use herestring to avoid SIGPIPE with pipefail when grep -q exits early
  if ! grep -q "4x ${cmd}" <<< "$cli_content"; then
    missing+=("$cmd")
  fi
done

if [ ${#missing[@]} -gt 0 ]; then
  echo "FAIL: The following CLI commands are missing from $CLI_DOC:"
  for cmd in "${missing[@]}"; do
    echo "  - 4x $cmd"
  done
  echo ""
  echo "Add documentation for these commands, then re-run."
  exit 1
fi

echo "OK: All $(echo "$COMMANDS" | wc -w | tr -d ' ') CLI commands documented in $CLI_DOC"
