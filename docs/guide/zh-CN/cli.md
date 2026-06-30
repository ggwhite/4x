# CLI 参考

所有 feature-id 参数支持大小写不敏感的前缀匹配。`4x run f001`、`4x run F001-user` 和 `4x run F001` 都会解析到 `F001-user-authentication-w`。歧义前缀会产生错误并列出匹配项。

---

## `4x init`

在当前目录初始化 `.4x/` 工作区。

```
4x init
```

- 自动检测项目语言和构建/测试/lint 命令
- 创建包含 6 个默认 runner（claude、codex、gemini、agy、copilot、cursor）的 `~/.4x/settings.json`
- 将嵌入的插件文件部署到 `.4x/plugins/`
- 在根目录文件中添加 `@import` 行（CLAUDE.md、AGENTS.md、GEMINI.md、AGY.md、.cursorrules）
- 如果 `.4x/` 已存在则报错

### `4x init --dump-templates`

将内置的角色 prompt 模板导出到 `.4x/templates/`，以便项目覆盖它们。

```
4x init --dump-templates          # 将内置模板写入 .4x/templates/
4x init --dump-templates --force  # 覆盖已存在的模板文件
```

- 要求 `.4x/` 已存在（先运行 `4x init`）
- 将所有内嵌的 `*.md.tmpl`（包括 `locale.tmpl`）写入 `.4x/templates/`
- 除非指定 `--force`，已有文件会附带警告跳过
- 生成 prompt 时，`.4x/templates/{file}` 优先于内嵌模板（整文件覆盖）；`locale.tmpl` 和各角色模板相互独立地回退

---

## `4x new <title>`

创建新 feature 并附带可选元数据。

```
4x new "Feature title" [flags]
```

| 标志 | 说明 |
|---|---|
| `--id` | 自定义 feature ID slug（跳过自动截断） |
| `--desc` | Feature 描述（默认为标题） |
| `--subtask` | 子任务，格式为 `"id:name"` 或 `"id:name:description"`（可重复） |
| `--rule` | 规则引用（可重复） |
| `--depends` | 依赖的 feature ID（可重复） |
| `--priority` | 优先级（0=紧急、1=高、2=中、3=低） |
| `--repo` | 范围内的仓库（可重复） |
| `--json` | 以 JSON 格式输出 |

创建状态为 `not-started` 的 `.4x/features/F{NNN}-{slug}.yaml`。自动生成的 slug 会在词边界截断；用 `--id` 可覆盖。创建逻辑走统一的 `feature.Create` 路径（参见[核心概念](concepts.md#feature-creation)）—— 仪表盘的 `POST /api/new` 使用相同逻辑，因此这里的标志与仪表盘新建表单一一对应。

示例：
```bash
4x new "Dashboard SPA file split"
4x new "Global settings" --id global-settings --desc "Add ~/.4x/settings.json"
4x new "Auth refactor" --subtask "extract-mw:Extract middleware" --subtask "add-tests:Add tests"
```

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
| `--profile` | auto | 流水线 profile（`full`/`normal`/`quick` 或自定义）；覆盖基于优先级的自动选择 |

`--profile` 选择运行哪些角色。内置 profile：`full`（全部 6 个角色）、`normal`（coder/reviewer/tester/acceptor）、`quick`（coder/reviewer）。不在 profile 中的角色会被跳过（状态沿合法边推进，不调用 runner）。省略时，若 `settings.json` 中存在 `profiles` 配置区块，则按 feature 优先级自动选择（否则为 `full`）。详见[配置 → Profiles](configuration.md#profiles)。

循环驱动：init → designing → design-reviewing → coding → reviewing → testing → deep-reviewing → accepting → pending-review。审查失败时，编码者再做一轮。测试失败时，循环重新进入编码阶段。

每个非设计者角色完成后，会自动执行护栏检查（范围、基线、必需文件）。违规时 feature 转为 `needs-attention` 并停止循环。设计者因不修改源代码而豁免。

审查裁定必须以 `PASS` 开头才算通过。`## Verdict` 标题与裁定文本之间的空行会被忽略。歧义输出（`TODO`、`ERROR`、乱码、缺少 `## Verdict` 区块）视为失败。

`settings.json` 或 feature YAML 中声明的阶段钩子会在循环内每次阶段转换前后自动执行。详见[阶段钩子](concepts.md#phase-hooks)。

进入 `testing` 阶段时（`pre_testing` 钩子之后、Tester runner 启动之前），如果配置了 `health_check`，会自动执行环境健康检查。检查命令按顺序运行；失败时恢复命令运行一次，然后重试检查。如果环境仍然异常，feature 转为 `needs-attention` 并停止循环。详见[健康检查](concepts.md#health-check)。

当 `settings.json` 中启用了 `auto_discover_features` 时，深度审查最终 **PASS** 时会解析 `deep-review-report.md` 中的 `[NEW-FEATURE]` 标记，自动为深度审查者标记的范围外问题创建 feature YAML（去重并设上限）。详见[配置 → 自动发现 Feature](configuration.md#auto-discover-features) 和[核心概念 → 自动发现的 Feature](concepts.md#auto-discovered-features)。

如果 feature 处于 `blocked` 或 `needs-attention` 阶段，会根据当前角色自动恢复到适当的恢复阶段。

自动检查依赖关卡 — 如果被依赖的 feature 未完成则阻塞。

如果配置中设置了 `isolation: "worktree"`，则在 `.worktrees/4x/<feature-id>/` 下的 git worktree 中运行。在多仓库模式下（配置了 workspace.repos），每个仓库在 `.worktrees/4x/<feature-id>/<repo-name>/` 下获得自己的 worktree，工作区级文件（go.work、Makefile 等）会被复制到旁边。编码者 prompt 包含 `== Workspace Repos ==` 区块；worktree 模式下每个条目显示仓库名作为相对路径（如 `core → core/`），以便编码者在正确的目录边界内操作。

---

## `4x status [feature-id]`

显示 feature 状态。

```
4x status              # 所有 feature，按状态分组
4x status <feature-id> # 单个 feature 详情及子任务
4x status --pending    # 隐藏 done/abandoned 的 feature
4x status --json       # 以 JSON 格式输出
```

| 标志 | 说明 |
|---|---|
| `--pending` | 隐藏 done/abandoned 的 feature |
| `--json` | 以 JSON 格式输出 |

分组：Running、Review、Pending、Todo、Done（done 最多显示 5 个）。包含 backlog 偏差警告。

查看单个 feature 详情（`4x status <feature-id>`）时，如果存在截图，还会打印：

`Screenshots: <total> (round 1: <n>, round 2: <n>, ...)`

---

## `4x subtask <feature-id> <subtask-id>`

更新 feature 内某个子任务的状态。

```
4x subtask <feature-id> <subtask-id> --status <status>
```

| 标志 | 说明 |
|---|---|
| `--status` | 新状态：`done`、`in-progress`、`blocked`、`not-started`、`ready-for-review`（必填） |

示例：
```
4x subtask F043-dashboard-screenshot-gall protocol-screenshot-type --status done
```

---

## `4x approve <feature-id>`

批准由富集自动发现产生的 `draft` feature，将其从 `draft → not-started`，以便 meta-loop 拾取。Draft 仅在启用 `enrich_discovered_features` 且 `enrich_auto_approve` 为 `false` 时创建。若 feature 不在 `draft` 状态则报错。

```
4x approve F042-some-discovered-feature
```

---

## `4x reject <feature-id>`

拒绝由富集自动发现产生的 `draft` feature，将其从 `draft → abandoned`，使其不进入 meta-loop。若 feature 不在 `draft` 状态则报错。

```
4x reject F042-some-discovered-feature
```

---

## `4x retry <feature-id>`

恢复卡在 `needs-attention` 或 `blocked` 的 feature，将其转换回工作阶段，然后立即启动 `4x run`。相当于 `4x transition --to <phase> <id> && 4x run <id>`。

默认目标阶段为 `accepting`（人工修复问题后重跑 Acceptor）。使用 `--to` 指定其他目标阶段。

```
4x retry F042-some-feature
4x retry F042-some-feature --to amending
```

| 标志 | 说明 |
|------|-------------|
| `--to <phase>` | 要恢复到的目标阶段（默认：`accepting`） |

若 feature 当前不在 `needs-attention` 或 `blocked` 状态则报错。

---

## `4x gate`

对挖掘出的候选 feature 应用 F097 evolve **value gate** 否决层。纯 CLI 确定性否决——不调用 LLM。`gate` LLM 角色在两阶段之间执行（由 evolve driver 编排），产出 `gate-verdicts.json`。

必须指定 `--pre` 或 `--post` 其中之一：

- `--pre` — PRE-否决：读取 `.4x/candidates.json`，丢弃与既有 feature 或批次内重复的 Jaccard 相似候选，将存活者写入 `.4x/gate-input.json`。
- `--post` — POST-否决：读取 `.4x/gate-input.json` + `.4x/gate-verdicts.json`，应用不可覆盖的硬否决（non-accept / 缺少 `why_not_hack` / 低于 `value_floor` / 与既有重复 / 超过 `max_accept_per_run` / 超过 `max_backlog_undone`），将通过的候选（含 `value_score`/`why_not_hack`）写入 `.4x/accepted-candidates.json`。

阈值来自 `settings.json` 的 `evolution` 区段（`value_floor`、`max_accept_per_run`、`max_backlog_undone`、`dedup_threshold`）。

```
4x gate --pre
4x gate --post
```

---

## `4x evolve`

执行一轮持续自我改进 pipeline，将既有的进化零件串成可重复执行的闭回路：

**mine → gate (pre → gate LLM 角色 → post) → enrich → enqueue → (可选) auto-run meta-loop → learnings 反馈下一轮。**

CLI 层绝不直接调用 LLM——gate 角色与 enrichment 都以 `runner` 子进程执行。每次调用恰好执行**一轮**；通过外部驱动（cron 或重复调用 `4x evolve`）进行多轮。每轮结果写入 `.4x/evolve-report.md`。

Pipeline 步骤：

1. **mine** — 扫描 `.4x/` 寻找失败信号（escalation / 卡住的 feature / 反复 FAIL 模式），去重后合并至 `.4x/candidates.json`。
2. **gate pre** — Jaccard 去重存活者至 `.4x/gate-input.json`。
3. **gate role** — 启动 `gate` LLM 角色写入 `.4x/gate-verdicts.json`。
4. **gate post** — 应用不可覆盖的否决 + 收敛上限，写入 `.4x/accepted-candidates.json`。
5. **enrich + enqueue** — 将每个通过的候选具现化为 `not-started` feature YAML（enrichment 失败时降级为从候选文字创建的基本 feature，标记 `enriched=false`）。
6. **auto-run**（可选）— 为每个排入的 feature 执行 meta-loop，受 F098 self-mod scope guard 保护。

反空转：当某轮未接受任何候选时，`.4x/evolve-state.json` 递增 `consecutiveNoAccept`；达到 `evolution.max_idle_rounds`（默认 3；设 `<= 0` 禁用）后，下次调用提早中止并标记报告为 `Halted`，以 exit 0 结束。使用 `--force` 可覆盖。

```
4x evolve                        # 执行一轮，feature 维持 not-started
4x evolve --dry-run              # 只读：打印 mine/dedupe 摘要，不写入任何文件
4x evolve --auto-run             # 同时为排入的 feature 执行 meta-loop
4x evolve --force                # 绕过反空转中止
```

| Flag | 说明 |
|---|---|
| `--auto-run` | 为每个排入的 feature 执行 meta-loop（F098 self-mod guard 始终强制） |
| `--dry-run` | 只读分析：打印 mined/deduped 数量，不写入文件、不启动 runner、不创建 feature |
| `--min-occurrences` | 失败模式成为候选的 distinct-feature 阈值（默认 3） |
| `--force` | 覆盖反空转中止，即使连续空转轮也执行 |
| `--runner` | gate / enrich / auto-run 使用的 runner plugin（默认 `evolution.gate_runner` 或项目默认） |
| `--timeout` | LLM 子进程超时秒数（默认 3600） |
| `--max-rounds` | `--auto-run` 时每个 feature 的最大轮数（默认 5） |

Dashboard 通过 `GET /api/evolve-report` 呈现最新报告。

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

## `4x doctor`

对合并后的配置（`.4x/settings.json` + `~/.4x/settings.json`）和工作区完整性执行一次性只读健康检查，在开始运行之前使用。不调用 LLM，不要求安装任何 runner。

```
4x doctor [--json]
```

| 标志 | 说明 |
|---|---|
| `--json` | 以 JSON 格式输出完整报告（供 CI 使用） |

检查按区块分组：

- **settings** — `settings.json` 可加载、`project.name` 非空、至少定义了一个 runner、`default_runner` 存在于 runners 映射中。
- **runners** — 每个 runner 的 `command` 在 `PATH` 上可找到（找不到 → WARN 而非 FAIL，因为 runner 可能在远程机器上）。
- **roles** — 解析每个角色（designer/coder/reviewer/tester/acceptor）通过默认 runner 实际使用的模型，以及 reviewer 的 `deep_model`。
- **workspace** — 孤立 worktree（feature 已 done/abandoned 但 `.worktrees/4x/<id>` 还在）、悬挂 worktree（目录存在但没有匹配的 feature）、过期状态（`active=true` 但进程已不在）、格式异常的 feature YAML。

每行前缀为 `✅`（PASS）、`⚠️`（WARN）或 `❌`（FAIL），最后附汇总计数。

退出码：没有 FAIL 时为 `0`（WARN 不影响退出码），有任何检查失败时为 `1`。`doctor` 严格只读——不会改写 `state.json`、清理 worktree 或修改配置。

```bash
# CI 关卡：有 FAIL 则构建失败
4x doctor --json | jq -e '[.checks[] | select(.severity == "FAIL")] | length == 0'
```

---

## `4x verify <feature-id>`

运行 feature 的 `test-strategy.yaml` 中的验证命令，并将结果写入 `rounds/round-{N}/verify.json`。

命令可以通过 `verify_groups` 组织为分组：组间并行运行，组内命令顺序执行。组内某个命令失败时，该组剩余命令被跳过，但其他组继续运行。仅定义 `verify_commands` 时，退化为单个顺序执行的 `default` 组。同时声明两者会报错。

并行执行完全由 CLI 处理——不涉及 LLM。Tester 角色调用此命令而非自己运行验证命令；人类也可以单独运行它来调试。

```
4x verify <feature-id> [--round N] [--timeout 5m] [--json]
```

| 标志 | 说明 |
|---|---|
| `--round` | 轮次编号（默认：state.json 中的当前轮次） |
| `--timeout` | 所有分组的总超时时间（默认：5m） |
| `--json` | 以 JSON 格式输出完整 verify.json |

所有未跳过的命令都通过时退出码为 0，有任何命令失败时为 1。

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

验证转换是否符合状态机的合法规则。如果状态不存在则自动初始化。`testing → accepting` 转换会运行额外的关卡检查（verify.json、test-report.md、final-report.md 必须存在且验证必须通过）。

如果 `settings.json` 或 feature YAML 声明了 `hooks`，`pre_{phase}` 钩子在转换前运行，`post_{phase}` 钩子在转换后运行。`block` 类型的 pre-hook 失败会中止转换；`block` 类型的 post-hook 失败会将 feature 移至 `needs-attention`。详见[阶段钩子](concepts.md#phase-hooks)。

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

支持语言注入（来自用户配置或 `LANG` 环境变量）、规划文档自动包含以及项目/角色 include。spec/plan 文档通过共享解析器（`protocol.ResolveDesignDoc`）定位——优先读取 feature YAML 的 `spec`/`plan` 字段，然后是 `docs/design/{id}-{type}.md`，再是去掉 `FNNN-` 前缀的 `docs/design/{slug}-{type}.md` 回退——因此 prompt 看到的文档与仪表盘 overview 一致。详见[设计文档解析](concepts.md#design-doc-resolution)。

对于 `tester` 角色，feature 的 `test-strategy.yaml` 中列出的 `profiles` 会被解析（通过 `loadProfiles`）并注入到 prompt 中，以 `== Test Profile: {name} ==` 区块呈现。每个 profile 的内容来自 `settings.json` 的 `test_profiles[name]`（`content` 或 `include`），否则来自内置的 `templates/profiles/{name}.md`。详见[测试 Profile](concepts.md#test-profiles)。

---

## `4x done <feature-id>`

将处于 pending-review 的 feature 标记为完成。如果 feature 有 worktree（`.worktrees/4x/<id>`），会自动将分支合并回 main 并移除 worktree 和分支。

```
4x done <feature-id>
```

仅在 feature 处于 `pending-review` 阶段时有效。其他阶段会报错。

如果发生合并冲突或合并错误，feature 保持 `pending-review` 状态，worktree 被保留，并打印指导信息。多仓库模式下，冲突的仓库名会以 `repo: <name>` 形式打印。可使用 `4x merge <id>` 在解决冲突后完成合并。

---

## `4x force-done <feature-id>`

<!-- alias: 4x forcedone -->

从任意非终态阶段强制将 feature 标记为完成。需要提供 `--reason` 说明为何跳过正常流水线。

```
4x force-done <feature-id> --reason "code reviewed and tests pass, e2e test deferred to post-merge"
```

将 feature 转换到 `pending-review`，记录带有原因的 `force-done` 事件，然后触发与 `4x done` 相同的合并流程。可从 `needs-attention`、`blocked` 或任何活跃阶段使用。

Dashboard 通过 `POST /api/force-done` 提供此功能，请求体为 `{id, reason}`。

| 标志 | 说明 |
|---|---|
| `--reason` | 强制完成的原因（必填） |
| `--json` | 以 JSON 格式输出结果 |

---

## `4x merge <feature-id>`

在解决 `4x done` 产生的冲突后完成合并。

```
4x merge <feature-id>
```

仅在 feature 处于 `pending-review` 或 `done` 阶段且 `.worktrees/4x/<id>` 存在 worktree 时可用。在 worktree 中提交已解决的冲突，合并到 main，然后移除 worktree 和分支。如果 feature 仍在 `pending-review`，合并成功后会标记为 `done`。

多仓库模式下，已解决的冲突按仓库分别提交（`.worktrees/4x/<id>/<repo-name>/` 下的每个仓库独立暂存和提交），然后所有仓库全有或全无地合并。如果冲突再次出现，冲突仓库名会以 `repo: <name>` 形式显示。

---

## `4x clean [feature-id]`

删除已完成 feature 的工作区产物（`logs/`、`rounds/`、报告、`state.json`、`events.jsonl`），释放磁盘空间。Feature 定义文件（`.4x/features/*.yaml`）和 feature 状态始终保留。

```
4x clean              # 列出可清理的 feature 及大小，确认后清理
4x clean --dry-run    # 仅列出，不删除
4x clean --force      # 跳过确认提示
4x clean <feature-id> # 清理单个 feature（仍须为 done/abandoned 状态）
```

只有状态为 `done` 或 `abandoned` 且工作区目录存在的 feature 才有资格。活跃（运行中）的 feature 不会被清理，`blocked`/`needs-attention` 的 feature 也会保留以便调试。清理不是状态机转换——不会改变 feature 生命周期。

---

## `4x learn`

管理回顾学习——在 `.4x/learnings.json` 中跨 feature 积累的开发经验。

每个 feature 的 Acceptor 会写入 `retro-learnings.json`；CLI 将其收集到 `.4x/learnings.json`。下一个 feature 的 Designer 会从中选取相关条目写入 `selected-learnings.json`，CLI 按类别过滤后注入每个角色的 prompt。Learnings 完全由 CLI 管理——runner 不会直接写 `learnings.json`，任何 learnings 操作失败只发出警告，不阻塞状态转换。

```
4x learn add --category <cat> --content <text>  # 手动新增 learning（standalone session 用）
4x learn add --category ops --content "..." --json  # JSON 输出：{"id":"L0xx","added":true}
4x learn list                     # 列出所有 learnings（id/category/status/used/content）
4x learn list --category=testing  # 按类别过滤
4x learn prune                    # 标记陈旧（>90 天未使用）条目并删除
4x learn prune --dry-run          # 预览陈旧条目，不删除
4x learn promote <id>             # 将某条 learning 标记为 promoted（保留但不再注入）
4x learn remove <id>              # 删除某条 learning
```

`learn add` 会检查是否有相似的既有条目（完全比对、正规化比对、Jaccard 相似度）。若发现模糊重复，会回报既有 ID 且不写入。

- 类别：`design`、`code-quality`、`testing`、`review`、`tooling`、`process`、`ops`
- 状态：`active`（可注入）、`stale`（>90 天未使用，读取时自动标记）、`promoted`（已升级为模板/指令）
- 100 条活跃条目的软上限会触发建议运行 `4x learn prune` 的警告——不会自动删除条目

---

## `4x mine`

扫描整个 `.4x/` 历史记录中的失败信号，将其聚合为 `.4x/candidates.json` 中的候选池。与自动发现（仅在单次运行的深度审查 PASS 时触发，解析 `[NEW-FEATURE]` 标记）不同，miner 扫描**所有** feature 中最密集的失败数据：escalation、卡住的 feature 以及反复出现的审查失败。

Miner 是纯 CLI/protocol 层扫描——从不调用 LLM，也不创建 feature。它只产生候选；候选是否被提升为真正的 feature，由后续的 F097 gate 决定。

```
4x mine                          # 扫描并写入 .4x/candidates.json
4x mine --dry-run                # 打印摘要，不写入
4x mine --min-occurrences 5      # 提高失败模式阈值（默认 3）
4x mine --output path.json       # 写入自定义路径
```

| 标志 | 默认值 | 说明 |
|---|---|---|
| `--min-occurrences` | `3` | 反复出现的审查问题成为候选所需的 distinct-feature 数量 |
| `--output` | `.4x/candidates.json` | 候选池输出路径 |
| `--dry-run` | `false` | 只打印摘要，不写入任何文件 |

三个扫描器向候选池输送数据，每个候选都带有 `source` 标记以追溯来源：

- **escalation** — 读取每轮的 `escalation.json`（`spec-mismatch` / `criteria-wrong` / `blocker` / `scope-change`）
- **stuck** — 处于 `needs-attention` / `abandoned` / `blocked` 的 feature，阻塞原因从 `state.json` 或最新轮的 escalation `detail` 提取
- **fail-pattern** — 跨 `>= --min-occurrences` 个不同 feature 反复出现的审查/深度审查 FAIL 问题（按 Jaccard 相似度聚类）；每个集群还生成一条建议将问题提升为审查清单的候选 learning

扫描是尽力而为的：单个损坏的 feature 只记录警告，绝不中止其余扫描。候选项会针对现有 feature YAML、前一次 `candidates.json` 以及当前批次内部进行去重。

---

## `4x config`

管理用户级配置（`~/.4x/settings.json`）。

```
4x config list          # 显示所有用户配置
4x config get <key>     # 获取某个值
4x config set <key> <value>  # 设置某个值
```

键使用点分路径。支持的形式：

| 键 | 示例 | 说明 |
|---|---|---|
| `locale` | `4x config set locale zh-TW` | UI / prompt 语言 |
| `theme` | `4x config set theme dark` | 仪表盘主题 |
| `default_runner` | `4x config set default_runner claude` | 默认 runner 插件 |
| `runner.<name>.<field>` | `4x config set runner.claude.model opus` | 每个 runner 的 `command`/`model`/`tty`/`stdin`/`quiet` |
| `role.<name>.<field>` | `4x config get role.deep-reviewer.model` | 每个角色的 `model`/`deep_model`/`parallel_reviewers`/`angles_per_reviewer` |

`role.deep-reviewer.parallel_reviewers` 控制深度审查扇出的并行子审查者数量（`1` = 单 agent 回退）；`role.deep-reviewer.angles_per_reviewer` 固定每组的审查角度数（不设则自动均分）。详见[核心概念 → 并行深度审查](concepts.md)。

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
4x batch next [--json]
```

| 标志 | 默认值 | 说明 |
|---|---|---|
| `--json` | `false` | 以 JSON 格式输出，包含子任务前沿 |

不带 `--json` 时，以纯文本输出 feature ID（向后兼容）。带 `--json` 时，输出 JSON 对象并包含 `subtaskFrontier`——所有依赖已完成的子任务。无可执行 feature 时在 JSON 模式下返回 `null`。

### `4x batch run`

按依赖顺序依次运行可执行的 feature。

```
4x batch run [--runner <name>] [--max-rounds <n>] [--timeout <seconds>] [--no-auto-merge]
```

| 标志 | 默认值 | 说明 |
|---|---|---|
| `--runner` | 配置默认值 | Runner 插件名称 |
| `--max-rounds` | `5` | 每个 feature 的最大轮次 |
| `--timeout` | `3600` | 每阶段超时时间（秒） |
| `--no-auto-merge` | `false` | 每个完成的 feature 停留在 `pending-review` 而非自动合并回 main |

在 feature 之间检查 `.4x/batch-stop` 文件以实现优雅关闭。

运行结束时——无论是正常完成、被停止、被中断（`SIGTERM`/`SIGINT`）还是崩溃——都会写入 `.4x/batch-report.json`，汇总运行结果（`outcome`、已完成/失败/剩余数量、runner、持续时间，以及每个 feature 的最终状态）。详见 [Batch 模式 → 运行报告](batch.md#run-report)。

默认情况下，feature 完成后（到达 `pending-review`），批量运行会自动将其 worktree 分支合并回 main，以便下一个 feature 从更新后的 main 分支出发——实现无人值守的持续批量运行。合并冲突时批量运行会优雅暂停，feature 保持 `pending-review` 状态、worktree 保留，并写入 `.4x/batch-conflict.json` 信号文件（包含 feature、冲突仓库、文件列表），供[仪表盘](dashboard.md)显示冲突；解决冲突后运行 `4x merge <id>`，然后重新运行 `4x batch run` 继续。冲突信号在每次运行开始时清除。非冲突类合并错误打印警告后继续处理下一个 feature。传入 `--no-auto-merge` 可恢复旧行为（feature 停在 `pending-review` 等待人工审查）。

如果配置中设置了 `isolation: "worktree"`，每个 feature 在自己的隔离 worktree 中运行。多仓库模式下，每个 feature 获得一个复合 worktree（`.worktrees/4x/<feature-id>/`），包含每个仓库的子目录，提交按轮次进行（不延迟到完成时）。Hub 仓库（来自 `hub_repos` 配置或 `workspace.repos[*].hub: true`）被排除在共享仓库集群之外，以允许并行执行。

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

---

## `4x mcp`

启动 Model Context Protocol (MCP) 服务器。

```
4x mcp
```

启动 4x MCP stdio 服务器，将 4x CLI 命令暴露为 MCP 工具，供 LLM 客户端（如 Claude Code、Cursor）使用。
