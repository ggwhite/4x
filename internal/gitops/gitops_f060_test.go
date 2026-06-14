package gitops

import (
	"os"
	"path/filepath"
	"testing"
)

// W6（AC-10）：來源不存在 → 回 nil（靜默成功）。
func TestCopyFileIfExists_MissingSourceReturnsNil(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "nope.txt")
	dst := filepath.Join(root, "out.txt")
	if err := CopyFileIfExists(src, dst); err != nil {
		t.Fatalf("missing source should return nil, got %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("dst should not be created when source missing")
	}
}

// W6（AC-10）：來源存在且可寫 → 成功複製、回 nil。
func TestCopyFileIfExists_CopiesContent(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "in.txt")
	dst := filepath.Join(root, "sub", "out.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFileIfExists(src, dst); err != nil {
		t.Fatalf("copy should succeed, got %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "payload" {
		t.Fatalf("dst content = %q (err %v), want payload", data, err)
	}
}

// W6（AC-10）：目標為既有目錄（無法當檔案寫）→ 回非 nil error。
func TestCopyFileIfExists_WriteFailureReturnsError(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "in.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dst 路徑本身是個目錄，os.WriteFile 會失敗。
	dst := filepath.Join(root, "dstdir")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CopyFileIfExists(src, dst); err == nil {
		t.Fatal("writing to an existing directory path should return error")
	}
}
