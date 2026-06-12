# 核心概念

## 四个角色

| 角色 | 职责 | 输入 | 输出 | 不可操作 |
|---|---|---|---|---|
| **设计者 (Designer)** | 分析需求，产出规格，定义验收标准和测试策略 | Feature 描述、代码库 | `task-brief.md`、`acceptance-criteria.md`、`test-strategy.yaml` | 修改源代码 |
| **编码者 (Coder)** | 按照规格实现 | `task-brief.md`、先前的测试/审查报告 | 源代码、`coder-report.md` | 修改验收标准或测试脚本 |
| **审查者 (Reviewer)** | 发现 bug、安全问题、规格违规 | Diff、规格、编码报告、项目规则 | `review-report.md` | 修改源代码 |
| **测试者 (Tester)** | 基于验收标准用证据验证 | 验收标准、编码报告、测试策略 | 测试脚本、`test-report.md`、`verify.json`、`final-report.md`、`commit-plan.md` | 修改源代码 |

每个角色都是**隔离的** — 编码者在实现过程中看不到先前的审查反馈。测试者按照设计者（而非编码者）编写的标准进行验证。

### 审查：两个阶段

1. **检查清单审查**（标准模型）— 根据项目硬性规则检查：安全性、并发、错误处理、代码风格
2. **对抗性审查**（深度模型）— "这个 diff 中隐藏的最严重 bug 是什么？"按严重程度对发现进行评级。

### 升级 (Escalation)

编码者或测试者可以在以下情况下升级回设计者：

| 原因 | 含义 |
|---|---|
| `spec-mismatch` | 数据库/API 与规格不匹配 |
| `criteria-wrong` | 验收标准不正确 |
| `blocker` | 缺少依赖或基础设施问题 |
| `scope-change` | 需要修改范围外的仓库 |

升级信息写入 `escalation.json`。循环自动路由回设计者。

---

## 状态机

```
init → designing → coding → reviewing → testing → accepting → pending-review → done
                     ↑          ↓           ↓
                     ├── amending ←──────────┘
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
| `testing` | `accepting`、`amending`、`designing` |
| `accepting` | `pending-review` |
| `pending-review` | `done` |
| `blocked` | `designing`、`coding`、`testing` |
| `needs-attention` | `designing`、`coding` |
| any | `blocked`、`needs-attention` |

### 轮次计数器

- 当轮次为 0 时进入 `coding` 会将轮次设为 1
- 进入 `amending` 会递增轮次
- 当轮次 >= maxRounds 或连续 3 轮以上无进展时，`ShouldStop` 触发

### 循环中的阶段决策

| 阶段 | 条件 | 操作 |
|---|---|---|
| `designing` | `task-brief.md` 缺失 | → `needs-attention` |
| `coding` / `amending` | `escalation.json` 包含 `spec-mismatch` 或 `criteria-wrong` | → `designing` |
| `reviewing` | 裁定行以 FAIL 开头或包含 `[CRITICAL]` | → `amending` |
| `testing` | `verify.json` 未通过或构件缺失 | → `amending` |

---

## 文件协议

角色通过 `.4x/` 目录进行通信，而非共享上下文窗口。

```
.4x/
├── settings.json                    # Project config
├── plugins/                         # Runner instruction files
├── batch-plan.json                  # Batch execution plan
├── batch-stop                       # Graceful stop signal
├── features/
│   └── {id}.yaml                    # Feature definition (canonical source)
└── {feature-id}/
    ├── state.json                   # Phase, role, round, active, runner, runners, stopReason
    ├── events.jsonl                 # Audit trail
    ├── baseline.json                # Pre-coding snapshot (HEAD, branch, dirty files)
    ├── task-brief.md                # Designer → Coder: spec + architecture
    ├── acceptance-criteria.md       # Designer → Tester: testable criteria
    ├── test-strategy.yaml           # Designer → Tester: test approach
    ├── final-report.md              # End-of-loop summary
    ├── commit-plan.md               # How to split changes into commits
    ├── logs/
    │   └── round-{N}-{role}.log     # Per-round per-role execution log
    └── rounds/round-{N}/
        ├── coder-report.md          # What the Coder did
        ├── review-report.md         # Reviewer findings + verdict
        ├── test-report.md           # Tester results
        ├── verify.json              # {passed, round, role, commands[]}
        └── escalation.json          # {needed, reason, detail}
```

### Feature YAML

```yaml
id: F001-user-authentication-w
name: User authentication with OAuth2
description: ...
status: not-started
priority: medium
repos: []
subtasks: []
rules: []
depends: []
```

`status` 与 `state.json` 阶段镜像，便于快速列表查询。`depends` 列出必须在此 feature 运行前完成的 feature ID。

---

## 护栏

由 CLI 强制执行的确定性检查 — 不依赖 AI 判断。

| 护栏 | 功能 |
|---|---|
| **必需文件** | 验证阶段对应的构件是否存在（如 designing 后的 `task-brief.md`） |
| **基线** | 捕获编码前状态（HEAD、分支、脏文件）；存在脏文件时发出警告 |
| **范围** | 将 `git diff --name-only HEAD` 的顶层目录与 feature 声明的仓库进行比对 |
| **依赖** | 如果被依赖的 feature 未完成，阻止 `4x run` |
| **Backlog 偏差** | 当 `.4x/features/*.yaml` 与外部镜像不同步时发出警告 |
| **测试 → 接受关卡** | 要求 `verify.json`（passed=true）、`test-report.md`、`final-report.md`、`commit-plan.md` |

可通过 `4x check <feature-id>` 手动运行。

---

## Pending Review 关卡

循环**不会**直接进入 `done`。接受后，feature 进入 `pending-review` — 等待人工审查 AI 的工作成果。

```
... → accepting → pending-review → (human reviews) → 4x done F001
```

这确保人类始终在 feature 被标记为完成前进行签字确认。
