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

func statusList() string {
	all := []Status{
		StatusNotStarted, StatusInProgress, StatusDone, StatusAbandoned,
		StatusBlocked, StatusNeedsAttention, StatusReadyForReview,
	}
	parts := make([]string, len(all))
	for i, s := range all {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
