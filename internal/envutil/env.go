package envutil

import (
	"os"
	"runtime"
	"strings"
)

// EnrichedEnv returns os.Environ() with common tool paths appended to PATH.
// GUI-launched processes (e.g. macOS dashboard) don't inherit the user's
// shell profile, so tools like go, node, cargo may be missing from PATH.
func EnrichedEnv() []string {
	env := os.Environ()

	var extraPaths []string
	pathSep := ":"
	pathKey := "PATH="

	switch runtime.GOOS {
	case "darwin":
		home := os.Getenv("HOME")
		extraPaths = append(extraPaths,
			"/usr/local/bin",
			"/usr/local/go/bin",
			"/opt/homebrew/bin",
			"/opt/homebrew/sbin",
			home+"/.local/bin",
			home+"/.cargo/bin",
			home+"/go/bin",
		)
	case "windows":
		userProfile := os.Getenv("USERPROFILE")
		appData := os.Getenv("LOCALAPPDATA")
		programFiles := os.Getenv("ProgramFiles")
		pathSep = ";"
		extraPaths = append(extraPaths,
			appData+"\\Programs\\claude-code\\resources\\bin",
			userProfile+"\\.cargo\\bin",
			userProfile+"\\.local\\bin",
			programFiles+"\\nodejs",
			programFiles+"\\Go\\bin",
			appData+"\\fnm\\aliases\\default",
		)
	case "linux":
		home := os.Getenv("HOME")
		extraPaths = append(extraPaths,
			"/usr/local/bin",
			"/usr/local/go/bin",
			home+"/.local/bin",
			home+"/.cargo/bin",
			home+"/go/bin",
			"/snap/bin",
			home+"/.nvm/current/bin",
			home+"/.fnm/aliases/default/bin",
		)
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

// PrependPath returns env with dirs prepended to PATH (deduped).
func PrependPath(env []string, dirs ...string) []string {
	pathKey := "PATH="
	pathSep := ":"
	if runtime.GOOS == "windows" {
		pathSep = ";"
	}

	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(pathKey)) {
			current := e[len(pathKey):]
			parts := strings.Split(current, pathSep)
			seen := make(map[string]bool, len(parts))
			for _, p := range parts {
				seen[p] = true
			}
			var newParts []string
			for _, d := range dirs {
				if d != "" && !seen[d] {
					newParts = append(newParts, d)
					seen[d] = true
				}
			}
			newParts = append(newParts, parts...)
			env[i] = pathKey + strings.Join(newParts, pathSep)
			return env
		}
	}

	return env
}
