# F050: Config merge pattern deduplication

## 現狀

`ReadConfig` + `ReadUserConfig` + `MergeConfig` 這組三行 boilerplate 在 7 處重複出現：

| 檔案 | 行號 |
|---|---|
| `cmd/4x/run.go` | 65-77 |
| `cmd/4x/batch.go` | 49-51 |
| `cmd/4x/batch.go` | 200-207 |
| `cmd/4x/prompt.go` | 55-59 |
| `cmd/4x/status.go` | 292-297 |
| `internal/server/server.go` | 677-682 |
| `internal/server/server.go` | 1180-1188 |

每處的 pattern 相同：讀 project config → 讀 user config → merge，user config 失敗印 warning 不中斷。

## 需求

在 `Workspace` 新增 `LoadMergedConfig()` method，封裝這三行。將 7 處呼叫改用此 method。

## 設計

```go
func (w *Workspace) LoadMergedConfig() (Config, error) {
    cfg, err := w.ReadConfig()
    if err != nil {
        return Config{}, err
    }
    if userCfg, err := ReadUserConfig(); err != nil {
        fmt.Fprintf(os.Stderr, "warning: failed to read user config: %v\n", err)
    } else {
        cfg = MergeConfig(userCfg, cfg)
    }
    return cfg, nil
}
```

## 約束

- 不改 `ReadConfig`、`ReadUserConfig`、`MergeConfig` 的 signature
- 不引入 caching 機制
- 錯誤處理保持一致：user config 讀取失敗印 warning 但不中斷
