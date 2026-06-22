# 4x — FB 發文草稿（繁體中文）

---

你的 AI 自己寫 code、自己 review、然後自己說 LGTM？

這就是為什麼我做了 4x。

4x 是一個開源框架，把 AI 開發拆成四個隔離角色：Design → Code → Review → Test。每個角色看不到其他角色的推理過程，就像真正的團隊分工——Designer 不寫 code，Coder 不能自己 approve，Reviewer 是對抗式審查。

跟單一 agent 寫完就丟給你看的模式完全不同。

幾個重點：

🔹 6 種 AI Runner — Claude Code、Codex、Gemini CLI、Copilot、Cursor、Antigravity，同一份 .4x/ 協定，混搭使用
🔹 確定性 Guardrail — 狀態機、scope lock、baseline snapshot、evidence gate，全部寫在 Go CLI 裡，不是寫在 prompt 裡
🔹 Dashboard（4x Live）— macOS native + Windows/Linux，即時監控每個 feature 的進度、log、截圖
🔹 Batch Mode — 依賴感知的 DAG 排程，排幾十個 feature 丟進去跑，早上起來看結果
🔹 Self-Evolution — 從失敗的 run 中自動挖掘改進訊號，評估 ROI，通過 value gate 的自動排入 pipeline 繼續跑。4x 用自己開發自己，目前已經跑完 80+ 個 feature
🔹 Crash Recovery — session 斷了從斷點恢復，API 暫態錯誤自動重試

4x 本身就是用 4x 開發的。框架自己吃自己的 dogfood。

MIT License，歡迎試用、提 issue、contribute。

GitHub：https://github.com/ggwhite/4x
文件：https://ggwhite.github.io/4x/

#ClaudeCode #AIagents #OpenSource #DevTools #4x
