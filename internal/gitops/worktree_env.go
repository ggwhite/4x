package gitops

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/envutil"
)

// ApplyWorktreeEnv 對「在 root 執行子程序」的環境變數注入 worktree 隔離設定。
// root 非 linked worktree（主工作區、非 git 目錄）時原樣回傳，行為不變。
//
// linked worktree 時：
//   - GOLANGCI_LINT_CACHE 指向 worktree-local 目錄，避免 golangci-lint 快取殘留
//     sibling worktree 路徑造成跨 worktree 的 lint 誤報。GOCACHE 刻意不隔離：
//     go build cache 是 content-addressed、與路徑無關，共享可加速 worktree build。
//   - worktree 內存在可執行的 bin/4x 時（4x 自我開發情境），FOURX_BIN 覆寫為該
//     worktree-local binary 並把其目錄 prepend 到 PATH，讓 guard-tool hook 與
//     agent 手測用的都是 feature branch 剛 build 的版本，而非主工作區的舊 binary。
//     os/exec 對重複 env key 以最後一筆為準，故以 append 覆寫先前的 FOURX_BIN。
func ApplyWorktreeEnv(env []string, root string) []string {
	info, ok := DetectWorktree(root)
	if !ok || !info.IsLinked {
		return env
	}

	env = append(env, "GOLANGCI_LINT_CACHE="+filepath.Join(info.Root, ".cache", "golangci-lint"))

	bin := filepath.Join(info.Root, "bin", "4x")
	if st, err := os.Stat(bin); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
		env = append(env, "FOURX_BIN="+bin)
		env = envutil.PrependPath(env, filepath.Dir(bin))
		slog.Info("worktree env: FOURX_BIN pinned to worktree-local binary", "bin", bin)
	}
	return env
}
