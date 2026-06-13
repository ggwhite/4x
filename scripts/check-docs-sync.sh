#!/usr/bin/env bash
set -euo pipefail

BASE="${1:-}"

if [ -z "$BASE" ]; then
  current=$(git rev-parse --abbrev-ref HEAD)
  if [ "$current" = "main" ] || [ "$current" = "master" ]; then
    # on main: compare against last tag, or last 20 commits if no tags
    BASE=$(git describe --tags --abbrev=0 2>/dev/null || echo "HEAD~20")
  else
    BASE="main"
  fi
fi

DIFF_FILES=$(git diff --name-only "$BASE"...HEAD 2>/dev/null || git diff --name-only HEAD~1)

# pattern → doc mapping (one per line, tab-separated)
RULES="cmd/4x/	docs/guide/cli.md
internal/state/machine.go	docs/guide/concepts.md
internal/protocol/	docs/guide/concepts.md
internal/runner/	docs/guide/runners.md
plugins/	docs/guide/runners.md
internal/server/	docs/guide/dashboard.md
internal/batch/	docs/guide/batch.md"

results=""
triggers=""

for file in $DIFF_FILES; do
  case "$file" in docs/*) continue ;; esac

  echo "$RULES" | while IFS=$'\t' read -r pattern doc; do
    case "$file" in
      "${pattern}"*) echo "$doc	$file" ;;
    esac
  done
done | sort -t$'\t' -k1,1 | {
  found=0
  prev_doc=""
  while IFS=$'\t' read -r doc file; do
    if [ "$doc" != "$prev_doc" ]; then
      [ -n "$prev_doc" ] && echo ""
      if [ -f "$doc" ]; then
        echo "  $doc"
      else
        echo "  $doc (MISSING)"
      fi
      prev_doc="$doc"
      found=1
    fi
    echo "    - $file"
  done

  if [ "$found" -eq 0 ]; then
    echo "OK: no doc updates needed"
  else
    echo ""
    echo "NEEDS_UPDATE" >&2
  fi
}
