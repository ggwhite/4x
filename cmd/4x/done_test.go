package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/gitops"
)

// TestDoneResult_MRUrls 涵蓋 AC-18：doneResult 的 MRUrls 以 json tag "mrUrls" 序列化，
// 有值時出現在 --json 輸出、為空時因 omitempty 省略。
func TestDoneResult_MRUrls(t *testing.T) {
	result := doneResult{FeatureID: "F127-x", Phase: "done", Merged: true, MRUrls: map[string]string{
		".": "https://github.com/acme/widget/pull/7",
	}}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"mrUrls"`) {
		t.Errorf("json output missing mrUrls field: %s", data)
	}
	if !strings.Contains(string(data), "https://github.com/acme/widget/pull/7") {
		t.Errorf("json output missing MR URL: %s", data)
	}
}

func TestDoneResult_MRUrls_OmittedWhenEmpty(t *testing.T) {
	result := doneResult{FeatureID: "F127-x", Phase: "done", Merged: true}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), "mrUrls") {
		t.Errorf("json output should omit empty mrUrls field: %s", data)
	}
}

// TestDoneResult_SharedPathsJSON 涵蓋 AC-11：三個新欄位以 sharedPaths / sharedPathsNotes /
// error 序列化，有值時出現、為零值時因 omitempty 省略（成功路徑輸出形狀不變）。
func TestDoneResult_SharedPathsJSON(t *testing.T) {
	withValues := doneResult{
		FeatureID:        "F186-x",
		Phase:            "done",
		Merged:           true,
		SharedPaths:      []string{"docker-compose.yml"},
		SharedPathsNotes: []string{"Dockerfile: missing in worktree, deletion not propagated"},
		Error:            "shared_paths dirty in main workspace, aborting merge: docker-compose.yml",
	}
	data, err := json.Marshal(withValues)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for _, key := range []string{`"sharedPaths"`, `"sharedPathsNotes"`, `"error"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("json output missing %s: %s", key, data)
		}
	}
	if !strings.Contains(string(data), "docker-compose.yml") {
		t.Errorf("json output missing merged path: %s", data)
	}
	if !strings.Contains(string(data), "deletion not propagated") {
		t.Errorf("json output missing note text: %s", data)
	}

	empty := doneResult{FeatureID: "F186-x", Phase: "done", Merged: true}
	data, err = json.Marshal(empty)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	for _, key := range []string{"sharedPaths", "sharedPathsNotes", "error"} {
		if strings.Contains(string(data), key) {
			t.Errorf("json output should omit empty %s: %s", key, data)
		}
	}
}

// TestPrintSharedPaths_PrintsOnSkippedResult 鎖住非 JSON stdout 這條輸出通道：兩類訊息的固定
// 前綴，以及 printSharedPaths 不得被包進 !result.Skipped——PushAndOpenMR 的 !anyAhead 路徑回
// Skipped: true 卻仍做 merge-back，包進去會讓那條路徑整段漏印，而 22 條 AC 仍會全綠。
func TestPrintSharedPaths_PrintsOnSkippedResult(t *testing.T) {
	out := captureStdout(t, func() {
		printSharedPaths(gitops.MergeResult{
			Skipped:           true,
			SharedPathsMerged: []string{"docker-compose.yml"},
			SharedPathsNotes:  []string{"Dockerfile: missing in worktree, deletion not propagated"},
		})
	})

	if !strings.Contains(out, "shared-path merged: docker-compose.yml") {
		t.Errorf("stdout missing merged line: %q", out)
	}
	if !strings.Contains(out, "shared-path WARNING: Dockerfile: missing in worktree") {
		t.Errorf("stdout missing WARNING line: %q", out)
	}
}

// TestPrintSharedPaths_SilentWithoutSharedPaths 鎖住零回歸：未宣告 shared_paths 的 feature
// 兩個欄位皆為 nil，stdout 不得多出任何一行。
func TestPrintSharedPaths_SilentWithoutSharedPaths(t *testing.T) {
	out := captureStdout(t, func() { printSharedPaths(gitops.MergeResult{}) })
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}
