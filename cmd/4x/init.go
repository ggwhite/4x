package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ggwhite/4x/internal/feature"
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

			ensureUserConfig()

			cfg := protocol.Config{
				Project: profile,
				Roles: map[string]protocol.RoleConfig{
					"designer": {Model: "opus"},
					"coder":    {Model: "sonnet"},
					"reviewer": {Model: "sonnet", DeepModel: "opus"},
					"tester":   {Model: "sonnet", ScreenshotDir: feature.DefaultScreenshotDir},
				},
			}

			if err := protocol.Init(cwd, cfg); err != nil {
				return err
			}

			installPlugins(cwd, cfg)

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

// ensureUserConfig 若 ~/.4x/settings.json 不存在，建立包含預設 runner 設定的初始檔。
func ensureUserConfig() {
	if _, err := protocol.UserConfigPath(); err != nil {
		return
	}
	if existing, err := protocol.ReadUserConfig(); err == nil && (existing.Locale != "" || len(existing.Runners) > 0) {
		return
	}

	cfg := protocol.UserConfig{
		Locale:        "en",
		DefaultRunner: "claude",
		Runners:       protocol.SupportedRunnerMap(),
	}
	if err := protocol.WriteUserConfig(cfg); err != nil {
		slog.Warn("failed to create user config", "error", err)
		return
	}
	path, _ := protocol.UserConfigPath()
	fmt.Printf("Created %s with default runner settings\n", path)
}
