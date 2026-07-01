package server

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestSwiftDefaultPortMatchesCanonical 確認 macOS 殼（main.swift）的預設 port
// 字面常量與 DefaultPort（單一事實來源）一致，防止兩處手動修改時漂移。
func TestSwiftDefaultPortMatchesCanonical(t *testing.T) {
	got := extractIntLiteral(t, "../../dashboard/macos/Sources/main.swift",
		regexp.MustCompile(`var serverPort: Int = (\d+)`))
	if got != DefaultPort {
		t.Errorf("main.swift serverPort = %d, want %d (DefaultPort)", got, DefaultPort)
	}
}

// TestTauriDefaultPortMatchesCanonical 確認 Windows/Linux 殼（main.rs）的預設 port
// 字面常量與 DefaultPort（單一事實來源）一致，防止兩處手動修改時漂移。
func TestTauriDefaultPortMatchesCanonical(t *testing.T) {
	got := extractIntLiteral(t, "../../dashboard/tauri/src-tauri/src/main.rs",
		regexp.MustCompile(`const DEFAULT_PORT: u16 = (\d+);`))
	if got != DefaultPort {
		t.Errorf("main.rs DEFAULT_PORT = %d, want %d (DefaultPort)", got, DefaultPort)
	}
}

// TestWebCoreJSHasNoHardcodedPort 確認 dashboard/web/core.js 不含硬編的 port 字面值
// （如 4567 或 localhost:<port>）。web 前端一律以相對路徑呼叫 API/SSE，不需要感知
// 實際 port；此測試防止未來意外引入硬編字面值。
func TestWebCoreJSHasNoHardcodedPort(t *testing.T) {
	src, err := os.ReadFile("../../dashboard/web/core.js")
	if err != nil {
		t.Fatalf("讀取 dashboard/web/core.js 失敗: %v", err)
	}

	portLiteral := regexp.MustCompile(fmt.Sprintf(`\b%d\b`, DefaultPort))
	if loc := portLiteral.FindIndex(src); loc != nil {
		t.Errorf("core.js 不應硬編 port 字面值 %d，但在 offset %d 找到", DefaultPort, loc[0])
	}
	if regexp.MustCompile(`localhost:\d+`).Match(src) {
		t.Error("core.js 不應硬編 localhost:<port>，前端應一律使用相對路徑")
	}
}

// extractIntLiteral 用 re 從 path 檔案內容擷取第一個整數子群組。
func extractIntLiteral(t *testing.T, path string, re *regexp.Regexp) int {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("讀取 %s 失敗: %v", path, err)
	}
	m := re.FindSubmatch(src)
	if m == nil {
		t.Fatalf("在 %s 找不到符合 %s 的字面常量", path, re.String())
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("解析 %s 內的整數失敗: %v", path, err)
	}
	return n
}
