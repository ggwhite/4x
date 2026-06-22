package enrich

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/ggwhite/4x/internal/protocol"
)

//go:embed enrich.md.tmpl
var promptTemplateRaw string

var promptTmpl = template.Must(template.New("enrich").Parse(promptTemplateRaw))

// promptData 是 enrichment template 渲染用的資料。
type promptData struct {
	Title        string
	Description  string
	FeatureList  string
	DirTree      string
	CodeSnippets string
}

// buildPrompt 把 candidate 與收集到的脈絡渲染成 enrichment prompt。
func buildPrompt(candidate protocol.DiscoveredFeature, ectx *enrichContext) (string, error) {
	data := promptData{
		Title:        candidate.Title,
		Description:  candidate.Description,
		FeatureList:  ectx.FeatureList,
		DirTree:      ectx.DirTree,
		CodeSnippets: ectx.CodeSnippets,
	}
	var buf bytes.Buffer
	if err := promptTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
