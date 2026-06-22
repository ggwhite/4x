package enrich

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

const (
	maxKeywords               = 5
	maxSnippetLinesPerKeyword = 10
	maxTotalSnippetLines      = 50
	maxFeatureDescLen         = 200
)

// enrichContext 收集的專案脈絡，供 enrichment prompt 渲染使用。
type enrichContext struct {
	FeatureList  string
	DirTree      string
	CodeSnippets string
}

// featureSummary 是 feature 的精簡摘要，用於 prompt 的現有 feature 列表。
type featureSummary struct {
	ID          string
	Name        string
	Description string
}

// collectContext 收集專案脈絡（現有 feature 列表、目錄樹、依 title keyword grep 的程式碼片段）
// 供 enrichment prompt 使用。grep / find 失敗只降級為佔位字串，不回 error，確保 enrichment best-effort。
func collectContext(ws *protocol.Workspace, title string) (*enrichContext, error) {
	features, err := ws.ListFeatures()
	if err != nil {
		return nil, fmt.Errorf("list features: %w", err)
	}

	summaries := make([]featureSummary, len(features))
	for i, f := range features {
		summaries[i] = featureSummary{ID: f.ID, Name: f.Name, Description: truncate(f.Description, maxFeatureDescLen)}
	}

	keywords := extractKeywords(title)

	return &enrichContext{
		FeatureList:  formatFeatureList(summaries),
		DirTree:      collectDirTree(ws.Root),
		CodeSnippets: grepSnippets(ws.Root, keywords),
	}, nil
}

// extractKeywords 從 title 拆出關鍵字，過濾 stop words 與過短 token，取前 maxKeywords 個。
func extractKeywords(title string) []string {
	stops := map[string]bool{
		"the": true, "are": true, "for": true, "and": true,
		"with": true, "from": true, "this": true, "that": true,
		"was": true, "were": true, "been": true, "has": true,
		"have": true, "had": true, "does": true, "did": true,
		"but": true, "not": true, "then": true,
	}
	words := strings.Fields(strings.ToLower(title))
	var keywords []string
	for _, w := range words {
		clean := strings.Trim(w, ".,;:!?\"'()-")
		if clean == "" || stops[clean] || len(clean) < 3 {
			continue
		}
		keywords = append(keywords, clean)
		if len(keywords) >= maxKeywords {
			break
		}
	}
	return keywords
}

// collectDirTree 執行 find 取得專案目錄結構（排除 vendor/node_modules/.git/.4x）。
func collectDirTree(root string) string {
	cmd := exec.Command("find", root, "-type", "d",
		"-not", "-path", "*/vendor/*",
		"-not", "-path", "*/node_modules/*",
		"-not", "-path", "*/.git/*",
		"-not", "-path", "*/.4x/*",
		"-maxdepth", "4",
	)
	out, err := cmd.Output()
	if err != nil {
		return "(directory tree unavailable)"
	}
	return string(out)
}

// grepSnippets 對每個 keyword grep 專案內 .go 檔，每個取前 maxSnippetLinesPerKeyword 行，
// 合計不超過 maxTotalSnippetLines 行。無 keyword 或無命中時回佔位字串。
func grepSnippets(root string, keywords []string) string {
	if len(keywords) == 0 {
		return "(no keywords to search)"
	}
	var b strings.Builder
	totalLines := 0
	for _, kw := range keywords {
		if totalLines >= maxTotalSnippetLines {
			break
		}
		limit := maxSnippetLinesPerKeyword
		if remaining := maxTotalSnippetLines - totalLines; remaining < limit {
			limit = remaining
		}
		cmd := exec.Command("grep", "-rn", "--include=*.go", "-m", fmt.Sprintf("%d", limit), kw, root)
		out, _ := cmd.Output()
		if len(out) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### keyword: %s\n%s\n", kw, string(out))
		totalLines += strings.Count(string(out), "\n")
	}
	if b.Len() == 0 {
		return "(no code snippets found)"
	}
	return b.String()
}

// formatFeatureList 格式化 feature 列表為 prompt 用文字。
func formatFeatureList(features []featureSummary) string {
	if len(features) == 0 {
		return "(no existing features)"
	}
	var b strings.Builder
	for _, f := range features {
		fmt.Fprintf(&b, "- %s: %s — %s\n", f.ID, f.Name, f.Description)
	}
	return b.String()
}

// truncate 截斷字串到指定長度，超出時補上省略符號。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
