# 配置

## 项目配置（`.4x/settings.json`）

由 `4x init` 创建。包含项目元数据、runner 定义和角色模型映射。

```json
{
  "project": {
    "name": "my-project",
    "language": "go",
    "build": ["go build ./..."],
    "test": ["go test ./..."],
    "lint": ["go vet ./..."],
    "setup": [],
    "docs": [],
    "rules": []
  },
  "runners": {
    "claude": {
      "command": "claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}", "--output-format", "stream-json", "--verbose"],
      "model": "opus",
      "output_format": "stream-json"
    },
    "codex": {
      "command": "codex",
      "args": ["exec"],
      "stdin": true
    },
    "gemini": {
      "command": "gemini",
      "args": ["-y", "-p", "{prompt}"]
    },
    "agy": {
      "command": "agy",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"]
    }
  },
  "default": "claude",
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

### Project 区块

| 字段 | 说明 |
|---|---|
| `name` | 项目名称（从目录名自动检测） |
| `language` | 检测到的语言 |
| `build` | 构建命令 |
| `test` | 测试命令 |
| `lint` | Lint 命令 |
| `setup` | 初始化命令（如 `docker-compose up -d`） |
| `docs` | 文档文件路径，供设计者参考 |
| `rules` | 项目特定规则，注入到角色 prompt 中 |

### Runner 配置

| 字段 | 说明 |
|---|---|
| `command` | 可执行文件名 |
| `args` | 参数。`{prompt}` 和 `{promptFile}` 在运行时替换。`{model}` 替换为角色的模型。 |
| `model` | 此 runner 的默认模型 |
| `model_map` | 角色模型名称与 runner 专用名称的映射（例：`{"opus": "claude-opus-4-5-20250514"}`）。查找顺序：角色 model → model_map 翻译 → 回退原名。 |
| `output_format` | 设为 `"stream-json"` 时，runner stdout 会解析为可读 `.log` 和原始 `.stream.jsonl`。 |
| `tty` | 使用 PTY 捕获输出。`output_format` 为 `"stream-json"` 时会跳过 PTY。 |
| `stdin` | 通过标准输入发送 prompt 而非命令行参数（Codex 使用） |
| `quiet` | 抑制 runner 的终端 stdout 输出；输出仍会写入 log 文件。 |

如果 `args` 中没有 `{model}`，runner 会自动附加 `--model <model>`。

### 角色配置

| 字段 | 说明 |
|---|---|
| `model` | 此角色的模型名称 |
| `deep_model` | 对抗性审查阶段的模型（仅限 reviewer） |
| `instructions` | 注入到角色 prompt 中的附加指令 |
| `includes` | 包含在角色 prompt 中的文件 |

### 其他配置字段

| 字段 | 说明 |
|---|---|
| `hub_repos` | 共享仓库（用于批量 DAG 分组） |
| `isolation` | 设为 `"worktree"` 以在 git worktree 中运行 feature |
| `max_concurrent_runs` | 通过仪表盘服务器的最大并发运行数 |
| `commit` | 提交策略：`"per-round"`（默认）、`"on-done"` 或 `"never"` |

## 用户配置（`~/.4x/settings.json`）

全局用户偏好设置。通过 `4x config` 管理。

```bash
4x config set locale zh-TW
4x config get locale
4x config list
```

### 语言 (Locale)

设置角色 prompt 指令的语言。支持的值：

| 值 | 语言 |
|---|---|
| `en` | English（默认） |
| `zh-TW` | 繁体中文 |
| `zh-CN` | 简体中文 |
| `ja` | 日语 |
| `ko` | 韩语 |
| `es` | 西班牙语 |
| `fr` | 法语 |
| `de` | 德语 |
| `pt` | 葡萄牙语 |
| `ru` | 俄语 |
| `vi` | 越南语 |

如果未显式设置，语言也会从 `LANG` 环境变量推断。
