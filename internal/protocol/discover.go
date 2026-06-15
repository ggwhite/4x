package protocol

import "strings"

// newFeatureMarker 是 deep reviewer 在 report 中標記「值得另開 feature 的 issue」的前綴。
const newFeatureMarker = "[NEW-FEATURE]"

// defaultMaxDiscoveredFeatures 是單次 run 自動建立 feature 的預設上限。
const defaultMaxDiscoveredFeatures = 3

// similarityThreshold 是 Jaccard token overlap 視為相似的門檻。
const similarityThreshold = 0.6

// DiscoveredFeature 代表從 deep-review-report.md 解析出的一筆候選 feature。
type DiscoveredFeature struct {
	Title       string
	Description string
}

// ResolveMaxDiscoveredFeatures 回傳自動建立 feature 的數量上限：
// cfg.MaxDiscoveredFeatures > 0 時用該值，否則套預設值（3）。
func ResolveMaxDiscoveredFeatures(cfg Config) int {
	if cfg.MaxDiscoveredFeatures > 0 {
		return cfg.MaxDiscoveredFeatures
	}
	return defaultMaxDiscoveredFeatures
}

// ParseDiscoveredFeatures 掃描 deep-review-report.md 文字，抽出所有 [NEW-FEATURE] block。
// 每個 block 的標題取 [NEW-FEATURE] 之後的文字（trim）；描述為其後續行，直到遇到空行、
// 下一個 [NEW-FEATURE] 或下一個 "##" heading 為止。標題為空的 block 整個丟棄。
// 回傳順序與在 report 中出現的順序一致。
func ParseDiscoveredFeatures(report string) []DiscoveredFeature {
	lines := strings.Split(report, "\n")
	var result []DiscoveredFeature

	i := 0
	for i < len(lines) {
		line := lines[i]
		idx := strings.Index(line, newFeatureMarker)
		if idx < 0 {
			i++
			continue
		}

		title := strings.TrimSpace(line[idx+len(newFeatureMarker):])
		i++

		var descLines []string
		for i < len(lines) {
			next := lines[i]
			if strings.TrimSpace(next) == "" {
				break
			}
			if strings.Contains(next, newFeatureMarker) {
				break
			}
			if strings.HasPrefix(strings.TrimSpace(next), "##") {
				break
			}
			descLines = append(descLines, strings.TrimSpace(next))
			i++
		}

		if title == "" {
			continue
		}
		result = append(result, DiscoveredFeature{
			Title:       title,
			Description: strings.Join(descLines, "\n"),
		})
	}

	return result
}

// IsSimilarFeature 判斷兩段文字是否相似：將文字正規化（小寫、去標點、切 token）後
// 計算 Jaccard token overlap，>= similarityThreshold（0.6）視為相似。
func IsSimilarFeature(a, b string) bool {
	ta := tokenSet(a)
	tb := tokenSet(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}

	inter := 0
	for tok := range ta {
		if _, ok := tb[tok]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return false
	}
	return float64(inter)/float64(union) >= similarityThreshold
}

// DedupeDiscovered 過濾候選 feature：保留的 candidate 必須與每個 existing feature
// （Name+" "+Description）都不相似，且與已保留的 candidate 也不相似（batch 內去重）。
// 比對對象是 candidate 的 Title+" "+Description。回傳順序維持輸入順序。
func DedupeDiscovered(candidates []DiscoveredFeature, existing []Feature) []DiscoveredFeature {
	var kept []DiscoveredFeature

	for _, c := range candidates {
		ctext := c.Title + " " + c.Description

		dup := false
		for _, e := range existing {
			if IsSimilarFeature(ctext, e.Name+" "+e.Description) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		for _, k := range kept {
			if IsSimilarFeature(ctext, k.Title+" "+k.Description) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		kept = append(kept, c)
	}

	return kept
}

// tokenSet 將文字正規化為 token 集合：小寫、非英數字轉空白、切 token、去空字串。
func tokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			set[b.String()] = struct{}{}
			b.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return set
}
