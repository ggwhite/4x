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
		{100, "A very long feature name that exceeds the limit", "F100-a-very-long-feature-nam"},
		{1000, "Four digit feature", "F1000-four-digit-feature"},
		{99999, "Five digit feature", "F99999-five-digit-feature"},
	}
	for _, tt := range tests {
		got := GenerateFeatureID(tt.num, tt.name)
		if got != tt.want {
			t.Errorf("GenerateFeatureID(%d, %q) = %q, want %q", tt.num, tt.name, got, tt.want)
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
