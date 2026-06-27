package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

const maxDiffLines = 200

const maxCodeMapLines = 40

// BuildCodeMap 掃描專案的 exported symbol，產出每個 package 一行的精簡摘要。
// 讓 agent 不用花 token 探索就知道 codebase 結構。非 Go 專案或掃描失敗回空字串。
func BuildCodeMap(root string) string {
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return ""
	}
	out, err := exec.Command("grep", "-rEn", `^(func|type) [A-Z]`,
		"--include=*.go", root).Output()
	if err != nil || len(out) == 0 {
		return ""
	}

	dirOrder := []string{}
	groups := map[string][]string{}
	seen := map[string]map[string]bool{}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "_test.go") || strings.Contains(line, "vendor/") ||
			strings.Contains(line, ".worktrees/") || strings.Contains(line, ".claude/") {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		fpath := strings.TrimPrefix(parts[0], root+"/")
		dir := filepath.Dir(fpath)
		code := parts[2]

		var name string
		for _, prefix := range []string{"func ", "type "} {
			if strings.HasPrefix(code, prefix) {
				rest := code[len(prefix):]
				rest = strings.TrimPrefix(rest, "(")
				idx := strings.IndexAny(rest, " ([{")
				if idx > 0 {
					name = rest[:idx]
				} else {
					name = rest
				}
				break
			}
		}
		if name == "" {
			continue
		}

		if seen[dir] == nil {
			seen[dir] = map[string]bool{}
			dirOrder = append(dirOrder, dir)
		}
		if !seen[dir][name] {
			seen[dir][name] = true
			groups[dir] = append(groups[dir], name)
		}
	}

	var b strings.Builder
	for _, dir := range dirOrder {
		syms := groups[dir]
		symStr := strings.Join(syms, " ")
		if len(syms) > 15 {
			symStr = strings.Join(syms[:15], " ") + " ..."
		}
		fmt.Fprintf(&b, "%s/ — %s\n", dir, symStr)
	}

	result := b.String()
	lines := strings.Split(result, "\n")
	if len(lines) > maxCodeMapLines {
		result = strings.Join(lines[:maxCodeMapLines], "\n") + "\n"
	}
	return strings.TrimSpace(result)
}

// BaselineDiff 從 baseline.json 讀取起始 commit，用 git diff 算出 coder 歷來的變更。
// 超過 maxDiffLines 行時截斷並附註。失敗回空字串（靜默降級）。
func BaselineDiff(ws *protocol.Workspace, featureID string) string {
	blPath := filepath.Join(ws.FeatureDir(featureID), protocol.BaselineFile)
	blData, err := os.ReadFile(blPath)
	if err != nil {
		return ""
	}
	var bl protocol.Baseline
	if err := json.Unmarshal(blData, &bl); err != nil || len(bl.Repos) == 0 {
		return ""
	}
	head := bl.Repos[0].Head
	if head == "" {
		return ""
	}

	diff, err := exec.Command("git", "-C", ws.Root, "diff", head+"..HEAD",
		"--stat", "--patch", "--", ".", ":(exclude).4x").CombinedOutput()
	if err != nil || len(diff) == 0 {
		return ""
	}

	lines := strings.Split(string(diff), "\n")
	if len(lines) <= maxDiffLines {
		return string(diff)
	}
	return strings.Join(lines[:maxDiffLines], "\n") +
		fmt.Sprintf("\n\n... (truncated, showing %d of %d lines) ...\n", maxDiffLines, len(lines))
}
