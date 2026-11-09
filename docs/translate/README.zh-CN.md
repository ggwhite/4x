<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/banner-dark.svg">
  <img src="../assets/banner-light.svg" alt="4X：设计、实现、审查、测试。" width="100%">
</picture>

**一个 agentic 开发循环：把 Design、Code、Review、Test 拆成彼此隔离的 AI 角色，再用确定性的 guardrail 把关。**

<p>
  <a href="https://github.com/ggwhite/4x/actions/workflows/ci.yml"><img src="https://github.com/ggwhite/4x/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ggwhite/4x/releases"><img src="https://img.shields.io/github/v/release/ggwhite/4x" alt="最新版本"></a>
  <a href="https://pkg.go.dev/github.com/ggwhite/4x"><img src="https://pkg.go.dev/badge/github.com/ggwhite/4x.svg" alt="Go Reference"></a>
  <a href="https://goreportcard.com/report/github.com/ggwhite/4x"><img src="https://goreportcard.com/badge/github.com/ggwhite/4x" alt="Go Report Card"></a>
  <a href="../../LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="许可证：MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey" alt="平台：macOS、Linux、Windows">
</p>

[安装](#安装) · [快速开始](#快速开始) · [功能](#功能) · [循环如何运作](#循环如何运作) · [Guardrail](#确定性-guardrail) · [架构](#架构) · [文档](#文档) · [FAQ](#faq)

[English](../../README.md) | [繁體中文](README.zh-TW.md) | **简体中文** | [日本語](README.ja.md) | [한국어](README.ko.md) | [Español](README.es.md)

</div>

---

<div align="center">
  <a href="../assets/tour.mp4"><img src="../assets/tour.webp" alt="4x 导览，取自一次真实运行并加速播放：(1) 4x init 在一个既有的 Go 项目 greet 上完成设置，并检测出 go build 与 go test 命令；(2) 4x new 创建 feature F001 Greet by name；(3) 4x run F001 让 Claude Code 依次扮演 Designer、Coder、Reviewer、Tester；Tester 的检查被 verify 白名单挡下，feature 因此 escalate 回 Designer 重写，第二轮通过后进入 Accept 与 pending-review，并打印总花费；(4) 4x status 列出处于 pending-review 的 feature，4x done F001 将它标记完成，go run . -name Ada 打印 hello, Ada" width="900"></a>
  <br>
  <sub>▶ <a href="../assets/tour.mp4">观看完整导览</a> · 一次真实的 Claude Code 运行（约 4 分钟、$2），agent 等待时间已剪短</sub>
</div>

## 为什么

单一 agent 写代码很快，但很脆弱。你让同一个 AI 设计、实现、审查、测试，一口气做完，带着同样的偏见。小任务还行，碰到真正的功能就撑不住。

4x 把循环拆开。每个角色职责专一、范围有限，也看不到其他角色的推理过程：Designer 不写代码，Coder 不评判自己的成果，Reviewer 天生就站在对立面，Tester 则依照实现前就写好的验收标准来验证。决定下一步能做什么的是 Go CLI，不是 LLM。就像 4X 策略游戏（eXplore、eXpand、eXploit、eXterminate），各有所长的不同角色朝同一个目标前进：做出经得起上线考验的功能。

## 功能

<table>
<tr>
<td width="42%" valign="middle">

### 四个隔离的角色

**Designer → Coder → Reviewer → Tester → Accept**，每个角色都是独立的 runner 进程，彼此只通过 `.4x/` 里的文件沟通。Reviewer 判定 **FAIL** 或验证失败，代码会送去 **amending**；规格有误则 **escalate** 回 Designer。Pipeline **profile**（`full`、`normal`、`lite`、`quick` 或自定义）决定要跑哪些阶段。

</td>
<td><img src="../assets/feature-loop.webp" alt="演示运行时的 4x Live Messages 标签页：先出现 Designer 产出 task-brief.md 与 acceptance-criteria.md 的消息，接着 Coder、Reviewer、Tester 的报告依次出现，每则都标注模型与花费；Tester 发起 escalation，Designer 重写 brief，第二轮 Coder、Reviewer、Tester 跑完后，Acceptor 把 feature 收进 Review"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 确定性的 guardrail

Guardrail 是 **Go 代码，不是 prompt**。`4x check` 检查 **scope**、**baseline** 快照、必要文件与 **verify 证据**；**状态机**拒绝不合法的阶段跳转；`4x verify` 只运行项目**白名单**内的命令。Runner 怎么说都绕不过去。

</td>
<td><img src="../assets/feature-guardrails.webp" alt="终端：4x check F001 回报已完成的演示 feature 通过所有 guardrail 检查；4x transition F001 --to coding --json 被拒，消息为 invalid transition: pending-review to coding；4x verify F001 依每条验收标准运行 test-strategy.yaml 里的 go build、go vet、go test 检查并打印 PASSED"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 4x Live 仪表板

`4x live` 通过 REST + **SSE** 提供**多项目仪表板**：feature 卡片随阶段变化从 Todo 移到 Running 再到 Review，并显示每个 feature 的轮次、runner 与花费。同一套 web UI 内嵌在 binary 里，也包装成 **macOS（Swift）** 与 **Windows / Linux（Tauri）** 应用程序。

</td>
<td><img src="../assets/feature-live.webp" alt="演示运行时的 4x Live 项目总览：F001 Greet by name 卡片从 Todo 移到 Running，阶段标签依次切换 designing、coding、reviewing、testing，Running 计数、状态环形图与 Currently running 面板同步更新；最后卡片停在 Review 并出现 Mark Done 按钮，Pending review 面板也列出它"></td>
</tr>
<tr>
<td width="42%" valign="middle">

### 依角色与轮次计算花费

每次 runner 调用都会记录模型、耗时与 **USD 花费或 token 数**。`4x cost` 依**角色**汇总，`--by-round` 显示有多少花在**重试**上，`--feature` 则把单一 feature 逐轮拆开，让你看清一次 review 失败实际花了多少。

</td>
<td><img src="../assets/feature-cost.webp" alt="终端：4x cost 打印演示项目依角色（designer、coder、reviewer、tester、acceptor）的花费，含调用次数、总额、平均与占比；接着 4x cost --feature F001-greet-by-name 依轮次与角色列出同样的调用"></td>
</tr>
</table>

### 更多功能

- **7 种 runner**：Claude Code、Codex、Gemini CLI、Antigravity、Copilot、Cursor 与 OpenCode 共用同一套 `.4x/` 文件协议，可依角色或阶段混搭（`--phase-override`）。
- **自适应 pipeline**：profile 顾问依 feature 复杂度建议 `full` / `normal` / `quick`；各角色的模型使用 `strong` / `fast` 两种等级。
- **崩溃恢复**：runner 崩溃后从最后保存的状态接续；暂时性的 API 错误（网络、rate limit）会以 backoff 重试。
- **人工审批**：每个 feature 都会停在 `pending-review`；`4x done` 将它标记完成并合并分支（启用 `issue_tracker` 时则改为开 PR/MR）。
- **Issue-first MR 流程**：可选的 `issue_tracker` 模式。`4x new` 创建或关联 issue，`4x done` 推送分支并开 PR/MR，而不是在本地合并。GitHub 或 GitLab（含自托管）依各 repo 的 remote 自动判断。
- **依赖关系**：`4x new --depends F001` 让 feature 等到依赖项目完成才能开始；仪表板会画出依赖图。
- **自我演化**：`4x mine` / `4x gate` / `4x evolve` 从过去的运行挖掘失败信号，经过价值闸门筛选候选项目，再排入改进用的 feature。回顾心得（`4x learn`）会注入之后的角色 prompt。
- **MCP server** 与 **Claude Code skill**（`4x-audit`、`4x-autopilot`），见下方说明。
- **30 个以上的 CLI 命令**：`run`、`status`、`check`、`verify`、`retry`、`cost`、`live`、`doctor`、`clean`、`mcp`、`skills`、phase hook、结构化日志 等。详见 [CLI 参考](../guide/zh-CN/cli.md)。

## 安装

### Homebrew（macOS / Linux）

```bash
brew install ggwhite/tap/fourx
```

### Go install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

### Shell 脚本

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 下载 binary

macOS、Linux、Windows（amd64 / arm64）的预编译 binary 都在 [Releases](https://github.com/ggwhite/4x/releases) 页面。

<details>
<summary><b>验证 checksum</b>（手动下载时）</summary>

Shell 脚本会自动验证下载文件的 checksum，不符就中止。若从 [Releases](https://github.com/ggwhite/4x/releases) 页面手动下载压缩包或 binary，请在解压缩前自行验证：把该版本的 `checksums.txt` 下载到同一个目录，然后运行以下其中一个命令：

```bash
# Linux (and macOS with coreutils)
sha256sum --check --ignore-missing checksums.txt

# macOS (default)
shasum -a 256 --ignore-missing --check checksums.txt
```

确认 checksum 显示 `OK` 后，再解压缩并运行 binary。

</details>

## 快速开始

```bash
# Initialize in your project
cd my-project
4x init

# Create a feature
4x new "User authentication with OAuth2"
# => Created feature: F001-user-authentication (User authentication with OAuth2)

# Run the full loop
4x run F001 --runner claude

# Check status
4x status

# Review and complete
4x done F001

# Or watch it live
4x live -w
```

`4x run` 会自动驱动 Design-Code-Review-Test 循环。Review 发现问题，Code 就再跑一次；Test 失败，循环就再迭代。你可以用 `--max-rounds`、`--profile` 与 `--timeout` 掌控整个流程。

## 四个角色

| 角色 | 职责 | 产出 |
|---|---|---|
| **Designer** | 分析需求，产出规格与验收标准 | `task-brief.md`、`acceptance-criteria.md`、`test-strategy.yaml` |
| **Coder** | 完全照规格实现 | 源代码、`coder-report.md` |
| **Reviewer** | 抓出 bug 与违反规格之处（检查清单 + 对抗式审查） | 附判定结果的 `review-report.md` |
| **Tester** | 依验收标准验证并附上证据 | `test-report.md`、`verify.json` |

每个角色都是**隔离**的。Coder 看不到 Reviewer 先前的反馈；Tester 依照 Designer 写的标准验证，而不是 Coder 写的。这种分工能避开单一 agent 工作流常见的盲点。`full` profile 会在四个核心角色之外，再加上 Design Reviewer、多角度的 Deep Reviewer、Fixer 与 Acceptor。

## 循环如何运作

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/loop-dark.svg">
  <img src="../assets/loop-light.svg" alt="Designer、Coder、Reviewer、Tester、Accept、Pending Review 与 Done 排成一列。Reviewer 判定 FAIL 或验证失败时，代码送去 amending，再回到 Reviewer；escalation 回到 Designer；进入 Done 前需由人以 4x done 审批。" width="100%">
</picture>

- **Review 失败**（判定 FAIL 或有 CRITICAL 发现）会把代码送回 amending
- **Test 失败**（verify 未通过）会把代码送回 amending
- **Escalation**（规格不符、验收标准有误）会转回 Designer
- **Pending review** 闸门确保标记完成前一定有人审查
- **轮次上限**（默认 5）避免无限循环

合法的状态转换定义在 [`internal/state/machine.go`](../../internal/state/machine.go)；完整状态机请见 [核心概念](../guide/zh-CN/concepts.md)。

## 确定性 Guardrail

由 CLI 强制运行，不靠 AI 判断：

| Guardrail | 作用 |
|---|---|
| **Scope 检查** | 变更的文件必须在声明的 repo 范围内 |
| **Baseline 快照** | 编码前先保存状态，方便安全回滚 |
| **状态机** | 阶段必须依合法顺序推进 |
| **证据要求** | Tester 必须提供含命令输出的 `verify.json` |
| **测试闸门** | 需要 `verify.json` + 测试报告 + 最终报告 |
| **Verify 白名单** | `4x verify` 只运行 `verify_command_allowlist` 列出的命令 |
| **依赖闸门** | 依赖项目未完成的 feature 不能开始 |

## 架构

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../assets/architecture-dark.svg">
  <img src="../assets/architecture-light.svg" alt="你操作 4x CLI（Go）：确定性的 guardrail，不调用 LLM，负责 scope 检查、协议、状态机与依赖闸门。它通过 .4x/ 目录的文件协议把工作交给 runner（Claude Code、Codex、Gemini、Antigravity、Copilot、Cursor、OpenCode）。4x Live 接收 SSE 事件，提供多项目实时监控。" width="100%">
</picture>

**第 1 层：CLI** 处理所有确定性的工作：scope 验证、状态转换、baseline 快照、证据收集。它从不调用 LLM，所以 guardrail 不依赖 AI 的判断。

**第 2 层：Runner** 把 CLI 协议接到你选择的 AI 工具。每个 runner 都使用同一套 `.4x/` 文件协议，但各自发挥原生平台的能力。

**第 3 层：Live** 是多项目仪表板。实时看 agent 工作、看阶段转换、流式查看日志。提供 REST + SSE API。

## Autopilot

先排好多个 feature（用 `--depends` 决定顺序），再交给 `4x-autopilot` 这个 Claude Code skill 驱动：它会挑出下一个可运行的 feature，运行、审批、合并后接着做下一个，全程无人值守。它取代了原本的 `4x batch` 命令。

## MCP server

`4x mcp` 通过 stdio 启动 Model Context Protocol server，把 CLI 以 tool 的形式提供给 MCP client，例如 `4x_status`、`4x_new`、`4x_run`、`4x_stop`、`4x_check`、`4x_verify`、`4x_done`、`4x_doctor` 与 `4x_learn_*`。

```bash
4x mcp
```

## Claude Code skill

4x 附带两个可选的 [Claude Code Skills](https://docs.anthropic.com/en/docs/claude-code)，让你在 Claude Code session 里驱动 pipeline。用 [`npx skills`](https://github.com/vercel-labs/skills) 安装即可，不必 clone 这个 repo：

```bash
npx skills add ggwhite/4x --skill 4x-audit       # scan past run artifacts, report + optionally file gap features
npx skills add ggwhite/4x --skill 4x-autopilot   # drive the full pick → run → merge → next loop unattended
npx skills add ggwhite/4x --all                  # install both
```

- **`4x-audit`**：扫描 discovered-feature-gaps、escalation／review 报告与反复出现的 learnings 模式，产出分类好的 HTML 报告，也可选择批量创建 feature YAML。
- **`4x-autopilot`**：轮询 4x 状态，驱动 feature 走完整个生命周期（design → code → review → test → merge），过程中不停下来确认，连合并也会自动完成。**仅限 repo 拥有者**：在共用 repo 上使用前，请先读过 skill 里的警告。

若是从这个 repo 的 clone 安装，可改用 `4x skills install <name>` 把 skill 以 symlink 连到 `~/.claude/skills/`。

## 权限模型

> [!WARNING]
> **4x 会以非交互模式运行 AI agent。** 请只在你能接受 AI agent 自主运行的项目中使用 4x。

<details>
<summary>这代表什么</summary>

`4x init` 时，runner 会被设置成带有略过权限询问的 flag（`--dangerously-skip-permissions`、`-y`、`--yolo`），让循环能自主运作。

安全边界由 CLI 的确定性 guardrail 提供（scope 锁定、baseline 快照、状态机、verify 白名单）。在启动 runner 之前，4x 也会从它的环境中滤掉敏感的环境变量（`*_TOKEN`、`*_KEY`、`*_SECRET`、`AWS_*` 等）；详见 [Runner 与 Plugin](../guide/zh-CN/runners.md)。

</details>

## 取舍

选择 4x，就是用速度与成本换取结构与正确性。请老实评估你的项目是否需要这样的取舍。

<details>
<summary><b>优势、劣势，以及何时（不）该用 4x</b></summary>

### 优势

- **角色隔离消除自我审查的偏见。** Coder 从不评判自己的成果，Reviewer 天生站在对立面。单一 agent 工作流让同一个模型写代码又批准代码；4x 不会。
- **确定性的 guardrail 不依赖 AI 判断。** Scope 锁定、状态机、证据要求：这些都由 Go 写的 CLI 强制运行，而不是靠 prompt 恳请 LLM「请待在范围内」。
- **以文件为基础的协议，不绑特定 LLM。** 可在 Claude、Gemini、Codex 之间切换，或依角色混搭。没有厂商锁定，也不依赖 SDK。
- **抗崩溃的状态。** 一切都存在 `.4x/` 文件里。Session 挂了、机器重启：`4x run` 会从停下的地方精准接续。
- **人始终在循环里。** `pending-review` 闸门确保 AI 的成果在标记完成前一定经过人审查。AI 提案，由你拍板。
- **驾驭大规模重构。** 单一 AI session 吃不下的大型改动（拆分 god object、抽出 package、迁移 API），可以拆成彼此依赖、各配合适 profile 的 feature。4x 负责跨多个阶段的排序、审查与验证，这些工作塞进单一 context window 会撑不住。
- **能扩展到整份 backlog。** 搭配依赖关系与 autopilot skill，你可以在晚上排进几十个 feature，隔天早上再来审查。

### 劣势

- **Token 成本明显更高。** 每个 feature 至少要经过 4 次以上独立的 LLM 调用，一次 review 失败就翻倍。同样的任务，预期 token 成本是单一 agent 做法的 3 到 10 倍。成本估算见 [使用技巧](../guide/zh-CN/usage-tips.md)。
- **简单任务反而更慢。** 一行的 bug 修正不需要 Designer、Reviewer 和 Tester，完整循环的开销在琐碎变更上是浪费。快速修正请用单一 agent 工具。
- **前置设置成本。** `4x init`、feature YAML、配置文件：开工前有一套仪式要走。一次性的脚本不值得。
- **循环结构固定。** Design → Code → Review → Test 的顺序是固定的。如果你的工作流程塞不进四个角色，你会一直在跟框架搏斗，而不是在使用它。
- **质量取决于 prompt 质量。** 模糊的 feature 描述产出模糊的规格，模糊的规格产出错误的代码。4x 增加了结构，但垃圾进、垃圾出依然成立，只是多了几个步骤。

### 何时使用 4x

- 必须正确的功能（支付、认证、数据 pipeline）
- 需要对抗式审查的工作（安全敏感的代码）
- 消化整份 feature backlog
- 需要 AI 产生代码审计轨迹的团队

### 何时不该使用 4x

- 一次性的快速修正或探索性原型
- 速度比正确性重要的任务
- Token 预算吃紧的项目
- 反正你会自己审查代码的个人 hacking 时

</details>

## 文档

| 文档 | 说明 |
|---|---|
| **[用户指南](../guide/zh-CN/)** | 完整的使用说明 |
| [入门](../guide/zh-CN/getting-started.md) | 安装与第一次运行 |
| [CLI 参考](../guide/zh-CN/cli.md) | 所有命令与 flag |
| [核心概念](../guide/zh-CN/concepts.md) | 角色、状态机、协议、guardrail |
| [设置](../guide/zh-CN/configuration.md) | 配置文件、模型、语言、runner |
| [Runner 与 Plugin](../guide/zh-CN/runners.md) | 支持的 runner 与 plugin 合约 |
| [仪表板](../guide/zh-CN/dashboard.md) | 4x Live 多项目仪表板 |
| [使用技巧](../guide/zh-CN/usage-tips.md) | 模型选择、成本、疑难排解 |

<details>
<summary><b>项目结构</b></summary>

```
4x/
  cmd/4x/              CLI entry point (Cobra), one file per subcommand
  internal/
    protocol/           .4x/ file format, workspace, types
    state/              State machine (phase transitions)
    guard/              Guardrail checks (scope, baseline, evidence)
    orchestrator/       Run loop orchestration
    runner/             Subprocess runner interface
    server/             SSE + REST server for Live dashboard
    mcp/                MCP server (stdio)
  plugins/
    claude-code/        Claude Code runner instructions
    codex/              Codex runner instructions
    gemini/             Gemini runner instructions
    agy/                Antigravity runner instructions
    copilot/            Copilot runner instructions
    cursor/             Cursor rules
    opencode/           OpenCode runner instructions
    embed.go            go:embed plugin files into binary
  dashboard/
    web/                Dashboard UI, embedded into the binary
    macos/              Swift native app
    tauri/              Windows / Linux app
  skills/               4x-audit, 4x-autopilot Claude Code skills
  templates/            Role prompt templates
  docs/
    guide/              User documentation
    architecture/       System-level design docs
    design/             Mechanism design docs
    reference/          Plugin contract
```

</details>

## FAQ

<details>
<summary><b>4x 会直接调用任何 LLM API 吗？</b></summary>

不会。CLI 是纯 Go，完全不依赖 LLM。所有 AI 交互都由 runner 通过其原生平台能力处理。

</details>

<details>
<summary><b>可以让不同角色使用不同的 LLM 吗？</b></summary>

可以。在 `.4x/settings.json` 设置各角色的模型。Design 用 Claude、Code 用 Gemini：大家读的都是同一批 `.4x/` 文件。

</details>

<details>
<summary><b>这跟 Devin / SWE-agent / OpenHands 有什么不同？</b></summary>

那些是一次包办所有事的自主 agent。4x 是一套*框架*，用确定性的 guardrail 组织多角色协作。它比较像是给 AI 用的 CI pipeline，而不是单一的自主 agent。

</details>

## 开发

```bash
git clone https://github.com/ggwhite/4x.git
cd 4x
make build      # bin/4x
make test       # go test -race ./...
make lint       # go vet + gofmt + golangci-lint
```

完整流程请见 [CONTRIBUTING.md](../../CONTRIBUTING.md)。

<details>
<summary><b>重新录制 README 的片段与图表</b></summary>

所有片段都来自从源代码编译的真实 binary，在 `/tmp/4xdemo` 底下的一次性项目与 `HOME` 中录制。录制全程 headless：终端用 [`vhs`](https://github.com/charmbracelet/vhs)，仪表板用 Python Playwright（headless Chromium）。需要 `vhs`、`ffmpeg`、`img2webp`（libwebp）、Python `playwright`，录制导览视频时还需要已登录的 `claude` CLI。

```bash
docs/assets/record/render.sh tour      # fresh /tmp/4xdemo, ONE real 4x run (about $2 of tokens), all clips
docs/assets/record/render.sh recut     # re-encode the clips from the last run's frames, no agent run
python3 docs/assets/diagrams/gen.py    # architecture / loop SVGs (light + dark)
python3 docs/assets/diagrams/snapshot.py /tmp/svg docs/assets/*-light.svg   # check frames + reduced motion
```

`render.sh tour` 会运行 [`setup-demo.sh`](../assets/record/setup-demo.sh)（从源代码编译 4x，创建一个小小的 Go 项目「greet」），在 [`capture-live.py`](../assets/record/capture-live.py) 截图 `4x live` 画面的同时录制 [`tour.tape`](../assets/record/tour.tape)，接着录制各功能片段，最后由 [`tour-end.tape`](../assets/record/tour-end.tape) 审批该 feature。[`retime.py`](../assets/record/retime.py) 会剪掉漫长的 agent 等待时间，并用 `img2webp` 编码成 WebP。Claude Code runner 通过一个 wrapper 调用，略过用户设置、hook、plugin 与 MCP server。

提交新片段前，请对每一帧做 OCR（`swiftc docs/assets/record/ocr.swift -o /tmp/ocr`，接着 `magick clip.webp -coalesce f-%03d.png` 与 `/tmp/ocr f-*.png`），并 grep 文字中是否出现用户名、主机名、email 与 token。

</details>

## 缘起

4x 诞生于一个名为 DCT（Designer-Coder-Tester）的生产系统，它为一次大规模平台改写交付了 60 个以上的功能。存活下来的模式（角色隔离、以文件为基础的协议、确定性的 scope 检查、以证据为本的测试）成了 4x；没能存活的部分（针对特定 LLM 的 hack、共享 context 的假设、靠信任的 guardrail）则被刻意舍弃。

## 许可证

[MIT](../../LICENSE)

---

<p align="center">
  <strong>别再祈祷 AI 写出正确的代码，开始验证它。</strong>
</p>
