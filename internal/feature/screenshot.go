package feature

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// IsScreenshotFile 判斷檔名是否為支援的截圖格式（png/jpg/jpeg/webp）。
func IsScreenshotFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp"
}

// NormalizeScreenshotPath 將截圖路徑正規化，去除前綴 ./、.4x/，清除 .. 分量，並 trim 空白。
func NormalizeScreenshotPath(path string) string {
	p := filepath.ToSlash(strings.TrimSpace(path))
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, ".4x/")
	p = filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	return p
}

// SortScreenshots 按 step 數字排序截圖；step 非數字時退回字串比較，再以 path 作 tie-break。
func SortScreenshots(items []Screenshot) {
	sort.Slice(items, func(i, j int) bool {
		leftN, leftOK := parseStepNumber(items[i].Step)
		rightN, rightOK := parseStepNumber(items[j].Step)
		if leftOK && rightOK && leftN != rightN {
			return leftN < rightN
		}
		if items[i].Step != items[j].Step {
			return items[i].Step < items[j].Step
		}
		return items[i].Path < items[j].Path
	})
}

// ParseScreenshotFilename 從截圖檔名解析 step 與 description（格式 {step}-{desc}.{ext}）。
func ParseScreenshotFilename(filename string) (string, string) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		return "", ""
	}
	idx := strings.Index(base, "-")
	if idx <= 0 {
		return "", strings.ReplaceAll(base, "-", " ")
	}
	step := base[:idx]
	desc := strings.TrimSpace(strings.ReplaceAll(base[idx+1:], "-", " "))
	return step, desc
}

func parseStepNumber(step string) (int, bool) {
	if step == "" {
		return 0, false
	}
	n, err := strconv.Atoi(step)
	if err != nil {
		return 0, false
	}
	return n, true
}
