// Package pathutil 收斂原本散在 cmd/4x/check.go、internal/server/screenshots.go、
// internal/protocol/design_doc.go 三處近似的 path-containment 判定，避免安全敏感邊界
// （尾斜線／「.」／根路徑本身）在各處實作間悄悄分歧（見 docs/reference/discovered-feature-gaps.md
// 的 F170 條目）。
package pathutil

import (
	"path/filepath"
	"strings"
)

// WithinRoot 判斷兩個「已解析過 symlink」的絕對路徑中，target 是否位於 root 之內。
// target == root 本身也算「在其內」——呼叫端若要排除根路徑自身（如 cmd/4x/check.go 的
// guard 語意：不允許把整個 root 當成寫入目標），在呼叫端額外加一個 != 判斷即可，語意
// 更貼近其呼叫端而非塞進共用函式。
//
// 本函式只做字串層級的 containment 比對，不負責解析 symlink：呼叫端各自需要的解析策略
// 不同（RealpathBestEffort 可容忍 target 尚未存在；screenshots／design_doc 兩處已假設
// target 存在、用 filepath.EvalSymlinks 即可），刻意不在此耦合。
func WithinRoot(resolvedRoot, resolvedTarget string) bool {
	if resolvedTarget == resolvedRoot {
		return true
	}
	return strings.HasPrefix(resolvedTarget, resolvedRoot+string(filepath.Separator))
}

// RealpathBestEffort 解析 path 的 symlink；path 本身不存在時（如尚未建立的寫入目標檔），
// 解析其存在的最長祖先目錄再接回剩餘相對段，仍能消除路徑前段的 symlink 差異。
func RealpathBestEffort(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir := path
	var tail []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return path // 走到根仍無法解析
		}
		tail = append([]string{filepath.Base(dir)}, tail...)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, tail...)...)
		}
	}
}
