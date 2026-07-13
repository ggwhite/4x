package prompt

import (
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// TestGenerate_ExtraPromptInjection 驗證 AC-2：Context.ExtraPrompt 有值時，Generate 產出的
// prompt 同時含 note 內容與 One-Shot Note 標頭；空字串時兩者皆不出現。
func TestGenerate_ExtraPromptInjection(t *testing.T) {
	ws := newTestWorkspace(t)
	featureID := "F185-extra"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}
	feature := feat.Feature{ID: featureID, Name: "Test"}

	const note = "FOCUS-XYZ"
	const header = "One-Shot Note"

	withNote := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{}, ExtraPrompt: note}
	got, err := Generate(withNote, protocol.RoleCoder, 1, 0, "")
	if err != nil {
		t.Fatalf("Generate with note: %v", err)
	}
	if !strings.Contains(got, note) {
		t.Errorf("prompt should contain note %q\n---\n%s", note, got)
	}
	if !strings.Contains(got, header) {
		t.Errorf("prompt should contain One-Shot Note header\n---\n%s", got)
	}

	empty := &Context{Ws: ws, RunnerWs: ws, Feature: feature, Cfg: protocol.Config{}, ExtraPrompt: ""}
	gotEmpty, err := Generate(empty, protocol.RoleCoder, 1, 0, "")
	if err != nil {
		t.Fatalf("Generate empty: %v", err)
	}
	if strings.Contains(gotEmpty, header) {
		t.Errorf("empty ExtraPrompt should NOT render One-Shot Note header\n---\n%s", gotEmpty)
	}
}
