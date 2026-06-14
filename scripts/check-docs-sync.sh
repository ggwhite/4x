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

# collect docs that also changed in the same diff range (already updated)
UPDATED_DOCS=""
for file in $DIFF_FILES; do
  case "$file" in docs/*) UPDATED_DOCS="$UPDATED_DOCS $file" ;; esac
done

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
  skip_doc=""
  pending_lines=""
  while IFS=$'\t' read -r doc file; do
    if [ "$doc" != "$prev_doc" ]; then
      # flush previous doc group if it wasn't skipped
      if [ -n "$prev_doc" ] && [ "$prev_doc" != "$skip_doc" ]; then
        [ "$found" -gt 0 ] && echo ""
        if [ -f "$prev_doc" ]; then
          echo "  $prev_doc"
        else
          echo "  $prev_doc (MISSING)"
        fi
        echo "$pending_lines"
        found=$((found + 1))
      fi
      prev_doc="$doc"
      pending_lines=""
      skip_doc=""
      # check if this doc was also updated in the diff
      for ud in $UPDATED_DOCS; do
        if [ "$ud" = "$doc" ]; then
          skip_doc="$doc"
          break
        fi
      done
    fi
    pending_lines="${pending_lines}    - $file
"
  done

  # flush last group
  if [ -n "$prev_doc" ] && [ "$prev_doc" != "$skip_doc" ]; then
    [ "$found" -gt 0 ] && echo ""
    if [ -f "$prev_doc" ]; then
      echo "  $prev_doc"
    else
      echo "  $prev_doc (MISSING)"
    fi
    echo "$pending_lines"
    found=$((found + 1))
  fi

  if [ "$found" -eq 0 ]; then
    echo "OK: no doc updates needed"
  else
    echo ""
    echo "NEEDS_UPDATE" >&2
  fi
}
