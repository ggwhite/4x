# 核心概念

## 四个角色

| 角色 | 职责 | 输入 | 输出 | 不可操作 |
|---|---|---|---|---|
| **设计者 (Designer)** | 分析需求，产出规格，定义验收标准和测试策略 | Feature 描述、代码库 | `task-brief.md`、`acceptance-criteria.md`、`test-strategy.yaml` | 修改源代码 |
| **编码者 (Coder)** | 按照规格实现 | `task-brief.md`、先前的测试/审查报告 | 源代码、`coder-report.md` | 修改验收标准或测试脚本 |
| **审查者 (Reviewer)** | 发现 bug、安全问题、规格违规 | Diff、规格、编码报告、项目规则 | `review-report.md` | 修改源代码 |
| **测试者 (Tester)** | 基于验收标准用证据验证 | 验收标准、编码报告、测试策略 | 测试脚本、`test-report.md`、`verify.json`、`final-report.md` | 修改源代码 |

每个角色都是**隔离的** — 编码者在实现过程中看不到先前的审查反馈。测试者按照设计者（而非编码者）编写的标准进行验证。

### 额外循环角色

循环后期还有两个额外角色：

| 角色 | 阶段 | 职责 |
|---|---|---|
| **深度审查者 (Deep Reviewer)** | `deep-reviewing` | 对抗性审查——在完整 diff 中寻找最坏情况的 bug |
| **接受者 (Acceptor)** | `accepting` | 汇总仍未解决的问题，生成 `final-report.md` 供人工审查 |

接受者使用独立的模型配置（`roles.acceptor.model`）——与设计者不同。它读取最终轮的 review/test/deep-review 报告以及各轮 escalation，找出仍未解决的问题，而不再逐份重读每一轮的完整报告。

### 流水线 Profile

**流水线 profile** 选择某个 feature 运行哪些角色，使简单的工作可以跳过角色，而非始终运行完整的六角色流水线。内置 profile：

| Profile | 角色 |
|---|---|
| `full` | designer、coder、reviewer、tester、deep-reviewer、acceptor |
| `normal` | coder、reviewer、tester、acceptor |
| `quick` | coder、reviewer |

`coder` 始终是必需的。配置了 `profiles` 时，按 feature 优先级自动选择（最高优先级 → `full`，然后 `normal`，再 `quick`）；`--profile` 覆盖该选择。不在活跃 profile 中的角色被跳过——循环沿相同的合法状态边推进但不调用该 runner。详见[配置](configuration.md)中的 `profiles`、`parallel_review_test` 和 `coder_model` 设置。

### 审查：两个阶段

1. **检查清单审查**（标准模型）— 根据项目硬性规则检查：安全性、并发、错误处理、代码风格
2. **对抗性审查**（深度模型）— "这个 diff 中隐藏的最严重 bug 是什么？"按严重程度对发现进行评级。

### 深度审查自修复

当深度审查者发现阻塞性问题时，`deep-reviewing` 阶段会**就地修复**，而非将工作一路送回 `amending → reviewing → testing`。因为审查者和测试者在深度审查前已经通过，重新运行整条昂贵的链路（尤其是深度模型）是浪费的。

在同一阶段内，循环派生两个作用域受限的子角色，反复运行直到报告通过或达到上限：

| 子角色 | 模型 | 读取 | 写入 | 作用域 |
|---|---|---|---|---|
| **mini-coder** | coder 模型 | `deep-review-report.md` 仅 `## Issues` 部分（不读 `task-brief.md`） | 源代码、`coder-report.md` | 仅限深度审查者指出的问题 |
| **re-verifier** | reviewer 模型 | 先前的问题 + 本次迭代 mini-coder 的 diff | `deep-reverify-{n}.md`，更新 `deep-review-report.md` 的 `## Verdict` | 验证旧问题已修复且新 diff 未引入 bug |

整个过程中阶段保持 `deep-reviewing`——子角色不是状态机阶段。当 re-verifier 确认全部 PASS 后，循环推进到 `accepting`。循环最多运行 `roles.deep-reviewer.max_fix_rounds` 次迭代（默认 2）；如果 mini-coder 修改了 feature 范围外的文件，或达到上限仍未通过，feature 升级为 `needs-attention` 并保留 FAIL 报告。

### 并行深度审查

深度审查涵盖 11 个不同的审查角度（正确性、质量、规范、历史、反馈等）。当 `roles.deep-reviewer.parallel_reviewers` 大于 1 时，循环将这些角度分散到多个专注的子审查者中，而非让一个 agent 覆盖全部 11 个。这类似于 `/code-review` 按维度拆分审查的方式，降低每个 agent 的上下文压力和注意力漂移。

扇出完全由 4x CLI 驱动——不依赖 LLM 自身的子 agent 或工具能力。`deep-reviewing` 阶段保持为单一阶段：

| 子角色 | 模型 | 读取 | 写入 |
|---|---|---|---|
| **sub-reviewer**（×N） | deep 模型 | diff + 分配的角度子集 | `deep-review-partial-{i}.md` |
| **synthesizer** | deep 模型 | 每份部分报告的完整内容 | `deep-review-report.md` |

角度均匀分配且不重叠：默认 `parallel_reviewers: 3` 时分组为 `[1-4]`、`[5-8]`、`[9-11]`（正确性 / 质量+规范 / 历史+反馈）。设置 `roles.deep-reviewer.angles_per_reviewer` 可显式固定组大小；留空则自动 `ceil(11/N)` 均分。N 个子审查者并行运行，然后由一个 synthesizer 去重、仲裁冲突，将置信度评分统一为同样的 `deep-review-report.md` 格式——自修复循环和 `parseReviewVerdict` 照常消费，下游完全无变化。

当 `parallel_reviewers` 未设置或 `<= 1` 时，循环回退到原始的单 agent 流程：一个深度审查者渲染全部 11 个角度并直接写入 `deep-review-report.md`，不生成部分报告或 synthesizer。

### 自动发现的 Feature

深度审查者经常发现确实存在但**超出当前 feature 范围**的问题——潜在 bug、技术债、缺失功能。如果没有着落点，这些发现就会被埋在报告里。启用 `auto_discover_features` 后，运行循环会自动捕获它们。

深度审查者将每个范围外的候选项以 `[NEW-FEATURE] <title>` 区块的形式（附带简短描述）写入 `deep-review-report.md` 的 `## Discovered Issues` 部分。在**最终深度审查 PASS** 后（仅有的两条到达 `accepting` 的路径——首次 PASS 和自修复 re-verifier 翻转为 PASS），循环解析这些区块，完全在 CLI 层（无 LLM 调用）执行：

- **去重**：每个候选项与现有 feature 及已保留的候选项进行 Jaccard token 重叠相似度比较。相似的被跳过。
- **设上限**：每次运行最多创建 `max_discovered_features` 个 feature（默认 `3`）；其余记录为已封顶。
- **创建**：被保留的候选项创建为新的 feature YAML（状态 `not-started`，复用 `4x new` 的编号方式），每次创建追加一个 `feature-discovered` 事件。
- **汇总**：结果（已创建 / 跳过为重复 / 已封顶）写入 `.4x/{feature-id}/discovered-features.md`。

此步骤是尽力而为的：任何错误仅记录日志，绝不阻塞向 `accepting` 的转换。仅在最终深度审查 PASS 时运行——中间轮次和 FAIL/`needs-attention` 路径不会触发。详见[配置 → 自动发现 Feature](configuration.md#auto-discover-features)。

### 升级 (Escalation)

编码者或测试者可以在以下情况下升级：

| 原因 | 含义 | 路由到 |
|---|---|---|
| `spec-mismatch` | 数据库/API 与规格不匹配 | 设计者 |
| `criteria-wrong` | 验收标准不正确 | 设计者 |
| `blocker` | 缺少依赖或基础设施问题 | `needs-attention`（人工介入） |
| `scope-change` | 需要修改范围外的仓库 | 设计者 |

升级信息写入 `escalation.json`。循环自动将 `spec-mismatch`、`criteria-wrong` 和 `scope-change` 路由回设计者。`blocker` 升级转到 `needs-attention` 需要人工介入。

---

## 状态机

```
init → designing → coding → reviewing → testing → deep-reviewing → accepting → pending-review → done
                     ↑          ↓           ↓            ↓
                     ├── amending ←──────────┴────────────┘
                     ↑      ↓
                     └──────┘
```

### 所有合法转换

| 从 | 到 |
|---|---|
| `init` | `designing` |
| `designing` | `coding` |
| `coding` | `reviewing`、`designing` |
| `reviewing` | `testing`、`amending` |
| `amending` | `reviewing`、`designing` |
| `testing` | `deep-reviewing`、`amending`、`designing` |
| `deep-reviewing` | `accepting`、`amending` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`、`coding`、`testing` |
| `needs-attention` | `designing`、`coding`、`testing` |
| any | `blocked`、`needs-attention`、`done`、`abandoned` |

### 轮次计数器

- 当轮次为 0 时进入 `coding` 会将轮次设为 1
- 进入 `amending` 会递增轮次
- 当轮次 >= maxRounds 或连续 3 轮以上无进展时，`ShouldStop` 触发

### 循环中的阶段决策

| 阶段 | 条件 | 操作 |
|---|---|---|
| `designing` | `task-brief.md` 或 `acceptance-criteria.md` 缺失 | → `needs-attention` |
| `coding` / `amending` | `escalation.json` 包含 `spec-mismatch`、`criteria-wrong` 或 `scope-change` | → `designing` |
| `reviewing` | 审查未通过（需要明确的 `PASS` 或 `CONDITIONAL PASS` 裁定且报告中零 `[CRITICAL]`/`[WARNING]` 问题） | → `amending` |
| `testing` | `verify.json` 未通过或产物缺失 | → `amending` |
| `deep-reviewing` | 深度审查 FAIL | 就地自修复（mini-coder + re-verifier），最多 `max_fix_rounds` 次；PASS → `accepting`，否则 → `needs-attention` |
| any（非设计者） | 护栏检查发现范围违规、基线漂移或必需文件缺失 | → `needs-attention` |

---

## 文件协议

角色通过 `.4x/` 目录进行通信，而非共享上下文窗口。

```
.4x/
├── settings.json                    # 项目配置
├── plugins/                         # Runner 指令文件
├── batch-plan.json                  # 批量执行计划
├── batch-stop                       # 优雅停止信号
├── batch-pid                        # 运行中的批量子进程 PID（服务器孤儿收养）
├── batch-conflict.json              # 批量自动合并冲突信号（暂停）
├── batch-report.json                # 上次批量运行报告（统计 + 每个 feature 结果）
├── features/
│   └── {id}.yaml                    # Feature 定义（权威源）
└── {feature-id}/
    ├── state.json                   # Phase、role、round、active、runner、runners、stopReason、profile
    ├── events.jsonl                 # 审计追踪
    ├── baseline.json                # 编码前快照（HEAD、分支、脏文件）
    ├── task-brief.md                # Designer → Coder：规格 + 架构
    ├── acceptance-criteria.md       # Designer → Tester：可测试的标准
    ├── test-strategy.yaml           # Designer → Tester：测试方案
    ├── final-report.md              # 循环结束摘要
    ├── logs/
    │   ├── round-{N}-{role}.log              # 每轮每角色执行日志
    │   ├── round-{N}-deep-reviewer-{i}.log   # 每个并行子审查者（扇出时）
    │   └── round-{N}-synthesizer.log         # Synthesizer 合并部分报告
    └── rounds/round-{N}/
        ├── coder-report.md            # 编码者做了什么
        ├── review-report.md           # 审查者发现 + 裁定
        ├── test-report.md             # 测试者结果
        ├── deep-review-partial-{i}.md # 某个并行子审查者的发现（扇出时）
        ├── deep-review-report.md      # 合并后的深度审查（synthesizer 输出或单 agent）
        ├── verify.json                # {passed, round, role, commands[]}
        └── escalation.json            # {needed, reason, detail}
```

### 批量信号文件

两个顶层信号文件协调运行中的批量与外部观察者（CLI 和仪表盘）：

- **`batch-stop`** — 空标记文件。`4x batch run` 在 feature 之间轮询它，存在即优雅停止（参见 [Batch 模式](batch.md)）。
- **`batch-conflict.json`** — 批量自动合并遇到合并冲突并暂停时写入。携带足够的细节供仪表盘渲染冲突而无需重新运行 git：

  ```json
  {
    "featureId": "F003-oauth",
    "featureName": "OAuth login",
    "conflictRepo": "core",
    "files": ["internal/auth/token.go"],
    "detectedAt": "2026-06-15T00:00:00Z"
  }
  ```

  单仓库模式下 `conflictRepo` 为空。该文件在每次批量运行开始时以及用户继续暂停的批量时清除。

- **`batch-report.json`** — 批量运行结束时写入（正常、停止、中断或崩溃）。与上述两个信号文件不同，它在运行间持久保存，作为仪表盘在没有活跃批量时显示的"上次批量报告"。记录 `outcome`、总计数（`total`/`completed`/`failed`/`remaining`）、runner、总持续时间，以及每个 feature 的明细（最终状态、轮次、停止原因）；`crashed` 结果还携带 `panicMessage`。原子写入（临时文件 + 重命名），仪表盘永远不会读到半写的报告。

### 原子状态写入

`state.json` 由多个参与者并发读写——运行循环、仪表盘服务器和后台协调器。为避免读者看到截断或半写的文件，`WriteState` 从不原地写入。它序列化状态，写入临时文件（`.state-*.json`）**在同一目录中**（保证同一文件系统使重命名原子化），然后 `os.Rename` 覆盖 `state.json`。读者因此只会看到完整的旧文件或完整的新文件——永远看不到部分内容。任何失败时临时文件会被删除，不会累积 `.state-*.json` 残留。不使用文件锁；正确性来自原子重命名加 `UpdatedAt` 比较。

### 工作区读缓存（仪表盘服务器）

CLI 是短命进程：每个命令读取所需的 `.4x/` 文件一次就退出，因此始终使用普通的 `*protocol.Workspace`。仪表盘服务器（`4x live`）相反——它是长驻的，每个 API 请求都重新读取相同的文件。在多项目 × 多 feature 的工作区中（如 5 个项目 × 50 个 feature），一个请求可能触发数百次 YAML/JSON 解析。

为避免这种开销，服务器将每个工作区包装在 `*protocol.CachedWorkspace`（`internal/protocol/cached.go`）中——一个基于 mtime 的内存缓存，覆盖 `WorkspaceReader` 接口（`internal/protocol/reader.go`）声明的只读操作：

- **`ReadConfig`** — 缓存 `settings.json`；`os.Stat` 比较文件 mtime，仅在变更时重新解析。
- **`ListFeatures`** — 缓存完整 feature 列表；`os.ReadDir` 比较 `.yaml` 文件集和每个文件的 mtime，仅在文件增删或修改时重新解析。返回副本以便调用者自由修改。使用宽松验证：格式有问题的 feature（如 subtask status 不合法）仍会列出并附带 `Warnings`，而非静默跳过。
- **`LoadFeature`** — 按 id 缓存每个 feature，键为 YAML 的 mtime。使用严格验证——任何格式问题都会返回 error。
- **`ReadState`** — 故意**不缓存**（变更频繁、文件小、解析快）；直接穿透到嵌入的 `*Workspace`。

失效是隐式的：写方法（`SaveFeature`、`WriteState` 等）无需通知缓存，因为下次读取会检测到新的 mtime。缓存是可选的——仅服务器构造 `CachedWorkspace`；CLI 继续使用 `*Workspace`，行为完全相同。因为 Go 嵌入没有虚分派，内部 `*Workspace` 方法调用（如 `CompareBacklogMirror` 调用 `w.ListFeatures()`）仍运行未缓存的原始方法；这可以接受，因为这些路径不是服务器的热路径。

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: 1  # 数字：0-1 = full profile，2 = normal，3+ = quick（省略表示 nil/未设置）
repos: []
subtasks: []
rules: []
depends: []
spec: ""     # 可选的设计规格显式路径（覆盖 docs/design/ 查找）
plan: ""     # 可选的实现计划显式路径
hooks: {}    # 可选的阶段钩子（与 settings.json 格式相同）
```

`status` 与 `state.json` 阶段镜像，便于快速列表查询。有效值：`not-started`、`in-progress`、`ready-for-review`、`needs-attention`、`blocked`、`done`、`abandoned`。`abandoned` 的 feature 视为已完成（不阻塞依赖），但在仪表盘中以删除线显示。`depends` 列出必须在此 feature 运行前完成（或 abandoned）的 feature ID。`repos` 列出此 feature 涉及的仓库名称（来自 `workspace.repos`）；为空表示所有仓库在范围内。

#### 设计文档解析

仪表盘 overview 和 `4x prompt` 的规划文档注入通过同一个共享解析器（`protocol.ResolveDesignDoc`）定位 feature 的 spec/plan，确保两者始终看到相同的文档。每种文档类型（`spec`/`plan`）的解析顺序：

1. Feature YAML 的 `spec`/`plan` 字段，非空时作为路径读取（相对路径相对于工作区根目录解析）。
2. `docs/design/{feature.ID}-{type}.md`。
3. `docs/design/{slug}-{type}.md`，其中 `slug` 去掉 ID 的 `FNNN-` 前缀（仅在与 ID 不同时尝试）。

第一个存在的文件胜出；如果都不匹配，文档视为不存在。

### Feature 创建

`Feature`/`Subtask`/`Status` 类型和创建逻辑位于独立的 `internal/feature` 包中（ID 生成、backlog 偏差、截图辅助工具也迁移到此）。`protocol.Workspace` 和 `protocol.CachedWorkspace` 满足 `feature.Store` 接口，而 `feature` 不导入 `protocol`（单向依赖，通过 `Store` 解耦）。CLI（`4x new`）和仪表盘（`POST /api/new`）都通过单一的 `feature.Create(store, opts)` 入口创建 feature，因此编号、ID 截断和默认字段的行为完全一致，不受入口影响。

### 工作区配置（多仓库）

默认情况下，4x 在单仓库模式下运行。要跨多个仓库工作，在 `.4x/settings.json` 中声明：

```json
{
  "workspace": {
    "repos": {
      "backend": { "path": "backend/", "hub": false },
      "frontend": { "path": "frontend/", "hub": false },
      "infra": { "path": "infra/", "hub": true }
    }
  }
}
```

每个条目将仓库名映射到其路径（相对于工作区根目录）和可选的 `hub` 标志。Hub 仓库是多个 feature 可能涉及的共享基础设施——它们被排除在 `4x batch plan` 的范围集群之外。

单仓库模式下（无 `workspace.repos`），所有范围检查和 git 操作使用单个仓库根目录。

---

## 护栏

由 CLI 强制执行的确定性检查 — 不依赖 AI 判断。

| 护栏 | 功能 |
|---|---|
| **必需文件** | 验证阶段对应的产物是否存在（如 designing 后的 `task-brief.md`） |
| **基线** | 捕获编码前状态（HEAD、分支、脏文件）；存在脏文件时发出警告 |
| **范围** | 单仓库模式：将 `git diff --name-only HEAD` 的顶层目录与 feature 声明的仓库进行比对。多仓库模式：使用 `gitops.Ops.DetectChangedRepos()` 跨所有工作区仓库检测 |
| **依赖** | 如果被依赖的 feature 未完成，阻止 `4x run` |
| **Backlog 偏差** | 当 `.4x/features/*.yaml` 与外部镜像不同步时发出警告 |
| **测试 → 接受关卡** | 要求 `verify.json`（passed=true）、`test-report.md`、`final-report.md` |

可通过 `4x check <feature-id>` 手动运行。

---

## 阶段钩子

阶段钩子允许你在阶段转换前后自动运行 shell 命令——适用于启动 Docker 容器、填充测试数据库或测试后清理。钩子由 CLI 执行，不由任何 AI 角色执行。

### 配置

钩子在 `settings.json` 的 `hooks` 键下声明。键格式为 `pre_{phase}` 或 `post_{phase}`：

```json
{
  "hooks": {
    "pre_coding": [
      { "run": "docker compose up -d", "on_fail": "block" }
    ],
    "post_testing": [
      { "run": "docker compose down", "on_fail": "warn" }
    ]
  }
}
```

每个条目是一个 `HookEntry`，包含两个字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `run` | string | 通过 `sh -c` 执行的 shell 命令 |
| `on_fail` | string | `"block"`（默认）或 `"warn"`（大小写不敏感） |

Feature YAML 文件也可以声明同样格式的 `hooks` 字段。当 feature 为同一键定义了钩子时，feature 的定义会**完全替换**全局定义（不在键内合并）。

### 执行顺序

```
pre_{target_phase} 钩子（按数组顺序）
  ↓ 任何 on_fail=block 的钩子失败 → 转为 needs-attention，中止
state.Transition()
  ↓
记录转换事件
  ↓
post_{target_phase} 钩子（按数组顺序）
  ↓ on_fail=block 的钩子失败 → 转为 needs-attention（不回滚）
```

### 失败行为

| `on_fail` | 钩子失败 | 效果 |
|---|---|---|
| `block`（默认） | pre 钩子 | Feature 移至 `needs-attention`；阶段转换中止 |
| `block`（默认） | post 钩子 | 阶段已变更；feature 移至 `needs-attention` |
| `warn` | 任意 | 结果记录日志；继续执行 |

### 日志

每次钩子执行追加一个 `type: "hook"` 事件到 `events.jsonl`：

```json
{
  "ts": "2026-06-14T10:00:00+08:00",
  "type": "hook",
  "phase": "coding",
  "action": "pre_coding",
  "cmd": "docker compose up -d",
  "status": "pass",
  "detail": "exit 0, 1.2s"
}
```

完整的 stdout/stderr 输出写入 `.4x/{feature-id}/hook-logs/{timestamp}-hook-{n}.log`。

### 钩子合并（`MergeHooks`）

全局和 feature 钩子通过 `MergeHooks` 合并：所有全局键被复制，然后 feature 键完全覆盖同名的全局键。仅出现在全局中的键被保留。两者都为 nil 时返回 nil。

---

## 健康检查

在测试者角色启动之前，CLI 可以自动验证环境是否健康——构建通过、服务运行、端点可达。在这里捕获到异常环境可以节省整个浪费的测试周期。健康检查由 CLI 执行，不由任何 AI 角色执行，仅在进入 `testing` 阶段时运行（`pre_testing` 钩子之后、Tester runner 启动之前）。

### 配置

健康检查包含三个字段（`internal/protocol/types.go` 中的 `HealthCheck`）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `commands` | `[]string` | 按顺序运行的检查命令；任何失败都停止运行 |
| `recovery` | `[]string` | 可选。检查失败时按顺序运行以修复环境 |
| `timeout` | `int` | 每个命令的超时秒数；`<= 0` 使用默认值 `30` |

可以在 `settings.json` 中全局声明（JSON，无 yaml 标签）：

```json
{
  "health_check": {
    "commands": ["make build"],
    "recovery": ["docker compose up -d"],
    "timeout": 30
  }
}
```

也可以在 `test-strategy.yaml` 中按 feature 声明（通过 `Workspace.ReadTestStrategy` 读取）：

```yaml
health_check:
  commands: ["make build", "curl -s http://localhost:8080/health"]
  recovery: ["make dev-up"]
  timeout: 60
```

**合并规则：** `ResolveHealthCheck` 执行整组覆盖，而非字段级合并。如果 `test-strategy.yaml` 定义了 `health_check`，它完全替换全局配置；否则使用全局配置。两者都未设置时，跳过健康检查，测试者直接启动。

### 执行流程

```
进入 testing 阶段（pre_testing 钩子已运行）
  ↓
按顺序运行命令（每个有自己的超时）
  ├─ 全部通过 → 启动测试者
  └─ 任何失败 →
      ├─ 无恢复命令 → 升级为 needs-attention
      └─ 有恢复命令 → 按顺序运行恢复命令
          ├─ 恢复失败 → 升级为 needs-attention
          └─ 恢复通过 → 重新运行所有检查命令一次
              ├─ 通过 → 启动测试者
              └─ 仍然失败 → 升级为 needs-attention
```

恢复最多触发一次——没有多次重试或退避循环。

### 失败行为

最终失败时，运行记录一个 `type: "health-check-failed"` 事件（角色 `tester`，`detail` 中包含失败命令和错误），将 feature 转为 `needs-attention`，设置 `StopReason` 为 `health-check-failed`，并停止循环。每个命令通过 `sh -c` 在每个命令的超时下运行；超时视为失败，输出写入 stderr 供调试。

---

## 测试 Profile

**测试 profile** 是一个可复用的测试方法论区块，设计者在 feature 上标记它，测试者的 prompt 就会自动注入对应的指导——而非在 `settings.json` 中手动维护一个所有 feature 共用的巨大 `roles.tester.instructions` 列表。

> 不要与**[流水线 profile](#pipeline-profiles)**（`Config.Profiles`）混淆——后者选择*运行哪些角色*。测试 profile（`Config.TestProfiles`）仅在测试者 prompt 中注入*测试方法论内容*。

### 声明 profile

设计者在 `test-strategy.yaml` 中列出 profile（`internal/protocol/types.go` 中的 `TestStrategy.Profiles`）：

```yaml
profiles:
  - unit
  - web
verify_commands:
  - "make test"
```

`profiles` 是 `omitempty` 的——没有它的 `test-strategy.yaml` 行为与以前完全一致（不注入）。

### 内置 profile

四个 profile 内嵌在二进制文件中（`templates/profiles/*.md`，通过 `templates.ProfilesFS` 暴露）：

| Profile | 方法论 |
|---|---|
| `unit` | Go `go test`、`t.TempDir()` 隔离、表驱动、错误用例、每个 AC 对应 verify.json |
| `web` | Playwright 对接 `4x live` 仪表盘；无头模式、隔离工作区 + 随机端口、截图作为证据、不干扰用户正在运行的服务器 |
| `api` | HTTP 端点测试——状态码、响应体、边界情况、认证 |
| `e2e` | 端到端多服务流程、数据库状态和跨服务一致性 |

### 在 settings.json 中覆盖

项目可以通过 `Config.TestProfiles`（`test_profiles`）替换或扩展任何 profile，按 profile 名称键入（`TestProfileOverride`）：

```json
{
  "test_profiles": {
    "web": { "content": "用 Cypress 而非 Playwright 测试..." },
    "lua": { "include": "docs/test-profiles/lua.md" }
  }
}
```

- `content` — 内联替换文本
- `include` — 文件路径（相对于工作区根目录），使用其内容

**解析顺序**（每个 profile 名称）：`test_profiles[name].content` → `test_profiles[name].include` → 内置 `profiles/{name}.md`。覆盖是整体替换，不是字段级合并。未知名称（无覆盖、无内置）打印警告到 stderr 并跳过。

测试者 prompt 将每个解析后的 profile 渲染为 `== Test Profile: {name} ==` 区块。加载由 `loadProfiles` / `resolveProfileContent`（`cmd/4x/prompt.go`）实现。

---

## Pending Review 关卡

循环**不会**直接进入 `done`。接受后，feature 进入 `pending-review` — 等待人工审查 AI 的工作成果。

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

这确保人类始终在 feature 被标记为完成前进行签字确认。
