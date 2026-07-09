package main

import (
	"encoding/json"
	"strings"
	"testing"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// 這些測試以 in-process 方式（t.Chdir + newXxxCmd().Execute）驅動指令，涵蓋 constructor +
// RunE + Find + 下游 render helper——與 f165_cmd_test.go 的 subprocess 整合驗證互補，
// 差別在 in-process 執行會計入 coverage profile。斷言的是輸出行為，非結構欄位（DR-2）。

func initRenderWSAt(t *testing.T, features ...feat.Feature) string {
	t.Helper()
	dir := t.TempDir()
	if err := protocol.Init(dir, protocol.Config{Project: protocol.ProjectConfig{Name: "f165-ip"}}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: dir}
	for _, f := range features {
		if err := ws.SaveFeature(f); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestStatusCmd_InProcess(t *testing.T) {
	dir := initRenderWSAt(t,
		feat.Feature{ID: "F001-a", Name: "F001: A", Status: feat.StatusInProgress},
		feat.Feature{ID: "F002-b", Name: "F002: B", Status: feat.StatusNotStarted},
	)
	t.Chdir(dir)

	// 文字視圖
	var err error
	out := captureStdout(t, func() {
		cmd := newStatusCmd()
		cmd.SetArgs([]string{})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("status Execute: %v", err)
	}
	if !strings.Contains(out, "F001-a") || !strings.Contains(out, "Total:") {
		t.Errorf("status output unexpected:\n%s", out)
	}

	// --json 視圖
	jout := captureStdout(t, func() {
		cmd := newStatusCmd()
		cmd.SetArgs([]string{"--json"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("status --json Execute: %v", err)
	}
	if !json.Valid([]byte(jout)) {
		t.Errorf("status --json not valid JSON:\n%s", jout)
	}

	// 單一 feature detail
	dout := captureStdout(t, func() {
		cmd := newStatusCmd()
		cmd.SetArgs([]string{"F001-a"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("status <id> Execute: %v", err)
	}
	if !strings.Contains(dout, "F001-a") {
		t.Errorf("status detail missing id:\n%s", dout)
	}
}

func TestCostCmd_InProcess(t *testing.T) {
	dir := initRenderWSAt(t)
	ws := &protocol.Workspace{Root: dir}
	writeStreamLog(t, ws, "F040-c", "round-1-coder.stream.jsonl", resultLine(3.0))
	writeStreamLog(t, ws, "F040-c", "round-2-coder.stream.jsonl", resultLine(1.0))
	t.Chdir(dir)

	var err error
	out := captureStdout(t, func() {
		cmd := newCostCmd()
		cmd.SetArgs([]string{"--json"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("cost --json Execute: %v", err)
	}
	var cj struct {
		View     string  `json:"view"`
		TotalUSD float64 `json:"totalUsd"`
	}
	if e := json.Unmarshal([]byte(out), &cj); e != nil {
		t.Fatalf("cost --json invalid: %v\n%s", e, out)
	}
	if cj.View != "by-role" || cj.TotalUSD != 4.0 {
		t.Errorf("cost --json = %+v, want by-role/4.0", cj)
	}

	// by-round 文字
	rout := captureStdout(t, func() {
		cmd := newCostCmd()
		cmd.SetArgs([]string{"--by-round"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("cost --by-round Execute: %v", err)
	}
	if !strings.Contains(rout, "Cost by round") {
		t.Errorf("cost --by-round header missing:\n%s", rout)
	}
}

func TestNewCmd_InProcess(t *testing.T) {
	dir := initRenderWSAt(t)
	t.Chdir(dir)

	var err error
	captureStdout(t, func() {
		cmd := newNewCmd()
		cmd.SetArgs([]string{"My Feature", "--id", "myfeat", "--desc", "do the thing", "--priority", "2"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("new Execute: %v", err)
	}

	ws := &protocol.Workspace{Root: dir}
	features, lerr := ws.ListFeatures()
	if lerr != nil {
		t.Fatalf("list features: %v", lerr)
	}
	if len(features) != 1 {
		t.Fatalf("got %d features, want 1 created", len(features))
	}
	f := features[0]
	if !strings.Contains(f.ID, "myfeat") {
		t.Errorf("feature ID = %q, want to contain custom slug myfeat", f.ID)
	}
	if f.Description != "do the thing" {
		t.Errorf("description = %q, want 'do the thing'", f.Description)
	}
	if f.Priority == nil || *f.Priority != 2 {
		t.Errorf("priority = %v, want 2", f.Priority)
	}
	if f.Status != feat.StatusNotStarted {
		t.Errorf("status = %q, want not-started", f.Status)
	}
}

func TestNewCmd_WithSubtasksRules_InProcess(t *testing.T) {
	dir := initRenderWSAt(t,
		feat.Feature{ID: "F070-dep", Name: "Dep", Status: feat.StatusDone},
	)
	t.Chdir(dir)

	var err error
	captureStdout(t, func() {
		cmd := newNewCmd()
		cmd.SetArgs([]string{
			"Rich Feature", "--id", "rich",
			"--subtask", "impl:Implement it",
			"--subtask", "test:Test it",
			"--rule", "spec: docs/design/rich-spec.md",
			"--depends", "F070-dep",
		})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("new --subtask Execute: %v", err)
	}

	ws := &protocol.Workspace{Root: dir}
	features, _ := ws.ListFeatures()
	var rich *feat.Feature
	for i := range features {
		if strings.Contains(features[i].ID, "rich") {
			rich = &features[i]
		}
	}
	if rich == nil {
		t.Fatal("rich feature not created")
	}
	if len(rich.Subtasks) != 2 || rich.Subtasks[0].ID != "impl" {
		t.Errorf("subtasks = %+v, want 2 with first id=impl", rich.Subtasks)
	}
	if len(rich.Depends) != 1 || rich.Depends[0] != "F070-dep" {
		t.Errorf("depends = %v, want [F070-dep]", rich.Depends)
	}
}

func TestSubtaskCmd_InProcess(t *testing.T) {
	dir := initRenderWSAt(t, feat.Feature{
		ID: "F080-st", Name: "ST", Status: feat.StatusInProgress,
		Subtasks: []feat.Subtask{{ID: "impl", Name: "Implement", Status: "not-started"}},
	})
	t.Chdir(dir)
	ws := &protocol.Workspace{Root: dir}

	// 成功更新 subtask 狀態
	var err error
	out := captureStdout(t, func() {
		cmd := newSubtaskCmd()
		cmd.SetArgs([]string{"F080-st", "impl", "--status", "done"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("subtask Execute: %v", err)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("subtask output missing new status:\n%s", out)
	}
	f, _ := ws.LoadFeature("F080-st")
	if f.Subtasks[0].Status != "done" {
		t.Errorf("subtask status = %q, want done", f.Subtasks[0].Status)
	}

	// 非法 status → error
	captureStdout(t, func() {
		cmd := newSubtaskCmd()
		cmd.SetArgs([]string{"F080-st", "impl", "--status", "bogus"})
		err = cmd.Execute()
	})
	if err == nil {
		t.Error("invalid status should error")
	}

	// 不存在的 subtask → error
	captureStdout(t, func() {
		cmd := newSubtaskCmd()
		cmd.SetArgs([]string{"F080-st", "nope", "--status", "done"})
		err = cmd.Execute()
	})
	if err == nil {
		t.Error("missing subtask should error")
	}
}

func TestResolvePathFeatureID(t *testing.T) {
	dir := initRenderWSAt(t,
		feat.Feature{ID: "F050-x", Name: "X", Status: feat.StatusInProgress},
		feat.Feature{ID: "F051-y", Name: "Y", Status: feat.StatusInProgress},
	)
	ws := &protocol.Workspace{Root: dir}
	if err := ws.InitFeatureDir("F050-x"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F050-x", protocol.State{FeatureID: "F050-x", Phase: protocol.PhaseCoding, Active: true}); err != nil {
		t.Fatal(err)
	}

	// 位置參數優先
	if id, ok := resolvePathFeatureID(ws, []string{"F051-y"}); !ok || id != "F051-y" {
		t.Errorf("arg branch: got (%q,%v), want (F051-y,true)", id, ok)
	}
	// 位置參數解析失敗 → (\"\",false)
	if id, ok := resolvePathFeatureID(ws, []string{"F999-none"}); ok || id != "" {
		t.Errorf("bad arg: got (%q,%v), want (\"\",false)", id, ok)
	}
	// FOURX_FEATURE_ID env 分支
	t.Setenv("FOURX_FEATURE_ID", "F050-x")
	if id, ok := resolvePathFeatureID(ws, nil); !ok || id != "F050-x" {
		t.Errorf("env branch: got (%q,%v), want (F050-x,true)", id, ok)
	}
}

func TestResolveActiveFeatureID(t *testing.T) {
	dir := initRenderWSAt(t,
		feat.Feature{ID: "F060-a", Name: "A", Status: feat.StatusInProgress},
		feat.Feature{ID: "F061-b", Name: "B", Status: feat.StatusNotStarted},
	)
	ws := &protocol.Workspace{Root: dir}

	// 0 個 active → (\"\",false)
	if id, ok := resolveActiveFeatureID(ws); ok || id != "" {
		t.Errorf("no active: got (%q,%v), want (\"\",false)", id, ok)
	}

	// 唯一 active → 該 id
	if err := ws.InitFeatureDir("F060-a"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F060-a", protocol.State{FeatureID: "F060-a", Phase: protocol.PhaseCoding, Active: true}); err != nil {
		t.Fatal(err)
	}
	if id, ok := resolveActiveFeatureID(ws); !ok || id != "F060-a" {
		t.Errorf("single active: got (%q,%v), want (F060-a,true)", id, ok)
	}

	// 多於 1 個 active → (\"\",false)
	if err := ws.InitFeatureDir("F061-b"); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteState("F061-b", protocol.State{FeatureID: "F061-b", Phase: protocol.PhaseCoding, Active: true}); err != nil {
		t.Fatal(err)
	}
	if id, ok := resolveActiveFeatureID(ws); ok || id != "" {
		t.Errorf("multiple active: got (%q,%v), want (\"\",false)", id, ok)
	}
}

func TestConfigCmd_InProcess(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	dir := initRenderWSAt(t)
	t.Chdir(dir)

	// config set locale zh-TW
	var err error
	captureStdout(t, func() {
		cmd := newConfigCmd()
		cmd.SetArgs([]string{"set", "locale", "zh-TW"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("config set Execute: %v", err)
	}

	// config get locale → zh-TW
	out := captureStdout(t, func() {
		cmd := newConfigCmd()
		cmd.SetArgs([]string{"get", "locale"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("config get Execute: %v", err)
	}
	if strings.TrimSpace(out) != "zh-TW" {
		t.Errorf("config get locale = %q, want zh-TW", strings.TrimSpace(out))
	}

	// config list 含設定值
	lout := captureStdout(t, func() {
		cmd := newConfigCmd()
		cmd.SetArgs([]string{"list"})
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("config list Execute: %v", err)
	}
	if !strings.Contains(lout, "zh-TW") {
		t.Errorf("config list missing zh-TW:\n%s", lout)
	}
}
