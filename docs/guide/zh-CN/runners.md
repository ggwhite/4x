# Runner 与插件

## 什么是 Runner？

Runner 是 4x CLI 和 AI 工具之间的桥梁。CLI 生成角色 prompt 并管理状态；runner 将 prompt 发送给 AI 并捕获输出。

Runner 在 `.4x/settings.json` 的 `runners` 键下配置。CLI 以子进程方式调用 runner。

## 内置 Runner

| Runner | AI 工具 | 模式 | 状态 |
|---|---|---|---|
| `claude` | Claude Code CLI | PTY (tty: true) | 可用 |
| `codex` | OpenAI Codex CLI | Stdin | 可用 |
| `gemini` | Google Gemini CLI | Argument | 可用 |
| `agy` | Antigravity CLI | Argument | 可用 |
| `copilot` | GitHub Copilot CLI | Argument | 可用（需手动配置） |
| `cursor` | Cursor IDE | Rules file | 可用（需手动配置） |

`4x init` 默认配置 claude、codex、gemini 和 agy。Copilot 和 cursor 需要手动添加到 `settings.json`。

## 插件文件

每个 runner 都有嵌入在 `4x` 二进制文件中的指令文件。`4x init` 将它们部署到 `.4x/plugins/` 并在根目录文件中添加导入行：

| Runner | 插件文件 | 根目录导入 |
|---|---|---|
| claude | `CLAUDE.md` | CLAUDE.md |
| codex | `AGENTS.md` + `codex.json` | AGENTS.md |
| gemini | `GEMINI.md` | GEMINI.md |
| agy | `AGY.md` | AGY.md |
| copilot | `AGENTS.md` + `workflow.js` | AGENTS.md |
| cursor | `.cursorrules` | .cursorrules |

此外，共用指令文件会部署到 `.4x/plugins/shared/` 供所有 runner 使用：

| 文件 | 用途 |
|---|---|
| `shared/CREATOR.md` | Feature Creator 流程 — 引导 AI 通过 `4x new` 创建 feature |

更新二进制文件后，使用 `4x upgrade` 重新部署插件文件。

## Runner 执行模型

```
4x run F001 --runner claude
    │
    ├── Generate prompt for current role
    ├── Invoke runner subprocess with prompt
    │     claude --dangerously-skip-permissions -p "..."
    ├── Capture output to .4x/F001/logs/round-N-role.log
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

### PTY 模式

`tty: true` 的 runner（Claude Code）使用伪终端捕获完整输出，包括 ANSI 转义序列。一个有状态的 ANSI 清理器会清洗日志文件。

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
