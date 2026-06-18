package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestEnrichedEnv_ExePathInPath 驗證 4x 執行檔所在目錄會被併入 PATH，
// 不被各 OS case 的 extraPaths 賦值沖掉（回歸測試 env-path-append）。
func TestEnrichedEnv_ExePathInPath(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skipf("unsupported GOOS %s", runtime.GOOS)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	exeDir := filepath.Dir(exe)

	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}

	env := enrichedEnv()

	var pathVal string
	found := false
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			pathVal = e[len("PATH="):]
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no PATH entry in enrichedEnv output")
	}

	for _, dir := range strings.Split(pathVal, sep) {
		if dir == exeDir {
			return
		}
	}
	t.Errorf("exe dir %q not found in PATH %q", exeDir, pathVal)
}

// TestEnrichedEnv_SetsFourxBin 驗證 FOURX_BIN 仍被設定（不應被本次修改影響）。
func TestEnrichedEnv_SetsFourxBin(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	env := enrichedEnv()
	want := "FOURX_BIN=" + exe
	for _, e := range env {
		if e == want {
			return
		}
	}
	t.Errorf("FOURX_BIN=%q not found in enrichedEnv output", exe)
}
