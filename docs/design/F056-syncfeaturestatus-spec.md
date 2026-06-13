# F056: syncFeatureStatus 統一為 Workspace 方法

## 現狀

Feature status 同步邏輯在兩處獨立實作：
- `cmd/4x/transition.go` 的 `syncFeatureStatus()` — LoadFeature → PhaseToStatus → SaveFeature
- `internal/server/server.go` 的 `transitionDone()` — 直接呼叫 PhaseToStatus + SaveFeature

未來新增 side effect（webhook、completion timestamp）時需改兩處。

## 設計

在 `internal/protocol/workspace.go` 新增方法：

```go
// SyncFeatureStatus 將 feature YAML 的 Status 欄位同步為對應 phase 的狀態
func (w *Workspace) SyncFeatureStatus(featureID string, phase Phase) error {
    f, err := w.LoadFeature(featureID)
    if err != nil {
        return fmt.Errorf("sync feature status: load: %w", err)
    }
    f.Status = PhaseToStatus(phase)
    if err := w.SaveFeature(f); err != nil {
        return fmt.Errorf("sync feature status: save: %w", err)
    }
    return nil
}
```

### 呼叫端改動

| 檔案 | 改動 |
|---|---|
| `cmd/4x/transition.go` | 刪除 `syncFeatureStatus` helper；行 109 改為 `ws.SyncFeatureStatus(featureID, phase)` |
| `cmd/4x/run.go` | 約 8 處 `syncFeatureStatus(ws, ...)` → `ws.SyncFeatureStatus(...)` |
| `cmd/4x/done.go` | 行 93 改為 `ws.SyncFeatureStatus(...)` |
| `internal/server/server.go` | `transitionDone()` 裡把 `f.Status = PhaseToStatus(...); ws.SaveFeature(f)` 改為 `ws.SyncFeatureStatus(featureID, PhaseDone)` |

## 約束

- `SyncFeatureStatus` 不處理 state.json（那是 `WriteState` 的責任）
- 不改 `PhaseToStatus` 的映射邏輯
- `transitionDone` 其他邏輯（WriteState、AppendEvent）不動
