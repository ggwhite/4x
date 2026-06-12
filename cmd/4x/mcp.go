package main

import (
	"context"
	"os"

	mcpPkg "github.com/ggwhite/4x/internal/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server (stdio mode)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			s := mcpPkg.NewServer(version, cwd)
			return s.Run(context.Background(), &mcp.StdioTransport{})
		},
	}
}
