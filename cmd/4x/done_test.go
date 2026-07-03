package main

import (
	"encoding/json"
	"strings"
	"testing"
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
