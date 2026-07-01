# Schema 單一事實來源同步機制

> 相關 feature：F122

## SoT 模型

Go struct 是唯一的單一事實來源（SoT）——使用最廣、註解最完整、含自訂型別與方法。
`schemas/{state,event,feature}.schema.json` 與 `dashboard/web/core.js` 皆為其鏡像，
以測試驗證一致性，**不**由 Go struct 反向生成 schema（避免覆蓋手工調校的
`omitempty`、自訂型別、GoDoc、`description`/`format`/`pattern` 等手寫語意）。

SoT 分兩個獨立維度：

- **欄位集合 SoT** = struct reflection。讀取 struct 的 `json:"..."` tag，
  `json:"-"` 視為不輸出、跳過；由 `internal/schemasync.StructJSONFields` 實作。
- **列舉集合 SoT** = Go 新增的 exported accessor（`AllPhases()` 等）。
  因 domain 常量常含非持久化值（如 `RoleGate`、sub-role），schema/驗證白名單
  不能直接列舉常量宣告，需由 accessor 明確界定哪些值可實際出現在
  `state.json` / event / feature。

## Accessor（enum SoT）

| Accessor | 位置 | 涵蓋範圍 |
|---|---|---|
| `AllPhases()` | `internal/protocol/enums.go` | 可持久化於 `state.json.phase` 的全部 15 個 Phase |
| `AllRoles()` | `internal/protocol/enums.go` | 可出現在 `state.json.role` 的 8 個 pipeline 角色（不含 sub-role/非 pipeline role） |
| `AllSubPhases()` | `internal/protocol/enums.go` | `deep-reviewing` phase 內的 4 個 SubPhase |
| `AllEventTypes()` | `internal/protocol/enums.go` | production 實際發出的 15 個 event type（以全庫 grep 為準，非現有 schema） |
| `AllNotifyLevels()` | `internal/protocol/enums.go` | `Event.Notify` 的 3 個合法值 |
| `AllStatuses()` | `internal/feature/enums.go` | `feature.Status` 的全部 8 個常量 |
| `AllSubtaskStatuses()` | `internal/feature/enums.go` | `Subtask.Status` 的 5 個合法值（含 `ready-for-review`） |

這些 accessor 同時被 Go 內部驗證白名單消費（`internal/feature/validate.go` 的
`validStatuses`/`validSubtaskStatuses`、`cmd/4x/subtask.go` 的 `validStatuses`），
消除 Go 內部平行清單，使 accessor 是真正的 SoT，而非只有測試在讀的死碼。

## 驗證機制

`internal/schemasync/` 提供純 Go（stdlib `reflect`/`encoding/json`/`embed`，
無第三方 codegen 依賴）驗證：

- `StructJSONFields(v any) []string` — reflect 擷取 struct 的 JSON 欄位名。
- `SchemaProperties(schemaBytes []byte) (fields []string, enums map[string][]string, err error)`
  — 解析 JSON Schema `properties` 的 key 集合與各欄位的 `enum` 值（含
  `subtasks.items.properties.*` 巢狀，key 格式為 `"<欄位名>.items.<子欄位名>"`）。
- `schemas/embed.go` 用 `//go:embed *.schema.json` 匯出 `schemas.FS`，供
  `internal/schemasync` 讀取 schema bytes（`schemas/` 在 repo root，無法用
  `//go:embed ../..` 跨層引用）。

`internal/schemasync/sync_test.go` 對三個 schema 各跑一個一致性測試
（`TestStateSchemaMatchesStruct`／`TestEventSchemaMatchesStruct`／
`TestFeatureSchemaMatchesStruct`），比較一律用集合相等/子集語意，drift 時
FAIL 並列出缺/多項欄位或 enum 值。另有 `TestDashboardMapsCoverCanonical`
讀取 `dashboard/web/core.js` 文字，驗證 `PHASE_ICON`/`ROLES` 物件的 key
集合分別覆蓋 `AllPhases()`/`AllRoles()`（單向子集：core.js 允許有 canonical
集合以外的額外 key，例如 sub-role 用的 `deep-fix`/`deep-reverify`/`synthesizer`）。

## 維護步驟

新增/修改欄位或列舉時：

1. 改 Go struct（欄位）或對應 accessor（列舉），必要時同步 `internal/state/machine.go`。
2. 補 `schemas/{state,event,feature}.schema.json` 的 `properties`/`enum`，
   型別與 `description` 沿用既有風格。
3. 若新增/移除 `AllPhases()`／`AllRoles()` 的值，檢查 `dashboard/web/core.js`
   的 `PHASE_ICON`／`ROLES` 是否需要補上對應 key（避免 dashboard 靜默缺渲染）。
4. 執行 `make check-schema-sync` 確認全綠。

## 已知邊界

- 不涵蓋 `internal/mcp/tools.go` 的 struct-tag jsonschema 描述字串（編譯期
  tag 無法由 accessor 生成），維護時需人工確認與 `AllSubtaskStatuses()` 等一致。
- 不強制 event `type` 在 handler 執行期驗證，僅 schema/accessor 層對齊。
