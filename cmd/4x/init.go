package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/templates"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var dumpTmpl, force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a 4x project in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			if dumpTmpl {
				dotDir := filepath.Join(cwd, protocol.DirName)
				if _, err := os.Stat(dotDir); err != nil {
					return fmt.Errorf("%s not found — run '4x init' first", protocol.DirName)
				}
				fmt.Println("Dumping built-in templates to .4x/templates/:")
				return dumpTemplates(dotDir, force)
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

	cmd.Flags().BoolVar(&dumpTmpl, "dump-templates", false, "dump built-in templates to .4x/templates/ for customization")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing template files (with --dump-templates)")
	return cmd
}

// dumpTemplates 把內建 role prompt template 倒出到 .4x/templates/，供專案整檔覆寫。
// 只倒出 templates.FS 內的頂層 *.tmpl（含 locale.tmpl）；profiles/ 不在此 FS 中
// （由 templates.ProfilesFS 管理，有自己的覆寫機制），故不會被倒出。
// 已存在的檔案預設跳過並 warn，force 為 true 時才覆蓋。
func dumpTemplates(dotDir string, force bool) error {
	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}

	entries, err := templates.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded templates: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dest := filepath.Join(tmplDir, e.Name())
		if !force {
			if _, err := os.Stat(dest); err == nil {
				slog.Warn("template exists, skipping (use --force to overwrite)", "file", e.Name())
				continue
			}
		}
		data, err := templates.FS.ReadFile(e.Name())
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		fmt.Printf("  %s\n", e.Name())
	}
	return nil
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

	p.VerifyCommandAllowlist = deriveAllowlist(p)

	return p
}

// subcommandTools 是「通用多工具」集合：其子命令可執行任意程式（go run、npm exec、
// cargo run 等），故推導 allowlist 前綴時須把「工具+子命令」兩個 token 一起釘進前綴，
// 避免僅放行工具名等於放行該工具的任意子命令。
var subcommandTools = map[string]bool{
	"go": true, "cargo": true, "npm": true, "npx": true, "pnpm": true,
	"yarn": true, "bun": true, "dotnet": true, "node": true, "deno": true,
	"python": true, "python3": true,
	"bundle": true, "poetry": true, "pip": true, "pip3": true,
	"composer": true, "gradle": true, "pdm": true, "uv": true,
}

// deriveAllowlist 依 p 的 Build/Test/Lint/DocsCheck 命令推導 verify 命令允許前綴清單（DR-8）。
// 對每條命令：tokens[0] ∈ subcommandTools 且有第二 token → 前綴取前兩 token（如 "go test"）；
// 否則取第一 token（如 "make"）。make、./gradlew、mvn、pytest、ruff 等不在集合內的工具執行
// in-repo 且人工可信的 target，granularity 到工具名即足夠。結果排序去重。
func deriveAllowlist(p protocol.ProjectConfig) []string {
	var cmds []string
	cmds = append(cmds, p.Build...)
	cmds = append(cmds, p.Test...)
	cmds = append(cmds, p.Lint...)
	cmds = append(cmds, p.DocsCheck...)

	seen := make(map[string]bool)
	var prefixes []string
	for _, cmd := range cmds {
		tokens := strings.Fields(cmd)
		if len(tokens) == 0 {
			continue
		}
		var prefix string
		if subcommandTools[tokens[0]] && len(tokens) >= 2 {
			prefix = tokens[0] + " " + tokens[1]
		} else {
			prefix = tokens[0]
		}
		if !seen[prefix] {
			seen[prefix] = true
			prefixes = append(prefixes, prefix)
		}
	}
	sort.Strings(prefixes)
	return prefixes
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
