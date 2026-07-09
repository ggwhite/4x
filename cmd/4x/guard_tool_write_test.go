package main

import (
	"strings"
	"testing"
)

// AC-7：guard-tool 對 Edit/Write 生效——reviewer 寫 source → stdout 含 deny JSON、exit 0；
// reviewer 寫 .4x artifact → 無 deny 輸出、exit 0。以真實 .4x 目錄 + 真實 bin/4x 子程序驗證。
func TestGuardToolWrite_EditIntercept(t *testing.T) {
	id := "F900-gtw"
	root := pathFixture(t, id,
		"id: "+id+"\nname: t\n",
		`{"featureId":"`+id+`","phase":"reviewing","role":"reviewer","active":true}`)

	t.Run("reviewer editing source denies", func(t *testing.T) {
		stdin := `{"tool_name":"Edit","tool_input":{"file_path":"cmd/4x/foo.go"}}`
		stdout, _, code := run4xIO(t, root, nil, stdin, "guard-tool")
		if code != 0 {
			t.Fatalf("guard-tool should always exit 0, got %d", code)
		}
		if !strings.Contains(stdout, `"permissionDecision":"deny"`) {
			t.Errorf("expected deny decision in stdout, got: %s", stdout)
		}
	})

	t.Run("reviewer editing artifact allows (no deny output)", func(t *testing.T) {
		stdin := `{"tool_name":"Write","tool_input":{"file_path":".4x/run/` + id + `/review-report.md"}}`
		stdout, _, code := run4xIO(t, root, nil, stdin, "guard-tool")
		if code != 0 {
			t.Fatalf("guard-tool should always exit 0, got %d", code)
		}
		if strings.Contains(stdout, `"permissionDecision":"deny"`) {
			t.Errorf("editing artifact should not deny, got: %s", stdout)
		}
	})
}
