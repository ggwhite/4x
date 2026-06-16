package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// enrichedEnv 回傳加強版的環境變數，補上 GUI app 啟動時缺少的 PATH 路徑。
func enrichedEnv() []string {
	env := os.Environ()

	var extraPaths []string
	pathSep := ":"
	pathKey := "PATH="

	if exe, err := os.Executable(); err == nil {
		extraPaths = append(extraPaths, filepath.Dir(exe))
		env = append(env, "FOURX_BIN="+exe)
	}

	switch runtime.GOOS {
	case "darwin":
		home := os.Getenv("HOME")
		extraPaths = []string{
			"/usr/local/bin",
			"/opt/homebrew/bin",
			"/opt/homebrew/sbin",
			home + "/.local/bin",
			home + "/.cargo/bin",
		}
	case "windows":
		userProfile := os.Getenv("USERPROFILE")
		appData := os.Getenv("LOCALAPPDATA")
		programFiles := os.Getenv("ProgramFiles")
		pathSep = ";"
		extraPaths = []string{
			appData + "\\Programs\\claude-code\\resources\\bin",
			userProfile + "\\.cargo\\bin",
			userProfile + "\\.local\\bin",
			programFiles + "\\nodejs",
			appData + "\\fnm\\aliases\\default",
		}
	case "linux":
		home := os.Getenv("HOME")
		extraPaths = []string{
			"/usr/local/bin",
			home + "/.local/bin",
			home + "/.cargo/bin",
			"/snap/bin",
			home + "/.nvm/current/bin",
			home + "/.fnm/aliases/default/bin",
		}
	default:
		return env
	}

	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(pathKey)) {
			current := e[len(pathKey):]
			parts := strings.Split(current, pathSep)
			seen := make(map[string]bool, len(parts))
			for _, p := range parts {
				seen[p] = true
			}
			for _, p := range extraPaths {
				if p != "" && !seen[p] {
					parts = append(parts, p)
				}
			}
			env[i] = pathKey + strings.Join(parts, pathSep)
			return env
		}
	}

	return env
}
