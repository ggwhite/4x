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

# source prefixes of every RULES entry (first tab-separated column). Used to
# reject any .docsyncignore glob that would disable a whole rule (see .docsyncignore).
RULES_PREFIXES=$(echo "$RULES" | cut -f1)

# --- load .docsyncignore (suppression allowlist) -----------------------------
# Format (see .docsyncignore header): one entry per line. Parse rule (F153
# Design Ruling 7): strip from the FIRST '#' to end (covers both whole-line
# comments and trailing "# 理由"), then trim surrounding whitespace; skip blanks.
# Remaining content is path-level (no TAB, single glob) or pair-level
# (glob<TAB>doc). glob semantics match the RULES `case` matcher: `*` matches any
# character including `/`. If .docsyncignore is absent, behaviour is identical
# to the pre-F153 script (no suppression).
PATH_ENTRIES=""
PAIR_ENTRIES=""
DOCSYNCIGNORE=".docsyncignore"
if [ -f "$DOCSYNCIGNORE" ]; then
  ds_tab=$(printf '\t')
  while IFS= read -r rawline || [ -n "$rawline" ]; do
    # strip comment from the first '#' (source paths never contain '#')
    line="${rawline%%#*}"
    # trim leading whitespace (preserves any internal TAB separator)
    line="${line#"${line%%[![:space:]]*}"}"
    # trim trailing whitespace
    line="${line%"${line##*[![:space:]]}"}"
    [ -z "$line" ] && continue
    case "$line" in
      *"$ds_tab"*) ds_glob="${line%%"$ds_tab"*}" ;;
      *)           ds_glob="$line" ;;
    esac
    # guard (F153 Ruling 4, hardened by F157): reject any glob that would
    # subsume an entire RULES source prefix — not only one that equals it.
    # Trailing-star forms like `internal/protocol/*`, `internal/protocol/*.go`,
    # `*`, or `**` match every file the rule tracks (bash `case` `*` spans `/`),
    # silently disabling the whole mapping. Probe each prefix with two
    # representative paths — the prefix itself (catches exact equality, `/*`,
    # `*`, `**`) and, for directory prefixes, a synthetic file under it (catches
    # `/*.go` and similar) — and reject the glob if it matches either.
    ds_skip=0
    while IFS= read -r ds_p; do
      [ -z "$ds_p" ] && continue
      case "$ds_p" in $ds_glob) ds_skip=1; break ;; esac
      case "$ds_p" in
        */)
          case "${ds_p}__docsyncignore_probe__.go" in
            $ds_glob) ds_skip=1; break ;;
          esac
          ;;
      esac
    done <<EOF
$RULES_PREFIXES
EOF
    if [ "$ds_skip" -eq 1 ]; then
      echo "WARNING: .docsyncignore glob '$ds_glob' would suppress an entire RULES source prefix; ignoring to avoid disabling the whole rule." >&2
      continue
    fi
    case "$line" in
      *"$ds_tab"*) PAIR_ENTRIES="${PAIR_ENTRIES}${line}
" ;;
      *)           PATH_ENTRIES="${PATH_ENTRIES}${ds_glob}
" ;;
    esac
  done < "$DOCSYNCIGNORE"
fi

# temp file to collect suppressed (doc, file) pairs for the informational
# stderr block. Populated by apply_suppression, which runs in a pipe subshell.
SUPPRESS_FILE=$(mktemp)
trap 'rm -f "$SUPPRESS_FILE"' EXIT

# apply_suppression: filter a stream of "doc<TAB>file" lines. A pair is dropped
# when a path-level glob matches file, or a pair-level glob matches file AND its
# doc equals the pair's doc. Dropped pairs are appended to SUPPRESS_FILE.
# NOTE: defined at top level so the `case` inside is parsed here (not re-parsed
# inside the `$( ... )` that calls map_check — bash 3.2 mis-parses that).
apply_suppression() {
  while IFS=$'\t' read -r sup_doc sup_file; do
    sup_hit=0
    if [ -n "$PATH_ENTRIES" ]; then
      while IFS= read -r sup_g; do
        [ -z "$sup_g" ] && continue
        case "$sup_file" in
          $sup_g) sup_hit=1; break ;;
        esac
      done <<EOF
$PATH_ENTRIES
EOF
    fi
    if [ "$sup_hit" -eq 0 ] && [ -n "$PAIR_ENTRIES" ]; then
      while IFS=$'\t' read -r sup_g sup_d; do
        [ -z "$sup_g" ] && continue
        [ "$sup_d" != "$sup_doc" ] && continue
        case "$sup_file" in
          $sup_g) sup_hit=1; break ;;
        esac
      done <<EOF
$PAIR_ENTRIES
EOF
    fi
    if [ "$sup_hit" -eq 1 ]; then
      printf '%s\t%s\n' "$sup_doc" "$sup_file" >> "$SUPPRESS_FILE"
    else
      printf '%s\t%s\n' "$sup_doc" "$sup_file"
    fi
  done
}

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
  done | apply_suppression | sort -t$'\t' -k1,1 | {
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

# informational (F153 Ruling 6): report suppressed (doc, file) pairs on stderr.
# This is purely informational and must NOT change the exit code.
if [ -s "$SUPPRESS_FILE" ]; then
  echo "Suppressed via .docsyncignore:" >&2
  sort -u "$SUPPRESS_FILE" | while IFS=$'\t' read -r sup_doc sup_file; do
    echo "  $sup_doc <- $sup_file" >&2
  done
fi

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
  # exit 非零讓 NEEDS_UPDATE 成為機器可驗的失敗訊號（與 check-i18n 一致）：
  # 先前只寫 stderr 卻 exit 0，導致 docs-gate 及任何自動化都誤判為通過（F136）。
  exit 1
fi
