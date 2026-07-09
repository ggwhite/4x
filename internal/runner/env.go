package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ggwhite/4x/internal/envutil"
)

// resolveCommand 在 enriched PATH 中查找 command 的完整路徑。
// exec.Command 用當前 process 的 PATH 查找，但 GUI app 的 PATH 很精簡，
// 所以需要先用 enriched PATH 手動 resolve。
func resolveCommand(command string, env []string) string {
	if filepath.IsAbs(command) {
		return command
	}
	if p, err := exec.LookPath(command); err == nil {
		return p
	}
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			pathVal := e[5:]
			sep := ":"
			if runtime.GOOS == "windows" {
				sep = ";"
			}
			for _, dir := range strings.Split(pathVal, sep) {
				candidate := filepath.Join(dir, command)
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate
				}
			}
		}
	}
	return command
}

// enrichEnv 對傳入的 base env（已過濾的 kept）補上 GUI app 啟動時缺少的 PATH 路徑，
// 並將 4x 自身的 exe 目錄 prepend 到 PATH 最前面、設定 FOURX_BIN。
// FOURX_BIN 由 4x 自注入，永遠不受 denylist 過濾影響。
func enrichEnv(base []string) []string {
	env := envutil.EnrichEnv(base)

	if exe, err := os.Executable(); err == nil {
		env = envutil.PrependPath(env, filepath.Dir(exe))
		env = append(env, "FOURX_BIN="+exe)
	}

	return env
}

// enrichedEnv 對 os.Environ() 做 enrich（未經過濾），供既有非 spawn 路徑與回歸測試使用。
func enrichedEnv() []string {
	return enrichEnv(os.Environ())
}
