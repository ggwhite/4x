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

needs_update=0

# WARNING: this check compares committed history (BASE...HEAD) only.
# Uncommitted edits to docs/ or source dirs are NOT counted as "already updated",
# so an OK result on a dirty tree can be misleading — commit first, then re-run.
UNCOMMITTED=$(git status --porcelain 2>/dev/null | grep -E '^.. (docs|cmd|internal|plugins)/' || true)
if [ -n "$UNCOMMITTED" ]; then
  echo "WARNING: uncommitted changes are NOT included in this comparison (BASE...HEAD compares committed history only)." >&2
  echo "         commit them and re-run to get an accurate result. Relevant uncommitted files:" >&2
  echo "$UNCOMMITTED" | sed 's/^/           /' >&2
  echo "" >&2
fi

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

# --- core symbol-to-doc mapping check (matching logic unchanged) ---
# NOTE: wrapped in a function because bash 3.2 (macOS default) mis-parses a
# `case` statement placed directly inside $( ... ) command substitution.
map_check() {
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
    fi
  }
}
MAP_OUTPUT=$(map_check)

if [ -n "$MAP_OUTPUT" ]; then
  echo "$MAP_OUTPUT"
  needs_update=1
fi

# --- relocation/delete check: grep docs prose for old paths of removed/renamed files ---
# check-docs-sync's symbol map only tracks the NEW path; it can't see that docs prose
# still hardcodes a path that was deleted or renamed away. Grep the whole docs tree
# (all languages + docs/architecture/) for each removed/renamed source path.
NAME_STATUS=$(git diff --name-status "$BASE"...HEAD 2>/dev/null || git diff --name-status HEAD~1)
RELOC_OUTPUT=""
while IFS=$'\t' read -r st old new; do
  case "$st" in
    D)  oldpath="$old" ;;
    R*) oldpath="$old" ;;
    *)  continue ;;
  esac
  [ -z "$oldpath" ] && continue
  case "$oldpath" in docs/*) continue ;; esac
  hits=$(grep -rlF "$oldpath" docs 2>/dev/null || true)
  [ -z "$hits" ] && continue
  RELOC_OUTPUT="${RELOC_OUTPUT}  $oldpath (removed/renamed) still referenced in:
"
  for h in $hits; do
    RELOC_OUTPUT="${RELOC_OUTPUT}    - $h
"
  done
done <<< "$NAME_STATUS"

if [ -n "$RELOC_OUTPUT" ]; then
  [ "$needs_update" -gt 0 ] && echo ""
  echo "Removed/renamed source paths still referenced in docs prose:"
  echo "$RELOC_OUTPUT"
  needs_update=1
fi

if [ "$needs_update" -eq 0 ]; then
  echo "OK: no doc updates needed"
else
  echo ""
  echo "NEEDS_UPDATE" >&2
fi
