package docscheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scriptPath 回傳 repo 根目錄下某個腳本的絕對路徑（測試 cwd 為本 package 目錄）。
func scriptPath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "scripts", name))
	if err != nil {
		t.Fatalf("resolve script path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("script not found: %s: %v", abs, err)
	}
	return abs
}

// realDocsyncignore 讀取 repo 根目錄「真實」的 .docsyncignore 內容（含 inline 註解），
// 供整合測試複製進 temp repo，端到端驗證 seed 檔本身在真實腳本下能生效。
func realDocsyncignore(t *testing.T) []byte {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", ".docsyncignore"))
	if err != nil {
		t.Fatalf("resolve .docsyncignore path: %v", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read real .docsyncignore: %v", err)
	}
	return data
}

// run 在 dir 執行命令，失敗即 fatal（用於 fixture 建置）。
func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

// runScript 執行 bash 腳本，回傳 stdout、stderr 與 exit code。
func runScript(t *testing.T, dir, script string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code = 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run script %s: %v", script, err)
		}
		code = exitErr.ExitCode()
	}
	return outBuf.String(), errBuf.String(), code
}

// writeFile 在 dir 下寫入相對路徑檔案，必要時建立父目錄。
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// initRepoWithBaseline 建立一個最小 git repo，寫入 baseline 檔案並提交到 main，
// 再切到 feature branch「work」。回傳後，呼叫端可改檔並 commitAll。
func initRepoWithBaseline(t *testing.T, dir string, baseline map[string]string) {
	t.Helper()
	run(t, dir, "git", "init", "-q")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	for rel, content := range baseline {
		writeFile(t, dir, rel, content)
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "baseline")
	run(t, dir, "git", "branch", "-M", "main")
	run(t, dir, "git", "checkout", "-q", "-b", "work")
}

// commitAll add -A 並提交。
func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", msg)
}
