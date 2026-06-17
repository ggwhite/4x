package main

import (
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

// runOutcome 依 run 結束後的最終狀態與迴圈錯誤，歸納出 OS 通知的等級、標題與單行內文。
// 等級對應：done/pending-review → success；中斷 → warning；其餘（needs-attention/blocked/error）→ error。
// title 固定為 "4x · <featureID>"，body 為「完成／中斷／失敗：<摘要>」。
func runOutcome(featureID string, finalState protocol.State, loopErr error) (level, title, body string) {
	title = "4x · " + featureID
	switch {
	case finalState.Phase == protocol.PhaseDone || finalState.Phase == protocol.PhasePendingReview:
		return protocol.NotifySuccess, title, "完成：" + string(finalState.Phase)
	case finalState.StopReason == "interrupted":
		return protocol.NotifyWarning, title, "中斷：" + finalState.StopReason
	default:
		reason := finalState.StopReason
		if reason == "" && loopErr != nil {
			reason = loopErr.Error()
		}
		if reason == "" {
			reason = string(finalState.Phase)
		}
		return protocol.NotifyError, title, "失敗：" + reason
	}
}

// sendSystemNotification 依當前 OS 以 shell out 方式推送一則原生通知，零新依賴。
// level 目前僅用於 log 標記（各平台命令不分等級）；title/body 為通知標題與內文。
// 命令不存在或執行失敗時靜默略過（僅 log debug），不中斷 run 流程。
//
// 平台對應：
//   - darwin：osascript display notification
//   - linux：notify-send
//   - windows：powershell + Windows.Forms balloon tip
//
// 範例：
//
//	sendSystemNotification(protocol.NotifySuccess, "4x · F076", "完成：done")
func sendSystemNotification(level, title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if nativeAppRunning() {
			cmd = exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e",
				darwinDistributedNotify(title, body))
		} else {
			script := "display notification " + quoteAppleScript(body) + " with title " + quoteAppleScript(title)
			cmd = exec.CommandContext(ctx, "osascript", "-e", script)
		}
	case "linux":
		cmd = exec.CommandContext(ctx, "notify-send", title, body)
	case "windows":
		// 透過 Windows.Forms balloon tip 顯示，無需第三方模組。
		ps := "[reflection.assembly]::loadwithpartialname('System.Windows.Forms') | Out-Null; " +
			"$n = New-Object System.Windows.Forms.NotifyIcon; " +
			"$n.Icon = [System.Drawing.SystemIcons]::Information; " +
			"$n.Visible = $true; " +
			"$n.ShowBalloonTip(5000, " + quotePowerShell(title) + ", " + quotePowerShell(body) + ", 'Info');"
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
	default:
		return
	}

	if err := cmd.Run(); err != nil {
		slog.Debug("system notification failed", "level", level, "os", runtime.GOOS, "error", err)
	}
}

// quoteAppleScript 將字串包成 AppleScript 字面量，escape 反斜線與雙引號。
func quoteAppleScript(s string) string {
	return `"` + escapeQuotes(s) + `"`
}

// quotePowerShell 將字串包成 PowerShell 單引號字面量，單引號以兩個單引號 escape。
func quotePowerShell(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '\'')
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'', '\'')
			continue
		}
		out = append(out, r)
	}
	out = append(out, '\'')
	return string(out)
}

// darwinDistributedNotify 產生一段 JXA（JavaScript for Automation）腳本，透過
// NSDistributedNotificationCenter 將通知請求發給 4x Live app，由 app 的
// UNUserNotificationCenter 發出，使通知顯示 app icon。
func darwinDistributedNotify(title, body string) string {
	return "ObjC.import('Foundation');" +
		"$.NSDistributedNotificationCenter.defaultCenter." +
		"postNotificationNameObjectUserInfoDeliverImmediately(" +
		"'com.ggwhite.4x.notify',$()," +
		"$.NSDictionary.dictionaryWithObjectsForKeys(" +
		"[" + quoteJS(title) + "," + quoteJS(body) + "]," +
		"['title','body']),true);"
}

// quoteJS 將字串包成 JavaScript 單引號字面量，escape 反斜線、單引號與換行。
func quoteJS(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '\'')
	for _, r := range s {
		switch r {
		case '\\', '\'':
			out = append(out, '\\', r)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		default:
			out = append(out, r)
		}
	}
	out = append(out, '\'')
	return string(out)
}

// nativeAppRunning 回報 4x Live macOS app 是否正在執行，用於決定通知歸屬對象。
func nativeAppRunning() bool {
	return exec.Command("pgrep", "-x", "4xLive").Run() == nil
}

// escapeQuotes escape 反斜線與雙引號，供雙引號字面量使用。
func escapeQuotes(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\\' || r == '"' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}
