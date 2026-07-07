package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/plugins"
)

// pluginInstall 定義一個 plugin 檔案的安裝規則
type pluginInstall struct {
	EmbedPath  string
	PluginName string
	RootFile   string
}

func runnerInstalls(name string) []pluginInstall {
	switch name {
	case "claude":
		return []pluginInstall{
			{EmbedPath: "claude-code/CLAUDE.md", PluginName: "CLAUDE.md", RootFile: "CLAUDE.md"},
		}
	case "codex":
		return []pluginInstall{
			{EmbedPath: "codex/AGENTS.md", PluginName: "codex-AGENTS.md", RootFile: "AGENTS.md"},
		}
	case "gemini":
		return []pluginInstall{
			{EmbedPath: "gemini/GEMINI.md", PluginName: "GEMINI.md", RootFile: "GEMINI.md"},
		}
	case "agy":
		return []pluginInstall{
			{EmbedPath: "agy/AGY.md", PluginName: "AGY.md", RootFile: "AGY.md"},
		}
	case "copilot":
		return []pluginInstall{
			{EmbedPath: "copilot/AGENTS.md", PluginName: "copilot-AGENTS.md", RootFile: "AGENTS.md"},
		}
	case "cursor":
		return []pluginInstall{
			{EmbedPath: "cursor/.cursorrules", PluginName: "cursorrules", RootFile: ".cursorrules"},
		}
	case "opencode":
		return []pluginInstall{
			{EmbedPath: "opencode/AGENTS.md", PluginName: "opencode-AGENTS.md", RootFile: "AGENTS.md"},
		}
	}
	return nil
}

// installPlugins 從 embed FS 安裝 plugin 指令檔到 .4x/plugins/，
// 並在使用者的根目錄指令檔中加入 @import 行
func installPlugins(root string, cfg protocol.Config) {
	pluginDir := filepath.Join(root, ".4x", "plugins")
	os.MkdirAll(pluginDir, 0o755)

	// 安裝 shared/ — 所有 runner 共用的指令檔
	sharedDir := filepath.Join(pluginDir, "shared")
	os.MkdirAll(sharedDir, 0o755)
	sharedFiles := []string{"shared/CREATOR.md"}
	for _, sf := range sharedFiles {
		data, err := plugins.FS.ReadFile(sf)
		if err != nil {
			continue
		}
		target := filepath.Join(pluginDir, sf)
		os.WriteFile(target, data, 0o644)
	}

	allRunners := make(map[string]bool)
	if ucfg, err := protocol.ReadUserConfig(); err == nil {
		for name := range ucfg.Runners {
			allRunners[name] = true
		}
	}
	for name := range cfg.Runners {
		allRunners[name] = true
	}
	for name := range allRunners {
		installs := runnerInstalls(name)

		if name == "codex" {
			installCodexConfig(root)
		}

		for _, d := range installs {
			data, err := plugins.FS.ReadFile(d.EmbedPath)
			if err != nil {
				continue
			}

			target := filepath.Join(pluginDir, d.PluginName)
			os.WriteFile(target, data, 0o644)

			if d.RootFile != "" {
				importLine := "@.4x/plugins/" + d.PluginName
				ensureImport(root, d.RootFile, importLine, name)
			}
		}
	}

	ensureAppendImport(root, "CLAUDE.md", "@.4x/"+protocol.LearningsContextFile)
}

// syncPlugins 在 4x run 開始前靜默同步 plugin 檔案，有更新時 log。
func syncPlugins(root string, cfg protocol.Config) {
	report := comparePlugins(root, cfg)
	needSync := false
	for _, r := range report {
		if r.status != statusCurrent {
			needSync = true
			break
		}
	}
	if !needSync {
		return
	}
	installPlugins(root, cfg)
	for _, r := range report {
		if r.status == statusUpdated {
			slog.Info("plugin updated", "file", r.path)
		} else if r.status == statusCreated {
			slog.Info("plugin installed", "file", r.path)
		}
	}
}

// ensureImport 確保根目錄指令檔包含指定的 @import 行
func ensureImport(root, filename, importLine, runner string) {
	target := filepath.Join(root, filename)

	if data, err := os.ReadFile(target); err == nil {
		if strings.Contains(string(data), importLine) {
			return
		}
		content := importLine + "\n\n" + string(data)
		os.WriteFile(target, []byte(content), 0o644)
		fmt.Printf("Plugin:   %s → updated %s\n", runner, filename)
	} else {
		os.WriteFile(target, []byte(importLine+"\n"), 0o644)
		fmt.Printf("Plugin:   %s → created %s\n", runner, filename)
	}
}

// ensureAppendImport 確保根目錄指令檔的尾部包含指定的 @import 行。
// 與 ensureImport 不同，此函式將 import 附加到檔案末端而非前端。
// 檔案不存在時不建立（learnings-context 需先由 learn context 產生）。
func ensureAppendImport(root, filename, importLine string) {
	target := filepath.Join(root, filename)
	data, err := os.ReadFile(target)
	if err != nil {
		return
	}
	if strings.Contains(string(data), importLine) {
		return
	}
	content := strings.TrimRight(string(data), "\n") + "\n\n" + importLine + "\n"
	os.WriteFile(target, []byte(content), 0o644)
}

// installCodexConfig 建立 codex.json 預設設定
func installCodexConfig(root string) {
	settingsPath := filepath.Join(root, "codex.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return
	}

	data, _ := json.MarshalIndent(map[string]any{
		"model":    "o3",
		"approval": "full-auto",
	}, "", "  ")
	os.WriteFile(settingsPath, data, 0o644)
	fmt.Printf("Plugin:   codex → codex.json\n")
}
