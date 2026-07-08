package gitops

import (
	"strings"
	"testing"
)

// TestFilterGoWorkUses 驗證單行 use、區塊 use 的混合輸入下，只保留 keep 放行的目標，
// 並保留 go 版本行；含裁切後 anyKept 的兩種狀態。
func TestFilterGoWorkUses(t *testing.T) {
	src := "go 1.26\n\nuse ./a\nuse (\n\t./b\n\t./c\n)\n"

	out, anyKept := filterGoWorkUses(src, func(rel string) bool {
		return rel == "a" || rel == "b"
	})

	if !anyKept {
		t.Fatal("anyKept should be true when a/b are kept")
	}
	if !strings.Contains(out, "./a") {
		t.Errorf("output should keep ./a:\n%s", out)
	}
	if !strings.Contains(out, "./b") {
		t.Errorf("output should keep ./b:\n%s", out)
	}
	if strings.Contains(out, "./c") {
		t.Errorf("output should drop ./c:\n%s", out)
	}
	if !strings.Contains(out, "go 1.26") {
		t.Errorf("output should preserve go version line:\n%s", out)
	}
}

// TestFilterGoWorkUses_AllRejected 驗證全部 use 被拒時 anyKept==false，但版本行仍保留。
func TestFilterGoWorkUses_AllRejected(t *testing.T) {
	src := "go 1.26\ntoolchain go1.26.0\nuse ./a\nuse ./b\n"

	out, anyKept := filterGoWorkUses(src, func(string) bool { return false })

	if anyKept {
		t.Error("anyKept should be false when everything is rejected")
	}
	if strings.Contains(out, "use ") {
		t.Errorf("output should contain no use directives:\n%s", out)
	}
	if !strings.Contains(out, "go 1.26") || !strings.Contains(out, "toolchain") {
		t.Errorf("output should preserve go/toolchain lines:\n%s", out)
	}
}
