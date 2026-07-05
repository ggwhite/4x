[English](../../README.md) | [繁體中文](README.zh-TW.md) | **简体中文** | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/ggwhite/4x.svg)](https://pkg.go.dev/github.com/ggwhite/4x)
[![Go Report Card](https://goreportcard.com/badge/github.com/ggwhite/4x)](https://goreportcard.com/report/github.com/ggwhite/4x)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg)](https://github.com/ggwhite/4x/actions/workflows/ci.yml)

<p align="center">
  <img src="../assets/4x-banner.svg" alt="4X — Design. Code. Review. Test." width="480">
</p>

<p align="center">
  <img src="../assets/demo.gif" alt="4x demo" width="720">
</p>

**4x 是一个多角色 AI 开发框架，将软件工程循环拆分为四个专业阶段** — 设计 (Design)、编码 (Code)、审查 (Review)、测试 (Test) — 每个阶段由一个专属 AI agent 驱动。正如 4X 策略游戏（探索、扩张、开发、征服）一样，这个名字体现了一个系统：不同角色各司其职，协力征服复杂性。

---

## 主要功能

| 类别 | 亮点 |
|---|---|
| **多角色循环** | Design → Code → Review → Test → Deep Review → Accept，角色隔离。Adaptive pipeline 按功能复杂度选择 profile（full / mini / quick）。 |
| **6 种 AI Runner** | Claude Code · Codex · Gemini CLI · Antigravity · Copilot · Cursor — 统一 `.4x/` 文件协议，可按角色混搭。 |
| **Dashboard（4x Live）** | macOS 原生（Swift）+ Windows / Linux（Tauri）。实时 SSE 监控、依赖图、Runner log streaming、截图 gallery、设置 UI、批量监控。6 语言 i18n、系统通知、menu bar 集成。 |
| **确定性护栏** | 状态机、scope lock、baseline snapshot、证据测试关卡、依赖关卡 — 由 Go CLI 强制执行，不是靠 LLM prompt。 |
| **Crash Recovery** | Runner 中断 → 从最后保存的状态自动恢复。暂态 API 错误（网络、rate limit）→ 自动 backoff 重试。 |
| **批量模式** | 依赖感知 DAG 调度、完成后自动合并、批量报告、优雅停止。几十个 feature 排入夜间执行，早上再 review。 |
| **MCP Server** | Model Context Protocol server，可与 MCP 兼容的 client 集成。 |
| **Issue-First MR Flow** | 可选的 `issue_tracker` 模式：`4x new` 创建或关联 issue，`4x done` 改为 push branch 并开 PR/MR，取代本地合并。依各 repo 的 remote 自动检测 GitHub 或 GitLab（含自建），无需额外配置。 |
| **20+ CLI 命令** | `run`、`batch`、`live`、`doctor`、`clean`、`verify`、`mcp`、phase hooks、health check、structured logging 等。 |
| **自我进化** | 从历史 run 挖掘改进信号、自动发现 feature 充实化、evolution value gate + anti-hack、自我修改 scope guard、持续改进驱动器（`4x evolve`）。4x 从自身失败中学习并自我迭代。 |

## 为什么选择 4x？

单 agent 编码快但脆弱。你让一个 AI 同时设计、实现、审查和测试 — 一口气完成，带着相同的偏见。小任务可以凑合，真正的功能开发就会崩盘。

4x 拆分了这个循环。每个角色都有明确的职责、有限的范围，且无法接触其他角色的推理过程。设计者不写代码，编码者不评判自己的成果，审查者天生是对抗性的，测试者按照实现前编写的标准进行验证。

最终结果：经得起生产环境考验的功能。

## 权衡取舍

选择 4x 意味着用速度和成本换取结构和正确性。请诚实评估你的项目是否需要这种取舍。

### 优势

- **角色隔离消除自我审查偏见。** 编码者永远不评判自己的成果。审查者天生是对抗性的。单 agent 工作流让同一个模型编写并审批代码 — 4x 不会。
- **确定性护栏不依赖 AI 判断。** 范围锁定、状态机、证据要求 — 这些由 Go 编写的 CLI 强制执行，而不是靠提示 LLM "请不要超出范围"。
- **基于文件的协议使其与 LLM 无关。** 在 Claude、Gemini、Codex 之间自由切换，或按角色混合使用。无供应商锁定，无 SDK 依赖。
- **抗崩溃的状态。** 一切保存在 `.4x/` 文件中。会话中断、机器重启 — `4x run` 从中断处精确恢复。
- **人始终在环中。** `pending-review` 关卡确保人类始终在 AI 工作被标记完成前进行审查。AI 提议，你决定。
- **驾驭大规模重构。** 单一 AI 会话无法处理的大型改动 — 拆分 God Object、提取 package、迁移 API — 可以拆成有依赖关系的多个 feature，各自指定合适的 profile。4x 负责调度、review 和跨阶段验证，不会超出单一 context window 的极限。
- **批量模式可扩展。** 依赖感知的调度让你可以把几十个功能排入队列过夜运行，早上再审查。

### 劣势

- **显著更高的 token 消耗。** 每个功能至少经过 4 次以上的独立 LLM 调用。审查失败会翻倍。预期同一任务的 token 消耗是单 agent 方法的 3-10 倍。参见[使用技巧](../guide/zh-CN/usage-tips.md)了解成本估算。
- **简单任务更慢。** 一行的 bug 修复不需要设计者、审查者和测试者。完整循环的开销在琐碎改动上是浪费的。简单修复请使用单 agent 工具。
- **初始化成本。** `4x init`、feature YAML、设置配置 — 开始前需要一些仪式性操作。对于一次性脚本不值得。
- **固定的循环结构。** 设计 → 编码 → 审查 → 测试的顺序是固定的。如果你的工作流不适合四个角色，你会在跟框架对抗而不是使用它。
- **质量取决于 prompt 质量。** 模糊的功能描述产生模糊的规格，进而产生错误的代码。4x 增加了结构，但垃圾输入仍然意味着垃圾输出 — 只是步骤更多。

### 何时使用 4x

- 需要正确性的功能（支付、认证、数据管道）
- 受益于对抗性审查的工作（安全敏感代码）
- 功能积压的批量处理
- 需要 AI 生成代码审计追踪的团队

### 何时不该使用 4x

- 快速的一次性修复或探索性原型
- 速度比正确性更重要的任务
- token 预算紧张的项目
- 你自己会审查代码的独立开发场景

## 架构

```
 You
  |
  v
+--------------------------------------------------+
|  4x CLI (Go)                                     |
|  Deterministic guardrails. No LLM calls.         |
|  Scope checks, protocol, state machine, batch    |
+--------+-----------------------------------------+
         |  .4x/ directory (file-based protocol)
         v
+--------------------------------------------------+
|  Runners                                         |
|  Claude Code | Codex | Gemini | Antigravity      |
|  Copilot | Cursor                                |
|  Each uses native platform capabilities          |
+--------+-----------------------------------------+
         |  SSE events
         v
+--------------------------------------------------+
|  4x Live (Dashboard)                             |
|  Multi-project real-time monitoring              |
+--------------------------------------------------+
```

**第一层 — CLI** 处理所有确定性操作：范围验证、状态转换、基线快照、证据收集。它从不调用 LLM。护栏不依赖 AI 判断。

**第二层 — Runner** 将 CLI 协议桥接到你选择的 AI 工具。Claude Code、Codex、Gemini、Antigravity、Copilot、Cursor — 每个都使用相同的 `.4x/` 文件协议，但使用各自平台的原生能力。

**第三层 — Live** 是多项目仪表盘。实时观察 AI agent 的工作进度，查看阶段转换，流式传输日志。REST + SSE API。

## 安装

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 下载 Binary

macOS、Linux、Windows（amd64 / arm64）的预编译 binary 可在 [Releases](https://github.com/ggwhite/4x/releases) 页面下载。

## 快速开始

```bash
# 在你的项目中初始化
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

# Run the full loop
4x run F001 --runner claude

# Check status
4x status

# Review and complete
4x done F001

# Or watch it live
4x live -w
```

`4x run` 自动驱动 设计-编码-审查-测试 循环。如果审查发现问题，编码者会再做一轮。如果测试失败，循环会迭代。你可以通过 `--max-rounds` 和 `--timeout` 标志保持控制。

## 四个角色

| 角色 | 职责 | 产出 |
|---|---|---|
| **设计者 (Designer)** | 分析需求，产出规格和验收标准 | `task-brief.md`、`acceptance-criteria.md` |
| **编码者 (Coder)** | 严格按照规格实现 | 源代码、`coder-report.md` |
| **审查者 (Reviewer)** | 发现 bug 和规格违规（检查清单 + 对抗性审查） | `review-report.md`（含裁定结果） |
| **测试者 (Tester)** | 基于验收标准用证据验证 | `test-report.md`、`verify.json` |

每个角色都是**隔离的**。编码者看不到审查者之前的反馈。测试者按照设计者（而非编码者）编写的标准进行验证。这种隔离防止了单 agent 工作流中常见的盲点。

## 循环如何运作

```
Designer → Coder → Reviewer → Tester → Accept → Pending Review → Done
                      ↓           ↓                                 ↑
                   amending ←─────┘                          human sign-off
```

- **审查失败**（裁定 FAIL 或 CRITICAL 级发现）将代码送回修改
- **测试失败**（验证未通过）将代码送回修改
- **升级 (Escalation)**（规格不匹配、标准错误）路由回设计者
- **Pending review** 关卡确保人类始终在标记完成前进行审查
- **轮次预算**（默认 5）防止无限循环

## 确定性护栏

由 CLI 强制执行，不依赖 AI 判断：

| 护栏 | 功能 |
|---|---|
| **范围检查** | 修改的文件必须在声明的仓库范围内 |
| **基线快照** | 编码前捕获状态，用于安全回滚 |
| **状态机** | 阶段必须按合法顺序推进 |
| **证据要求** | 测试者必须提供带有命令输出的 verify.json |
| **测试关卡** | 必须有 verify.json + test-report + final-report |
| **依赖关卡** | 未满足依赖的功能不能启动 |

## 批量模式

```bash
4x batch plan            # generate dependency-aware execution plan
4x batch run --runner claude  # run all eligible features in order
4x batch stop            # graceful shutdown after current feature
```

## 权限模型

**4x 以非交互模式运行 AI agent。** 在 `4x init` 时，runner 配置了跳过权限提示的标志（`--dangerously-skip-permissions`、`-y`、`approval: full-auto`），使循环自主运行。

CLI 的确定性护栏（范围锁定、基线快照、状态机）提供安全边界。

**请仅在你能接受自主 AI agent 执行的项目中运行 4x。**

## 文档

| 文档 | 说明 |
|---|---|
| **[用户指南](../guide/zh-CN/)** | 完整的使用文档 |
| [快速开始](../guide/zh-CN/getting-started.md) | 安装与首次运行 |
| [CLI 参考](../guide/zh-CN/cli.md) | 所有命令和标志 |
| [核心概念](../guide/zh-CN/concepts.md) | 角色、状态机、协议、护栏 |
| [配置](../guide/zh-CN/configuration.md) | 设置、模型、语言、runner |
| [Runner 与插件](../guide/zh-CN/runners.md) | 支持的 runner 和插件合约 |
| [仪表盘](../guide/zh-CN/dashboard.md) | 4x Live 多项目仪表盘 |
| [批量模式](../guide/zh-CN/batch.md) | 依赖感知的批量执行 |

## 项目结构

```
4x/
  cmd/4x/              CLI entry point (Cobra)
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    batch/              Dependency DAG, batch scheduler
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
  plugins/
    claude-code/        Claude Code skill + workflow
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions + workflow
    cursor/             Cursor rules
    embed.go            go:embed plugin files into binary
  dashboard/
    macos/              Swift native app (planned)
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

## FAQ

**Q: 4x 是否直接调用任何 LLM API？**
不。CLI 是纯 Go 实现，零 LLM 依赖。Runner 使用各自平台的原生能力处理所有 AI 交互。

**Q: 可以为不同角色使用不同的 LLM 吗？**
可以。在 `.4x/settings.json` 中配置每个角色的模型。用 Claude 做设计、Gemini 做编码 — 每个都读取相同的 `.4x/` 文件。

**Q: 4x 与 Devin / SWE-agent / OpenHands 有什么不同？**
它们是一次性完成所有工作的自主 agent。4x 是一个*框架*，通过确定性护栏来组织多角色协作。它更接近 AI 的 CI 管道，而不是一个单一的自主 agent。

## 起源故事

4x 诞生于一个名为 DCT（Designer-Coder-Tester）的生产系统，该系统为一次大规模平台重写交付了 60 多个功能。存活下来的模式 — 角色隔离、基于文件的协议、确定性范围检查、基于证据的测试 — 成为了 4x。没有存活下来的部分 — LLM 特定的 hack、共享上下文假设、基于信任的护栏 — 被刻意排除了。

## 贡献

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
go build ./cmd/4x
go test ./...
```

## 许可证

[MIT](LICENSE)

---

<p align="center">
  <strong>别再指望你的 AI 写出正确的代码。开始验证它。</strong>
</p>
