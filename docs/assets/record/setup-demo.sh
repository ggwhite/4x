#!/usr/bin/env bash
# Build a throwaway 4x demo under /tmp/4xdemo: an isolated HOME, a tiny Go
# project ("greet") with one commit, a fresh 4x binary and a claude wrapper.
# The tour tape then runs `4x init` itself and calls demo-config.sh.
# Nothing here touches the real ~/.4x or the 4x repo's own .4x/.
#
# Usage: docs/assets/record/setup-demo.sh
# Then:  docs/assets/record/render.sh tour   (one real agent run, spends tokens)
set -euo pipefail

REPO="$(cd "$(dirname "$0")/../../.." && pwd)"
D=/tmp/4xdemo
REAL_HOME="$HOME"
REAL_CLAUDE="$(command -v claude || true)"

rm -rf "$D"
mkdir -p "$D/bin" "$D/home" "$D/work/greet"
(cd "$REPO" && go build -o "$D/bin/4x" ./cmd/4x)

# Claude Code needs the real HOME for its login (macOS keychain). The wrapper
# restores it only for the runner process, skips user settings, hooks, plugins
# and MCP servers, and asks for plain-English output.
if [ -n "$REAL_CLAUDE" ]; then
	cat >"$D/bin/claude" <<WRAP
#!/usr/bin/env bash
export HOME="$REAL_HOME"
exec "$REAL_CLAUDE" --setting-sources project,local --strict-mcp-config \\
	--append-system-prompt "Answer in plain English. Do not add language-practice notes." "\$@"
WRAP
	chmod +x "$D/bin/claude"
fi

export HOME="$D/home" PATH="$D/bin:$PATH"
cd "$D/work/greet"
git init -q -b main
git config user.name demo
git config user.email demo@example.invalid
cat >go.mod <<'GO'
module example.com/greet

go 1.22
GO
cat >main.go <<'GO'
package main

import "fmt"

func main() {
	fmt.Println("hello")
}
GO
git add -A && git commit -qm "initial commit"

echo "demo project ready: $D/work/greet"
