package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

func p166Int(v int) *int { return &v }

// AC-7：`4x new` 未指定 --profile 時印出建議行；指定 --profile 或 --json 時不印。
func TestNew_ProfileSuggestion(t *testing.T) {
	dir := initRenderWSAt(t)
	t.Chdir(dir)

	// 未指定 --profile → 印出建議（含 "profile 建議"）。
	out := captureStdout(t, func() {
		cmd := newNewCmd()
		cmd.SetArgs([]string{"Advisor Feature", "--id", "adv", "--desc", "do the thing", "--priority", "2"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("new Execute: %v", err)
		}
	})
	if !strings.Contains(out, "profile 建議") {
		t.Errorf("new without --profile should print suggestion, got:\n%s", out)
	}

	// 指定 --profile → 不印建議。
	out2 := captureStdout(t, func() {
		cmd := newNewCmd()
		cmd.SetArgs([]string{"Explicit Profile", "--id", "expl", "--profile", "quick"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("new --profile Execute: %v", err)
		}
	})
	if strings.Contains(out2, "profile 建議") {
		t.Errorf("new --profile should NOT print suggestion, got:\n%s", out2)
	}

	// --json → 不印建議，且輸出為合法 JSON。
	jout := captureStdout(t, func() {
		cmd := newNewCmd()
		cmd.SetArgs([]string{"Json Feature", "--id", "jsonf", "--json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("new --json Execute: %v", err)
		}
	})
	if strings.Contains(jout, "profile 建議") {
		t.Errorf("new --json should NOT print suggestion, got:\n%s", jout)
	}
	if err := json.Unmarshal([]byte(jout), &struct {
		FeatureID string `json:"featureId"`
	}{}); err != nil {
		t.Errorf("new --json output not valid JSON: %v\n%s", err, jout)
	}
}

// AC-8：`4x run` 非互動未指定 profile 時印建議但不改變解析結果（DR-2）。
func TestRun_ProfileSuggestion_NonInteractive(t *testing.T) {
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "f166"}}
	f := feat.Feature{ID: "F166-x", Name: "big feature",
		Subtasks: []feat.Subtask{{ID: "1"}, {ID: "2"}, {ID: "3"}},
		Repos:    []string{"a", "b", "c"},
		Priority: p166Int(0),
	}

	// helper 在 ok=true 時寫入建議。
	var buf bytes.Buffer
	maybePrintProfileSuggestion(&buf, cfg, f)
	if !strings.Contains(buf.String(), "profile 建議") {
		t.Errorf("maybePrintProfileSuggestion should write suggestion, got:\n%s", buf.String())
	}

	// resolveProfileFlag 在非互動、無 profile 時仍回傳 s.Profile（不因建議改變）。
	// 測試環境 stdout 非 char device，isInteractiveTerminal()==false，走非互動路徑（dryRun=false）。
	s := protocol.State{Profile: ""}
	var got string
	var rerr error
	captureStdout(t, func() {
		got, rerr = resolveProfileFlag("", s, cfg, f, false)
	})
	if rerr != nil {
		t.Fatalf("resolveProfileFlag err: %v", rerr)
	}
	if got != "" {
		t.Errorf("resolveProfileFlag = %q, want \"\" (= s.Profile, not auto-adopted)", got)
	}
}

// AC-8/AC-9：advisor 停用時 helper 不寫任何內容。
func TestMaybePrintProfileSuggestion_Disabled(t *testing.T) {
	cfg := protocol.Config{
		Project:        protocol.ProjectConfig{Name: "f166"},
		ProfileAdvisor: &protocol.ProfileAdvisorConfig{Enabled: boolPtr166(false)},
	}
	f := feat.Feature{ID: "F166-y", Name: "x", Priority: p166Int(1)}
	var buf bytes.Buffer
	maybePrintProfileSuggestion(&buf, cfg, f)
	if buf.Len() != 0 {
		t.Errorf("disabled advisor should write nothing, got:\n%s", buf.String())
	}
}

// AC-9：未觸發情境既有行為零改動——feature.Profile 已設時 resolveProfileFlag 直接回既有值、無建議輸出。
func TestResolveProfileFlag_FeatureProfileSet(t *testing.T) {
	cfg := protocol.Config{Project: protocol.ProjectConfig{Name: "f166"}}
	f := feat.Feature{ID: "F166-z", Name: "x", Profile: "quick", Priority: p166Int(0)}
	// s.Profile 空、feature.Profile 非空 → 觸發條件不成立，回 s.Profile（空）。
	got, err := resolveProfileFlag("", protocol.State{Profile: ""}, cfg, f, false)
	if err != nil {
		t.Fatalf("resolveProfileFlag err: %v", err)
	}
	if got != "" {
		t.Errorf("resolveProfileFlag = %q, want \"\" (feature.Profile path unchanged)", got)
	}

	// --profile 明確指定 → 直接回該值。
	got2, err := resolveProfileFlag("normal", protocol.State{}, cfg, f, false)
	if err != nil {
		t.Fatalf("resolveProfileFlag err: %v", err)
	}
	if got2 != "normal" {
		t.Errorf("resolveProfileFlag = %q, want normal", got2)
	}
}

func boolPtr166(v bool) *bool { return &v }
