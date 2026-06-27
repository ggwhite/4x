package feature

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultIDPrefix = "F"
	DefaultIDDigits = 3
)

var (
	nonAlphaNum            = regexp.MustCompile(`[^a-z0-9]+`)
	maxFeatureIDSlugLength = 23
)

// IDFormat 描述 feature ID 的前綴與流水號位數。
type IDFormat struct {
	Prefix string
	Digits int
}

// ResolveIDFormat 回傳有效的 IDFormat，零值欄位退回預設。
func ResolveIDFormat(prefix string, digits int) IDFormat {
	if prefix == "" {
		prefix = DefaultIDPrefix
	}
	if digits <= 0 {
		digits = DefaultIDDigits
	}
	return IDFormat{Prefix: prefix, Digits: digits}
}

// numRe 回傳用於從 feature ID 萃取流水號的 regex，例如 prefix="F" → `^F(\d+)(?:-|$)`。
// 用 (?:-|$) 相容無 slug 的舊格式（如 ws-094）與有 slug 的新格式（如 ws-094-name）。
func (f IDFormat) numRe() *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(f.Prefix) + `(\d+)(?:-|$)`)
}

// prefixRe 回傳用於偵測並剝除使用者輸入中重複前綴的 regex（case-insensitive）。
func (f IDFormat) prefixRe() *regexp.Regexp {
	return regexp.MustCompile(`(?i)^` + regexp.QuoteMeta(f.Prefix) + `\d+-`)
}

// formatID 組合前綴、流水號、slug 為完整 feature ID。
func (f IDFormat) formatID(num int, slug string) string {
	return fmt.Sprintf("%s%0*d-%s", f.Prefix, f.Digits, num, slug)
}

// FormatDisplayName 產生人類可讀的顯示名稱，如 "F001: My Feature"。
func (f IDFormat) FormatDisplayName(num int, name string) string {
	return fmt.Sprintf("%s%0*d: %s", f.Prefix, f.Digits, num, name)
}

// NextNumber 掃描現有 feature，回傳下一個可用流水號。
func NextNumber(store Store, idf IDFormat) (int, error) {
	features, err := store.ListFeatures()
	if err != nil {
		return 0, fmt.Errorf("list features for next number: %w", err)
	}
	re := idf.numRe()
	max := 0
	for _, f := range features {
		matches := re.FindStringSubmatch(f.ID)
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

// GenerateFeatureID 產生 {prefix}{NNN}-{slug} 格式的 feature ID。
// 超過長度上限時在 word boundary（"-"）截斷，避免斷在字中間。
func GenerateFeatureID(num int, name string, idf IDFormat) string {
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
	return idf.formatID(num, slug)
}

// GenerateFeatureIDFromSlug 用使用者指定的 slug 產生 feature ID，不做截斷。
// 若 slug 已帶前綴會自動去除，避免產生重複前綴。
func GenerateFeatureIDFromSlug(num int, slug string, idf IDFormat) string {
	if m := idf.prefixRe().FindStringIndex(slug); m != nil {
		slug = slug[m[1]:]
	}
	slug = strings.ToLower(slug)
	slug = nonAlphaNum.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return idf.formatID(num, slug)
}
