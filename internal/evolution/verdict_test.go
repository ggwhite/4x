package evolution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVerdicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gate-verdicts.json")
	content := `{"verdicts":[
		{"title":"A","verdict":"accept","value_score":0.8,"why_not_hack":"real gap","reason":"covers escalation"},
		{"title":"B","verdict":"reject","value_score":0.2,"reason":"low value"}
	]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ParseVerdicts(path)
	if err != nil {
		t.Fatalf("ParseVerdicts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Verdict != VerdictAccept || got[0].ValueScore != 0.8 || got[0].WhyNotHack != "real gap" {
		t.Errorf("verdict[0] = %+v", got[0])
	}
	if got[1].Verdict != VerdictReject || got[1].Reason != "low value" {
		t.Errorf("verdict[1] = %+v", got[1])
	}
}

func TestParseVerdicts_MissingFile(t *testing.T) {
	if _, err := ParseVerdicts(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}
