package protocol

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	nonAlphaNum            = regexp.MustCompile(`[^a-z0-9]+`)
	featureNumRe           = regexp.MustCompile(`^F(\d{3,})-`)
	featurePrefixRe        = regexp.MustCompile(`(?i)^F\d{3,}-`)
	maxFeatureIDSlugLength = 23
)

// NextFeatureNumber 掃描現有 feature，回傳下一個可用流水號。
func NextFeatureNumber(ws *Workspace) (int, error) {
	features, err := ws.ListFeatures()
	if err != nil {
		return 1, nil
	}
	max := 0
	for _, f := range features {
		matches := featureNumRe.FindStringSubmatch(f.ID)
		if matches == nil {
			continue
		}
		n, err := strconv.Atoi(matches[1])
		if err == nil && n > max {
			max = n
		}
	}
	return max + 1, nil
}

// GenerateFeatureID 產生 F{NNN}-{slug} 格式的 feature ID。
// 超過長度上限時在 word boundary（"-"）截斷，避免斷在字中間。
func GenerateFeatureID(num int, name string) string {
	slug := strings.ToLower(name)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > maxFeatureIDSlugLength {
		truncated := slug[:maxFeatureIDSlugLength]
		if idx := strings.LastIndex(truncated, "-"); idx > 0 {
			slug = truncated[:idx]
		} else {
			slug = strings.TrimRight(truncated, "-")
		}
	}
	return fmt.Sprintf("F%03d-%s", num, slug)
}

// GenerateFeatureIDFromSlug 用使用者指定的 slug 產生 feature ID，不做截斷。
// 若 slug 已帶 F{NNN}- 前綴會自動去除，避免產生 F043-f043-... 的重複前綴。
func GenerateFeatureIDFromSlug(num int, slug string) string {
	if m := featurePrefixRe.FindStringIndex(slug); m != nil {
		slug = slug[m[1]:]
	}
	slug = strings.ToLower(slug)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return fmt.Sprintf("F%03d-%s", num, slug)
}
