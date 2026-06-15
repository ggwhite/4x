#!/usr/bin/env bash
set -euo pipefail

LOCALES_DIR="dashboard/web/locales"
BASE="$LOCALES_DIR/en.json"

if [ ! -f "$BASE" ]; then
  echo "ERROR: base file $BASE not found"
  exit 1
fi

base_keys=$(python3 -c "import json; print('\n'.join(sorted(json.load(open('$BASE')).keys())))")
exit_code=0

for f in "$LOCALES_DIR"/*.json; do
  lang=$(basename "$f" .json)
  [ "$lang" = "en" ] && continue

  if ! python3 -c "import json; json.load(open('$f'))" 2>/dev/null; then
    echo "ERROR: $f is not valid JSON"
    exit_code=1
    continue
  fi

  file_keys=$(python3 -c "import json; print('\n'.join(sorted(json.load(open('$f')).keys())))")

  missing=$(comm -23 <(echo "$base_keys") <(echo "$file_keys"))
  extra=$(comm -13 <(echo "$base_keys") <(echo "$file_keys"))

  if [ -n "$missing" ]; then
    echo "ERROR: $lang.json missing keys:"
    echo "$missing" | sed 's/^/  /'
    exit_code=1
  fi
  if [ -n "$extra" ]; then
    echo "WARNING: $lang.json extra keys:"
    echo "$extra" | sed 's/^/  /'
  fi
done

if [ $exit_code -eq 0 ]; then
  echo "OK: all locale files have matching keys"
fi
exit $exit_code
