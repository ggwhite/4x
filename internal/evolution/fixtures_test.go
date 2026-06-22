package evolution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// TestGoldenFixtures_VetoCatchesJunk 是 holdout-as-test：餵一個「橡皮圖章 gate」
// （把全部 candidate 都 accept，但垃圾項缺 why_not_hack 且低於 floor），驗證 POST-veto
// 仍擋下 expected.reject 全部、放行 expected.accept 全部，確保閘門不會漂移成橡皮圖章。
// fixtures 僅供測試讀，不得進任何 runtime prompt。
func TestGoldenFixtures_VetoCatchesJunk(t *testing.T) {
	base := filepath.Join("testdata", "gate-fixtures")
	pool, err := protocol.LoadCandidates(filepath.Join(base, "candidates.json"))
	if err != nil {
		t.Fatal(err)
	}
	cands := pool.Candidates

	var exp struct {
		Accept []string `json:"accept"`
		Reject []string `json:"reject"`
	}
	data, err := os.ReadFile(filepath.Join(base, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &exp); err != nil {
		t.Fatal(err)
	}

	// 橡皮圖章 gate：全部 accept；唯有垃圾項缺 why_not_hack 且低於 floor，模擬 gate 漏看。
	var verdicts []Verdict
	for _, c := range cands {
		v := Verdict{Title: c.Title, Verdict: VerdictAccept, ValueScore: 0.9, WhyNotHack: "stated"}
		if c.Title == "asdf test junk" {
			v.ValueScore = 0.1
			v.WhyNotHack = ""
		}
		verdicts = append(verdicts, v)
	}

	cfg := ResolvedEvolution{ValueFloor: 0.6, MaxAcceptPerRun: 10, MaxBacklogUndone: 100, DedupThreshold: 0.6}
	accepted, _ := PostVeto(cands, verdicts, nil, cfg)

	gotAccept := map[string]bool{}
	for _, a := range accepted {
		gotAccept[a.Title] = true
	}
	for _, title := range exp.Reject {
		if gotAccept[title] {
			t.Errorf("veto failed to reject junk candidate %q", title)
		}
	}
	for _, title := range exp.Accept {
		if !gotAccept[title] {
			t.Errorf("veto wrongly rejected valid candidate %q", title)
		}
	}
}
