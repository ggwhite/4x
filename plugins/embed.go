package plugins

import "embed"

//go:embed claude-code/SKILL.md claude-code/workflow.js codex/AGENTS.md gemini/GEMINI.md agy/AGY.md cursor/.cursorrules copilot/AGENTS.md copilot/workflow.js
var FS embed.FS
