package feature

import "testing"

func TestCreate_BasicFields(t *testing.T) {
	store := newMockStore()
	f, err := Create(store, CreateOpts{Name: "My Feature"})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "F001-my-feature" {
		t.Errorf("ID = %q, want F001-my-feature", f.ID)
	}
	if f.Name != "F001: My Feature" {
		t.Errorf("Name = %q, want 'F001: My Feature'", f.Name)
	}
	if f.Description != "My Feature" {
		t.Errorf("Description should default to Name, got %q", f.Description)
	}
	if f.Status != StatusNotStarted {
		t.Errorf("Status = %q, want not-started", f.Status)
	}
	if _, ok := store.features[f.ID]; !ok {
		t.Error("feature not saved to store")
	}
	if len(store.dirs) != 1 || store.dirs[0] != f.ID {
		t.Errorf("InitFeatureDir not called, dirs = %v", store.dirs)
	}
}

func TestCreate_WithDescription(t *testing.T) {
	store := newMockStore()
	f, err := Create(store, CreateOpts{
		Name:        "Auth Refactor",
		Description: "Refactor auth middleware for compliance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Description != "Refactor auth middleware for compliance" {
		t.Errorf("Description = %q", f.Description)
	}
}

func TestCreate_WithCustomID(t *testing.T) {
	store := newMockStore()
	f, err := Create(store, CreateOpts{
		Name:     "Global Settings",
		CustomID: "global-settings",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "F001-global-settings" {
		t.Errorf("ID = %q, want F001-global-settings", f.ID)
	}
}

func TestCreate_WithAllOpts(t *testing.T) {
	store := newMockStore()
	store.features["F010-existing"] = Feature{ID: "F010-existing"}
	p := 2
	f, err := Create(store, CreateOpts{
		Name:        "Batch Mode",
		Description: "Add batch processing",
		Subtasks:    []Subtask{{ID: "sub-1", Name: "Subtask one"}},
		Rules:       []string{"spec: docs/batch-spec.md"},
		Depends:     []string{"F010-existing"},
		Priority:    &p,
		Repos:       []string{"api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "F011-batch-mode" {
		t.Errorf("ID = %q, want F011-batch-mode (next after F010)", f.ID)
	}
	if len(f.Subtasks) != 1 || f.Subtasks[0].ID != "sub-1" {
		t.Errorf("Subtasks = %+v", f.Subtasks)
	}
	if len(f.Rules) != 1 {
		t.Errorf("Rules = %v", f.Rules)
	}
	if len(f.Depends) != 1 {
		t.Errorf("Depends = %v", f.Depends)
	}
	if f.Priority == nil || *f.Priority != 2 {
		t.Errorf("Priority = %v", f.Priority)
	}
	if len(f.Repos) != 1 || f.Repos[0] != "api" {
		t.Errorf("Repos = %v", f.Repos)
	}
}

func TestCreate_WithProfile(t *testing.T) {
	store := newMockStore()
	f, err := Create(store, CreateOpts{Name: "Quick Feature", Profile: "quick"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Profile != "quick" {
		t.Errorf("Profile = %q, want quick", f.Profile)
	}
}

func TestCreate_SequentialNumbering(t *testing.T) {
	store := newMockStore()

	f1, _ := Create(store, CreateOpts{Name: "First"})
	if f1.ID != "F001-first" {
		t.Errorf("f1.ID = %q", f1.ID)
	}

	f2, _ := Create(store, CreateOpts{Name: "Second"})
	if f2.ID != "F002-second" {
		t.Errorf("f2.ID = %q", f2.ID)
	}
}
