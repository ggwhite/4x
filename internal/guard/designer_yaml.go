package guard

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"gopkg.in/yaml.v3"
)

// checkDesignerYAMLMod verifies that during the designing phase, the Designer
// only modified the repos and shared_paths fields in the feature YAML (F181 put
// shared_paths on the same footing as repos). Any other field change (name,
// description, priority, profile, subtasks, rules, depends, etc.) is a guard
// violation.
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
	if string(currentFeature.Status) != orig.Status && !isOrchestratorStatusFlip(s, orig.Status, string(currentFeature.Status)) {
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
		r.Errors = append(r.Errors, fmt.Sprintf("designer modified fields outside repos/shared_paths in feature YAML: %s", strings.Join(diffs, "; ")))
		r.RetryableErrors++
	}
}

// featureSnapshot is a minimal struct for unmarshalling the original YAML to
// compare the fields the Designer may not touch. We intentionally omit repos
// since it's allowed.
//
// 注意：shared_paths（Feature.SharedPaths）刻意不列入此 snapshot——比照 repos，
// 屬 Designer 於 designing phase 可自由修改的欄位。誤加進來會讓 Designer 宣告
// shared_paths 被判為 violation。
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

// isOrchestratorStatusFlip 判斷 status 變更是否確實為 orchestrator 於 run-start（initPhase）
// 自行寫入的轉換，而非 Designer 偽造出同值變更逃過檢查。
//
// 只比對數值 pair（如 not-started→in-progress）無法分辨是誰寫的——state.json 才是可信來源：
// initPhase 在呼叫 SyncFeatureStatus 前就把即將寫入的 (from, to) 記錄進 state.json 的
// OrchestratorStatusFlipFrom/To，而 state.json 只由 orchestrator 本身寫入，spawned role agent
// （含 Designer）無法偽造（見 F157 role-scope PreToolUse gate）。故這裡要求觀察到的 (orig, current)
// 與 state.json 記錄的期望值完全一致才放行；未記錄（欄位空字串）或數值對不上一律視為違規。
func isOrchestratorStatusFlip(s protocol.State, orig, current string) bool {
	if s.OrchestratorStatusFlipFrom == "" && s.OrchestratorStatusFlipTo == "" {
		return false
	}
	return orig == s.OrchestratorStatusFlipFrom && current == s.OrchestratorStatusFlipTo
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
