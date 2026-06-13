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
func GenerateFeatureID(num int, name string) string {
	slug := strings.ToLower(name)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > maxFeatureIDSlugLength {
		slug = slug[:maxFeatureIDSlugLength]
		slug = strings.TrimRight(slug, "-")
	}
	return fmt.Sprintf("F%03d-%s", num, slug)
}
