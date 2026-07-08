package gitops

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ggwhite/4x/internal/envutil"
	"github.com/ggwhite/4x/internal/protocol"
)

// runPostScaffold 在 worktree 新建完成後，依序在 wtDir 執行 cfg.Worktree.PostScaffold 的命令。
// 每個命令的 stdout/stderr 併寫入該 feature 的 run log
// (<主 .4x>/run/<id>/logs/post-scaffold.log)，並各發一筆 type=hook / action=post-scaffold event。
// 單一命令失敗只 slog.Warn 並繼續（絕不回傳 error 中止 scaffold），best-effort。
func runPostScaffold(cfg protocol.Config, ws *protocol.Workspace, wtDir, featureID string) {
	if len(cfg.Worktree.PostScaffold) == 0 {
		return
	}

	var logFile *os.File
	logDir := filepath.Join(ws.FeatureDir(featureID), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.Warn("post-scaffold: cannot create log dir", "feature", featureID, "err", err)
	} else if f, err := os.OpenFile(filepath.Join(logDir, "post-scaffold.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err != nil {
		slog.Warn("post-scaffold: cannot open log", "feature", featureID, "err", err)
	} else {
		logFile = f
		defer logFile.Close()
	}

	for _, c := range cfg.Worktree.PostScaffold {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = wtDir
		cmd.Env = envutil.EnrichedEnv()
		out, err := cmd.CombinedOutput()

		if logFile != nil {
			fmt.Fprintf(logFile, "$ %s\n", c)
			fmt.Fprintf(logFile, "%s", out)
			if len(out) > 0 && out[len(out)-1] != '\n' {
				fmt.Fprintln(logFile)
			}
		}

		status := "pass"
		detail := ""
		if err != nil {
			status = "fail"
			detail = err.Error()
			slog.Warn("post-scaffold hook failed", "feature", featureID, "cmd", c, "err", err)
		}
		ws.AppendEvent(featureID, protocol.Event{
			Type:    "hook",
			Action:  "post-scaffold",
			Command: c,
			Status:  status,
			Detail:  detail,
		})
	}
}
