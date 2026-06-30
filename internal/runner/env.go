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

// enrichedEnv 回傳加強版的環境變數，補上 GUI app 啟動時缺少的 PATH 路徑，
// 並將 4x 自身的 exe 目錄 prepend 到 PATH 最前面、設定 FOURX_BIN。
func enrichedEnv() []string {
	env := envutil.EnrichedEnv()

	if exe, err := os.Executable(); err == nil {
		env = envutil.PrependPath(env, filepath.Dir(exe))
		env = append(env, "FOURX_BIN="+exe)
	}

	return env
}
