package protocol

import "testing"

func TestParseDiscoveredFeatures(t *testing.T) {
	t.Run("multiple blocks", func(t *testing.T) {
		report := `# Deep Review Report

## Discovered Issues
[NEW-FEATURE] Add rate limiting
The API has no rate limiting.
Suggest token bucket per client.

[NEW-FEATURE] Cache feature list
ListFeatures re-reads disk every call.

## Statistics
- Total: 2
`
		got := ParseDiscoveredFeatures(report)
		if len(got) != 2 {
			t.Fatalf("want 2 blocks, got %d: %+v", len(got), got)
		}
		if got[0].Title != "Add rate limiting" {
			t.Errorf("block 0 title = %q", got[0].Title)
		}
		if got[0].Description != "The API has no rate limiting.\nSuggest token bucket per client." {
			t.Errorf("block 0 desc = %q", got[0].Description)
		}
		if got[1].Title != "Cache feature list" {
			t.Errorf("block 1 title = %q", got[1].Title)
		}
	})

	t.Run("no block", func(t *testing.T) {
		got := ParseDiscoveredFeatures("# Report\nNothing here\n## Verdict\nPASS\n")
		if len(got) != 0 {
			t.Fatalf("want 0, got %d", len(got))
		}
	})

	t.Run("empty title dropped", func(t *testing.T) {
		report := "[NEW-FEATURE]\nsome description\n"
		got := ParseDiscoveredFeatures(report)
		if len(got) != 0 {
			t.Fatalf("want 0 (empty title dropped), got %d: %+v", len(got), got)
		}
	})

	t.Run("description truncated at heading", func(t *testing.T) {
		report := "[NEW-FEATURE] Title A\nline 1\nline 2\n## Next Heading\nirrelevant\n"
		got := ParseDiscoveredFeatures(report)
		if len(got) != 1 {
			t.Fatalf("want 1, got %d", len(got))
		}
		if got[0].Description != "line 1\nline 2" {
			t.Errorf("desc = %q", got[0].Description)
		}
	})

	t.Run("description truncated at blank line", func(t *testing.T) {
		report := "[NEW-FEATURE] Title B\nonly line\n\nseparate paragraph\n"
		got := ParseDiscoveredFeatures(report)
		if len(got) != 1 {
			t.Fatalf("want 1, got %d", len(got))
		}
		if got[0].Description != "only line" {
			t.Errorf("desc = %q", got[0].Description)
		}
	})
}

func TestIsSimilarFeature(t *testing.T) {
	t.Run("similar", func(t *testing.T) {
		a := "Add rate limiting to the API endpoints"
		b := "Add rate limiting to API endpoints"
		if !IsSimilarFeature(a, b) {
			t.Errorf("expected similar: %q vs %q", a, b)
		}
	})

	t.Run("unrelated", func(t *testing.T) {
		a := "Add rate limiting to the API"
		b := "Refactor the dashboard color theme system"
		if IsSimilarFeature(a, b) {
			t.Errorf("expected not similar: %q vs %q", a, b)
		}
	})

	t.Run("empty", func(t *testing.T) {
		if IsSimilarFeature("", "anything") {
			t.Errorf("empty should not be similar")
		}
	})
}

func TestDedupeDiscovered(t *testing.T) {
	existing := []Feature{
		{Name: "F010: Rate limiting", Description: "Add rate limiting to the API endpoints"},
	}
	candidates := []DiscoveredFeature{
		{Title: "Add rate limiting to API endpoints", Description: ""},               // similar to existing → drop
		{Title: "Cache the feature list on disk", Description: "avoid re-reading"},   // new → keep
		{Title: "Cache feature list on disk", Description: "avoid re-reading every"}, // dup of previous kept → drop
		{Title: "Improve dashboard color theme contrast", Description: "for a11y"},   // new → keep
	}

	got := DedupeDiscovered(candidates, existing)
	if len(got) != 2 {
		t.Fatalf("want 2 kept, got %d: %+v", len(got), got)
	}
	if got[0].Title != "Cache the feature list on disk" {
		t.Errorf("kept[0] = %q", got[0].Title)
	}
	if got[1].Title != "Improve dashboard color theme contrast" {
		t.Errorf("kept[1] = %q", got[1].Title)
	}
}

func TestResolveMaxDiscoveredFeatures(t *testing.T) {
	if got := ResolveMaxDiscoveredFeatures(Config{}); got != defaultMaxDiscoveredFeatures {
		t.Errorf("unset: want %d, got %d", defaultMaxDiscoveredFeatures, got)
	}
	if got := ResolveMaxDiscoveredFeatures(Config{MaxDiscoveredFeatures: 5}); got != 5 {
		t.Errorf("set: want 5, got %d", got)
	}
	if got := ResolveMaxDiscoveredFeatures(Config{MaxDiscoveredFeatures: -1}); got != defaultMaxDiscoveredFeatures {
		t.Errorf("negative: want %d, got %d", defaultMaxDiscoveredFeatures, got)
	}
}
