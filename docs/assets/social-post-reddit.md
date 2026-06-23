# Reddit Post Draft

Title 和 Body 分開，直接複製貼上。每個 subreddit 用同一篇，Title 換一下前綴。

---

## r/ClaudeAI

**Title:** I built an open-source framework that stops Claude from reviewing its own code

**Body:**

I got tired of asking Claude to write a feature, then asking it to review its own work, and having it say "LGTM" every time. It's like asking a student to grade their own exam.

So I built 4x — a CLI framework that splits the AI development loop into four isolated roles: Designer, Coder, Reviewer, and Tester. Each role runs in a separate session with no access to the others' reasoning. The Designer writes the spec but can't touch code. The Coder implements but can't self-approve. The Reviewer is adversarial by design. The Tester verifies against criteria written before implementation.

The key insight: the guardrails are enforced by a Go CLI, not by prompting. A state machine controls the phase transitions (design → code → review → test → done), scope lock prevents files outside the declared boundary from being modified, and an evidence gate requires the Tester to produce actual test results — not just "all tests pass."

It supports 6 AI runners (Claude Code, Codex, Gemini CLI, Copilot, Cursor, Antigravity) through a shared file protocol (.4x/ directory). You can mix runners per role — e.g., Claude designs, Codex codes, Gemini reviews.

Some other things it does:
- Batch mode with dependency-aware DAG scheduling — queue 20 features overnight
- Native dashboard (macOS Swift / Windows+Linux Tauri) with real-time SSE monitoring
- Crash recovery — runner dies, resumes from checkpoint
- Self-evolution — it mines its own failure history, evaluates candidates through a value gate, and queues improvements back into the pipeline. 4x literally develops itself (80+ features shipped this way)

MIT license, written in Go.

GitHub: https://github.com/ggwhite/4x

Happy to answer questions about the architecture or how the role isolation actually works in practice.

---

## r/OpenAI (or r/Codex)

**Title:** I built a multi-agent dev framework that works with Codex, Claude, Gemini, and 3 other runners

**Body:**

(Same body as above)

---

## r/ChatGPTCoding

**Title:** Instead of one AI doing everything, I split the dev loop into 4 isolated roles — here's what happened

**Body:**

(Same body as above)

---

## r/programming or r/opensource

**Title:** Show r/programming: 4x — a Go CLI that orchestrates AI agents as Designer, Coder, Reviewer, and Tester

**Body:**

(Same body as above, but remove the first paragraph about Claude specifically and start from "I built 4x —")

---

## Notes

- Reddit 不喜歡 emoji，全部用純文字
- 用第一人稱敘事，不要像廣告
- 結尾的 "Happy to answer questions" 很重要，Reddit 社群重視作者互動
- 發完後前 1-2 小時要回覆留言，這決定了帖子會不會被推上去
- 每個 subreddit 間隔 1-2 天發，不要同一天全發完，會被當 spam
