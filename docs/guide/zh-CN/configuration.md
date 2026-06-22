# 配置

## 项目配置（`.4x/settings.json`）

由 `4x init` 创建。包含项目元数据、runner 定义和角色模型映射。

你也可以在 **4x Live 仪表盘**中可视化编辑此文件——点击 "4x Live" 标题旁的齿轮图标（⚙），或按 `Cmd+Shift+,`。编辑器支持表单视图和原始 JSON 视图，验证必填字段，并在写入前将旧配置备份到 `settings.json.bak`。

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
  "default_runner": "claude",
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
| `description` | 项目描述（可选） |
| `docs` | 文档文件路径，供设计者参考 |
| `rules` | 项目特定规则，注入到角色 prompt 中 |
| `includes` | 包含在角色 prompt 中的文件 |

### Runner 配置

| 字段 | 说明 |
|---|---|
| `command` | 可执行文件名 |
| `args` | 参数。`{prompt}` 和 `{promptFile}` 在运行时替换。`{model}` 替换为角色的模型。 |
| `model` | 此 runner 的默认模型 |
| `tiers` | tier 名称到 runner 专用模型名的映射（例：`{"opus": "claude-opus-4-5-20250514"}`）。查找顺序：角色 model → tiers 翻译 → 回退原名。 |
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
| `max_fix_rounds` | `deep-reviewing` 阶段的最大自修复迭代次数（仅限 deep-reviewer；默认 2）。每次迭代运行一个作用域受限的 mini-coder + re-verifier；超过上限则升级为 `needs-attention`。 |
| `instructions` | 注入到角色 prompt 中的附加指令 |
| `includes` | 包含在角色 prompt 中的文件 |
| `screenshot_dir` | 测试者截图的目录路径 |
| `parallel_reviewers` | 深度审查的并行子审查者数量（仅限 deep-reviewer；<=1 回退到单 agent 模式） |
| `angles_per_reviewer` | 每个子审查者的审查角度数（仅限 deep-reviewer；0 表示自动均匀分配） |

### 其他配置字段

| 字段 | 说明 |
|---|---|
| `hub_repos` | 共享仓库（用于批量 DAG 分组） |
| `isolation` | 设为 `"worktree"` 以在 git worktree 中运行 feature |
| `max_concurrent_runs` | 通过仪表盘服务器的最大并发运行数 |
| `commit` | 提交策略：`"per-round"`（默认）、`"on-done"` 或 `"never"` |
| `profiles` | 命名的流水线 profile（角色子集）；详见 [Profiles](#profiles) |
| `parallel_review_test` | 在 reviewing 阶段并行运行审查者和测试者（默认 `false`） |
| `auto_discover_features` | 从深度审查报告中的 `[NEW-FEATURE]` 标记自动创建 feature（默认 `false`）；详见[自动发现 Feature](#auto-discover-features) |
| `workspace` | 多仓库工作区配置（仓库名 → 路径映射） |
| `hooks` | 生命周期钩子（按钩子点键入，如 post-run） |
| `health_check` | 全局测试前环境检查命令（可在 test-strategy.yaml 中按 feature 覆盖） |
| `test_profiles` | 自定义或覆盖的测试 profile 定义（按 profile 名称键入） |
| `max_discovered_features` | 每次运行自动创建的最大 feature 数；未设置或 `<= 0` 使用默认值（`3`） |

### 自动发现 Feature

当 `auto_discover_features` 为 `true` 时，运行循环解析最终深度审查报告（`deep-review-report.md`）在其 **通过** 后，将每个 `[NEW-FEATURE]` 标记转为新的 feature YAML——捕获深度审查者发现的范围外问题，避免它们被埋没。

- **触发时机**：仅在最终深度审查通过时触发（首次 PASS 或自修复后的 PASS）。中间轮次、审查者/测试者失败、深度审查 FAIL/needs-attention 路径不会触发。
- **去重**：每个候选项与所有现有 feature 的名称+描述进行 token 重叠相似度比较，也与同批次中已保留的候选项比较。相似的被跳过。
- **设上限**：每次运行最多创建 `max_discovered_features`（默认 `3`）个 feature；其余记录为已封顶。
- **输出**：在 `.4x/<feature-id>/` 下写入 `discovered-features.md` 摘要，列出已创建/跳过为重复/已封顶的候选项，每个创建的 feature 追加一个 `feature-discovered` 事件。

以上全部在 CLI 层完成（纯文本解析 + 文件写入，无 LLM 调用），绝不阻塞向 `accepting` 的转换——任何错误仅做尽力日志记录。

### Profiles

Profile 选择某个 feature 运行哪些 phase，使简单的 feature 可以跳过完整流水线。不在列表中的 phase 被直接跳过——状态沿合法边推进但不调用 runner、不检查产物、不运行护栏。`coding` 是唯一必需的 phase；缺少它的 profile 是配置错误。可选的 `design-reviewing` phase 仅在列入时运行，且其 `design-review-report.md` 必须 PASS 后才能进入 coding。

```json
"profiles": {
  "full": {
    "phases": [
      { "phase": "designing" },
      { "phase": "design-reviewing" },
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "deep-reviewing" },
      { "phase": "accepting" }
    ]
  },
  "normal": {
    "phases": [
      { "phase": "coding" },
      { "phase": "reviewing" },
      { "phase": "testing" },
      { "phase": "accepting" }
    ]
  },
  "quick": {
    "phases": [
      { "phase": "coding", "model": "opus" },
      { "phase": "reviewing" }
    ]
  }
}
```

每个 phase 条目支持可选的 `runner` 和 `model` 覆盖：

| 字段 | 说明 |
|---|---|
| `phase` | Phase 名称（必须是可选用的 phase：designing、design-reviewing、coding、reviewing、testing、deep-reviewing、accepting） |
| `runner` | 此 phase 的可选 runner 覆盖 |
| `model` | 此 phase 的可选模型 tier 覆盖 |

**选择优先级：**

1. `4x run --profile <name>` — 显式覆盖（在 `profiles` 中查找，然后是内置默认值）。
2. 否则，如果存在 `profiles` 区块，按 feature 的 `priority` 自动选择：`null`/`0`/`1` → `full`、`2` → `normal`、`≥3` → `quick`。
3. 如果不存在 `profiles` 区块，每个 feature 都运行 `full`（基于优先级的自动选择被禁用——向后兼容）。

三个内置 profile（`full`/`normal`/`quick`）始终可用作回退，即使没有 `profiles` 区块。活跃 profile 名称记录在 feature 状态中并显示在仪表盘卡片上。

当 `parallel_review_test` 为 `true` 且活跃 profile 启用了 `reviewer` 和 `tester` 时，两个只读角色在 reviewing 阶段并行运行在同一 worktree 中；两者都通过则推进到深度审查，否则循环重新进入编码。

## 用户配置（`~/.4x/settings.json`）

全局用户偏好和 runner 默认值。通过 `4x config` 或仪表盘的**全局设置**编辑器（侧边栏中的 ⚙G 按钮）管理跨项目设置。

```json
{
  "locale": "zh-TW",
  "theme": "dark",
  "default_runner": "claude",
  "runners": {
    "claude": {
      "command": "/usr/local/bin/claude",
      "args": ["--dangerously-skip-permissions", "-p", "{prompt}"],
      "tty": true
    }
  },
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" }
  }
}
```

### 用户配置字段

| 字段 | 说明 |
|---|---|
| `locale` | 角色 prompt 指令的语言 |
| `theme` | 仪表盘主题（`dark`/`light`） |
| `default_runner` | 默认 runner 名称（被项目配置覆盖） |
| `runners` | Runner 定义（command、args、tty 等） |
| `roles` | 角色模型默认值 |
| `logLevel` | 最低日志级别（debug/info/warn/error；默认 "info"；被 FOURX_LOG_LEVEL 环境变量覆盖） |
| `logRetainDays` | ~/.4x/logs/ 中日志文件的保留天数（默认 7） |

### CLI

```bash
4x config set locale zh-TW
4x config set theme dark
4x config set default_runner claude
4x config set runner.claude.command /usr/local/bin/claude
4x config set runner.claude.tty true
4x config set role.designer.model opus
4x config get runner.claude.command
4x config list
```

`args` 是数组字段——直接编辑 `~/.4x/settings.json` 来设置。

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

## 设置合并

当 `4x run` 或 `4x prompt` 执行时，用户级和项目级设置会深度合并：

- **优先级：** 项目 > 用户 > 默认值
- **Runner 合并：** 按字段——项目中非零字段覆盖用户的。`args` 整体替换（不追加）。`tiers` 按键级合并。
- **角色合并：** 按字段——与 runner 相同。
- **仅项目字段**：除 `default_runner`、`runners` 和 `roles` 外的所有字段都是仅项目级的，不会被用户配置覆盖。

仪表盘的项目设置编辑器显示**原始**项目配置，而非合并结果。使用项目设置中的 **Merged** 标签页查看合并后的最终有效配置。
