package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/plugins"
)

// pluginDeploy 定義一個 plugin 檔案的部署規則
type pluginDeploy struct {
	EmbedPath  string
	PluginName string
	RootFile   string
}

func runnerDeploys(name string) []pluginDeploy {
	switch name {
	case "claude":
		return []pluginDeploy{
			{EmbedPath: "claude-code/CLAUDE.md", PluginName: "CLAUDE.md", RootFile: "CLAUDE.md"},
			{EmbedPath: "claude-code/CREATOR-SKILL.md", PluginName: "CREATOR-SKILL.md"},
		}
	case "codex":
		return []pluginDeploy{
			{EmbedPath: "codex/AGENTS.md", PluginName: "codex-AGENTS.md", RootFile: "AGENTS.md"},
		}
	case "gemini":
		return []pluginDeploy{
			{EmbedPath: "gemini/GEMINI.md", PluginName: "GEMINI.md", RootFile: "GEMINI.md"},
		}
	case "agy":
		return []pluginDeploy{
			{EmbedPath: "agy/AGY.md", PluginName: "AGY.md", RootFile: "AGY.md"},
		}
	case "copilot":
		return []pluginDeploy{
			{EmbedPath: "copilot/AGENTS.md", PluginName: "copilot-AGENTS.md", RootFile: "AGENTS.md"},
			{EmbedPath: "copilot/workflow.js", PluginName: "copilot-workflow.js"},
		}
	case "cursor":
		return []pluginDeploy{
			{EmbedPath: "cursor/.cursorrules", PluginName: "cursorrules", RootFile: ".cursorrules"},
		}
	}
	return nil
}

// deployPlugins 從 embed FS 部署 plugin 指令檔到 .4x/plugins/，
// 並在使用者的根目錄指令檔中加入 @import 行
func deployPlugins(root string, cfg protocol.Config) {
	pluginDir := filepath.Join(root, ".4x", "plugins")
	os.MkdirAll(pluginDir, 0o755)

	// 部署 shared/ — 所有 runner 共用的指令檔
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

	for name := range cfg.Runners {
		deploys := runnerDeploys(name)

		if name == "codex" {
			deployCodexConfig(root)
		}

		for _, d := range deploys {
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

// deployCodexConfig 產生 codex.json
func deployCodexConfig(root string) {
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
