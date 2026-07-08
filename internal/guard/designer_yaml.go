package guard

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"gopkg.in/yaml.v3"
)

// checkDesignerYAMLMod verifies that during the designing phase, the Designer
// only modified the repos field in the feature YAML. Any other field change
// (name, description, priority, profile, subtasks, rules, depends, etc.) is
// a guard violation.
func checkDesignerYAMLMod(ws *protocol.Workspace, featureID string, r *CheckResult) {
	s, err := ws.ReadState(featureID)
	if err != nil || s.Phase != protocol.PhaseDesigning {
		return
	}

	currentFeature, err := ws.LoadFeature(featureID)
	if err != nil {
		return
	}

	yamlPath := fmt.Sprintf(".4x/features/%s.yaml", featureID)
	original, err := gitShowFile(ws.Root, yamlPath)
	if err != nil {
		return
	}

	var orig featureSnapshot
	if err := yaml.Unmarshal([]byte(original), &orig); err != nil {
		return
	}

	var diffs []string
	if currentFeature.Name != orig.Name {
		diffs = append(diffs, fmt.Sprintf("name: %q → %q", orig.Name, currentFeature.Name))
	}
	if currentFeature.Description != orig.Description {
		diffs = append(diffs, "description changed")
	}
	if fmtPriority(currentFeature.Priority) != fmtPriority(orig.Priority) {
		diffs = append(diffs, fmt.Sprintf("priority: %s → %s", fmtPriority(orig.Priority), fmtPriority(currentFeature.Priority)))
	}
	if string(currentFeature.Status) != orig.Status {
		diffs = append(diffs, fmt.Sprintf("status: %s → %s", orig.Status, currentFeature.Status))
	}
	if currentFeature.Profile != orig.Profile {
		diffs = append(diffs, fmt.Sprintf("profile: %q → %q", orig.Profile, currentFeature.Profile))
	}
	if !stringsEqual(currentFeature.Rules, orig.Rules) {
		diffs = append(diffs, "rules changed")
	}
	if !stringsEqual(currentFeature.Depends, orig.Depends) {
		diffs = append(diffs, "depends changed")
	}
	if len(currentFeature.Subtasks) != len(orig.Subtasks) {
		diffs = append(diffs, fmt.Sprintf("subtasks count: %d → %d", len(orig.Subtasks), len(currentFeature.Subtasks)))
	}

	if len(diffs) > 0 {
		r.Pass = false
		r.Errors = append(r.Errors, fmt.Sprintf("designer modified non-repos fields in feature YAML: %s", strings.Join(diffs, "; ")))
		r.RetryableErrors++
	}
}

// featureSnapshot is a minimal struct for unmarshalling the original YAML to
// compare non-repos fields. We intentionally omit repos since it's allowed.
type featureSnapshot struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Status      string   `yaml:"status"`
	Priority    *int     `yaml:"priority"`
	Profile     string   `yaml:"profile"`
	Rules       []string `yaml:"rules"`
	Depends     []string `yaml:"depends"`
	Subtasks    []struct {
		ID string `yaml:"id"`
	} `yaml:"subtasks"`
}

func gitShowFile(root, path string) (string, error) {
	cmd := exec.Command("git", "show", "HEAD:"+path)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func fmtPriority(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *p)
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
