# 4x Live 仪表盘

实时监控你的 AI 开发循环。

## macOS Gatekeeper

4x Live app 未经 Apple Developer 签名。macOS 会在首次启动时阻止。

**方法 A：移除隔离属性（推荐）**

```bash
xattr -cr /Applications/4x\ Live.app
```

**方法 B：从系统设置允许**

1. 双击 app — macOS 显示"无法打开，因为无法验证开发者"
2. 打开**系统设置 → 隐私与安全性**
3. 向下滚动至**安全性**区域 — 会看到被阻止的 app 消息
4. 点击**仍要打开**，输入密码或使用 Touch ID 确认
5. macOS 会记住你的选择，之后不再询问

## 启动仪表盘

```bash
# 使用最近项目启动
4x live

# 打开特定项目
4x live /path/to/project1 /path/to/project2

# 自定义端口
4x live -p 8080

# 自动在浏览器中打开
4x live -w

# 打开 macOS 原生应用
4x live -a
```

### Port 单一事实来源

默认 port（`4567`）以 `internal/server.DefaultPort` 为单一事实来源。`cmd/4x/live.go` 的 `--port` flag 默认值即读取此常量；macOS 壳（`main.swift` 的 `serverPort`）与 Tauri 壳（`main.rs` 的 `DEFAULT_PORT`）各自持有一份等值的本地常量（跨语言编译无法直接引用 Go 常量），由 `internal/server/port_sync_test.go` 守护，若三处字面值漂移会导致 `make test` 失败。

## 多项目支持

仪表盘同时支持多个项目。不提供路径参数时，从 `~/.4x/recent-projects.json` 加载（LRU，最多 20 个条目）。

项目标签栏末尾有两个操作：**添加项目**（文件夹加号图标）和**全局设置**（齿轮图标）。侧边栏标题包含活跃项目的**项目设置**齿轮，旁边还有一个**清理**按钮（垃圾桶图标）。点击清理会弹出确认对话框，警告清理后的 feature 将丢失详细日志、报告和轮次历史（feature 定义和状态保留）；确认后调用 [`POST /api/clean`](#post-apiclean) 清理整个项目，并显示结果提示。

## Feature 卡片

每张 feature 卡片显示优先级、依赖、停止原因（如果 feature 异常停止）的标签，以及——当非默认[流水线 profile](concepts.md#pipeline-profiles) 活跃时——**profile 标签**（如 `quick`、`normal`）。高优先级 feature（P0/P1）带强调边框。已完成的依赖显示绿色勾号。`profile`、`stopReason` 和 `stopMessage` 字段包含在 `/api/tasks` JSON 中。`stopReason` 是短分类码（如 `runner-error`、`guard-fail`、`no-progress`），用于颜色标记；`stopMessage` 是显示在分类标签下方的人可读详细说明。

## 新建 Feature 表单

**新建 Feature** 弹窗是一个渐进式表单。基础区域始终显示**名称**（必填）、**描述**（可选，默认为名称）和**优先级**选择（P0-P3 或无）。**高级**开关展开**自定义 ID**（留空则自动生成）、**依赖**（逗号分隔的 feature ID）、**规则**（逗号分隔）以及动态**子任务**列表（添加/删除 id + name 行）。提交后 `POST` 到 [`/api/new`](#rest)；CLI `4x new` 和仪表盘现在共享单一的创建路径（`feature.Create`，参见[核心概念](concepts.md#feature-creation)），因此两者使用相同的标志/字段和 ID 生成逻辑。

## 依赖 DAG

概览以内联 SVG 渲染所有 feature 的依赖图——不加载外部图表库（d3、mermaid、chart.js）。Feature 按依赖深度分层布局；边从每个 feature 连向它依赖的 feature。节点颜色跟随阶段状态：绿色 = done，蓝色 = 运行中（活跃运行或进行中的阶段如 coding/reviewing/testing），灰色 = 待做，红色 = blocked / needs-attention。点击节点打开该 feature 的详情，与点击 feature 卡片路径相同。图表在每个轮询周期从缓存的 `/api/tasks` 数据重建，颜色随 feature 推进实时更新。

## 批量面板

概览还包含一个批量控制面板，由[批量控制 API](#batch-control) 驱动。显示 **Start / Stop / Continue Batch** 按钮（Start 在启动前确认），运行指示器，带每个 feature 进度的调度队列（done 勾号、运行标记或等待位置），以及——当合并冲突暂停批量时——冲突卡片，列出 feature、仓库和冲突文件及 Continue Batch 操作。面板与仪表盘其余部分在同一轮询循环中从 `GET /api/batch/status` 刷新。

## 服务器 API

仪表盘暴露 REST 和 SSE 端点：

读密集型端点（`/api/tasks`、`/api/overview`、`/api/projects`、`/api/settings` 等）通过 `*protocol.CachedWorkspace` 而非普通 `*protocol.Workspace` 提供服务。由于服务器是长驻的，这种基于 mtime 的内存缓存避免了每个请求重新解析所有 feature YAML 和 `settings.json`——参见[工作区读缓存](concepts.md#workspace-read-cache-dashboard-server)。缓存失效是自动的：写操作（通过仪表盘或 runner）改变文件 mtime，下次读取会透明地重新解析。

### REST

| 端点 | 方法 | 说明 |
|---|---|---|
| `/api/tasks` | GET | 列出所有 feature（feature YAML 有格式问题时包含 `warnings` 数组） |
| `/api/new` | POST | 创建新 feature（接受 `name`、`description`，以及可选的 `customId`、`priority`、`depends`、`rules`、`subtasks`） |
| `/api/run` | POST | 启动 feature 运行（生成 `4x run` 子进程） |
| `/api/stop` | POST | 停止正在运行的 feature |
| `/api/done` | POST | 将 feature 标记为完成；如果有 worktree 则自动合并（多仓库：全有或全无） |
| `/api/clean` | POST | 删除项目中所有可清理（done/abandoned）feature 的工作区产物 |
| `/api/runs` | GET | 列出活跃的运行 |
| `/api/batch/start` | POST | 启动批量运行（`4x batch run` 子进程）；如果存在未解决的批量冲突则返回 409 |
| `/api/batch/stop` | POST | 优雅停止批量（写入 `.4x/batch-stop`） |
| `/api/batch/continue` | POST | 清除冲突信号并重启批量（在 worktree 中解决冲突后使用） |
| `/api/batch/status` | GET | 批量运行状态、调度队列、当前 feature 和冲突信号 |
| `/api/events/{id}` | GET | 获取 feature 的事件 |
| `/api/overview/{id}` | GET | 获取 feature 概览（YAML 字段 + spec/plan 内容，通过共享的 `protocol.ResolveDesignDoc` 解析——参见[设计文档解析](concepts.md#design-doc-resolution)） |
| `/api/messages/{id}` | GET | 获取 feature 的消息 |
| `/api/evolve-report` | GET | 最新 `4x evolve` 轮次摘要（`.4x/evolve-report.md`）；`{content, exists}`，不存在时 `exists:false` |
| `/api/features/{id}/screenshots` | GET | 获取按轮次分组的截图 |
| `/api/features/{id}/screenshots/{filename}` | GET | 获取单张截图 |
| `/api/logs/{id}` | GET | 列出 feature 的日志文件 |
| `/api/logs/{id}/{file}` | GET | 获取特定日志文件 |
| `/api/projects` | GET | 列出已注册的项目 |
| `/api/projects` | POST | 添加项目（支持 `init: true` 进行即时初始化） |
| `/api/projects/{id}` | DELETE | 移除项目 |
| `/api/browse` | GET | 文件夹选择器 |
| `/api/settings` | GET | 获取项目设置（`.4x/settings.json`） |
| `/api/settings` | PUT | 更新项目设置（验证、备份、写入） |
| `/api/user-config` | GET | 获取用户配置（`~/.4x/settings.json`） |
| `/api/user-config` | PUT | 更新用户配置（备份到 `.bak`，然后写入） |
| `/api/merged-config` | GET | 只读视图，显示项目 + 用户合并后的有效配置 |
| `/api/locales` | GET | 返回支持的 locale 列表 |
| `/api/locales/{lang}` | GET | 返回对应语言的翻译 JSON |
| `/api/supported-runners` | GET | 列出支持的 runner 名称 |

#### `POST /api/done` 响应

正常情况下返回 HTTP 200。`status` 字段仅在状态转换成功后为 `"done"`。如果发生合并冲突或合并错误，`status` 保持 `"pending-review"`。额外字段说明合并结果：

| 字段 | 类型 | 含义 |
|---|---|---|
| `merged` | bool | `true` 表示分支已合并且 worktree 已清理 |
| `merged` | bool | `false` 表示不存在 worktree（仅状态转换） |
| `merge_conflict` | bool | `true` 表示合并有冲突；worktree 保留 |
| `merge_error` | string | 合并错误消息；feature 保持 pending-review |
| `conflicts` | string[] | 冲突文件列表（仅在 `merge_conflict: true` 时出现） |

冲突后，在 worktree 中解决文件并运行 `4x merge <id>` 完成合并。

如果 feature 的阶段在合并期间发生变化（runner 或后台协调器在合并运行时更新了 `state.json`），端点返回 **HTTP 409 Conflict** 并带有 `{"status":"<currentPhase>","error":"state changed during merge"}`，不执行 done 转换——防止用过期的合并前快照覆盖更新的状态。

#### `POST /api/clean`

删除项目中**所有**可清理 feature 的 `.4x/run/{feature-id}/` 工作区产物（logs、`rounds/`、报告、`state.json`、`events.jsonl`）——与 `4x clean` 清理的集合相同：`done`/`abandoned`、非活跃、工作区目录存在。Feature 定义文件（`.4x/features/*.yaml`）保留，因此清理后的 feature 仍以最终状态显示在列表中。参见[工作区清理](concepts.md#workspace-cleanup)了解底层协议函数。

非 `POST` 请求返回 **HTTP 405**。每个 feature 独立清理；某个失败（如竞态使其变为活跃）会被跳过而不中止其余。处理器始终返回 HTTP 200：

| 字段 | 类型 | 含义 |
|---|---|---|
| `cleaned` | int | 已清理产物的 feature 数量 |
| `freed` | int64 | 释放的总字节数 |
| `freed_human` | string | `freed` 的人类可读格式（如 `38M`） |
| `features` | string[] | 已清理 feature 的 ID（无可清理时为 `[]`） |

无可清理内容时响应为 `{"cleaned":0,"freed":0,"freed_human":"0B","features":[]}`。

#### 批量控制

仪表盘可以端到端驱动批量运行而无需回到终端。专用的 `BatchManager`（独立于每个 feature 的 `ProcessManager`）管理一个项目的单个 `4x batch run` 子进程——同一时间只允许一个批量运行。

- **Start**（`POST /api/batch/start`）— UI 先确认以避免误触，然后启动运行。如果 `.4x/batch-conflict.json` 仍然存在，端点返回 **HTTP 409**，因此过期冲突必须先解决或继续。请求体可携带 `{runner, maxRounds}`；省略的字段回退到合并后的项目/用户配置。
- **Stop**（`POST /api/batch/stop`）— 写入 `.4x/batch-stop` 以优雅停止（批量完成当前 feature 后退出）。不会杀掉子进程。
- **Continue**（`POST /api/batch/continue`）— 清除 `.4x/batch-conflict.json`，然后重启批量。在 worktree 中解决冲突后使用。
- **Status**（`GET /api/batch/status`）— 返回运行标志、调度队列、当前 feature、冲突信号（或 `null`），以及 `lastReport`（解析后的 `.4x/batch-report.json`，无报告时省略）：

  ```json
  {
    "running": true,
    "queue": [
      {"featureId": "F001-auth", "name": "Auth", "status": "done", "state": "done", "position": 0},
      {"featureId": "F002-api", "name": "API", "status": "coding", "state": "running", "position": 1}
    ],
    "currentFeature": "F002-api",
    "conflict": null,
    "lastReport": null
  }
  ```

  队列由 `batch.PlanBatch` 构建，遵循与 CLI 相同的依赖+优先级排序。每个条目的 `state` 为 `done`（feature 完成/待审查）、`running`（活跃运行且未完成）、`error`（blocked/needs-attention）或 `waiting`；`position` 编号未完成项（排除 `done` 和 `error`）。

  `lastReport` 携带最近一次批量运行的报告（`outcome`、计数、runner、持续时间和每个 feature 的明细——参见 [Batch 模式](batch.md#run-report)）。无批量运行时，面板将其渲染为"上次批量报告"摘要卡片，可展开查看每个 feature 的详情；`crashed` 结果还会显示 `panicMessage`。

### 截图标签页

Feature 详情中包含**截图**标签页（当该 feature 存在截图时）。截图按轮次分组，以缩略图显示，可在灯箱中打开并左右导航，ESC 关闭。

### SSE（Server-Sent Events）

| 端点 | 说明 |
|---|---|
| `/sse/events/{id}` | 流式传输 feature 事件（1 秒轮询） |
| `/sse/logs/{id}` | 流式传输 feature 的活跃日志文件（一个或多个） |

事件流跟踪 `events.jsonl` 中的字节偏移量，仅发送新追加的行。如果文件被**截断或轮换**——例如 `4x transition --to init` 重置 feature 并从头重写 `events.jsonl`——新文件大小低于跟踪的偏移量。流检测到这一情况（`size < lastOffset`），将偏移量重置为 0 并从头重读整个文件，客户端恢复而非永久静默停滞。大小等于偏移量仍表示"无新内容"并被跳过。

日志流（`/sse/logs/{id}`）同样跟踪字节偏移量，仅发送新追加的内容。为避免每次 tick 的垃圾分配，它复用每个连接分配一次的固定 32KB 读缓冲区。每次 tick 从偏移量循环读到 EOF；大于 32KB 的增量分拆为多条 SSE 消息，每条携带相同的 `{"file": "...", "content": "..."}` 载荷。客户端按到达顺序追加内容，拆分是透明的。当 `size <= lastOffset`（无新内容）时跳过 tick，不打开文件。

当多个角色同时写日志——并行深度审查子审查者，或并发的审查者+测试者——流尾随**所有**当前活跃的日志，而非仅最近修改的那个。不带 `?file=` 查询参数时，它跟踪 mtime 落在最近窗口内的每个日志（各自有独立偏移量），每条消息的 `file` 字段让客户端将内容路由到匹配的面板。传入 `?file=<name>` 可固定流到单个日志。

### 完成通知

每次 SSE tick 时，dashboard 从 `/api/events/{id}` 读取最新事件，当事件携带 `notify` 提示（`run-end` 成功、`guard-fail` 或 `escalation`）时，触发原生 OS 通知。派发器根据运行环境选择正确的通知渠道：macOS 原生应用使用 `nativeNotify` WebKit 桥接，Tauri shell 调用由 `tauri-plugin-notification` 支持的 `notify` 命令，普通浏览器使用 Web Notification API（需先请求权限）。不支持或无权限的环境会静默降级。通知文本通过 `notifications.*` i18n 键进行本地化。

### 多项目路由

当有多个项目时，端点前缀为 `/api/project/{project-id}/...` 和 `/sse/project/{project-id}/...`。单项目模式使用无前缀路径以保持向后兼容。

#### 工作区解析

叶子路由（`/api/tasks`、`/api/settings`、`/api/run`、`/api/batch/*`、`/sse/events/...` 等）在 `NewMux`（`internal/server/server.go`）中**只定义一次**。`NewMux` 不绑定固定工作区，而是接受一个 `WorkspaceResolver`——一个给定传入请求返回目标 `*protocol.CachedWorkspace`、其 `*ProcessManager` 和 `*BatchManager`（或错误）的函数。每个数据驱动的处理器先调用解析器；不需要它们的路由（`/api/user-config`、`/api/supported-runners`、`/api/locales`、静态资源）跳过。这消除了单项目和多项目模式以前各自携带的约 150 行重复处理器注册。

两个解析器支持两种模式：

- **`singleResolver(ws, pm)`** — 单项目模式（`server.Start`）。闭包持有一个工作区，始终返回相同的 `ws`/`pm`/`bm` 三元组。
- **`multiResolver(reg)`** — 多项目模式（`NewMultiMux`）。解析是三步流程：
  1. **前缀分发（外层 mux）。** `NewMultiMux` 注册 `/api/project/` 和 `/sse/project/` 处理器，剥离 `/api/project/{id}`（或 `/sse/project/{id}`）前缀，通过 `getEntry(id)` 查找条目（未知 id → **404**），将 `r.URL.Path` 改写为剩余子路径，将解析后的条目注入请求 `context`，然后转发给共享的内层 `NewMux` 处理器。前缀剥离必须在外层 mux 中完成，因为 `http.ServeMux` 在运行前就选择了处理器——未剥离的 `/api/project/{id}/api/tasks` 只会匹配静态 `/` 路由。
  2. **Context 读取。** 在内层处理器中，`multiResolver` 先检查请求 context 中步骤 1 注入的条目，存在时直接返回。
  3. **无前缀兼容。** 未注入条目时（无前缀路径），回退到 `reg.Count()`：`0` → **400** `no projects loaded`、`1` → 该唯一项目、`>=2` → **400** `multiple projects loaded — use /api/project/{id}...`。

`NewMultiMux` 本身只注册全局端点（`/api/projects`、`/api/projects/`、`/api/browse`）加上两个前缀分发器和一个转发到共享 `inner := NewMux(multiResolver(reg))` 的 catch-all。添加项目不再构建每个条目的 mux；`registryEntry` 仅携带 `id`/`ws`/`pm`/`bm`。

## 键盘快捷键

| 快捷键 | 操作 |
|---|---|
| `Cmd+K` | 搜索 |
| `Cmd+,` | 项目设置（在项目中）/ 全局设置（在首页） |
| `Cmd+Shift+,` | 全局设置 |
| `Esc` | 关闭当前弹窗 |

## 进程管理

仪表盘管理 runner 子进程：

- 遵循项目配置中的 `max_concurrent_runs`
- 将 stdout/stderr 捕获为 run-output/run-error 事件
- 优雅关闭：SIGTERM → 5 秒 → SIGKILL

当 runner 子进程退出时，服务器将 feature 标记为非活跃（`Active=false`、`StopReason=process-exit`）。这有竞态保护：runner 可能在退出前写入自己的最终 `state.json`（如 `pending-review`）。服务器记录进程退出时间，覆盖前重新读取状态——如果 `state.json` 在退出时间**或之后**更新（`UpdatedAt >= endTime`），则保留 runner 的最终状态，跳过非活跃写入。这防止服务器用过期的内存快照覆盖刚写入的阶段或其 `StopReason`。

## 共享 Web 前端

仪表盘 UI（HTML/CSS/JS + locale JSON）以 `dashboard/web/` 为单一真相源，通过 `dashboard/web/embed.go`（`web.Assets embed.FS`）嵌入 `4x` 二进制文件。Go 服务器（`internal/server/server.go`、`internal/server/multi.go`）直接从 `web.Assets` 提供静态资源和 locale 文件，因此同一前端支持每个平台外壳——Go 服务的 web UI、macOS WKWebView 和 Tauri webview。没有需要同步的平台专属 UI 副本。

## 平台

| 平台 | 外壳 | 打包方式 |
|---|---|---|
| Web UI（嵌入式） | Go 服务器提供 `web.Assets` | `4x live` |
| macOS 原生 | Swift WKWebView，自动启动内置的 `4x live` 服务器 | universal `.dmg`（`make package-macos`） |
| Windows | Tauri v2 webview，`4x` sidecar | `.msi`（`dashboard/tauri`） |
| Linux | Tauri v2 webview，`4x` sidecar | `.AppImage`（`dashboard/tauri`） |

所有桌面外壳通过 `http://localhost:<port>` 加载相同的 `dashboard/web/` 前端，由嵌入的 `4x` 服务器支持。`.github/workflows/desktop.yml` 中的 CI 矩阵交叉编译各平台的 `4x` 二进制文件并生成 `.dmg`/`.msi`/`.AppImage` 产物。
