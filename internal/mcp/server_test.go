package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestNewServer 驗證 NewServer 能正常建立並回傳非 nil 的 MCP server。
// AddTool 推導 input schema 失敗時會 panic（例如 jsonschema tag 寫成 "description=...,required"
// 這種 SDK 不接受的格式），此處把 panic 轉成 t.Fatalf，NEVER 改成 t.Skip 掩蓋。
func TestNewServer(t *testing.T) {
	var srv *mcp.Server
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewServer panicked: %v", r)
			}
		}()
		srv = NewServer("1.0.0", "/tmp/workspace")
	}()
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
}

// TestNewServer_ListTools 透過 in-memory transport 建立 client/server session，
// 驗證每個預期的工具都有註冊，且 input schema 的 required 欄位與 struct 定義一致、每個欄位都有 description。
func TestNewServer_ListTools(t *testing.T) {
	// 工具名稱 → 預期 required 欄位（依 struct field 順序）
	want := map[string][]string{
		"4x_status":        nil,
		"4x_new":           {"name"},
		"4x_run":           {"featureId"},
		"4x_stop":          {"featureId"},
		"4x_check":         {"featureId"},
		"4x_transition":    {"featureId", "to"},
		"4x_done":          {"featureId"},
		"4x_verify":        {"featureId"},
		"4x_approve":       {"featureId"},
		"4x_reject":        {"featureId"},
		"4x_mine":          nil,
		"4x_subtask":       {"featureId", "subtaskId", "status"},
		"4x_clean":         nil,
		"4x_doctor":        nil,
		"4x_learn_add":     {"category", "content"},
		"4x_learn_list":    nil,
		"4x_learn_prune":   nil,
		"4x_learn_promote": {"id"},
		"4x_learn_remove":  {"id"},
		"4x_evolve":        nil,
		"4x_config_get":    {"key"},
		"4x_config_set":    {"key", "value"},
		"4x_config_list":   nil,
		"4x_gate":          {"phase"},
		"4x_merge":         {"featureId"},
	}

	var srv *mcp.Server
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewServer panicked: %v", r)
			}
		}()
		srv = NewServer("1.0.0", t.TempDir())
	}()

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		got[tool.Name] = tool
	}
	if len(got) != len(want) {
		t.Errorf("tool count = %d, want %d", len(got), len(want))
	}

	for name, wantRequired := range want {
		tool, ok := got[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("%s: marshal input schema: %v", name, err)
		}
		var schema struct {
			Required   []string `json:"required"`
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: unmarshal input schema: %v", name, err)
		}
		if !slices.Equal(schema.Required, wantRequired) {
			t.Errorf("%s: required = %v, want %v", name, schema.Required, wantRequired)
		}
		for prop, ps := range schema.Properties {
			if ps.Description == "" {
				t.Errorf("%s: property %q has no description", name, prop)
			}
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected tool %q registered", name)
		}
	}
}

// TestWrapResult_Success 驗證有資料且無 error 時，回傳正確的成功 result。
func TestWrapResult_Success(t *testing.T) {
	res, extra, err := wrapResult([]byte(`{"ok":true}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if extra != nil {
		t.Errorf("extra should be nil, got %v", extra)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if res.IsError {
		t.Error("IsError should be false on success")
	}
	if len(res.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is not *mcp.TextContent, got %T", res.Content[0])
	}
	if tc.Text != `{"ok":true}` {
		t.Errorf("unexpected text: %q", tc.Text)
	}
}

// TestWrapResult_ErrorWithBody 驗證同時有資料與 error 時，IsError=true 且 content 來自 res 而非 error message。
func TestWrapResult_ErrorWithBody(t *testing.T) {
	body := []byte("partial output")
	res, extra, err := wrapResult(body, errors.New("something went wrong"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if extra != nil {
		t.Errorf("extra should be nil, got %v", extra)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if !res.IsError {
		t.Error("IsError should be true when error is non-nil")
	}
	if len(res.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is not *mcp.TextContent, got %T", res.Content[0])
	}
	if tc.Text != "partial output" {
		t.Errorf("expected content from res body, got %q", tc.Text)
	}
}

// TestWrapResult_ErrorWithoutBody 驗證無資料但有 error 時，IsError=true 且 content 來自 error message。
func TestWrapResult_ErrorWithoutBody(t *testing.T) {
	res, extra, err := wrapResult(nil, errors.New("exec failed"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if extra != nil {
		t.Errorf("extra should be nil, got %v", extra)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if !res.IsError {
		t.Error("IsError should be true when error is non-nil")
	}
	if len(res.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is not *mcp.TextContent, got %T", res.Content[0])
	}
	if tc.Text != "exec failed" {
		t.Errorf("expected error message in content, got %q", tc.Text)
	}
}
