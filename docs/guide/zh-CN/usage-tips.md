# 使用技巧与最佳实践

## Token 用量提醒

4x 会消耗**显著多于单一 agent** 的 token。每个 feature 至少经过 4 个角色（Designer → Coder → Reviewer → Tester），每个角色都是独立的 LLM 调用。如果 Review 或 Test 失败触发重跑，token 会再翻倍。

粗估每个 feature 的 token 用量：

| 情境 | 约 LLM 调用次数 | 说明 |
|---|---|---|
| 一次通过（最佳情况） | 5 次 | Designer + Coder + Reviewer(2 pass) + Tester |
| Review 打回 1 次 | 8 次 | 多一轮 Coder + Reviewer + Tester |
| 跑满 5 rounds | ~20 次 | 每 round 都是 Coder + Reviewer + Tester |

**省 token 建议：**
- 简单任务降低 `--max-rounds`（`--max-rounds 2`）
- 简单任务全用 sonnet 等级 model（便宜 5-10 倍）
- 善用 `--dry-run` 先确认 prompt 品质，避免浪费
- Feature description 写清楚，减少 escalation 和重跑
- 连续 3 轮无进步时 loop 会自动停，不会白烧到 max-rounds

---

## 完整工作流程

从创建任务到交付的完整流程——4x 负责 AI 开发，你负责最终 review 和合并。

### Step 1: 创建任务

```bash
4x new "Add Redis cache for order query API"
# => Created: F001-add-redis-cache-for-or
```

如果需要，编辑 `.4x/features/F001-add-redis-cache-for-or.yaml` 补充 description、priority、depends、repos 等字段。

### Step 2: 执行 loop

```bash
# 建议先 dry run 看 prompt
4x run F001 --dry-run

# 正式跑
4x run F001 --runner claude
```

可以开 dashboard 实时观察：

```bash
4x live -w   # 另一个 terminal
```

### Step 3: Loop 完成 → pending-review

Loop 跑完后，feature 停在 `pending-review`——这是故意的。AI 做完了，但需要你来 review。

```bash
4x status F001
# Phase: pending-review
```

### Step 4: 人工 Review

检查 AI 产出的成果：

```bash
# 看最终报告
cat .4x/F001/final-report.md

# 看 commit 计划
cat .4x/F001/commit-plan.md

# 看 code diff
git diff                          # 非 worktree 模式
git diff main...4x/F001-add-redis  # worktree 模式
```

如果不满意，可以：

```bash
# 手动修改后重跑 review + test
4x transition F001 --to reviewing
4x run F001

# 或完全重来
4x transition F001 --to designing
4x run F001
```

### Step 5: 合并 & 清理

**非 worktree 模式**（改动直接在 working tree）：

```bash
# 满意后标记完成
4x done F001

# 按 commit-plan.md 提交
git add -A
git commit -m "feat: add Redis cache for order query API"
```

**Worktree 模式**（改动在独立 branch）：

```bash
# 标记完成
4x done F001

# 合并到主分支
git merge 4x/F001-add-redis-cache-for-or

# 清理 worktree 和 branch
git worktree remove .worktrees/4x/F001-add-redis-cache-for-or
git branch -d 4x/F001-add-redis-cache-for-or
```

### 流程总览

```
4x new "..."                     # 创建任务
    ↓
4x run F001 --runner claude      # AI 自动跑 Design→Code→Review→Test
    ↓
pending-review                   # 等你 review
    ↓
review final-report / diff       # 你看成果
    ↓
4x done F001                     # 标记完成
    ↓
git merge + cleanup              # 合并、清 worktree/branch
```

---

## 编写好的 Feature 描述

Feature description 是设计者的唯一输入——写得越清楚，产出的 spec 越准。

```bash
# Bad: 太模糊，Designer 会自己脑补
4x new "改善效能"

# Good: 明确目标、边界、验收条件
4x new "optimize order query API — add Redis cache, target p99 < 200ms, cache TTL 5min"
```

建议在 description 包含：
- **要做什么**（具体功能或修改）
- **为什么做**（业务动机或问题描述）
- **边界**（不要动的东西、已知限制）
- **验收标准**（可量化的成功定义）

## Feature 粒度

一个 feature 对应一个可独立交付的变更。太大会让编码者迷失、审查者漏检、测试难验。

| 粒度 | 适合 | 不适合 |
|---|---|---|
| 一个 API endpoint | OK | — |
| 一个 refactor（改命名、抽接口） | OK | — |
| 一个 bug fix | OK | — |
| 整个模块从零开始 | — | 拆成多个 feature + depends |
| 跨 3 个 repo 的大功能 | — | 每个 repo 一个 feature，用 depends 串 |

善用 `depends` 拆解大任务：

```bash
4x new "Add user model and migrations"           # F001
4x new "Add user registration API"               # F002, depends: [F001]
4x new "Add OAuth2 login flow"                    # F003, depends: [F002]
```

## 先 Dry Run 再正式跑

第一次用新 feature 或改过 settings 后，先用 `--dry-run` 看 prompt 是否合理：

```bash
4x run F001 --dry-run
```

这会打印出四个角色的完整 prompt 但不调用 LLM，可以确认：
- 设计者有没有拿到足够的 context
- 你的 project rules 有没有正确注入
- locale 是否正确

## 模型选择建议

| 角色 | 建议 | 理由 |
|---|---|---|
| Designer | opus 或同等级 | 需要深度理解需求、拆解架构 |
| Coder | sonnet 或同等级 | 产出量大，但不需要最强推理 |
| Reviewer（检查清单） | sonnet | 规则式检查，速度优先 |
| Reviewer（对抗性） | opus | 需要深度推理找隐藏 bug |
| Tester | sonnet | 写测试、跑验证，不需要最强推理 |

调整方式：

```json
// .4x/settings.json
{
  "roles": {
    "designer": { "model": "opus" },
    "coder": { "model": "sonnet" },
    "reviewer": { "model": "sonnet", "deep_model": "opus" },
    "tester": { "model": "sonnet" }
  }
}
```

如果项目简单（小 bug fix、小 refactor），全用 sonnet 也行，省成本。

## Rounds 调校

默认 5 rounds 适合多数情况。根据 feature 复杂度调整：

| 情境 | 建议 rounds |
|---|---|
| 简单 bug fix、小改动 | 2-3 |
| 一般功能开发 | 5（默认） |
| 复杂跨模块功能 | 7-10 |

```bash
4x run F001 --max-rounds 3   # 简单任务
4x run F001 --max-rounds 8   # 复杂任务
```

注意：loop 会在 3 轮连续无进步时自动停止（不用跑满 max-rounds）。

## 处理 Review 失败

Review 失败（verdict FAIL 或 CRITICAL findings）会自动送回编码者修改，不需要人工介入。但如果反复失败：

1. **看 review-report.md** — 在 `.4x/{feature-id}/rounds/round-{N}/review-report.md`
2. **看 coder-report.md** — 编码者是否理解了问题
3. **考虑调整**：
   - feature description 太模糊 → 重写 description，重跑设计者
   - 审查者太严格 → 在 `roles.reviewer.instructions` 放宽特定规则
   - 真的是 hard problem → 人工介入修改，再用 `4x transition` 推进

## 处理 Escalation

编码者或测试者发现 spec 跟实际不符时，会自动 escalate 回设计者。常见情境：

- DB schema 跟 spec 描述的不同（`spec-mismatch`）
- 验收标准不合理（`criteria-wrong`）
- 缺少外部依赖（`blocker`）

Escalation 被记录在 `.4x/{feature-id}/rounds/round-{N}/escalation.json`。设计者会收到 escalation 内容重新出 spec。

如果设计者也解不了（通常是缺 context），loop 会停在 `needs-attention`，这时需要人工介入：

```bash
# 看状态
4x status F001

# 手动修 spec 或 codebase
vim .4x/F001/task-brief.md

# 推回 coding 继续
4x transition F001 --to coding
```

## 恢复中断的 Feature

4x 是基于文件的——会话断了、机器重开，状态都在 `.4x/` 里。直接重跑即可：

```bash
4x run F001 --runner claude
```

会从上次的 phase 和 round 继续，不会从头来。

## Worktree 隔离

如果同时跑多个 feature，或想隔离 AI 的修改，启用 worktree：

```json
// .4x/settings.json
{
  "isolation": "worktree"
}
```

效果：
- 每个 feature 在 `.worktrees/4x/{feature-id}/` 独立工作
- 自动建 branch `4x/{feature-id}`
- 完成后提示 merge 指令

```bash
# 完成后合并
git merge 4x/F001-user-auth
git worktree remove .worktrees/4x/F001-user-auth
git branch -d 4x/F001-user-auth
```

## Batch 使用时机

| 情境 | 用 `4x run` | 用 `4x batch run` |
|---|---|---|
| 做一个 feature | OK | — |
| 做多个有依赖的 feature | 要手动排序 | 自动处理依赖顺序 |
| 跑一晚上消化 backlog | — | OK，搭配 `batch stop` 随时停 |

Batch 的 commit 策略固定是 `"never"`——所有改动都在 working tree，完成后由人工 review 再 commit。

## Dashboard 使用情境

```bash
# 开着 dashboard 跑 feature，实时看 log
4x live -w &
4x run F001 --runner claude

# 从 dashboard 直接启动 feature（不用开 terminal）
# POST /api/run 搭配 web UI

# 多项目监控
4x live /path/to/project-a /path/to/project-b -w
```

## Locale 设置

让 AI 用你的语言回应：

```bash
4x config set locale zh-TW
```

也可以不设——会自动从 `LANG` 环境变量推断。

## 故障排除

### Feature 卡在 needs-attention

代表某个 phase 缺少必要的 artifact（例如设计者没产出 task-brief.md）。

```bash
4x status F001          # 看缺什么
4x check F001           # 跑完整检查
```

手动补文件或重跑该 phase：

```bash
4x transition F001 --to designing
4x run F001
```

### Feature 卡在 blocked

通常是 runner exit code 1（soft failure）。看 log：

```bash
ls .4x/F001/logs/
cat .4x/F001/logs/round-1-coder.log
```

解决后推回：

```bash
4x transition F001 --to coding
4x run F001
```

### Dependency gate 挡住

```
blocked: F001-user-model is not done (status: coding)
```

先完成被依赖的 feature，或手动标记：

```bash
4x done F001
4x run F002
```
