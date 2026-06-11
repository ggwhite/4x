package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/plugins"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a 4x project in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			dotDir := filepath.Join(cwd, protocol.DirName)
			if _, err := os.Stat(dotDir); err == nil {
				return fmt.Errorf("%s already exists", protocol.DirName)
			}

			projectName := filepath.Base(cwd)
			profile := detectProjectProfile(cwd)
			profile.Name = projectName

			cfg := protocol.Config{
				Project: profile,
				Runners: map[string]protocol.RunnerConfig{
					"claude": {
						Command: "claude",
						Args:    []string{"--dangerously-skip-permissions", "-p", "{prompt}"},
						Model:   "opus",
					},
					"codex": {
						Command: "codex",
						Args:    []string{"exec", "--prompt-file", "{promptFile}"},
					},
					"gemini": {
						Command: "gemini",
						Args:    []string{"-y", "-p", "{prompt}"},
					},
					"agy": {
						Command: "agy",
						Args:    []string{"--dangerously-skip-permissions", "-p", "{prompt}"},
					},
				},
				Default: "claude",
				Roles: map[string]protocol.RoleConfig{
					"designer": {Model: "opus"},
					"coder":    {Model: "sonnet"},
					"reviewer": {Model: "sonnet", DeepModel: "opus"},
					"tester":   {Model: "sonnet"},
				},
			}

			if err := protocol.Init(cwd, cfg); err != nil {
				return err
			}

			deployPlugins(cwd, cfg)

			fmt.Printf("Initialized 4x project in %s/\n", protocol.DirName)
			if profile.Language != "" {
				fmt.Printf("Detected: %s\n", profile.Language)
			}
			if len(profile.Build) > 0 {
				fmt.Printf("Build:    %s\n", profile.Build[0])
			}
			if len(profile.Test) > 0 {
				fmt.Printf("Test:     %s\n", profile.Test[0])
			}
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  Edit .4x/settings.json to customize project profile")
			fmt.Println("  4x new \"feature name\"    Create a feature")
			fmt.Println("  4x run <feature-id>      Run the loop")
			fmt.Println("  4x live                  Open dashboard")
			return nil
		},
	}
}

func detectProjectProfile(root string) protocol.ProjectConfig {
	p := protocol.ProjectConfig{}

	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	}

	switch {
	case exists("go.mod"):
		p.Language = "go"
		if exists("Makefile") {
			p.Build = []string{"make build"}
			p.Test = []string{"make test"}
			p.Lint = []string{"make lint"}
		} else {
			p.Build = []string{"go build ./..."}
			p.Test = []string{"go test ./..."}
			p.Lint = []string{"go vet ./..."}
		}
	case exists("package.json"):
		if exists("pnpm-lock.yaml") {
			p.Language = "typescript"
			p.Setup = []string{"pnpm install"}
			p.Build = []string{"pnpm build"}
			p.Test = []string{"pnpm test"}
			p.Lint = []string{"pnpm lint"}
		} else if exists("yarn.lock") {
			p.Language = "typescript"
			p.Setup = []string{"yarn install"}
			p.Build = []string{"yarn build"}
			p.Test = []string{"yarn test"}
			p.Lint = []string{"yarn lint"}
		} else {
			p.Language = "javascript"
			p.Setup = []string{"npm install"}
			p.Build = []string{"npm run build"}
			p.Test = []string{"npm test"}
			p.Lint = []string{"npm run lint"}
		}
	case exists("build.gradle") || exists("build.gradle.kts"):
		p.Language = "java"
		p.Build = []string{"./gradlew build"}
		p.Test = []string{"./gradlew test"}
		p.Lint = []string{"./gradlew check"}
	case exists("pom.xml"):
		p.Language = "java"
		p.Build = []string{"mvn compile"}
		p.Test = []string{"mvn test"}
		p.Lint = []string{"mvn verify"}
	case exists("Cargo.toml"):
		p.Language = "rust"
		p.Build = []string{"cargo build"}
		p.Test = []string{"cargo test"}
		p.Lint = []string{"cargo clippy"}
	case exists("requirements.txt") || exists("pyproject.toml"):
		p.Language = "python"
		p.Test = []string{"pytest"}
		p.Lint = []string{"ruff check ."}
	}

	if exists("docker-compose.yml") || exists("docker-compose.yaml") || exists("compose.yaml") {
		p.Setup = append([]string{"docker compose up -d"}, p.Setup...)
	}

	for _, doc := range []string{
		"docs/architecture.md", "docs/design.md", "ARCHITECTURE.md",
		"CONTRIBUTING.md", "docs/coding-standards.md",
	} {
		if exists(doc) {
			p.Docs = append(p.Docs, doc)
		}
	}

	return p
}

// pluginDeploy 定義一個 plugin 檔案的部署規則
type pluginDeploy struct {
	EmbedPath  string // plugins embed FS 裡的路徑
	PluginName string // 寫到 .4x/plugins/ 的檔名
	RootFile   string // 使用者專案根目錄的指令檔（加 import 行）；空字串表示不需要 import
}

// deployPlugins 從 embed FS 部署 plugin 指令檔到 .4x/plugins/，
// 並在使用者的根目錄指令檔中加入 @import 行
func deployPlugins(root string, cfg protocol.Config) {
	pluginDir := filepath.Join(root, ".4x", "plugins")
	os.MkdirAll(pluginDir, 0o755)

	for name := range cfg.Runners {
		var deploys []pluginDeploy

		switch name {
		case "claude":
			deploys = []pluginDeploy{
				{EmbedPath: "claude-code/SKILL.md", PluginName: "CLAUDE.md", RootFile: "CLAUDE.md"},
				{EmbedPath: "claude-code/workflow.js", PluginName: "workflow.js"},
			}
		case "codex":
			deploys = []pluginDeploy{
				{EmbedPath: "codex/AGENTS.md", PluginName: "codex-AGENTS.md", RootFile: "AGENTS.md"},
			}
			deployCodexConfig(root)
		case "gemini":
			deploys = []pluginDeploy{
				{EmbedPath: "gemini/GEMINI.md", PluginName: "GEMINI.md", RootFile: "GEMINI.md"},
			}
		case "agy":
			deploys = []pluginDeploy{
				{EmbedPath: "agy/AGY.md", PluginName: "AGY.md", RootFile: "AGY.md"},
			}
		case "copilot":
			deploys = []pluginDeploy{
				{EmbedPath: "copilot/AGENTS.md", PluginName: "copilot-AGENTS.md", RootFile: "AGENTS.md"},
				{EmbedPath: "copilot/workflow.js", PluginName: "copilot-workflow.js"},
			}
		case "cursor":
			deploys = []pluginDeploy{
				{EmbedPath: "cursor/.cursorrules", PluginName: "cursorrules", RootFile: ".cursorrules"},
			}
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
// 檔案不存在則新建；已存在則檢查是否已含 import，沒有才 prepend
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

// deployCodexConfig 產生 codex.json（Codex 的 non-interactive 模式由此設定檔控制）
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
