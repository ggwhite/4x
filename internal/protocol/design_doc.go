package protocol

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
)

// DesignDoc 是設計文件（spec/plan）的解析結果。
// 找不到對應文件時，Content 與 Source 皆為空字串。
type DesignDoc struct {
	// Content 是檔案內容，找不到時為空字串。
	Content string
	// Source 是相對於 root 的來源路徑（YAML 欄位命中時為原始 YAML 路徑字串），空表示找不到。
	Source string
}

// ResolveDesignDoc 依優先序解析 feature 的設計文件，docType 為 "spec" 或 "plan"。
// 此函式統一 server 與 prompt 兩處原本各自不一致的解析邏輯，新增來源時只需改這裡。
//
// extraDirs 為額外搜尋目錄（相對於 root），優先於預設 docs/design/。
//
// 解析優先序：
//  1. Feature YAML 的 Spec/Plan 欄位（依 docType 選取）。
//  2. extraDirs 中各目錄下的 {feature.ID}-{docType}.md 與 {slug}-{docType}.md。
//  3. docs/design/{feature.ID}-{docType}.md。
//  4. docs/design/{slug}-{docType}.md。
//  5. 都找不到 → 回傳零值 DesignDoc{}。
func ResolveDesignDoc(root string, feature feature.Feature, docType string, extraDirs ...string) DesignDoc {
	yamlPath := ""
	switch docType {
	case "spec":
		yamlPath = feature.Spec
	case "plan":
		yamlPath = feature.Plan
	}

	if yamlPath != "" {
		abs := yamlPath
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, yamlPath)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			resolvedRoot, rootErr := filepath.EvalSymlinks(root)
			if rootErr == nil {
				cleanRoot := resolvedRoot + string(filepath.Separator)
				if strings.HasPrefix(resolved, cleanRoot) || resolved == resolvedRoot {
					if content, err := os.ReadFile(resolved); err == nil {
						return DesignDoc{Content: string(content), Source: yamlPath}
					}
				}
			}
		}
	}

	slug := stripFeaturePrefix(feature.ID)
	dirs := make([]string, len(extraDirs)+1)
	copy(dirs, extraDirs)
	dirs[len(extraDirs)] = filepath.Join("docs", "design")
	for _, dir := range dirs {
		rel := filepath.Join(dir, feature.ID+"-"+docType+".md")
		if content, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
			return DesignDoc{Content: string(content), Source: rel}
		}
		if slug != feature.ID {
			rel = filepath.Join(dir, slug+"-"+docType+".md")
			if content, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
				return DesignDoc{Content: string(content), Source: rel}
			}
		}
	}

	return DesignDoc{}
}

// stripFeaturePrefix 移除 FNNN- prefix（如 F022-multi-project → multi-project），
// 不符合 FNNN- 格式時原樣回傳。
func stripFeaturePrefix(id string) string {
	if len(id) > 5 && id[0] == 'F' && id[4] == '-' {
		return id[5:]
	}
	return id
}
