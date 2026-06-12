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

## 查看进度

```bash
# See which feature is next
4x batch next

# Overview of all features
4x status
```
