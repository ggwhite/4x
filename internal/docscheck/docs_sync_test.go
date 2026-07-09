package docscheck

import (
	"strings"
	"testing"
)

// rulesPrefixes 對應 scripts/check-docs-sync.sh 的 RULES 第一欄 source prefix 集合。
// AC-1 需驗證 .docsyncignore 的 glob 與此集合無交集（不得整條停用某規則）。
var rulesPrefixes = []string{
	"cmd/4x/",
	"internal/state/machine.go",
	"internal/protocol/",
	"internal/runner/",
	"plugins/",
	"internal/server/",
	"internal/batch/",
}

// parseDocsyncignore 以 F153 Design Ruling 7 的規則解析真實 .docsyncignore，
// 回傳每筆資料行的「原始行」與「還原 glob」。
func parseDocsyncignore(data []byte) (rawDataLines []string, globs []string) {
	for _, raw := range strings.Split(string(data), "\n") {
		// 從第一個 '#' 起截斷為註解
		content := raw
		if i := strings.IndexByte(content, '#'); i >= 0 {
			content = content[:i]
		}
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		glob := content
		if i := strings.IndexByte(content, '\t'); i >= 0 {
			glob = content[:i]
		}
		rawDataLines = append(rawDataLines, raw)
		globs = append(globs, glob)
	}
	return rawDataLines, globs
}

// AC-1: repo 根 .docsyncignore seed 合法。
func TestDocsyncignoreSeedValid(t *testing.T) {
	data := realDocsyncignore(t)
	rawLines, globs := parseDocsyncignore(data)

	if len(globs) == 0 {
		t.Fatal("no data entries parsed from .docsyncignore")
	}

	// 三筆目標 glob 皆須存在
	want := []string{"*_test.go", "*_mock.go", "*testdata/*"}
	for _, w := range want {
		found := false
		for _, g := range globs {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("seed glob %q missing from .docsyncignore", w)
		}
	}

	prefixSet := make(map[string]bool, len(rulesPrefixes))
	for _, p := range rulesPrefixes {
		prefixSet[p] = true
	}

	for i, g := range globs {
		raw := rawLines[i]
		// 每筆資料原始行須含 '#'（即有同行理由註解）
		if !strings.Contains(raw, "#") {
			t.Errorf("data line %q has no inline '# 理由' comment", raw)
		}
		// 還原 glob 非空、不含 '#'、不含內嵌空白
		if g == "" {
			t.Errorf("line %q yields empty glob", raw)
		}
		if strings.Contains(g, "#") {
			t.Errorf("glob %q from line %q still contains '#'", g, raw)
		}
		if strings.ContainsAny(g, " \t") {
			t.Errorf("glob %q from line %q contains residual whitespace", g, raw)
		}
		// glob 不得恰好等於某條 RULES source prefix
		if prefixSet[g] {
			t.Errorf("glob %q equals a RULES source prefix — would disable the whole rule", g)
		}
	}
}

// docsSyncBaseline 提供 docs-sync 測試共用的最小 docs/source fixture。
func docsSyncBaseline() map[string]string {
	return map[string]string{
		"docs/guide/cli.md":       "# CLI\n",
		"docs/guide/dashboard.md": "# Dashboard\n",
		"docs/guide/concepts.md":  "# Concepts\n",
		"internal/server/mux.go":  "package server\n",
		"cmd/4x/main.go":          "package main\n",
	}
}

// AC-2: path-level 條目命中的測試檔改動不再點名 doc，無其他缺漏則 exit 0。
func TestDocsSyncSuppressesTestFiles(t *testing.T) {
	dir := t.TempDir()
	base := docsSyncBaseline()
	base[".docsyncignore"] = string(realDocsyncignore(t))
	initRepoWithBaseline(t, dir, base)

	writeFile(t, dir, "internal/server/foo_test.go", "package server\n")
	commitAll(t, dir, "add server test")

	stdout, _, code := runScript(t, dir, scriptPath(t, "check-docs-sync.sh"), "main")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "OK: no doc updates needed") {
		t.Errorf("stdout missing OK marker:\n%s", stdout)
	}
	if strings.Contains(stdout, "NEEDS_UPDATE") {
		t.Errorf("stdout should not contain NEEDS_UPDATE:\n%s", stdout)
	}
	if strings.Contains(stdout, "dashboard.md") {
		t.Errorf("stdout should not name dashboard.md (suppressed):\n%s", stdout)
	}
}

// AC-3: 未被抑制的 production 檔改動仍點名 doc、exit 1。
func TestDocsSyncRealGapStillFlagged(t *testing.T) {
	dir := t.TempDir()
	base := docsSyncBaseline()
	base[".docsyncignore"] = string(realDocsyncignore(t))
	initRepoWithBaseline(t, dir, base)

	writeFile(t, dir, "internal/server/mux.go", "package server // changed\n")
	commitAll(t, dir, "edit mux")

	stdout, stderr, code := runScript(t, dir, scriptPath(t, "check-docs-sync.sh"), "main")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "NEEDS_UPDATE") {
		t.Errorf("output missing NEEDS_UPDATE:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "docs/guide/dashboard.md") {
		t.Errorf("output should name docs/guide/dashboard.md:\n%s", stdout)
	}
}

// AC-4: 抑制透明——stderr 印出被抑制的 (doc, file)，exit code 不變（仍為 0）。
func TestDocsSyncSuppressionTransparent(t *testing.T) {
	dir := t.TempDir()
	base := docsSyncBaseline()
	base[".docsyncignore"] = string(realDocsyncignore(t))
	initRepoWithBaseline(t, dir, base)

	writeFile(t, dir, "internal/server/foo_test.go", "package server\n")
	commitAll(t, dir, "add server test")

	stdout, stderr, code := runScript(t, dir, scriptPath(t, "check-docs-sync.sh"), "main")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "Suppressed") {
		t.Errorf("stderr missing Suppressed block:\n%s", stderr)
	}
	if !strings.Contains(stderr, "internal/server/foo_test.go") {
		t.Errorf("stderr should list suppressed file:\n%s", stderr)
	}
}

// AC-5: relocation/delete 檢查不受抑制影響——被刪的 *_test.go 仍出現在 docs prose 時 exit 1。
func TestDocsSyncRelocationNotSuppressed(t *testing.T) {
	dir := t.TempDir()
	base := docsSyncBaseline()
	base[".docsyncignore"] = string(realDocsyncignore(t))
	base["internal/foo_test.go"] = "package internalpkg\n"
	base["docs/guide/x.md"] = "see internal/foo_test.go for details\n"
	initRepoWithBaseline(t, dir, base)

	run(t, dir, "git", "rm", "-q", "internal/foo_test.go")
	commitAll(t, dir, "remove foo_test.go")

	stdout, stderr, code := runScript(t, dir, scriptPath(t, "check-docs-sync.sh"), "main")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "internal/foo_test.go") {
		t.Errorf("output should reference removed path internal/foo_test.go:\n%s", combined)
	}
	if !strings.Contains(combined, "removed/renamed") {
		t.Errorf("output should mark path as removed/renamed:\n%s", combined)
	}
	if !strings.Contains(combined, "docs/guide/x.md") {
		t.Errorf("output should name the doc still referencing it:\n%s", combined)
	}
}
