package feature

import "testing"

func ptrInt(n int) *int { return &n }

func TestCompareBacklogMirror_Matching(t *testing.T) {
	features := []Feature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
	}}
	mirror := BacklogMirror{Features: []BacklogFeature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
	}}}

	drift := CompareBacklogMirror(features, "feature_list.json", mirror)
	if len(drift) != 0 {
		t.Fatalf("drift = %+v, want none", drift)
	}
}

func TestCompareBacklogMirror_MissingExtraAndMismatch(t *testing.T) {
	features := []Feature{
		{ID: "feat-b", Name: "Feature B", Description: "desc b", Status: "done"},
		{ID: "feat-a", Name: "Feature A", Description: "desc a", Status: "not-started"},
	}
	mirror := BacklogMirror{Features: []BacklogFeature{
		{ID: "feat-a", Name: "Old Feature A", Description: "desc a", Status: "todo"},
		{ID: "feat-c", Name: "Feature C", Status: "todo"},
	}}

	drift := CompareBacklogMirror(features, "feature_list.json", mirror)
	if len(drift) != 4 {
		t.Fatalf("drift count = %d, want 4: %+v", len(drift), drift)
	}

	want := []BacklogDrift{
		{Kind: BacklogDriftMismatch, FeatureID: "feat-a", Field: "name"},
		{Kind: BacklogDriftMismatch, FeatureID: "feat-a", Field: "status"},
		{Kind: BacklogDriftMissing, FeatureID: "feat-b"},
		{Kind: BacklogDriftExtra, FeatureID: "feat-c"},
	}
	for i := range want {
		if drift[i].Kind != want[i].Kind || drift[i].FeatureID != want[i].FeatureID || drift[i].Field != want[i].Field {
			t.Fatalf("drift[%d] = %+v, want %+v", i, drift[i], want[i])
		}
		if drift[i].Message == "" {
			t.Fatalf("drift[%d] missing message", i)
		}
	}
}

func TestCompareBacklogMirror_MissingPriority(t *testing.T) {
	features := []Feature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
		Priority:    ptrInt(2),
	}}
	mirror := BacklogMirror{Features: []BacklogFeature{{
		ID:          "feat-a",
		Name:        "Feature A",
		Description: "desc",
		Status:      "not-started",
	}}}

	drift := CompareBacklogMirror(features, "feature_list.json", mirror)
	if len(drift) != 1 {
		t.Fatalf("drift count = %d, want 1: %+v", len(drift), drift)
	}
	if drift[0].Kind != BacklogDriftMismatch || drift[0].FeatureID != "feat-a" || drift[0].Field != "priority" {
		t.Fatalf("drift[0] = %+v, want priority mismatch", drift[0])
	}
}
