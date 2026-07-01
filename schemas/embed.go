package schemas

import "embed"

// FS 嵌入 schemas/ 內所有 JSON Schema 檔，供 internal/schemasync 等 package 讀取，
// 不需依賴檔案系統相對路徑（避免 //go:embed ../.. 的限制）。
//
//go:embed *.schema.json
var FS embed.FS
