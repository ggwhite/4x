package protocol

import (
	"fmt"
	"strings"
)

var validRoleNames = map[string]bool{
	string(RoleDesigner):     true,
	string(RoleCoder):        true,
	string(RoleReviewer):     true,
	string(RoleDeepReviewer): true,
	string(RoleTester):       true,
	string(RoleAcceptor):     true,
}

var validIsolations = map[string]bool{
	"":         true,
	"worktree": true,
}

var validCommitStrategies = map[string]bool{
	"":          true,
	"per-round": true,
	"on-done":   true,
	"never":     true,
}

var validPhases = map[Phase]bool{
	PhaseInit:           true,
	PhaseDesigning:      true,
	PhaseCoding:         true,
	PhaseReviewing:      true,
	PhaseDeepReviewing:  true,
	PhaseTesting:        true,
	PhaseAmending:       true,
	PhaseAccepting:      true,
	PhasePendingReview:  true,
	PhaseDone:           true,
	PhaseAbandoned:      true,
	PhaseBlocked:        true,
	PhaseNeedsAttention: true,
}

// ValidateConfig 驗證 Config 結構的合法性，回傳所有錯誤的彙整。
func ValidateConfig(c Config) error {
	var errs []string

	if c.Project.Name == "" {
		errs = append(errs, "project.name is required")
	}

	if !validIsolations[c.Isolation] {
		errs = append(errs, fmt.Sprintf("isolation %q is invalid, must be one of: worktree", c.Isolation))
	}

	if !validCommitStrategies[c.Commit] {
		errs = append(errs, fmt.Sprintf("commit %q is invalid, must be one of: per-round, on-done, never", c.Commit))
	}

	if c.Default != "" && len(c.Runners) > 0 {
		if _, ok := c.Runners[c.Default]; !ok {
			errs = append(errs, fmt.Sprintf("default_runner %q not found in runners", c.Default))
		}
	}

	for name, rc := range c.Runners {
		if rc.Command == "" {
			errs = append(errs, fmt.Sprintf("runners.%s.command is required", name))
		}
	}

	for name := range c.Roles {
		if !validRoleNames[name] {
			errs = append(errs, fmt.Sprintf("roles.%s is not a valid role, must be one of: %s", name, roleNameList()))
		}
	}

	for name, pc := range c.Profiles {
		if err := validateProfileConfig(name, pc); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid settings: %s", strings.Join(errs, "; "))
}

func validateProfileConfig(name string, pc ProfileConfig) error {
	var errs []string
	hasCoder := false
	for _, r := range pc.Roles {
		if !validRoleNames[r] {
			errs = append(errs, fmt.Sprintf("profiles.%s.roles contains unknown role %q", name, r))
		}
		if r == string(RoleCoder) {
			hasCoder = true
		}
	}
	if len(pc.Roles) > 0 && !hasCoder {
		errs = append(errs, fmt.Sprintf("profiles.%s.roles must include \"coder\"", name))
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, "; "))
}

// ValidateState 驗證 State 結構的關鍵欄位。
func ValidateState(s State) error {
	var errs []string

	if s.FeatureID == "" {
		errs = append(errs, "featureId is required")
	}

	if !validPhases[s.Phase] {
		errs = append(errs, fmt.Sprintf("phase %q is invalid", s.Phase))
	}

	if s.Runner == "" {
		errs = append(errs, "runner is required")
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid state: %s", strings.Join(errs, "; "))
}

func roleNameList() string {
	roles := []string{
		string(RoleDesigner), string(RoleCoder), string(RoleReviewer),
		string(RoleDeepReviewer), string(RoleTester), string(RoleAcceptor),
	}
	return strings.Join(roles, ", ")
}
