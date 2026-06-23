# LinkedIn Post Draft

直接複製 --- 以下的內容貼到 LinkedIn。

---

I kept asking AI to write code, review it, and tell me if it was good.

It always said yes.

That's the problem. When one AI session handles design, coding, review, and testing, there's no real check. It's reviewing its own work with its own biases. For small tasks it's fine. For production features, it falls apart.

So I built 4x — an open-source framework that splits the AI development loop into four isolated roles:

Designer → Coder → Reviewer → Tester

Each role runs in a separate session. The Designer writes the spec but never touches code. The Coder implements but can't self-approve. The Reviewer is adversarial — it's looking for what's wrong, not confirming what's right. The Tester verifies against criteria written before implementation.

The critical design decision: guardrails are enforced by a Go CLI, not by prompting.

A state machine controls phase transitions. Scope lock prevents files outside the declared boundary from being modified. An evidence gate requires the Tester to produce actual test results — not just "all tests pass." If Review or Test fails, the loop automatically iterates.

Humans gate at the final Accept phase. AI proposes, you decide.

Some things I didn't expect when building this:

→ Cross-model review catches more bugs than same-model review. You can assign Claude to code and Gemini to review — different models have different blind spots.

→ The framework started developing itself. It mines failure signals from past runs, evaluates candidates through a value gate (with anti-hack protection), and queues improvements back into the pipeline. 80+ features shipped this way.

→ Batch mode changed how I work. Queue 20 features before bed, review results in the morning. Dependency-aware DAG scheduling handles the ordering.

4x supports 6 AI runners (Claude Code, Codex, Gemini CLI, Copilot, Cursor, Antigravity) through a shared file protocol. It comes with a native dashboard for real-time monitoring.

MIT License. Written in Go.

GitHub: https://github.com/ggwhite/4x

If you're using AI for anything beyond one-off scripts, I'd love to hear how you handle the "AI reviewing its own work" problem.

#OpenSource #AIEngineering #DevTools #ClaudeCode #SoftwareEngineering
