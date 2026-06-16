package runner

import (
	"os"
	"runtime"
	"strings"
)

// enrichedEnv 回傳加強版的環境變數，補上 GUI app 啟動時缺少的 PATH 路徑。
func enrichedEnv() []string {
	env := os.Environ()

	var extraPaths []string
	pathSep := ":"
	pathKey := "PATH="

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
