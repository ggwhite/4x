package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/internal/server"
	"github.com/spf13/cobra"
)

func newLiveCmd() *cobra.Command {
	var (
		port    int
		webFlag bool
		appFlag bool
	)

	cmd := &cobra.Command{
		Use:   "live [path...]",
		Short: "Start the 4x Live dashboard server",
		Long: `Start the multi-project dashboard server.

Without arguments, loads recent projects and opens the project picker.
With paths, opens each as a project tab.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := server.NewProjectRegistry()

			recentPath, err := server.DefaultRecentProjectsPath()
			if err != nil {
				return err
			}

			if len(args) > 0 {
				rp, _ := server.LoadRecentProjects(recentPath)
				for _, path := range args {
					ws, err := protocol.Find(path)
					if err != nil {
						fmt.Fprintf(os.Stderr, "warning: %s — %v\n", path, err)
						continue
					}
					reg.Add(ws)
					rp.Touch(ws.Root)
				}
				_ = server.SaveRecentProjects(recentPath, rp)
			} else {
				rp, _ := server.LoadRecentProjects(recentPath)
				for _, entry := range rp.Projects {
					ws, err := protocol.Find(entry.Path)
					if err != nil {
						continue
					}
					reg.Add(ws)
				}
			}

			url := fmt.Sprintf("http://localhost:%d", port)
			projects := reg.List()
			fmt.Printf("4x Live — %s\n", url)
			if len(projects) > 0 {
				for _, p := range projects {
					fmt.Printf("  + %s (%s)\n", p.Name, p.Path)
				}
			} else {
				fmt.Println("  No projects loaded — use the picker in the browser")
			}

			if webFlag {
				openBrowser(url)
			}
			if appFlag {
				launchNativeApp(port)
			}

			return server.StartMulti(reg, port, recentPath)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 4567, "dashboard port")
	cmd.Flags().BoolVarP(&webFlag, "web", "w", false, "open browser after start")
	cmd.Flags().BoolVarP(&appFlag, "app", "a", false, "launch native app after start")
	return cmd
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func launchNativeApp(port int) {
	if runtime.GOOS != "darwin" {
		fmt.Fprintf(os.Stderr, "native app not supported on %s yet\n", runtime.GOOS)
		return
	}
	cmd := exec.Command("open", "-a", "4x Live", "--args", fmt.Sprintf("--port=%d", port))
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not launch native app: %v\n", err)
	}
}
