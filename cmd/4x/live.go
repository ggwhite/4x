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

			// Auth 準備必須在 bind port 之前完成。token 檔（~/.4x/live-token）是桌面殼
			// （macOS / Tauri）唯一的跨程序 token 通道，而它們以「port 已被占用」為 server
			// readiness 訊號。若先 bind port 再寫 token，桌面殼可能在 token 檔尚未落地的空窗
			// 通過 readiness、讀不到 token、以無 token URL 連線而被 middleware 401 擋死
			// （Round 2 review blocker）。先寫 token 再 bind port，可保證「port 被占用」
			// happens-after「token 已落地」，使 port-readiness 成為 token-readiness 的有效代理。
			// token 產生/寫檔皆不依賴 port，故此順序安全。
			userCfg, err := protocol.ReadUserConfig()
			if err != nil {
				slog.Warn("user config read failed, dashboard auth falls back to default (enabled)", "error", err)
			}
			authOn := server.AuthEnabled(userCfg)
			token, err := prepareLiveAuth(authOn)
			if err != nil {
				return err
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

			url := server.LiveURL(actualPort, token)
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
			_, srvErr := server.ServeMulti(ctx, reg, ln, recentPath, server.WithAuth(token))
			return srvErr
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", server.DefaultPort, "dashboard port (0 = auto)")
	cmd.Flags().BoolVarP(&webFlag, "web", "w", false, "open browser after start")
	cmd.Flags().BoolVarP(&appFlag, "app", "a", false, "launch native app after start")
	return cmd
}

// prepareLiveAuth 依 authOn 決定是否為本次 live session 產生並落地 token。
// authOn=false 回 ("", nil)，不產 token，並清掉前一個 auth-enabled session 殘留的
// ~/.4x/live-token——桌面殼依賴「auth 停用時檔案不存在」的不變式判斷是否帶 token；
// authOn=true 產隨機 token 並寫入 ~/.4x/live-token，任一步失敗即回 err（fail-hard，
// 見 Design Ruling 9）—— token 檔是桌面殼唯一的跨程序 token 通道，寫入失敗必須中止啟動，不可略過。
func prepareLiveAuth(authOn bool) (string, error) {
	if !authOn {
		path, err := server.LiveTokenPath()
		if err != nil {
			return "", err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}
	token, err := server.GenerateToken()
	if err != nil {
		return "", err
	}
	if err := server.WriteLiveToken(token); err != nil {
		return "", err
	}
	return token, nil
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
	if err := cmd.Start(); err != nil {
		slog.Warn("failed to open browser", "url", url, "error", err)
	}
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
