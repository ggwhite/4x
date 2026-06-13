package protocol

import "testing"

func TestGenerateFeatureID(t *testing.T) {
	tests := []struct {
		num  int
		name string
		want string
	}{
		{1, "My Feature", "F001-my-feature"},
		{25, "Server Write API", "F025-server-write-api"},
		{100, "A very long feature name that exceeds the limit", "F100-a-very-long-feature"},
		{1000, "Four digit feature", "F1000-four-digit-feature"},
		{99999, "Five digit feature", "F99999-five-digit-feature"},
		{40, "Dashboard SPA file split — separate HTML, JS, CSS for maintainability", "F040-dashboard-spa-file"},
		{1, "single", "F001-single"},
		{1, "abcdefghijklmnopqrstuvwxyz", "F001-abcdefghijklmnopqrstuvw"},
	}
	for _, tt := range tests {
		got := GenerateFeatureID(tt.num, tt.name)
		if got != tt.want {
			t.Errorf("GenerateFeatureID(%d, %q) = %q, want %q", tt.num, tt.name, got, tt.want)
		}
	}
}

func TestGenerateFeatureIDFromSlug(t *testing.T) {
	tests := []struct {
		num  int
		slug string
		want string
	}{
		{1, "my-custom-id", "F001-my-custom-id"},
		{40, "dashboard-spa-split", "F040-dashboard-spa-split"},
		{5, "A Very Long Custom Slug That Should Not Be Truncated", "F005-a-very-long-custom-slug-that-should-not-be-truncated"},
		{10, "UPPER--CASE", "F010-upper-case"},
		{43, "F043-dashboard-screenshot-gall", "F043-dashboard-screenshot-gall"},
		{43, "f043-dashboard-screenshot-gall", "F043-dashboard-screenshot-gall"},
	}
	for _, tt := range tests {
		got := GenerateFeatureIDFromSlug(tt.num, tt.slug)
		if got != tt.want {
			t.Errorf("GenerateFeatureIDFromSlug(%d, %q) = %q, want %q", tt.num, tt.slug, got, tt.want)
		}
	}
}

func TestNextFeatureNumber(t *testing.T) {
	root := t.TempDir()
	cfg := Config{Project: ProjectConfig{Name: "test"}}
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	ws := &Workspace{Root: root}

	n, err := NextFeatureNumber(ws)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("got %d, want 1", n)
	}

	f := Feature{ID: "F003-test", Name: "test", Status: "not-started"}
	if err := ws.SaveFeature(f); err != nil {
		t.Fatal(err)
	}
	n, err = NextFeatureNumber(ws)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("got %d, want 4", n)
	}

	f2 := Feature{ID: "F1000-four-digit", Name: "four-digit", Status: "not-started"}
	if err := ws.SaveFeature(f2); err != nil {
		t.Fatal(err)
	}
	n, err = NextFeatureNumber(ws)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1001 {
		t.Errorf("got %d, want 1001", n)
	}
}
