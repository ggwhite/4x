package docscheck

import (
	"strings"
	"testing"
)

var guideLangs = []string{"es", "ja", "ko", "zh-CN", "zh-TW"}

// AC-6: heading 數量不符時輸出位置對齊逐列結果，缺席側填 (missing)。
func TestGuideI18nAlignedMismatch(t *testing.T) {
	dir := t.TempDir()
	en := "### Alpha\n### Beta\n### Gamma\n"
	writeFile(t, dir, "docs/guide/concepts.md", en)
	// 四個語系對齊，僅 zh-TW 少一個 heading，製造單一乾淨 mismatch。
	for _, l := range guideLangs {
		content := en
		if l == "zh-TW" {
			content = "### 甲\n### 乙\n"
		}
		writeFile(t, dir, "docs/guide/"+l+"/concepts.md", content)
	}

	stdout, stderr, code := runScript(t, dir, scriptPath(t, "check-guide-i18n.sh"), "docs/guide/concepts.md")
	combined := stdout + stderr
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\n%s", code, combined)
	}
	if !strings.Contains(combined, "heading count mismatch") {
		t.Errorf("output missing 'heading count mismatch':\n%s", combined)
	}
	if !strings.Contains(combined, "(missing)") {
		t.Errorf("output missing '(missing)' marker:\n%s", combined)
	}
	if !strings.Contains(combined, "3 |") {
		t.Errorf("output missing aligned row '3 |':\n%s", combined)
	}
}

// AC-7: heading 結構一致時仍印 OK 並 exit 0，行為不變。
func TestGuideI18nOKUnchanged(t *testing.T) {
	dir := t.TempDir()
	en := "## Overview\n### Alpha\n### Beta\n"
	writeFile(t, dir, "docs/guide/concepts.md", en)
	for _, l := range guideLangs {
		writeFile(t, dir, "docs/guide/"+l+"/concepts.md", en)
	}

	stdout, stderr, code := runScript(t, dir, scriptPath(t, "check-guide-i18n.sh"), "docs/guide/concepts.md")
	combined := stdout + stderr
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\n%s", code, combined)
	}
	if !strings.Contains(stdout, "OK") {
		t.Errorf("stdout missing OK:\n%s", stdout)
	}
}
