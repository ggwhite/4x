package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer 建立 MCP server 並註冊所有 4x 工具，透過 stdio transport 對外提供服務。
func NewServer(version, workspaceRoot string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "4x",
		Version: version,
	}, nil)

	h := &Handlers{
		Exec:          DefaultExec,
		WorkspaceRoot: workspaceRoot,
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_status",
		Description: "List all features and their status, or get detail for a single feature",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input StatusInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Status(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_new",
		Description: "Create a new feature",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input NewInput) (*mcp.CallToolResult, any, error) {
		res, err := h.New(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_run",
		Description: "Start the Design-Code-Review-Test loop for a feature",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RunInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Run(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_stop",
		Description: "Stop a running feature loop",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input StopInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Stop(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_check",
		Description: "Run guardrail checks on a feature",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input CheckInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Check(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_transition",
		Description: "Manually transition a feature to a new phase",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input TransitionInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Transition(ctx, input)
		return wrapResult(res, err)
	})

	return server
}

func wrapResult(res []byte, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		if len(res) > 0 {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: string(res)}},
			}, nil, nil
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(res)}},
	}, nil, nil
}
