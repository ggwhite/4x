package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

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
			server.Version = version
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
						slog.Warn("project load failed", "path", path, "error", err)
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
					if err != nil || ws.Root != entry.Path {
						continue
					}
					reg.Add(ws)
				}
			}

			ln, err := server.ListenForMulti(port)
			if err != nil && port != 0 {
				slog.Warn("port unavailable, picking a free port", "port", port, "error", err)
				ln, err = server.ListenForMulti(0)
			}
			if err != nil {
				return err
			}
			actualPort := ln.Addr().(*net.TCPAddr).Port

			url := fmt.Sprintf("http://localhost:%d", actualPort)
			projects := reg.List()
			fmt.Printf("4x Live — %s\n", url)
			if len(projects) > 0 {
				for _, p := range projects {
					slog.Info("project loaded", "name", p.Name, "path", p.Path)
					fmt.Printf("  + %s (%s)\n", p.Name, p.Path)
				}
			} else {
				fmt.Println("  No projects loaded — use the picker in the browser")
			}

			if webFlag {
				openBrowser(url)
			}
			if appFlag {
				launchNativeApp(actualPort)
			}

			signal.Ignore(syscall.SIGPIPE)
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()
			slog.Info("server started", "port", actualPort, "pid", os.Getpid())
			fmt.Printf("  pid: %d\n", os.Getpid())
			_, srvErr := server.ServeMulti(ctx, reg, ln, recentPath)
			return srvErr
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 4567, "dashboard port (0 = auto)")
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
		slog.Warn("native app not supported", "os", runtime.GOOS)
		return
	}
	cmd := exec.Command("open", "-a", "4x Live", "--args", fmt.Sprintf("--port=%d", port))
	if err := cmd.Start(); err != nil {
		slog.Warn("could not launch native app", "error", err)
	}
}
