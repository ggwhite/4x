package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/protocol"
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
						Args:    []string{"-p", "{prompt}"},
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

			setupRunnerPermissions(cwd, cfg)

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

// setupRunnerPermissions 為每個 runner 設定 non-interactive 執行所需的權限
// 每個 agent 工具都有自己的 sandbox/permission 機制，4x init 要全部配好
func setupRunnerPermissions(root string, cfg protocol.Config) {
	for name := range cfg.Runners {
		switch name {
		case "claude":
			setupClaudePermissions(root, cfg)
		case "codex":
			setupCodexPermissions(root, cfg)
		case "gemini", "agy":
			setupGeminiPermissions(root, cfg)
		}
	}
}

func langCommands(lang string) []string {
	common := []string{
		"make *", "ls *", "mkdir *", "grep *", "cat *",
		"head *", "tail *", "find *", "git *", "wc *",
		"4x *", "./bin/4x *",
	}
	switch lang {
	case "go":
		return append(common, "go build*", "go test*", "go vet*")
	case "javascript":
		return append(common, "npm *", "npx *")
	case "typescript":
		return append(common, "pnpm *", "npm *", "npx *", "yarn *", "tsc *")
	case "java":
		return append(common, "mvn *", "./gradlew *", "gradle *")
	case "rust":
		return append(common, "cargo *")
	case "python":
		return append(common, "pytest*", "python *", "pip *", "ruff *")
	default:
		return common
	}
}

// Claude Code: .claude/settings.json
func setupClaudePermissions(root string, cfg protocol.Config) {
	dir := filepath.Join(root, ".claude")
	settingsPath := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return
	}
	os.MkdirAll(dir, 0o755)

	allows := []string{"Read", "Edit", "Write"}
	for _, cmd := range langCommands(cfg.Project.Language) {
		allows = append(allows, "Bash("+cmd+")")
	}

	data, _ := json.MarshalIndent(map[string]any{
		"permissions": map[string]any{"allow": allows},
	}, "", "  ")
	os.WriteFile(settingsPath, data, 0o644)
	fmt.Printf("Runner:   claude → .claude/settings.json\n")
}

// Codex CLI: codex --full-auto 需要 AGENTS.md 裡的指令，加上 sandbox 設定
func setupCodexPermissions(root string, cfg protocol.Config) {
	settingsPath := filepath.Join(root, "codex.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return
	}

	commands := langCommands(cfg.Project.Language)

	data, _ := json.MarshalIndent(map[string]any{
		"model":       "o3",
		"approval":    "full-auto",
		"allowedTools": commands,
		"writableDirectories": []string{
			filepath.Join(root, ".4x"),
			root,
		},
	}, "", "  ")
	os.WriteFile(settingsPath, data, 0o644)
	fmt.Printf("Runner:   codex → codex.json\n")
}

// Gemini/Agy CLI: .gemini/settings.json
func setupGeminiPermissions(root string, cfg protocol.Config) {
	dir := filepath.Join(root, ".gemini")
	settingsPath := filepath.Join(dir, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return
	}
	os.MkdirAll(dir, 0o755)

	commands := langCommands(cfg.Project.Language)

	data, _ := json.MarshalIndent(map[string]any{
		"sandbox": map[string]any{
			"allowedCommands": commands,
			"allowedPaths":    []string{".4x/", "internal/", "cmd/", "templates/", "plugins/"},
		},
	}, "", "  ")
	os.WriteFile(settingsPath, data, 0o644)
	fmt.Printf("Runner:   gemini/agy → .gemini/settings.json\n")
}
