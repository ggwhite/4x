package feature

import "testing"

// TestFeatureClone 驗證 Clone 後修改任一 reference 欄位都不影響原值，且純量欄位相等。
func TestFeatureClone(t *testing.T) {
	prio := 3
	orig := Feature{
		ID:       "F001-test",
		Name:     "test",
		Priority: &prio,
		Repos:    []string{"a", "b"},
		Rules:    []string{"r1"},
		Depends:  []string{"F000"},
		Warnings: []string{"w1"},
		Subtasks: []Subtask{
			{ID: "s1", Name: "sub", Depends: []string{"d1"}},
		},
		Hooks: map[string][]HookEntry{
			"coding": {{Run: "echo hi", OnFail: "warn"}},
		},
	}

	c := orig.Clone()

	// 純量欄位值相等
	if c.ID != orig.ID || c.Name != orig.Name {
		t.Errorf("scalar fields mismatch: %+v", c)
	}
	if c.Priority == nil || *c.Priority != 3 {
		t.Fatalf("Priority not copied: %v", c.Priority)
	}

	// 修改 clone 的 reference 欄位，原值必須不變
	*c.Priority = 99
	c.Repos[0] = "X"
	c.Repos = append(c.Repos, "c")
	c.Rules[0] = "X"
	c.Depends[0] = "X"
	c.Warnings[0] = "X"
	c.Subtasks[0].Name = "X"
	c.Subtasks[0].Depends[0] = "X"
	c.Hooks["coding"][0].Run = "X"
	c.Hooks["new"] = []HookEntry{{Run: "y"}}

	if *orig.Priority != 3 {
		t.Errorf("Priority polluted: %d", *orig.Priority)
	}
	if orig.Repos[0] != "a" || len(orig.Repos) != 2 {
		t.Errorf("Repos polluted: %v", orig.Repos)
	}
	if orig.Rules[0] != "r1" {
		t.Errorf("Rules polluted: %v", orig.Rules)
	}
	if orig.Depends[0] != "F000" {
		t.Errorf("Depends polluted: %v", orig.Depends)
	}
	if orig.Warnings[0] != "w1" {
		t.Errorf("Warnings polluted: %v", orig.Warnings)
	}
	if orig.Subtasks[0].Name != "sub" || orig.Subtasks[0].Depends[0] != "d1" {
		t.Errorf("Subtasks polluted: %+v", orig.Subtasks)
	}
	if orig.Hooks["coding"][0].Run != "echo hi" {
		t.Errorf("Hooks polluted: %v", orig.Hooks["coding"])
	}
	if _, ok := orig.Hooks["new"]; ok {
		t.Error("Hooks map key polluted")
	}
}

// TestFeatureCloneNilFields 驗證 nil reference 欄位 clone 後仍為 nil（保留未設定語義）。
func TestFeatureCloneNilFields(t *testing.T) {
	c := Feature{ID: "F002"}.Clone()
	if c.Priority != nil || c.Repos != nil || c.Rules != nil ||
		c.Depends != nil || c.Warnings != nil || c.Subtasks != nil || c.Hooks != nil {
		t.Errorf("nil fields should stay nil after Clone: %+v", c)
	}
}
