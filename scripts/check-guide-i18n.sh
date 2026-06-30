#!/usr/bin/env bash
# check-guide-i18n.sh — 比對 docs/guide/ 各語系與英文版的結構是否一致。
#
# 檢查項目：
# 1. cli.md: heading 含 backtick command（如 ## `4x run`）不該被翻譯，直接比對文字
# 2. 其他 .md: heading 會被翻譯，只比對各層級的數量是否一致
#
# 用法：bash scripts/check-guide-i18n.sh [docs/guide/cli.md ...]
#   不帶參數時檢查 docs/guide/ 下所有 .md 檔。
set -euo pipefail

GUIDE_DIR="docs/guide"
LANGS=(es ja ko zh-CN zh-TW)

if [ $# -gt 0 ]; then
  EN_FILES=("$@")
else
  EN_FILES=()
  for f in "$GUIDE_DIR"/*.md; do
    [ -f "$f" ] && EN_FILES+=("$f")
  done
fi

exit_code=0

for en_file in "${EN_FILES[@]}"; do
  basename=$(basename "$en_file")

  for lang in "${LANGS[@]}"; do
    lang_file="$GUIDE_DIR/$lang/$basename"

    if [ ! -f "$lang_file" ]; then
      echo "ERROR: $lang/$basename missing (exists in en)"
      exit_code=1
      continue
    fi

    if [ "$basename" = "cli.md" ]; then
      # cli.md: command headings 不該被翻譯，直接比對含 backtick 的 heading
      en_cmds=$(grep '^##' "$en_file" | grep '`' | sed 's/^ *//')
      lang_cmds=$(grep '^##' "$lang_file" | grep '`' | sed 's/^ *//')

      missing=$(comm -23 <(echo "$en_cmds" | sort) <(echo "$lang_cmds" | sort))
      extra=$(comm -13 <(echo "$en_cmds" | sort) <(echo "$lang_cmds" | sort))

      if [ -n "$missing" ]; then
        echo "ERROR: $lang/$basename missing command sections:"
        echo "$missing" | sed 's/^/  /'
        exit_code=1
      fi
      if [ -n "$extra" ]; then
        echo "WARNING: $lang/$basename extra command sections:"
        echo "$extra" | sed 's/^/  /'
      fi
    else
      # 其他檔案：heading 會被翻譯，只比對各層級的數量
      for level in "## " "### "; do
        en_count=$(grep -c "^${level}" "$en_file" 2>/dev/null || echo 0)
        lang_count=$(grep -c "^${level}" "$lang_file" 2>/dev/null || echo 0)

        if [ "$en_count" != "$lang_count" ]; then
          echo "ERROR: $lang/$basename '${level}' heading count mismatch: en=$en_count $lang=$lang_count"
          exit_code=1
        fi
      done
    fi
  done
done

if [ $exit_code -eq 0 ]; then
  echo "OK: all guide translations have matching structure"
fi
exit $exit_code
