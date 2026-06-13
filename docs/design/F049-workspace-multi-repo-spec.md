# F049: Workspace Multi-Repo — Design Spec

## Problem

4x 的 git 操作層假設 monorepo：`setupWorktree` 一次建、`commitWorktree` 一次 commit、`worktree.Merge` 一次 merge、`detectChangedRepos` 從 root 跑一次 `git diff`。對 multi-repo workspace（workspace root 下多個獨立 .git repo，用 go.work 串接）完全無法運作。

這不只是 Kairos 的問題——微服務 polyrepo、Go workspace、monorepo 外掛獨立 repo 都是常見場景。

## Decision Record

| 問題 | 決定 |
|------|------|
| Batch mode 是否一起做 | 是，batch 也支援 multi-repo worktree isolation |
| Feature.Repos schema | `map[string]string` → `[]string`（repo name only），path 集中在 workspace config |
| 未涉及的 repo | 全部建 worktree，確保 go.work 等能編譯 |
| Merge 策略 | All-or-nothing，任一 repo 衝突全部 abort |
| Monorepo 行為 | 無 workspace config = monorepo，零設定成本 |
| Workspace-level 檔案 | 複製 workspace root 下所有非 repo 子目錄的檔案 |
| 架構方案 | 方案 A：新增 `internal/gitops` package 封裝 git 操作 |
| Breaking change | 直接改，不向下相容 Feature.Repos 舊格式 |

## Config Model

### WorkspaceConfig（新增到 types.go）

```go
type WorkspaceConfig struct {
    Repos map[string]RepoConfig `json:"repos,omitempty"`
}

type RepoConfig struct {
    Path string `json:"path"`
    Hub  bool   `json:"hub,omitempty"`
}
```

加到 `Config`：

```go
type Config struct {
    // ... existing fields ...
    Workspace WorkspaceConfig `json:"workspace,omitempty"`
}
```

偵測邏輯：`len(cfg.Workspace.Repos) > 0` → multi-repo，否則 monorepo。

### settings.json 範例

```json
{
  "workspace": {
    "repos": {
      "kairos-core": { "path": "kairos-core", "hub": true },
      "kairos-gate": { "path": "kairos-gate" },
      "kairos-admin": { "path": "kairos-admin" },
      "kairos-game": { "path": "kairos-game" },
      "kairos-service": { "path": "kairos-service" },
      "kairos-payment": { "path": "kairos-payment" },
      "kairos-web": { "path": "kairos-web" },
      "kairos-e2e": { "path": "kairos-e2e" }
    }
  }
}
```

### Feature.Repos Schema 變更

```go
// Before
Repos map[string]string `yaml:"repos,omitempty" json:"repos,omitempty"`

// After
Repos []string `yaml:"repos,omitempty" json:"repos,omitempty"`
```

Feature YAML：

```yaml
# Before
repos:
  kairos-core: kairos-core
  kairos-gate: kairos-gate

# After
repos:
  - kairos-core
  - kairos-gate
```

### HubRepos 整合

現有 `Config.HubRepos []string` 保留。workspace config 的 `Hub: true` 會 merge 進 `HubRepos`。batch clustering 合併兩個來源。

### 路徑解析

```go
// ResolveRepoPaths 從 workspace config 解析 repo name → absolute path。
// monorepo 模式回傳 {"." : root}。
func ResolveRepoPaths(cfg Config, root string) map[string]string

// ResolveFeatureRepoPaths 解析 feature 涉及的 repo name → path。
// feature.Repos 為空時：
//   multi-repo → 回傳 workspace config 裡所有 repo（feature 未限定 scope）
//   monorepo → 回傳 {".": root}
func ResolveFeatureRepoPaths(feature Feature, cfg Config, root string) map[string]string
```

## Architecture: `internal/gitops` Package

### Interface

```go
package gitops

type Ops interface {
    SetupWorktree(featureID string) (wtRoot string, err error)
    Commit(wtRoot, featureID, featureName string, round int) error
    Merge(featureID, featureName string) MergeResult
    Cleanup(featureID string) error
    DetectChangedRepos() []string
    CaptureBaseline(featureID string, featureRepos []string) error
    IsMultiRepo() bool
}

func New(root string, ws *protocol.Workspace, cfg protocol.Config) Ops
```

Factory 根據 `len(cfg.Workspace.Repos) > 0` 回傳 `monoRepo` 或 `multiRepo`。

### monoRepo 實作

從現有程式碼搬入，行為不變：

| 方法 | 來源 |
|------|------|
| `SetupWorktree` | `run.go:setupWorktree()` |
| `Commit` | `run.go:commitWorktree()` |
| `Merge` | `worktree/merge.go:Merge()` |
| `Cleanup` | `worktree/merge.go:Cleanup()` |
| `DetectChangedRepos` | `guard/check.go:detectChangedRepos()` |
| `CaptureBaseline` | `guard/check.go:CaptureBaseline()` |

### multiRepo 實作

#### SetupWorktree

1. 建立組合目錄 `.worktrees/4x/<featureID>/`
2. 對 workspace config 裡的**所有** repo：
   `git -C <repo-path> worktree add .worktrees/4x/<featureID>/<repo-name> -b 4x/<featureID>`
3. 複製 workspace root 下所有非 repo 子目錄的檔案（go.work、Makefile、docker-compose.yml 等）
4. 複製 `.4x/` 目錄（settings.json + plugins/）

組合目錄結構：

```
.worktrees/4x/<featureID>/
├── .4x/             ← 從 main 複製
├── kairos-core/     ← git worktree
├── kairos-gate/     ← git worktree
├── kairos-admin/    ← git worktree
├── go.work          ← 從 workspace root 複製
└── Makefile         ← 從 workspace root 複製
```

#### Commit

只對 feature.Repos 列出的 repo 執行：

```
for each repo in feature.Repos:
    git -C <wtRoot>/<repo-name> add -A
    if has staged changes:
        git -C <wtRoot>/<repo-name> commit -m "wip(<featureID>): round N"
```

未涉及的 repo 不 commit（worktree 純粹為了編譯）。

#### Merge（all-or-nothing）

```
1. 對每個有 worktree 的 repo，記錄 merge 前的 HEAD
2. 依序 merge：
   git -C <repo-path> merge --no-ff -m "..." 4x/<featureID>
3. 任一失敗：
   - abort 失敗的 repo
   - 對已成功的 repo：git -C <repo-path> reset --hard <pre-merge-HEAD>
   - 回傳 MergeResult{Conflict: true, ConflictRepo: name, Files: [...]}
4. 全部成功 → cleanup
```

#### Cleanup

對每個有 worktree 的 repo：

```
git -C <repo-path> worktree remove <wtDir>/<repo-name>
git -C <repo-path> branch -D 4x/<featureID>
```

移除組合目錄。

#### DetectChangedRepos

```
for each repo in workspace config:
    git -C <repo-path> diff --name-only HEAD
    if output not empty → repo name 加入結果
```

#### CaptureBaseline

```
for each repo in featureRepos (resolved to path):
    git rev-parse HEAD
    git rev-parse --abbrev-ref HEAD
    git status --short
    → BaselineRepo{Name, Path, Branch, Head, DirtyFiles}
```

### MergeResult 擴展

```go
type MergeResult struct {
    Skipped      bool
    Conflict     bool
    Error        string
    Files        []string
    ConflictRepo string   // multi-repo 時標記衝突的 repo
}
```

## Scope Guard 整合

### checkScope

`guard.Check` 新增 `ops gitops.Ops` 參數：

```go
func Check(ws *protocol.Workspace, featureID string, ops gitops.Ops) CheckResult
```

`checkScope` 內部用 `ops.DetectChangedRepos()` 取代自己呼叫 `git diff`。

`allowedRepos` 從 `feature.Repos`（`[]string`）建 set。

### CaptureBaseline

降級為 `gitops` 內部使用。`run.go` 改呼叫 `ops.CaptureBaseline(featureID, feature.Repos)`。

### checkBaseline

不改。只讀 `baseline.json`，跟 git 操作無關。

## Prompt 與 Template

### Designer template

```tmpl
{{- if .Feature.Repos}}
Repos:
{{- range .Feature.Repos}}
  - {{.}}
{{- end}}
{{- end}}
```

### Coder/Tester prompt 注入

`promptData` 新增 `RepoMap map[string]string`（multi-repo 時從 workspace config 填入）。

Coder template：

```tmpl
{{- if .RepoMap}}
## Workspace Repos
{{- range $name, $path := .RepoMap}}
- `{{$name}}` → `{{$path}}/`
{{- end}}
{{- end}}
```

### Commit plan per-repo

Acceptor template 加指引，multi-repo 時 commit-plan.md 分 repo 列出 commit message。

## Batch Mode 整合

### batch.go worktree 支援

```go
ops := gitops.New(ws.Root, ws, cfg)

if cfg.Isolation == "worktree" {
    wtPath, err := ops.SetupWorktree(featureID)
    runnerWs = &protocol.Workspace{Root: wtPath}
}
```

跟 `run.go` 的 pattern 一致。

### Batch clustering

`mergeBySharedRepos` 從 `range feature.Repos`（map key）改為 `[]string` 遍歷。

Hub repos 合併來源：`cfg.HubRepos` + workspace config 裡 `Hub: true` 的 repo。

## 呼叫端改動摘要

| 檔案 | 改動 |
|------|------|
| `cmd/4x/run.go` | `setupWorktree` → `ops.SetupWorktree`；`commitWorktree` → `ops.Commit`；`repoPathsFromFeature` → `ops.CaptureBaseline`；`guard.Check` 傳入 ops |
| `cmd/4x/batch.go` | 新增 worktree isolation 邏輯（同 run.go pattern） |
| `cmd/4x/done.go` | `worktree.Merge` → `ops.Merge` |
| `cmd/4x/merge.go` | `worktree.Merge` → `ops.Merge` |
| `internal/guard/check.go` | `Check` 新增 ops 參數；`checkScope` 用 ops；`CaptureBaseline` + `detectChangedRepos` 移至 gitops |
| `internal/batch/group.go` | `mergeBySharedRepos` 適配 `[]string` |
| `internal/protocol/types.go` | 新增 `WorkspaceConfig`/`RepoConfig`；`Feature.Repos` 改型別 |
| `templates/designer.md.tmpl` | 適配 `[]string` |
| `templates/coder.md.tmpl` | 新增 RepoMap section |

## 不動的部分

| 元件 | 理由 |
|------|------|
| State machine | 不涉及 git 操作 |
| Protocol 檔案格式 | .4x/ 目錄結構不變 |
| Runner interface | 不涉及 git 操作 |
| SSE/REST server | 不涉及 git 操作 |
| Dashboard | 不涉及 git 操作 |
| syncFeatureToWorktree / FromWorktree | 已經是純檔案複製 |
| startLiveSync | 已經是純檔案複製 |
| checkBaseline | 只讀 baseline.json |

## Verification

```bash
go build ./cmd/4x && go vet ./... && go test ./...
```

新增測試：

- `internal/gitops/monorepo_test.go` — 驗證搬遷後行為不變
- `internal/gitops/multirepo_test.go` — per-repo worktree/commit/merge/cleanup/scope
- `internal/batch/group_test.go` — 更新現有 test fixtures 適配 `[]string`
- `internal/guard/check_test.go` — 更新 scope check test 傳入 ops
