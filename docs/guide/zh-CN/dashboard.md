# 4x Live 仪表盘

实时监控你的 AI 开发循环。

## 启动仪表盘

```bash
# Start with recent projects
4x live

# Open specific projects
4x live /path/to/project1 /path/to/project2

# Custom port
4x live -p 8080

# Auto-open in browser
4x live -w

# Open macOS native app
4x live -a
```

## 多项目支持

仪表盘同时支持多个项目。不提供路径参数时，从 `~/.4x/recent-projects.json` 加载（LRU，最多 20 个条目）。

## 服务器 API

仪表盘暴露 REST 和 SSE 端点：

### REST

| 端点 | 方法 | 说明 |
|---|---|---|
| `/api/tasks` | GET | 列出所有 feature |
| `/api/new` | POST | 创建新 feature |
| `/api/run` | POST | 启动 feature 运行（生成 `4x run` 子进程） |
| `/api/stop` | POST | 停止正在运行的 feature |
| `/api/done` | POST | 将 feature 标记为完成 |
| `/api/runs` | GET | 列出活跃的运行 |
| `/api/events/{id}` | GET | 获取 feature 的事件 |
| `/api/messages/{id}` | GET | 获取 feature 的消息 |
| `/api/logs/{id}` | GET | 列出 feature 的日志文件 |
| `/api/logs/{id}/{file}` | GET | 获取特定日志文件 |
| `/api/projects` | GET | 列出已注册的项目 |
| `/api/projects` | POST | 添加项目（支持 `init: true` 进行即时初始化） |
| `/api/projects` | DELETE | 移除项目 |
| `/api/browse` | GET | 文件夹选择器 |

### SSE（Server-Sent Events）

| 端点 | 说明 |
|---|---|
| `/sse/events/{id}` | 流式传输 feature 事件（1 秒轮询） |
| `/sse/logs/{id}` | 流式传输 feature 的最新日志文件 |

### 多项目路由

当有多个项目时，端点前缀为 `/api/project/{project-id}/...` 和 `/sse/project/{project-id}/...`。单项目模式使用无前缀路径以保持向后兼容。

## 进程管理

仪表盘管理 runner 子进程：

- 遵循项目配置中的 `max_concurrent_runs`
- 将 stdout/stderr 捕获为 run-output/run-error 事件
- 优雅关闭：SIGTERM → 5 秒 → SIGKILL

## 平台

| 平台 | 状态 |
|---|---|
| Web UI（嵌入式） | 可用 |
| macOS 原生（Swift） | 计划中 |
| Electron（Windows/Linux） | 计划中 |
