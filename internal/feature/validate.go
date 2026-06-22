package feature

import (
	"fmt"
	"regexp"
	"strings"
)

var featureIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*[A-Za-z0-9]$`)

var validStatuses = map[Status]bool{
	StatusNotStarted:     true,
	StatusInProgress:     true,
	StatusDone:           true,
	StatusAbandoned:      true,
	StatusBlocked:        true,
	StatusNeedsAttention: true,
	StatusReadyForReview: true,
	StatusDraft:          true,
}

var validSubtaskStatuses = map[string]bool{
	"not-started": true,
	"in-progress": true,
	"done":        true,
	"blocked":     true,
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
		errs = append(errs, fmt.Sprintf("%s.status %q is invalid, must be one of: not-started, in-progress, done, blocked", prefix, s.Status))
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
		warnings = append(warnings, fmt.Sprintf("%s.status %q is not recognized (valid: not-started, in-progress, done, blocked)", prefix, s.Status))
	}
	return warnings
}

func statusList() string {
	all := []Status{
		StatusNotStarted, StatusInProgress, StatusDone, StatusAbandoned,
		StatusBlocked, StatusNeedsAttention, StatusReadyForReview, StatusDraft,
	}
	parts := make([]string, len(all))
	for i, s := range all {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
