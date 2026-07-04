# Runner 与插件

## 什么是 Runner？

Runner 是 4x CLI 和 AI 工具之间的桥梁。CLI 生成角色 prompt 并管理状态；runner 将 prompt 发送给 AI 并捕获输出。

Runner 在 `.4x/settings.json` 的 `runners` 键下配置。CLI 以子进程方式调用 runner。

## 内置 Runner

| Runner | AI 工具 | 模式 | 状态 |
|---|---|---|---|
| `claude` | Claude Code CLI | Stream JSON | 可用 |
| `codex` | OpenAI Codex CLI | Stdin | 可用 |
| `gemini` | Google Gemini CLI | Argument | 可用 |
| `agy` | Antigravity CLI | Argument | 可用 |
| `opencode` | OpenCode CLI | Argument | 可用 |
| `copilot` | GitHub Copilot CLI | Argument | 可用（需手动配置） |
| `cursor` | Cursor IDE | Rules file | 可用（需手动配置） |

`4x init` 默认配置 claude、codex、gemini、agy 和 opencode。Copilot 和 cursor 需要手动添加到 `settings.json`。

## 插件文件

每个 runner 都有嵌入在 `4x` 二进制文件中的指令文件。`4x init` 将它们部署到 `.4x/plugins/` 并在根目录文件中添加导入行：

| Runner | 插件文件 | 根目录导入 |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| opencode | `AGENTS.md` | AGENTS.md |
| copilot | `AGENTS.md` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

此外，共用指令文件会部署到 `.4x/plugins/shared/` 供所有 runner 使用：

| 文件 | 用途 |
|---|---|
| `shared/CREATOR.md` | Feature Creator 流程 — 引导 AI 通过 `4x new` 创建 feature |

更新二进制文件后，使用 `4x sync` 重新部署插件文件。

## Runner 执行模型

```
4x run F001 --runner claude
    │
    ├── Generate prompt for current role
    ├── Invoke runner subprocess with prompt
    │     claude --dangerously-skip-permissions -p "..." --output-format stream-json --verbose
    ├── Capture output to .4x/run/F001/logs/round-N-role.log
    ├── Check output artifacts
    └── Transition state, repeat
```

### 退出码

| 退出码 | 含义 | 操作 |
|---|---|---|
| 0 | 成功 | 进入下一阶段 |
| 1 | 软失败 | Feature 移至 `blocked` |
| 2 | 硬错误 | 循环停止，需要关注 |
| timeout | 超过时限无响应 | 视为软失败 |

### 占位符解析

Runner 的 `args` 可包含占位符，CLI 在调用子进程前会将其替换：

| 占位符 | 替换为 |
|---|---|
| `{prompt}` | 角色 prompt 文本，作为参数内联传入 |
| `{promptFile}` | 包含 prompt 内容的临时文件路径 |
| `{model}` | 该角色解析后的模型覆盖值 |

占位符解析**失败时立即报错**，而非将字面占位符传给 AI CLI：

- `{model}` 存在但没有解析到模型覆盖值 → runner 报错 `model not resolved for runner <name>`，而非发送 `--model {model}`（后者会导致 CLI 报出含义不明的错误）。
- `{promptFile}` 但临时文件无法创建或写入（如 `/tmp` 已满）→ runner 返回包装后的底层错误（`runner <name>: create prompt temp file: ...`）并删除任何已创建的临时文件，而非发送字面字符串 `{promptFile}`。

解析期间创建的临时文件始终会被清理，即使后续步骤失败也是如此。

### Stream JSON 模式

设置 `output_format: "stream-json"` 的 runner 会写入两种文件：dashboard tail 的可读 `.log`，以及用于调试的原始 `.stream.jsonl`。Claude Code 默认使用此模式。`.log` 中的 tool-use 摘要（例如 Bash 命令）会被截断到固定长度，截断点对齐 UTF-8 字符边界，避免多字节字符被从中间切断。

### 非 PTY 进程组处理

非 PTY runner（stream-json 模式、stdin 模式、普通参数模式）使用独立进程组（Unix 上的 `Setpgid`）。当运行上下文被取消时，进程组会立即收到 `SIGKILL`——没有 SIGTERM 宽限期。Windows 上使用默认的 `exec.CommandContext` 行为。

### PTY 模式

`tty: true` 的 runner（且不使用 `output_format: "stream-json"`）使用伪终端捕获完整输出，包括 ANSI 转义序列。一个有状态的 ANSI 清理器会清洗日志文件。PTY 路径使用 `exec.Command` 配合专用的上下文监视器进行优雅关闭，而非 PTY runner 使用带进程组级取消的 `exec.CommandContext`（见上文）。

PTY 子进程在自己的会话/进程组中运行。当运行上下文被取消（如超时或 Ctrl+C）时，整个进程组收到 `SIGTERM`，若 5 秒内未退出则升级为 `SIGKILL`——确保没有孤儿子进程存活于运行结束之后。

### Stdin 模式

`stdin: true` 的 runner（Codex）通过标准输入接收 prompt，而不是命令行参数。

## 为不同角色使用不同模型

在 `.4x/settings.json` 中配置：

```json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

> **注意：** `deep_model` 配置在 **reviewer** role 上（不是 deep-reviewer）。如果未设置 `roles.reviewer.deep_model`，`deep-reviewing` 阶段会被**完全跳过** — 流程直接从 `testing` 转到 `accepting`。这是设计意图：深度审查是 opt-in 的。

你也可以混合使用 runner — 用 Claude 做设计、Gemini 做编码等 — 通过手动使用不同的 `--runner` 标志运行每个阶段，并用 `4x transition` 在阶段间切换。

## 编写插件

插件遵循简单的合约 — 读取 `.4x/` 文件，执行 AI 工作，将结果写回：

1. 读取 `.4x/features/{id}.yaml` 了解 feature
2. 读取 `state.json` 了解当前阶段
3. 读取阶段特定输入（task-brief.md、scope 等）
4. 执行工作（调用 LLM、运行工具）
5. 写入阶段特定输出（coder-report.md、review-report.md 等）
6. 以适当的退出码退出（0 = 成功，1 = 软失败，2 = 硬错误）

无需 SDK。无运行时依赖。只有文件。
