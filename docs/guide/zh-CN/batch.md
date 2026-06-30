# 批量模式

按依赖感知的顺序运行多个 feature。

## 工作流

```bash
# 1. Generate execution plan
4x batch plan

# 2. Check what's next
4x batch next

# 3. Run all eligible features
4x batch run --runner claude

# 4. Gracefully stop (finishes current feature)
4x batch stop
```

## 规划

`4x batch plan` 分析所有未完成的 feature 并生成 `.4x/batch-plan.json`：

1. **依赖 DAG** — 根据 feature 的 `depends` 字段构建有向图
2. **循环检测** — 存在循环依赖时报错
3. **Union-Find 聚类** — 将共享仓库的 feature 分组
4. **拓扑排序** — 对每个集群内的 feature 排序
5. **链式调度** — 拆分过长的依赖链（最大长度可通过 `--max-chain` 配置）

```bash
# Preview the plan
4x batch plan --dry-run

# Limit chain length
4x batch plan --max-chain 3
```

输出示例：

```
  cluster-1: F001-auth → F003-oauth | F002-api
  cluster-2: F004-payment

Schedule (4 features):
  [slot 1] F001-auth —
  [slot 2] F002-api —
  [slot 2] F004-payment —
  [slot 3] F003-oauth after [F001-auth]
```

## 运行

`4x batch run` 按依赖顺序依次执行 feature：

```bash
4x batch run --runner claude --max-rounds 3 --timeout 7200
```

- 使用提交策略 `"never"`（完成后由你手动提交）
- 在 feature 之间检查 `.4x/batch-stop` 文件
- 跳过依赖未完成的 feature
- 每个 feature 完成后报告进度

## 停止

```bash
4x batch stop
```

创建 `.4x/batch-stop` 信号文件。批处理完成当前 feature 后优雅退出。

## 合并冲突

当自动合并遇到冲突时，批处理暂停并写入 `.4x/batch-conflict.json`，记录 feature、冲突的仓库（多仓库模式）和受影响的文件。Worktree 被保留以便解决冲突。此信号文件让[仪表盘](dashboard.md)显示冲突并提供**继续批处理**操作——底层逻辑是清除信号文件并重启 `4x batch run`。从 CLI 操作时，解决文件冲突后运行 `4x merge <id>`，再重新运行 `4x batch run` 继续。冲突文件在每次批处理运行开始时自动清除。

## 运行报告

每次批处理运行结束时——无论是正常完成、被停止、被中断还是崩溃——都会写入 `.4x/batch-report.json`。报告记录整体统计（total / completed / failed / remaining）、runner、总持续时间，以及每个 feature 的名称、最终状态、持续时间、轮次数和停止原因。

`outcome` 字段记录运行的结束方式：

- `completed` — 所有 feature 已完成
- `stopped` — 你按下停止（`.4x/batch-stop`）或自动合并冲突暂停了运行
- `interrupted` — 批处理进程收到 `SIGTERM`/`SIGINT`；报告记录当时正在运行的 feature
- `crashed` — 批处理进程发生 panic；报告尽力而为，包含 `panicMessage`

[仪表盘](dashboard.md)在没有活跃批处理时读取此文件，显示展开为每个 feature 详情的"上次批处理报告"摘要卡片。报告仅在运行停止后写入，从不在每个 feature 的执行循环内写入，因此不会增加批处理吞吐量的开销。

## 查看进度

```bash
# See which feature is next
4x batch next

# Overview of all features
4x status
```
