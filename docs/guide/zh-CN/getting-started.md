# 快速开始

## 安装

### Homebrew (macOS / Linux)

```bash
brew install ggwhite/tap/fourx
```

### Go Install

```bash
go install github.com/ggwhite/4x/cmd/4x@latest
```

需要 Go 1.26+。

### Shell Script

```bash
curl -sSfL https://raw.githubusercontent.com/ggwhite/4x/main/install.sh | sh
```

### 下载 Binary

macOS、Linux、Windows（amd64 / arm64）的预编译 binary 可在 [Releases](https://github.com/ggwhite/4x/releases) 页面下载。

### macOS Gatekeeper

CLI 可执行文件与 4x Live dashboard app 未经 Apple Developer 签名。macOS Gatekeeper 会在首次启动时阻止。两种解决方式：

**方法 A：移除隔离属性（推荐）**

```bash
# CLI 可执行文件
xattr -cr /usr/local/bin/4x

# Dashboard app
xattr -cr /Applications/4x\ Live.app
```

**方法 B：从系统设置允许**

1. 双击 app — macOS 显示"无法打开，因为无法验证开发者"
2. 打开**系统设置 → 隐私与安全性**
3. 向下滚动至**安全性**区域 — 会看到被阻止的 app 消息
4. 点击**仍要打开**
5. 输入密码或使用 Touch ID 确认
6. App 将启动；macOS 会记住你的选择，之后不再询问

### Windows SmartScreen

可执行文件未经代码签名。Chrome 和 Edge 可能阻止下载，Windows SmartScreen 可能阻止执行。

**浏览器阻止下载：**

1. Chrome：点击下载警告 → **保留** → **仍要保留**
2. Edge：点击下载栏的 `...` → **保留** → **显示更多** → **仍要保留**

**SmartScreen 阻止执行：**

1. 双击 exe — Windows 显示"Windows 已保护你的电脑"
2. 点击**详细信息**
3. 点击**仍要运行**

或通过 PowerShell 解除阻止：

```powershell
Unblock-File -Path .\4x.exe
```

### 验证

验证安装：

```bash
4x --help
```

## 初始化项目

```bash
cd my-project
4x init
```

这会创建一个 `.4x/` 目录，包含：
- `settings.json` — 项目配置、runner 定义、角色模型映射
- `plugins/` — runner 指令文件（SKILL.md、AGENTS.md 等）
- 根目录导入文件（CLAUDE.md、AGENTS.md、GEMINI.md 等）

4x 会自动检测项目语言（Go、TypeScript、Java、Rust、Python）并预填构建/测试/lint 命令。

如果 `.4x/` 已存在，`init` 会报错退出 — 使用 `4x sync` 来刷新插件文件。

## 创建 Feature

```bash
4x new "User authentication with OAuth2"
# => Created: F001-user-authentication-w

4x new "Payment processing" --repo payment-service --repo shared-lib
# => Created: F002-payment-processing
```

Feature 存储在 `.4x/features/{id}.yaml`。ID 格式为 `F{NNN}-{slug}`（slug 最多 23 个字符）。

使用 `--repo` 声明哪些仓库在范围内（用于多仓库项目）。

## 运行循环

```bash
# Run with default runner (usually claude)
4x run F001

# Specify a runner
4x run F001 --runner claude

# Limit iterations
4x run F001 --max-rounds 3

# Set timeout (seconds)
4x run F001 --timeout 7200

# Preview prompts without calling LLM
4x run F001 --dry-run
```

Feature ID 支持前缀匹配 — `4x run F001` 和 `4x run f001` 都能工作。

循环流程：**设计 → 编码 → 审查 → 测试 → 接受 → Pending Review**。如果审查发现问题，编码者会再做一轮。如果测试失败，循环会迭代（最多 `--max-rounds` 次）。

## 查看状态

```bash
# All features
4x status

# Single feature with details
4x status F001

# Filter pending review
4x status --pending
```

## 完成 Feature

循环结束后，feature 进入 `pending-review` 状态 — 等待人工确认。

```bash
# Review the outputs
cat .4x/F001/final-report.md
cat .4x/F001/commit-plan.md

# Mark as done
4x done F001
```

## 升级插件文件

更新 `4x` 二进制文件后，重新部署嵌入的插件：

```bash
4x sync            # deploy new files
4x sync --dry-run  # preview changes only
```

## 下一步

- [CLI 参考](cli.md) — 所有命令和标志
- [核心概念](concepts.md) — 了解角色、状态机、协议
- [配置](configuration.md) — 自定义模型、runner、语言
