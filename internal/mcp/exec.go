package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// ExecFunc 定義執行 4x 指令的函式簽章，供 MCP handler 注入使用。
type ExecFunc func(ctx context.Context, args ...string) (json.RawMessage, error)

// DefaultExec 執行 4x CLI binary 並解析其 JSON 輸出，自動補上 --json flag。
func DefaultExec(ctx context.Context, args ...string) (json.RawMessage, error) {
	binPath, err := exec.LookPath("4x")
	if err != nil {
		// Fallback to local build path if not in system PATH
		binPath = "./bin/4x"
	}

	finalArgs := buildArgs(args...)
	cmd := exec.CommandContext(ctx, binPath, finalArgs...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			parsed, parseErr := parseJSONOutput(out)
			if parseErr == nil {
				return parsed, exitErr
			}
			return nil, fmt.Errorf("exit %d: %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, err
	}
	return parseJSONOutput(out)
}

func buildArgs(args ...string) []string {
	hasJSON := false
	for _, arg := range args {
		if arg == "--json" {
			hasJSON = true
			break
		}
	}
	if !hasJSON {
		return append(args, "--json")
	}
	return args
}

func parseJSONOutput(out []byte) (json.RawMessage, error) {
	trimmed := trimToJSON(out)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil, fmt.Errorf("invalid JSON output: %s", string(out))
	}
	return json.RawMessage(trimmed), nil
}

func trimToJSON(data []byte) []byte {
	start := -1
	for i, b := range data {
		if b == '{' || b == '[' {
			start = i
			break
		}
	}
	if start == -1 {
		return nil
	}
	end := -1
	for i := len(data) - 1; i >= start; i-- {
		if data[i] == '}' || data[i] == ']' {
			end = i
			break
		}
	}
	if end == -1 {
		return data[start:]
	}
	return data[start : end+1]
}
