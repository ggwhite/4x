package runner

import (
	"os"
	"runtime"
	"strings"
)

// enrichedEnv 回傳加強版的環境變數，補上 macOS GUI app 啟動時缺少的 PATH 路徑。
func enrichedEnv() []string {
	env := os.Environ()
	if runtime.GOOS != "darwin" {
		return env
	}

	extraPaths := []string{
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		os.ExpandEnv("$HOME/.local/bin"),
		os.ExpandEnv("$HOME/.cargo/bin"),
		"/Applications/cmux.app/Contents/Resources/bin",
	}

	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			current := strings.TrimPrefix(e, "PATH=")
			parts := strings.Split(current, ":")
			seen := make(map[string]bool, len(parts))
			for _, p := range parts {
				seen[p] = true
			}
			for _, p := range extraPaths {
				if p != "" && !seen[p] {
					parts = append(parts, p)
				}
			}
			env[i] = "PATH=" + strings.Join(parts, ":")
			return env
		}
	}

	return env
}
