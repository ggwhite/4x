package gitops

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// selfManagedPathspecs 回傳「4x 自己在 pipeline 期間會寫入主工作區」的路徑清單，
// 供 git 子命令當 pathspec 使用（`git ... -- <pathspec>...`）。
//
// 這是 4x 自管路徑的單一來源：merge 前置 commit（commitSelfManaged）與髒檔偵測
// （dirtySelfManagedPaths）都只認這三條，其餘一律視為使用者的變更。
// 全部由 internal/protocol 的常量組出，不寫死 ".4x" 字面值。
func selfManagedPathspecs() []string {
	return []string{
		filepath.Join(protocol.DirName, protocol.FeaturesDir),
		filepath.Join(protocol.DirName, protocol.LearningsFile),
		filepath.Join(protocol.DirName, protocol.LearningsContextFile),
	}
}

// isFeatureYAMLPath 判斷 repo 相對路徑 rel 是否為 .4x/features/ 底下的 feature YAML。
func isFeatureYAMLPath(rel string) bool {
	prefix := protocol.DirName + "/" + protocol.FeaturesDir + "/"
	return strings.HasPrefix(rel, prefix) && strings.HasSuffix(rel, ".yaml")
}

// isSelfManagedPath 判斷 repo 相對路徑 rel 是否屬於 4x 自管路徑，
// 即 feature YAML、.4x/learnings.json、.4x/learnings-context.md 三者之一。
func isSelfManagedPath(rel string) bool {
	if isFeatureYAMLPath(rel) {
		return true
	}
	return rel == filepath.Join(protocol.DirName, protocol.LearningsFile) ||
		rel == filepath.Join(protocol.DirName, protocol.LearningsContextFile)
}

// dirtySelfManagedPaths 回傳 root 內目前有 tracked 未 commit 變更的 4x 自管路徑，
// 順序與 git status 輸出一致。root 非 git repo 或 git 執行失敗時回傳 nil。
//
// 只收 tracked 變更：untracked（"??"）本來就不會被 preflight 擋、也不該被 4x 擅自 commit。
// rename（含 " -> "）與含特殊字元的 quoted path（以 '"' 開頭）保守跳過，不做解析。
func dirtySelfManagedPaths(root string) []string {
	args := append([]string{"-C", root, "status", "--porcelain", "--"}, selfManagedPathspecs()...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil
	}
	// 不可先 TrimSpace 整段輸出：狀態碼佔前兩欄，未 staged 的修改是 " M path" 這種
	// 以空白開頭的行，整段 trim 會吃掉第一行的前導空白，導致下方固定位移切錯路徑。
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) <= 3 || strings.HasPrefix(line, "??") {
			continue
		}
		rel := line[3:]
		if strings.Contains(rel, " -> ") || strings.HasPrefix(rel, `"`) {
			continue
		}
		if isSelfManagedPath(rel) {
			paths = append(paths, rel)
		}
	}
	return paths
}

// commitSelfManaged 把 root 內髒掉的 4x 自管路徑以 path-scoped 方式 commit 掉，
// 回傳是否真的產生 commit。無髒路徑時不執行任何 git 指令、直接回 false。
//
// 呼叫時機是 merge --squash 的 preflight 之前（見 monorepo.go / multirepo.go 的 Merge）。
// 目的有二：一是讓 pipeline 期間 4x 自己寫入主工作區的狀態不再擋住 preflight，
// 二是讓 merge 失敗路徑的 reset --hard HEAD 不會把剛收割的 retro learnings 抹掉。
//
// add/commit 一律帶 "--" 與明確路徑清單，不可改用涵蓋整個 working tree 的 staging
// 或 commit 選項——否則會把使用者自己 staged 的變更一併帶進這個 commit。
//
// 失敗後果（可接受）：merge 之後若發生衝突或錯誤，這個 commit 已經存在於 main。
// 它的內容只有 4x 自身的 pipeline 狀態（feature YAML、learnings），不含任何原始碼、
// 也不是半套的 feature merge，main 停在該 commit 上仍是一致狀態；
// 使用者解完衝突後重跑 `4x merge` 即可繼續。
func commitSelfManaged(root, featureID string) bool {
	paths := dirtySelfManagedPaths(root)
	if len(paths) == 0 {
		return false
	}

	addArgs := append([]string{"-C", root, "add", "--"}, paths...)
	if out, err := exec.Command("git", addArgs...).CombinedOutput(); err != nil {
		slog.Warn("git add self-managed paths failed", "root", root, "output", strings.TrimSpace(string(out)), "error", err)
		return false
	}

	msg := fmt.Sprintf("chore(%s): 4x pipeline state", featureID)
	commitArgs := append([]string{"-C", root, "commit", "-m", msg, "--"}, paths...)
	if out, err := exec.Command("git", commitArgs...).CombinedOutput(); err != nil {
		slog.Warn("git commit self-managed paths failed", "root", root, "output", strings.TrimSpace(string(out)), "error", err)
		return false
	}
	return true
}
