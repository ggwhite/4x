package prompt

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/templates"
	"gopkg.in/yaml.v3"
)

// RunnerAutoReads 列出各 runner CLI 會自動讀取的慣例檔，4x prompt 不需重複注入。
var RunnerAutoReads = map[string][]string{
	"claude":   {"CLAUDE.md"},
	"codex":    {"AGENTS.md"},
	"gemini":   {"GEMINI.md"},
	"copilot":  {"AGENTS.md"},
	"cursor":   {".cursorrules", "CURSORRULES"},
	"opencode": {"AGENTS.md"},
}

// LoadIncludes 讀取指定路徑的檔案內容，路徑相對於 root 解析。
// skip 列出要跳過的檔名（basename），用於避免載入 runner 會自動讀取的慣例檔。
func LoadIncludes(root string, paths []string, skip ...string) []IncludeContent {
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}
	var result []IncludeContent
	for _, p := range paths {
		if skipSet[filepath.Base(p)] {
			continue
		}
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(root, p)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			slog.Warn("include file read failed", "path", p, "error", err)
			continue
		}
		result = append(result, IncludeContent{Path: p, Content: string(data)})
	}
	return result
}

// DiscoverConventionFiles 探測專案根目錄下的 agent 慣例檔案（CLAUDE.md、AGENTS.md 等），
// 排除已在 explicit includes 中列出的檔案與當前 runner 會自動讀取的檔案，避免重複注入。
func DiscoverConventionFiles(root, runner string, explicitIncludes []string) []IncludeContent {
	conventionFiles := []string{
		"CLAUDE.md",
		"AGENTS.md",
		"GEMINI.md",
		"COPILOT.md",
		"CURSORRULES",
		".cursorrules",
	}

	skip := make(map[string]bool, len(explicitIncludes))
	for _, p := range explicitIncludes {
		skip[p] = true
	}
	for _, p := range RunnerAutoReads[runner] {
		skip[p] = true
	}

	var result []IncludeContent
	for _, name := range conventionFiles {
		if skip[name] {
			continue
		}
		abs := filepath.Join(root, name)
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		result = append(result, IncludeContent{Path: name, Content: string(data)})
	}
	return result
}

// LoadProfiles 讀取 test-strategy.yaml 的 profiles 欄位，依序載入各 profile 的測試方法論內容。
// 沒有 profiles（或讀不到 test-strategy.yaml）時回傳 nil。
func LoadProfiles(ws *protocol.Workspace, featureID string, cfg protocol.Config) []ProfileContent {
	stratPath := filepath.Join(ws.FeatureDir(featureID), protocol.TestStratFile)
	data, err := os.ReadFile(stratPath)
	if err != nil {
		return nil
	}

	var ts protocol.TestStrategy
	if err := yaml.Unmarshal(data, &ts); err != nil {
		slog.Warn("invalid test-strategy.yaml", "path", stratPath, "error", err)
		return nil
	}

	if len(ts.Profiles) == 0 {
		return nil
	}

	var result []ProfileContent
	for _, name := range ts.Profiles {
		content := resolveProfileContent(ws.Root, name, cfg)
		if content == "" {
			slog.Warn("unknown test profile, skipping", "profile", name)
			continue
		}
		result = append(result, ProfileContent{Name: name, Content: content})
	}
	return result
}

func resolveProfileContent(root, name string, cfg protocol.Config) string {
	if override, ok := cfg.TestProfiles[name]; ok {
		if override.Content != "" {
			return override.Content
		}
		if override.Include != "" {
			p := override.Include
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				slog.Warn("test profile include read failed", "include", override.Include, "error", err)
				return ""
			}
			return string(data)
		}
	}

	data, err := templates.ProfilesFS.ReadFile("profiles/" + name + ".md")
	if err != nil {
		return ""
	}
	return string(data)
}

// LoadPlanningDocs 解析 feature 的 spec 與 plan 設計文件並串接，供 prompt 注入。
// condense 為 true 時對 plan 做精簡。
func LoadPlanningDocs(root string, feature feat.Feature, designDocDirs []string, condense bool) string {
	var parts []string
	for _, docType := range []string{"spec", "plan"} {
		doc := protocol.ResolveDesignDoc(root, feature, docType, designDocDirs...)
		if doc.Content != "" {
			content := doc.Content
			if condense && docType == "plan" {
				content = CondensePlan(content)
			}
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// CondensePlan 精簡 plan 文件：保留 Task 前的架構段落，
// 每個 Task 只保留標題與描述區，去掉 `- [ ] Step` 詳細步驟與 code block。
func CondensePlan(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	inTask := false
	skipRest := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### Task ") {
			inTask = true
			skipRest = false
			out = append(out, line)
			continue
		}
		if inTask && strings.HasPrefix(trimmed, "- [ ]") {
			skipRest = true
			continue
		}
		if skipRest {
			if strings.HasPrefix(trimmed, "### Task ") || strings.HasPrefix(trimmed, "## ") {
				skipRest = false
				inTask = strings.HasPrefix(trimmed, "### Task ")
				out = append(out, line)
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
