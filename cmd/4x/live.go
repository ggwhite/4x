package main

import (
	"fmt"
	"os"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/server"
	"github.com/spf13/cobra"
)

func newLiveCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "live",
		Short: "Start the 4x Live dashboard server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			ws, err := protocol.Find(cwd)
			if err != nil {
				return err
			}

			fmt.Printf("4x Live — http://localhost:%d\n", port)
			fmt.Printf("Watching: %s\n", ws.DotDir())
			return server.Start(ws, port)
		},
	}

	cmd.Flags().IntVar(&port, "port", 4567, "dashboard port")
	return cmd
}
