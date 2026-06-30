# Coder Report — Round 1

## What Was Done

實作 F119: learning effectiveness tracking — 追蹤 learning 注入後是否減少同類問題，標記無效 learning 供 review。

### Task 1: Entry 新增欄位
- `ActivatedAt time.Time` 欄位，記錄 candidate→active 升級時間
- `Ineffective bool` 欄位，正交於 status 的標記
- Harvest() 跨 feature 升級時設定 ActivatedAt
- PromoteCandidates() 升級時設定 ActivatedAt

### Task 2: MarkIneffective 方法
- 掃描所有 active 條目，三條件全滿足才標記：
  1. UsedCount >= 3
  2. ActivatedAt（零值時 fallback 到 CreatedAt）距今 > 30 天
  3. 最近 3 個來自不同 feature 的 entries 中有同 category 的 learning
- 回傳新標記數，已標記的不重複計算

### Task 3: HarvestLearnings 整合
- 在 harvest 完成後呼叫 MarkIneffective()
- 若有新增或新標記的 ineffective entries 才 save
- 不影響原有的「無 harvest 不寫檔」行為

### Task 4: CLI
- `learn list --ineffective`：只列出 Ineffective == true 的條目
- ineffective 的 active entry 在 STATUS 欄顯示 `active!`

### Task 5: 測試
- TestMarkIneffective_MeetsAllConditions
- TestMarkIneffective_NotEnoughUsage
- TestMarkIneffective_TooRecent
- TestMarkIneffective_NoCategoryContinuation
- TestMarkIneffective_FallsBackToCreatedAt
- TestMarkIneffective_AlreadyMarked
- TestHarvest_CrossFeaturePromotion_SetsActivatedAt
- PromoteCandidates ActivatedAt 驗證

### 附帶修正
- 註冊 pre-existing 的 force_done command（修復 lint unused error）
- 修正 force_done.go 的 gofmt 格式問題
- 新增 force-done 文件至 docs/guide/cli.md

## Files Changed

- `internal/learning/store.go` — 新增 ActivatedAt、Ineffective 欄位，MarkIneffective() 方法，PromoteCandidates 設定 ActivatedAt
- `internal/learning/store_test.go` — 8 個新測試覆蓋 MarkIneffective 所有條件
- `internal/prompt/learnings.go` — HarvestLearnings 整合 MarkIneffective 呼叫
- `cmd/4x/learn.go` — learn list 新增 --ineffective flag 和 active! 顯示
- `cmd/4x/main.go` — 註冊 force_done command
- `cmd/4x/force_done.go` — gofmt 格式修正
- `docs/guide/cli.md` — 新增 --ineffective flag 說明、force-done 命令文件
- `docs/guide/{es,ja,ko,zh-CN,zh-TW}/cli.md` — 各語系同步更新

## Verification

- `make build`: OK
- `make lint`: 0 issues
- `make test` (go test -race ./...): 1373 passed in 23 packages
- `make check-docs`: OK (27 commands documented)
- `make check-i18n`: OK
- `make check-guide-i18n`: OK
