# 4x — FB Post Draft (English)

---

Your AI writes the code, reviews it, and approves it — all by itself?

That's the problem 4x was built to solve.

4x is an open-source framework that splits AI development into four isolated roles: Design → Code → Review → Test. Each role can't see the others' reasoning — like a real team. The Designer never writes code. The Coder can't self-approve. The Reviewer is adversarial by design.

It's the opposite of "one agent does everything."

Here's what makes it different:

🔹 6 AI Runners — Claude Code, Codex, Gemini CLI, Copilot, Cursor, Antigravity. Same .4x/ file protocol, mix and match per role.
🔹 Deterministic Guardrails — State machine, scope lock, baseline snapshots, evidence gate. All enforced in the Go CLI, not in prompts.
🔹 Dashboard (4x Live) — macOS native (Swift) + Windows/Linux (Tauri). Real-time monitoring of every feature's phase, logs, and screenshots.
🔹 Batch Mode — Dependency-aware DAG scheduling. Queue dozens of features, review results in the morning.
🔹 Self-Evolution — Automatically mines improvement signals from failed runs, evaluates ROI through a value gate, and queues survivors back into the pipeline. 4x develops itself. 80+ features shipped this way.
🔹 Crash Recovery — Session dies? Resumes from checkpoint. Transient API errors? Auto-backoff retry.

The framework eats its own dogfood — 4x is built with 4x.

MIT License. Try it, file issues, contribute.

GitHub: https://github.com/ggwhite/4x
Docs: https://ggwhite.github.io/4x/

Install:
```
brew install ggwhite/tap/fourx
```
or
```
go install github.com/ggwhite/4x/cmd/4x@latest
```

#ClaudeCode #Codex #GeminiCLI #AIagents #OpenSource #DevTools #MultiAgent #4x
