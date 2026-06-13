# CLI 参考

所有 feature-id 参数支持大小写不敏感的前缀匹配。`4x run f001`、`4x run F001-user` 和 `4x run F001` 都会解析到 `F001-user-authentication-w`。歧义前缀会产生错误并列出匹配项。

---

## `4x init`

在当前目录初始化 `.4x/` 工作区。

```
4x init
```

- 自动检测项目语言和构建/测试/lint 命令
- 创建包含 4 个默认 runner（claude、codex、gemini、agy）的 `.4x/settings.json`
- 将嵌入的插件文件部署到 `.4x/plugins/`
- 在根目录文件中添加 `@import` 行（CLAUDE.md、AGENTS.md、GEMINI.md、AGY.md）
- 如果 `.4x/` 已存在则报错

---

## `4x new <title>`

创建新 feature。

```
4x new "Feature title" [--repo <repo>...] [--json]
```

| 标志 | 说明 |
|---|---|
| `--repo` | 范围内的仓库（可重复使用，用于多仓库 feature） |
| `--json` | 以 JSON 格式输出 |

创建状态为 `not-started` 的 `.4x/features/F{NNN}-{slug}.yaml`。

---

## `4x run <feature-id>`

为某个 feature 运行 设计-编码-审查-测试 循环。

```
4x run <feature-id> [flags]
```

| 标志 | 默认值 | 说明 |
|---|---|---|
| `--runner` | 配置默认值 | Runner 插件名称 |
| `--max-rounds` | `5` | 最大循环迭代次数 |
| `--timeout` | `3600` | 每阶段超时时间（秒） |
| `--dry-run` | `false` | 打印角色 prompt 但不调用 LLM |
| `--json` | `false` | 启动运行并立即以 JSON 格式返回 |

循环驱动：init → designing → coding → reviewing → testing → accepting → pending-review。审查失败时，编码者再做一轮。测试失败时，循环重新进入编码阶段。

如果 feature 处于 `blocked` 或 `needs-attention` 阶段，会根据当前角色自动恢复到适当的恢复阶段。

自动检查依赖关卡 — 如果被依赖的 feature 未完成则阻塞。

如果配置中设置了 `isolation: "worktree"`，则在 `.worktrees/4x/<feature-id>/` 下的 git worktree 中运行。

---

## `4x status [feature-id]`

显示 feature 状态。

```
4x status              # all features, grouped by state
4x status <feature-id> # single feature details with subtasks
4x status --pending    # filter pending-review features
4x status --json       # output as JSON
```

| 标志 | 说明 |
|---|---|
| `--pending` | 筛选待审核功能 |
| `--json` | 以 JSON 格式输出 |

分组：Running、Review、Pending、Todo、Done（done 最多显示 5 个）。包含 backlog 偏差警告。

---

## `4x check <feature-id>`

运行护栏检查但不转换状态。

```
4x check <feature-id> [--json]
```

| 标志 | 说明 |
|---|---|
| `--json` | 以 JSON 格式输出结果 |

检查项：必需文件、基线、范围、依赖、backlog 偏差。通过返回退出码 0，失败返回 1。

---

## `4x transition <feature-id>`

强制状态转换。

```
4x transition <feature-id> --to <phase> [--role <role>] [--json]
```

| 标志 | 说明 |
|---|---|
| `--to` | 目标阶段（必填） |
| `--role` | 执行转换的角色 |
| `--json` | 以 JSON 格式输出 |

验证转换是否符合状态机的合法规则。如果状态不存在则自动初始化。`testing → accepting` 转换会运行额外的关卡检查（verify.json、test-report.md、final-report.md、commit-plan.md 必须存在且验证必须通过）。

---

## `4x event <feature-id>`

向 `events.jsonl` 追加事件。

```
4x event <feature-id> --type <type> [--role <role>] [--round <n>] [--action <action>] [--detail <text>]
```

| 标志 | 说明 |
|---|---|
| `--type` | 事件类型（必填） |
| `--role` | 触发事件的角色 |
| `--round` | 轮次编号 |
| `--action` | 操作名称 |
| `--detail` | 附加详情文本 |

---

## `4x prompt <feature-id>`

打印 feature 的角色 prompt。

```
4x prompt <feature-id> [--role <role>] [--round <n>]
```

| 标志 | 说明 |
|---|---|
| `--role` | 目标角色（省略时从当前状态推断） |
| `--round` | 轮次编号 |

支持语言注入（来自用户配置或 `LANG` 环境变量）、规划文档自动包含（`docs/design/{id}-spec.md` 和 `{id}-plan.md`）以及项目/角色 include。

---

## `4x done <feature-id>`

将处于 pending-review 的 feature 标记为完成。

```
4x done <feature-id>
```

仅在 feature 处于 `pending-review` 阶段时有效。其他阶段会报错。

---

## `4x config`

管理用户级配置（`~/.4x/settings.json`）。

```
4x config list          # show all user config
4x config get <key>     # get a value
4x config set <key> <value>  # set a value
```

当前支持的键：`locale`。

---

## `4x sync`

重新部署嵌入的插件文件到现有项目。

```
4x sync [--dry-run]
```

| 标志 | 说明 |
|---|---|
| `--dry-run` | 报告差异但不写入文件 |

报告每个文件的状态：created、updated 或 current。

---

## `4x batch`

多个 feature 的批量操作。

### `4x batch plan`

生成依赖感知的执行计划。

```
4x batch plan [--dry-run] [--max-chain <n>]
```

| 标志 | 默认值 | 说明 |
|---|---|---|
| `--dry-run` | `false` | 打印调度计划但不写入文件 |
| `--max-chain` | `4` | 每个集群的最大链长度 |

写入 `.4x/batch-plan.json`。

### `4x batch next`

显示下一个可执行的 feature（基于计划和当前状态）。

```
4x batch next
```

### `4x batch run`

按依赖顺序依次运行可执行的 feature。

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>]
```

| 标志 | 默认值 | 说明 |
|---|---|---|
| `--runner` | 配置默认值 | Runner 插件名称 |
| `--max-rounds` | `5` | 每个 feature 的最大轮次 |
| `--timeout` | `3600` | 每阶段超时时间（秒） |

在 feature 之间检查 `.4x/batch-stop` 文件以实现优雅关闭。

### `4x batch stop`

通知正在运行的批处理在当前 feature 完成后停止。

```
4x batch stop
```

创建 `.4x/batch-stop` 信号文件。

---

## `4x live [path...]`

启动 4x Live 仪表盘服务器。

```
4x live [path...] [flags]
```

| 标志 | 短标志 | 默认值 | 说明 |
|---|---|---|---|
| `--port` | `-p` | `4567` | 服务器端口 |
| `--web` | `-w` | `false` | 在浏览器中打开 |
| `--app` | `-a` | `false` | 打开 macOS 原生应用 |

不提供路径时，从 `~/.4x/recent-projects.json` 加载最近项目（LRU，最多 20 个）。提供路径时，将每个路径作为项目标签页打开。
