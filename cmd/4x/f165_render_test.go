package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/doctor"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// renderWorkspace 建一個含 features 的 in-process workspace（不 spawn 子程序）。
// captureStdout 定義於 evolve_test.go，同 package 共用。
func renderWorkspace(t *testing.T) *protocol.Workspace {
	t.Helper()
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{Project: protocol.ProjectConfig{Name: "f165-render"}}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	return ws
}

func TestShowAllFeatures_InProcess(t *testing.T) {
	ws := renderWorkspace(t)
	if err := ws.SaveFeature(feat.Feature{ID: "F001-alpha", Name: "F001: Alpha", Status: feat.StatusInProgress}); err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveFeature(feat.Feature{ID: "F002-beta", Name: "F002: Beta", Status: feat.StatusNotStarted}); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F001-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F001-alpha", protocol.State{FeatureID: "F001-alpha", Phase: protocol.PhaseCoding, Round: 1, MaxRounds: 5, Active: true}); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := captureStdout(t, func() { callErr = showAllFeatures(ws, false) })
	if callErr != nil {
		t.Fatalf("showAllFeatures: %v", callErr)
	}
	if !strings.Contains(out, "F001-alpha") || !strings.Contains(out, "F002-beta") {
		t.Errorf("output missing feature ids:\n%s", out)
	}
	if !strings.Contains(out, "Total:") {
		t.Errorf("output missing Total summary:\n%s", out)
	}

	// pending-only 過濾掉 done/not-started 以外者仍應含 in-progress feature
	pendOut := captureStdout(t, func() { callErr = showAllFeatures(ws, true) })
	if callErr != nil {
		t.Fatalf("showAllFeatures(pending): %v", callErr)
	}
	if !strings.Contains(pendOut, "F001-alpha") {
		t.Errorf("pending view should include in-progress F001-alpha:\n%s", pendOut)
	}

	// 空 workspace → "No features found"
	empty := renderWorkspace(t)
	emptyOut := captureStdout(t, func() { _ = showAllFeatures(empty, false) })
	if !strings.Contains(emptyOut, "No features found") {
		t.Errorf("empty workspace should print No features found:\n%s", emptyOut)
	}
}

func TestShowAllFeaturesJSON_InProcess(t *testing.T) {
	ws := renderWorkspace(t)
	if err := ws.SaveFeature(feat.Feature{ID: "F010-x", Name: "F010: X", Status: feat.StatusNotStarted}); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := captureStdout(t, func() { callErr = showAllFeaturesJSON(ws) })
	if callErr != nil {
		t.Fatalf("showAllFeaturesJSON: %v", callErr)
	}
	var parsed struct {
		Features []struct {
			ID string `json:"id"`
		} `json:"features"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if len(parsed.Features) != 1 || parsed.Features[0].ID != "F010-x" {
		t.Errorf("features = %+v, want single F010-x", parsed.Features)
	}
}

func TestShowFeatureDetail_InProcess(t *testing.T) {
	ws := renderWorkspace(t)
	if err := ws.SaveFeature(feat.Feature{ID: "F020-detail", Name: "F020: Detail", Status: feat.StatusInProgress}); err != nil {
		t.Fatal(err)
	}
	if err := ws.InitFeatureDir("F020-detail"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F020-detail", protocol.State{FeatureID: "F020-detail", Phase: protocol.PhaseReviewing, Round: 2, MaxRounds: 5}); err != nil {
		t.Fatal(err)
	}

	var callErr error
	out := captureStdout(t, func() { callErr = showFeatureDetail(ws, "F020-detail") })
	if callErr != nil {
		t.Fatalf("showFeatureDetail: %v", callErr)
	}
	if !strings.Contains(out, "F020-detail") || !strings.Contains(out, "reviewing") {
		t.Errorf("detail output missing id/phase:\n%s", out)
	}

	jout := captureStdout(t, func() { callErr = showFeatureDetailJSON(ws, "F020-detail") })
	if callErr != nil {
		t.Fatalf("showFeatureDetailJSON: %v", callErr)
	}
	var detail struct {
		Feature struct {
			ID string `json:"id"`
		} `json:"feature"`
		State struct {
			Phase string `json:"phase"`
		} `json:"state"`
	}
	if err := json.Unmarshal([]byte(jout), &detail); err != nil {
		t.Fatalf("detail JSON invalid: %v\n%s", err, jout)
	}
	if detail.Feature.ID != "F020-detail" {
		t.Errorf("detail.feature.id = %q, want F020-detail", detail.Feature.ID)
	}
	if detail.State.Phase != string(protocol.PhaseReviewing) {
		t.Errorf("detail.state.phase = %q, want reviewing", detail.State.Phase)
	}

	// 不存在的 feature → error
	if err := showFeatureDetail(ws, "F999-nope"); err == nil {
		t.Error("showFeatureDetail on missing feature should error")
	}
}

func TestRenderCost_InProcess(t *testing.T) {
	ws := renderWorkspace(t)
	writeStreamLog(t, ws, "F030-cost", "round-1-coder.stream.jsonl", resultLine(4.0))
	writeStreamLog(t, ws, "F030-cost", "round-2-coder.stream.jsonl", resultLine(1.0))

	data, err := collectCost(ws, "")
	if err != nil {
		t.Fatalf("collectCost: %v", err)
	}

	// by-role 文字
	roleTxt := captureStdout(t, func() {
		if e := renderByRole(data, false); e != nil {
			t.Errorf("renderByRole text: %v", e)
		}
	})
	if !strings.Contains(roleTxt, "Cost by role") {
		t.Errorf("by-role text header missing:\n%s", roleTxt)
	}

	// by-role JSON
	roleJSON := captureStdout(t, func() {
		if e := renderByRole(data, true); e != nil {
			t.Errorf("renderByRole json: %v", e)
		}
	})
	var cj struct {
		View     string  `json:"view"`
		TotalUSD float64 `json:"totalUsd"`
	}
	if err := json.Unmarshal([]byte(roleJSON), &cj); err != nil {
		t.Fatalf("by-role JSON invalid: %v\n%s", err, roleJSON)
	}
	if cj.View != "by-role" || cj.TotalUSD != 5.0 {
		t.Errorf("by-role JSON = %+v, want by-role/5.0", cj)
	}

	// by-round 文字 + JSON
	roundTxt := captureStdout(t, func() { _ = renderByRound(data, "", false) })
	if !strings.Contains(roundTxt, "Cost by round") {
		t.Errorf("by-round text header missing:\n%s", roundTxt)
	}
	roundJSON := captureStdout(t, func() { _ = renderByRound(data, "", true) })
	if !json.Valid([]byte(roundJSON)) {
		t.Errorf("by-round JSON invalid:\n%s", roundJSON)
	}

	// by-feature 明細
	featData, err := collectCost(ws, "F030-cost")
	if err != nil {
		t.Fatalf("collectCost feature: %v", err)
	}
	featTxt := captureStdout(t, func() { _ = renderByFeature(featData, "F030-cost", false) })
	if !strings.Contains(featTxt, "F030-cost") {
		t.Errorf("by-feature text missing feature id:\n%s", featTxt)
	}
}

func TestPrintDoctorReport_InProcess(t *testing.T) {
	ws := renderWorkspace(t)
	report, err := doctor.Diagnose(doctor.Options{Root: ws.Root})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	out := captureStdout(t, func() { printDoctorReport(report) })
	if !strings.Contains(out, "──") || !strings.Contains(out, "Summary:") {
		t.Errorf("doctor report missing section headers/summary:\n%s", out)
	}
}
