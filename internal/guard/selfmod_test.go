package guard

import (
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestMatchProtected(t *testing.T) {
	protected := []string{"internal/state/", "internal/guard/"}
	cases := []struct {
		path string
		want bool
	}{
		{"internal/state/machine.go", true},
		{"internal/guard/check.go", true},
		{"internal/protocol/types.go", false},
		{"cmd/4x/run.go", false},
		{"internal/state", false}, // 無結尾斜線不算落在前綴下
	}
	for _, c := range cases {
		if got := MatchProtected(c.path, protected); got != c.want {
			t.Errorf("MatchProtected(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestProtectedHits(t *testing.T) {
	files := []protocol.ChangedFile{
		{Path: "internal/guard/check.go", Lines: 10},
		{Path: "cmd/4x/run.go", Lines: 99},
		{Path: "internal/state/machine.go", Lines: 5},
	}
	hits := ProtectedHits(files, DefaultProtectedPaths)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d: %+v", len(hits), hits)
	}
	total := 0
	for _, h := range hits {
		total += h.Lines
	}
	if total != 15 {
		t.Errorf("expected total 15 lines, got %d", total)
	}
}

func TestHasAccompanyingTests(t *testing.T) {
	protected := DefaultProtectedPaths
	cases := []struct {
		name  string
		paths []string
		want  bool
	}{
		{"prod 無 test → false", []string{"internal/guard/check.go"}, false},
		{"prod + test → true", []string{"internal/guard/check.go", "internal/guard/check_test.go"}, true},
		{"只有 test → true", []string{"internal/guard/check_test.go"}, true},
		{"無受保護 .go 變更 → true", []string{"cmd/4x/run.go"}, true},
		{"非受保護 prod + 受保護 test → true", []string{"cmd/4x/run.go", "internal/guard/x_test.go"}, true},
		{"空 → true", nil, true},
	}
	for _, c := range cases {
		if got := HasAccompanyingTests(c.paths, protected); got != c.want {
			t.Errorf("%s: HasAccompanyingTests(%v) = %v, want %v", c.name, c.paths, got, c.want)
		}
	}
}

func TestSelfModNeedsApproval(t *testing.T) {
	cases := []struct {
		name     string
		touched  bool
		approved bool // state.SelfModApproved
		flag     bool // --approve-self-mod
		want     bool
	}{
		{"未觸及", false, false, false, false},
		{"觸及未核可未帶旗標 → 需 approve", true, false, false, true},
		{"觸及已核可 → 放行", true, true, false, false},
		{"觸及帶旗標 → 放行", true, false, true, false},
	}
	for _, c := range cases {
		s := protocol.State{SelfModTouched: c.touched, SelfModApproved: c.approved}
		if got := SelfModNeedsApproval(s, c.flag); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestResolveSelfMod_Defaults(t *testing.T) {
	// nil → 全套預設
	p := ResolveSelfMod(protocol.Config{})
	if len(p.ProtectedPaths) != len(DefaultProtectedPaths) {
		t.Errorf("nil SelfMod should fall back to default protected paths, got %v", p.ProtectedPaths)
	}
	if p.MaxDiffLines != DefaultSelfModMaxDiffLines {
		t.Errorf("MaxDiffLines = %d, want default %d", p.MaxDiffLines, DefaultSelfModMaxDiffLines)
	}
	if !p.RequireTests {
		t.Error("RequireTests should default to true")
	}

	// 欄位零值 → 個別退回預設
	p = ResolveSelfMod(protocol.Config{SelfMod: &protocol.SelfModSettings{}})
	if len(p.ProtectedPaths) != len(DefaultProtectedPaths) || p.MaxDiffLines != DefaultSelfModMaxDiffLines || !p.RequireTests {
		t.Errorf("zero-value SelfMod should fall back to defaults, got %+v", p)
	}

	// 明確設定 → 沿用
	reqFalse := false
	p = ResolveSelfMod(protocol.Config{SelfMod: &protocol.SelfModSettings{
		ProtectedPaths: []string{"foo/"},
		MaxDiffLines:   50,
		RequireTests:   &reqFalse,
	}})
	if len(p.ProtectedPaths) != 1 || p.ProtectedPaths[0] != "foo/" {
		t.Errorf("explicit ProtectedPaths not used, got %v", p.ProtectedPaths)
	}
	if p.MaxDiffLines != 50 {
		t.Errorf("explicit MaxDiffLines not used, got %d", p.MaxDiffLines)
	}
	if p.RequireTests {
		t.Error("explicit RequireTests=false should be honored")
	}
}
