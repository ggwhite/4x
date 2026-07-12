package feature

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var featureIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*[A-Za-z0-9]$`)

var validStatuses = buildStatusSet()

var validSubtaskStatuses = buildSubtaskStatusSet()

func buildStatusSet() map[Status]bool {
	m := make(map[Status]bool, len(AllStatuses()))
	for _, s := range AllStatuses() {
		m[s] = true
	}
	return m
}

func buildSubtaskStatusSet() map[string]bool {
	m := make(map[string]bool, len(AllSubtaskStatuses()))
	for _, s := range AllSubtaskStatuses() {
		m[s] = true
	}
	return m
}

// Validate 驗證 Feature 結構的必要欄位與合法值，回傳所有錯誤的彙整。
func (f Feature) Validate() error {
	var errs []string

	if f.ID == "" {
		errs = append(errs, "id is required")
	} else if !featureIDRe.MatchString(f.ID) {
		errs = append(errs, fmt.Sprintf("id %q must match [A-Za-z0-9-] (alphanumeric and dashes, no leading/trailing dash, min 2 chars)", f.ID))
	}

	if f.Name == "" {
		errs = append(errs, "name is required")
	}

	if f.Status != "" && !validStatuses[f.Status] {
		errs = append(errs, fmt.Sprintf("status %q is invalid, must be one of: %s", f.Status, statusList()))
	}

	for i, st := range f.Subtasks {
		if err := st.validate(i); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid feature: %s", strings.Join(errs, "; "))
}

func (s Subtask) validate(index int) error {
	var errs []string
	prefix := fmt.Sprintf("subtasks[%d]", index)

	if s.ID == "" {
		errs = append(errs, prefix+".id is required")
	}
	if s.Name == "" {
		errs = append(errs, prefix+".name is required")
	}
	if s.Status != "" && !validSubtaskStatuses[s.Status] {
		errs = append(errs, fmt.Sprintf("%s.status %q is invalid, must be one of: %s", prefix, s.Status, subtaskStatusList()))
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, "; "))
}

// ValidateLoose 執行寬鬆驗證：只有無法辨識身分的 feature（缺 id）才回傳 error，
// 其餘格式問題（無效 status、subtask 欄位缺漏等）收集為 warnings 回傳，讓呼叫端決定是否顯示。
func (f Feature) ValidateLoose() (warnings []string, fatalErr error) {
	if f.ID == "" {
		return nil, fmt.Errorf("invalid feature: id is required")
	}

	if !featureIDRe.MatchString(f.ID) {
		warnings = append(warnings, fmt.Sprintf("id %q does not match expected format [A-Za-z0-9-]", f.ID))
	}
	if f.Name == "" {
		warnings = append(warnings, "name is empty")
	}
	if f.Status != "" && !validStatuses[f.Status] {
		warnings = append(warnings, fmt.Sprintf("status %q is not recognized (valid: %s)", f.Status, statusList()))
	}
	for i, st := range f.Subtasks {
		warnings = append(warnings, st.validateLoose(i)...)
	}
	for _, issue := range sharedPathIssues(f.SharedPaths) {
		warnings = append(warnings, "shared_paths "+issue)
	}
	return warnings, nil
}

func (s Subtask) validateLoose(index int) []string {
	var warnings []string
	prefix := fmt.Sprintf("subtasks[%d]", index)

	if s.ID == "" {
		warnings = append(warnings, prefix+".id is empty")
	}
	if s.Name == "" {
		warnings = append(warnings, prefix+".name is empty")
	}
	if s.Status != "" && !validSubtaskStatuses[s.Status] {
		warnings = append(warnings, fmt.Sprintf("%s.status %q is not recognized (valid: %s)", prefix, s.Status, subtaskStatusList()))
	}
	return warnings
}

// ValidateSharedPaths 驗證 feature YAML 宣告的 shared_paths 是否皆為合法的「根層共用路徑」。
//
// shared_paths 會被灌進 Coder prompt 並明示「允許改動」，因此必須嚴格限制為 monorepo hub
// 根目錄的直接子項（如 Dockerfile、docker-compose.yml、dev.sh），不得逸出 workspace：
//   - 空值（trim 後為空）→ 拒絕
//   - 含路徑分隔符（"/" 或 "\"）→ 拒絕；此舉一併擋掉 "/abs/path" 絕對路徑、"sub/dir/file"
//     nested path 與 "../x" traversal（根層檔案在 detectChangedRepos 的判定即是「不含 '/'」，
//     故只允許直接子項）
//   - "." 或 ".."（純目錄參照）→ 拒絕
//   - 含控制字元（換行、CR、tab、NUL 等）→ 拒絕；shared_paths 未轉義即逐項插入
//     templates/coder.md.tmpl 的 prompt bullet，換行等控制字元可讓內容突破單一 bullet
//     結構、偽造後續指令，屬 prompt injection 風險（round-3 review finding）
//
// 回傳的 error 會列出所有非法項目與原因；全部合法（含空清單）時回傳 nil。
func ValidateSharedPaths(paths []string) error {
	issues := sharedPathIssues(paths)
	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("invalid shared_paths: %s", strings.Join(issues, "; "))
}

// containsControlRune 回傳 s 是否含有任何 Unicode control rune（含 \r、\n、\t、NUL）。
func containsControlRune(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// sharedPathIssues 回傳 shared_paths 中每個非法項目的說明字串（合法則不列入），供
// ValidateSharedPaths 與 ValidateLoose 共用同一套判定規則，避免門檻語意分歧。
func sharedPathIssues(paths []string) []string {
	var issues []string
	for _, p := range paths {
		tp := strings.TrimSpace(p)
		var reason string
		switch {
		case tp == "":
			reason = "empty"
		case containsControlRune(p):
			reason = "must not contain control characters (newline, tab, etc.)"
		case strings.ContainsAny(tp, "/\\"):
			reason = "must be a root-level file with no path separator (absolute paths, nested paths and '..' traversal are not allowed)"
		case tp == "." || tp == "..":
			reason = "must name a file, not a directory reference"
		}
		if reason != "" {
			issues = append(issues, fmt.Sprintf("%q (%s)", p, reason))
		}
	}
	return issues
}

func statusList() string {
	all := AllStatuses()
	parts := make([]string, len(all))
	for i, s := range all {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

func subtaskStatusList() string {
	return strings.Join(AllSubtaskStatuses(), ", ")
}
