package plugins

import "embed"

//go:embed claude-code/CLAUDE.md codex/AGENTS.md gemini/GEMINI.md agy/AGY.md cursor/.cursorrules copilot/AGENTS.md copilot/workflow.js shared/CREATOR.md
var FS embed.FS
