package gitops

import (
	"path/filepath"
	"strconv"
	"strings"
)

// filterGoWorkUses 解析 go.work 內容，只保留 keep(rel) 回傳 true 的 use 目標，
// 保留原始的 go / toolchain 版本行。回傳裁切後內容與是否還有任何 use 保留（anyKept）。
// 支援單行 `use ./x` 與區塊 `use (\n  ./a\n  ./b\n)` 兩種語法，統一輸出為單行 `use` 形式。
// keep 收到的 rel 為 filepath.Clean 後的相對路徑（去引號）。
func filterGoWorkUses(src string, keep func(rel string) bool) (out string, anyKept bool) {
	lines := strings.Split(src, "\n")
	var result []string
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inBlock {
			if trimmed == ")" {
				inBlock = false
				continue
			}
			if trimmed == "" {
				continue
			}
			path := unquoteGoWorkPath(trimmed)
			if keep(filepath.Clean(path)) {
				result = append(result, "use "+path)
				anyKept = true
			}
			continue
		}

		// 區塊起始：`use (` 或 `use(`。
		if strings.HasPrefix(trimmed, "use") && strings.HasSuffix(trimmed, "(") {
			inBlock = true
			continue
		}

		// 單行 use：`use ./x`。
		if strings.HasPrefix(trimmed, "use ") {
			path := unquoteGoWorkPath(strings.TrimSpace(strings.TrimPrefix(trimmed, "use ")))
			if keep(filepath.Clean(path)) {
				result = append(result, "use "+path)
				anyKept = true
			}
			continue
		}

		// 非 use 行（go / toolchain / 註解 / 空行）原樣保留。
		result = append(result, line)
	}

	return strings.Join(result, "\n"), anyKept
}

// unquoteGoWorkPath 去除 go.work use 路徑外層的雙引號（含空白的路徑會被引號包住）。
func unquoteGoWorkPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if uq, err := strconv.Unquote(s); err == nil {
			return uq
		}
		return s[1 : len(s)-1]
	}
	return s
}

// isGoWorkFile 回報根層檔名是否由 copyGoWork 專責管轄。
//
// 這份清單是 SoT，兩個消費端共用：copyWorkspaceFiles 據它跳過複製（worktree 內的 go.work
// 要的是裁切版），ValidateSharedPathsInRoot 據它拒絕宣告（merge-back 會用裁切版覆寫完整版）。
// 後者的存在完全依附於前者，寫成兩份字面值會讓 copyGoWork 日後擴充管轄範圍時放行一個
// 會被裁切覆寫的宣告。
func isGoWorkFile(name string) bool {
	return name == "go.work" || name == "go.work.sum"
}
