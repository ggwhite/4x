package batch

import (
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

func TestBuildSubtaskGraph_Basic(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "not-started"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"a", "b"}},
	}
	adj, err := BuildSubtaskGraph(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(adj["a"]) != 2 {
		t.Errorf("adj[a] = %v, want 2 successors", adj["a"])
	}
	if len(adj["b"]) != 1 {
		t.Errorf("adj[b] = %v, want 1 successor", adj["b"])
	}
}

func TestBuildSubtaskGraph_UnknownDep(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"unknown"}},
	}
	_, err := BuildSubtaskGraph(subtasks)
	if err == nil {
		t.Error("expected error for unknown dependency")
	}
}

func TestBuildSubtaskGraph_DuplicateID(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "done"},
		{ID: "a", Status: "not-started"},
	}
	_, err := BuildSubtaskGraph(subtasks)
	if err == nil {
		t.Error("expected error for duplicate subtask ID")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate, got: %v", err)
	}
}

func TestDetectSubtaskCycle_NoCycle(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "not-started"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"b"}},
	}
	adj, _ := BuildSubtaskGraph(subtasks)
	cycle := DetectSubtaskCycle(subtasks, adj)
	if cycle != nil {
		t.Errorf("expected no cycle, got %v", cycle)
	}
}

func TestDetectSubtaskCycle_WithCycle(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"c"}},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"b"}},
	}
	adj, _ := BuildSubtaskGraph(subtasks)
	cycle := DetectSubtaskCycle(subtasks, adj)
	if cycle == nil {
		t.Error("expected cycle detection")
	}
	if len(cycle) < 2 {
		t.Errorf("cycle path too short: %v", cycle)
	}
	cycleSet := make(map[string]bool)
	for _, id := range cycle {
		cycleSet[id] = true
	}
	for _, expected := range []string{"a", "b", "c"} {
		if !cycleSet[expected] {
			t.Errorf("cycle %v should contain %q", cycle, expected)
		}
	}
}

func TestSubtaskFrontier_NoDeps(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "not-started"},
		{ID: "b", Status: "not-started"},
		{ID: "c", Status: "not-started"},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 3 {
		t.Errorf("frontier = %v, want [a b c]", frontier)
	}
}

func TestSubtaskFrontier_Linear(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "done"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"b"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 1 || frontier[0] != "b" {
		t.Errorf("frontier = %v, want [b]", frontier)
	}
}

func TestSubtaskFrontier_Diamond(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "done"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
		{ID: "c", Status: "not-started", Depends: []string{"a"}},
		{ID: "d", Status: "not-started", Depends: []string{"b", "c"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 2 {
		t.Errorf("frontier = %v, want [b c]", frontier)
	}
	got := make(map[string]bool)
	for _, id := range frontier {
		got[id] = true
	}
	if !got["b"] || !got["c"] {
		t.Errorf("frontier = %v, want b and c", frontier)
	}
}

func TestSubtaskFrontier_AllDone(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "done"},
		{ID: "b", Status: "done", Depends: []string{"a"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if frontier == nil {
		t.Error("frontier should be non-nil empty slice, got nil")
	}
	if len(frontier) != 0 {
		t.Errorf("frontier = %v, want []", frontier)
	}
}

func TestSubtaskFrontier_CycleError(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"b"}},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
	}
	_, err := SubtaskFrontier(subtasks)
	if err == nil {
		t.Error("expected cycle error")
	}
}

func TestSubtaskFrontier_UnknownDepError(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "not-started", Depends: []string{"ghost"}},
	}
	_, err := SubtaskFrontier(subtasks)
	if err == nil {
		t.Error("expected unknown dep error")
	}
}

func TestSubtaskFrontier_PartialDone(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "done"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 1 || frontier[0] != "b" {
		t.Errorf("frontier = %v, want [b]", frontier)
	}
}

func TestSubtaskFrontier_ReadyForReview(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "ready-for-review"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
	}
	frontier, err := SubtaskFrontier(subtasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(frontier) != 1 || frontier[0] != "b" {
		t.Errorf("frontier = %v, want [b] (ready-for-review should count as completed)", frontier)
	}
}

func TestSubtaskFrontier_DuplicateIDError(t *testing.T) {
	subtasks := []feature.Subtask{
		{ID: "a", Status: "done"},
		{ID: "a", Status: "not-started"},
		{ID: "b", Status: "not-started", Depends: []string{"a"}},
	}
	_, err := SubtaskFrontier(subtasks)
	if err == nil {
		t.Error("expected error for duplicate subtask ID")
	}
}
