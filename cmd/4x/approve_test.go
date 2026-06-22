package main

import (
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
)

func TestApproveFeature_DraftToNotStarted(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	ws.SaveFeature(feat.Feature{ID: "F099-test-draft", Name: "F099: Test", Status: feat.StatusDraft})

	if err := approveFeature(ws, "F099-test-draft"); err != nil {
		t.Fatalf("approveFeature() error = %v", err)
	}

	updated, err := ws.LoadFeature("F099-test-draft")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want %q", updated.Status, feat.StatusNotStarted)
	}
}

func TestApproveFeature_NonDraft_Error(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	ws.SaveFeature(feat.Feature{ID: "F099-test", Name: "F099: Test", Status: feat.StatusNotStarted})

	if err := approveFeature(ws, "F099-test"); err == nil {
		t.Error("expected error for non-draft feature")
	}
}

func TestRejectFeature_DraftToAbandoned(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	ws.SaveFeature(feat.Feature{ID: "F099-test-draft", Name: "F099: Test", Status: feat.StatusDraft})

	if err := rejectFeature(ws, "F099-test-draft"); err != nil {
		t.Fatalf("rejectFeature() error = %v", err)
	}

	updated, err := ws.LoadFeature("F099-test-draft")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != feat.StatusAbandoned {
		t.Errorf("status = %q, want %q", updated.Status, feat.StatusAbandoned)
	}
}

func TestRejectFeature_NonDraft_Error(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	ws.SaveFeature(feat.Feature{ID: "F099-test", Name: "F099: Test", Status: feat.StatusNotStarted})

	if err := rejectFeature(ws, "F099-test"); err == nil {
		t.Error("expected error for non-draft feature")
	}
}

func TestApproveFeature_NotFound_Error(t *testing.T) {
	ws := setupDiscoverWorkspace(t)
	if err := approveFeature(ws, "F999-missing"); err == nil {
		t.Error("expected error for missing feature")
	}
}
