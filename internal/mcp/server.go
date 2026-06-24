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

	// --- mcp-core: lifecycle tools ---

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_done",
		Description: "Mark a pending-review feature as done (auto-merges back to main)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input DoneInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Done(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_verify",
		Description: "Run verify commands from a feature's test-strategy.yaml",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input VerifyInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Verify(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_approve",
		Description: "Approve a draft feature (draft → not-started)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ApproveInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Approve(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_reject",
		Description: "Reject a draft feature (draft → abandoned)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input RejectInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Reject(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_mine",
		Description: "Scan .4x/ history for failure signals and produce a candidate pool",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input MineInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Mine(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_subtask",
		Description: "Update the status of a subtask within a feature",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input SubtaskInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Subtask(ctx, input)
		return wrapResult(res, err)
	})

	// --- mcp-mgmt: batch and maintenance tools ---

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_batch_next",
		Description: "Show the next eligible feature to run from the batch plan",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, any, error) {
		res, err := h.BatchNext(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_batch_stop",
		Description: "Signal a running batch to stop after the current feature completes",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, any, error) {
		res, err := h.BatchStop(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_clean",
		Description: "Remove workspace artifacts for completed features (requires force to delete)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input CleanInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Clean(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_doctor",
		Description: "Run project environment health checks",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, any, error) {
		res, err := h.Doctor(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_learn_list",
		Description: "List all retro learnings",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input LearnListInput) (*mcp.CallToolResult, any, error) {
		res, err := h.LearnList(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_learn_prune",
		Description: "Remove stale learnings",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input LearnPruneInput) (*mcp.CallToolResult, any, error) {
		res, err := h.LearnPrune(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_learn_promote",
		Description: "Mark a learning as promoted",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input LearnPromoteInput) (*mcp.CallToolResult, any, error) {
		res, err := h.LearnPromote(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_learn_remove",
		Description: "Remove a learning entry",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input LearnRemoveInput) (*mcp.CallToolResult, any, error) {
		res, err := h.LearnRemove(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_evolve",
		Description: "Run one self-evolution round (mine → gate → enrich → enqueue); autoRun=true is long-blocking",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EvolveInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Evolve(ctx, input)
		return wrapResult(res, err)
	})

	// --- mcp-ops: config and gate tools ---

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_config_get",
		Description: "Get a user-level configuration value",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ConfigGetInput) (*mcp.CallToolResult, any, error) {
		res, err := h.ConfigGet(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_config_set",
		Description: "Set a user-level configuration value",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input ConfigSetInput) (*mcp.CallToolResult, any, error) {
		res, err := h.ConfigSet(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_config_list",
		Description: "Show all user-level configuration",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input struct{}) (*mcp.CallToolResult, any, error) {
		res, err := h.ConfigList(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_gate",
		Description: "Apply the evolve value gate veto (phase: pre or post)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input GateInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Gate(ctx, input)
		return wrapResult(res, err)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "4x_merge",
		Description: "Complete a feature merge after resolving conflicts",
	}, func(ctx context.Context, req *mcp.CallToolRequest, input MergeInput) (*mcp.CallToolResult, any, error) {
		res, err := h.Merge(ctx, input)
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
