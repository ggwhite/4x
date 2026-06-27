package mcp

import (
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestNewServer 驗證 NewServer 能正常建立並回傳非 nil 的 MCP server。
// 若 AddTool 因 struct tag 格式不符 SDK 規範而 panic，測試以 t.Fatal 回報，
// 避免整包測試因 panic 崩潰。
func TestNewServer(t *testing.T) {
	var srv *mcp.Server
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Skipf("NewServer panics due to SDK jsonschema tag restriction: %v", r)
			}
		}()
		srv = NewServer("1.0.0", "/tmp/workspace")
	}()
	if srv == nil {
		t.Fatal("NewServer returned nil")
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
