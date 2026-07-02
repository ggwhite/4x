package prompt

import (
	"bytes"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestTesterTemplate_TeachesExpectedExitCode 驗證 AC-3：Tester prompt template 教學
// expectedExitCode 欄位的用途，並附一個 grep 無結果、exitCode:1 搭配
// expectedExitCode:1 的具體範例，避免 Tester 漏標此欄位而被 W7 誤判謊報。
func TestTesterTemplate_TeachesExpectedExitCode(t *testing.T) {
	tmpl, err := LoadRoleTemplate("", protocol.RoleTester)
	if err != nil {
		t.Fatalf("LoadRoleTemplate: %v", err)
	}

	data := Data{
		Feature: feat.Feature{ID: "F999-test", Name: "test feature"},
		Round:   1,
		DotDir:  ".4x",
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "expectedExitCode") {
		t.Fatal("tester template must document the expectedExitCode field")
	}
	if !strings.Contains(out, "grep") {
		t.Fatal("tester template must include a grep example for expectedExitCode")
	}
	if !strings.Contains(out, `"exitCode": 1`) || !strings.Contains(out, `"expectedExitCode": 1`) {
		t.Fatal("tester template must show a concrete exitCode:1 + expectedExitCode:1 example")
	}
}
