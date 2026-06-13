# F054: Design doc resolution unification

## 現狀

設計文件（spec/plan）的解析邏輯在兩處獨立實作：

| | server `resolveDoc` | prompt `designDocPath` |
|---|---|---|
| YAML spec/plan 欄位 | ✅ 當 path 讀取 | ❌ 不看 |
| docs/design/ fallback | `{id}-{type}.md` | `{id}{suffix}` |
| strip prefix fallback | ❌ | ✅ 去掉 `FNNN-` 再找 |
| 回傳值 | (content, source) | path only |

新增來源時要改兩處，行為也不一致。

## 設計

在 `protocol` package 新增統一函式：

```go
type DesignDoc struct {
    Content string // 檔案內容，找不到時為空字串
    Source  string // 相對路徑，空表示找不到
}

func ResolveDesignDoc(root string, feature Feature, docType string) DesignDoc
```

解析優先序：
1. Feature YAML 的 `spec`/`plan` 欄位（非空時當 path 讀取）
2. `docs/design/{featureID}-{docType}.md`
3. `docs/design/{slug}-{docType}.md`（strip `FNNN-` prefix）
4. 都找不到 → 空 DesignDoc

`docType` 為 `"spec"` 或 `"plan"`。

## 呼叫端改動

- `server.go`：刪除 `resolveDoc()`，改呼叫 `protocol.ResolveDesignDoc()`
- `prompt.go`：刪除 `designDocPath()`、`loadPlanningDocs()`、`stripFeaturePrefix()`，改呼叫 `protocol.ResolveDesignDoc()`

## 實作位置

| 動作 | 檔案 | 內容 |
|---|---|---|
| 新增 | `internal/protocol/design_doc.go` | `DesignDoc` struct、`ResolveDesignDoc()` 函式、`stripFeaturePrefix()` |
| 新增 | `internal/protocol/design_doc_test.go` | 優先序測試 |
| 修改 | `internal/server/server.go` | 刪 `resolveDoc()`，改用 `protocol.ResolveDesignDoc()` |
| 修改 | `cmd/4x/prompt.go` | 刪 `designDocPath()`/`loadPlanningDocs()`/`stripFeaturePrefix()`，改用 `protocol.ResolveDesignDoc()` |

## 約束

- 不改 feature YAML schema
- 不改 `docs/design/` 的檔案命名慣例
