package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWithinRoot(t *testing.T) {
	root := "/a/b"
	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"child file", "/a/b/c.txt", true},
		{"nested child", "/a/b/c/d.txt", true},
		{"root itself", "/a/b", true},
		{"sibling with shared prefix", "/a/bc/d.txt", false},
		{"parent", "/a", false},
		{"unrelated", "/x/y", false},
		{"prefix-looking but not separated", "/a/b-evil/c.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WithinRoot(root, tt.target); got != tt.want {
				t.Errorf("WithinRoot(%q, %q) = %v, want %v", root, tt.target, got, tt.want)
			}
		})
	}
}

func TestRealpathBestEffort_NonExistentTarget(t *testing.T) {
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 存在的祖先目錄 + 尚未建立的檔名，模擬 guard 檢查一個還沒寫入的目標檔。
	// 斷言接回存在祖先目錄的「已解析」路徑，而非字面拼接——t.TempDir() 在 macOS 上位於
	// /var/folders/... 而該路徑本身是 /private/var/folders/... 的 symlink，
	// RealpathBestEffort 的核心價值正是消除這層差異（見 cmd/4x/check.go 原始註解）。
	target := filepath.Join(dir, "not-yet-created.txt")
	want := filepath.Join(resolvedDir, "not-yet-created.txt")
	if got := RealpathBestEffort(target); got != want {
		t.Errorf("RealpathBestEffort(%q) = %q, want %q", target, got, want)
	}
}

func TestRealpathBestEffort_ExistingSymlink(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("symlink resolution can be flaky under sandboxed CI temp dirs")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	got := RealpathBestEffort(filepath.Join(link, "child.txt"))
	want := filepath.Join(resolvedReal, "child.txt")
	if got != want {
		t.Errorf("RealpathBestEffort through symlink = %q, want %q", got, want)
	}
}
