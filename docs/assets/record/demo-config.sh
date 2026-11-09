#!/usr/bin/env bash
# Called (hidden) by tour.tape right after `4x init` in /tmp/4xdemo/work/greet:
#  - points the claude runner at the /tmp/4xdemo/bin/claude wrapper
#  - adds a compact "demo" profile: Design -> Code -> Review -> Test -> Accept
set -euo pipefail
D=/tmp/4xdemo
python3 - "$D" <<'PY'
import json, sys
d = sys.argv[1]
p = f"{d}/home/.4x/settings.json"
s = json.load(open(p))
s["runners"]["claude"]["command"] = f"{d}/bin/claude"
json.dump(s, open(p, "w"), indent=2)
p = f"{d}/work/greet/.4x/settings.json"
s = json.load(open(p))
s["profiles"] = {"demo": {"phases": [{"phase": x} for x in
    ["designing", "coding", "reviewing", "testing", "accepting"]]}}
s["default_profile"] = "demo"
json.dump(s, open(p, "w"), indent=2)
PY
cd "$D/work/greet" && git add -A && git commit -qm "4x init"
