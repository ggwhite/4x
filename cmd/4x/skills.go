package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ownerOnlySkills 列出屬於 owner 專用（會執行破壞性/不可逆動作，如全自動 merge）的 skill，
// 這類 skill 在 list / install 時會額外印出 WARNING 提示。
var ownerOnlySkills = map[string]string{
	"4x-autopilot": "owner-only，全自動 merge",
}

// skillMeta 是從 SKILL.md frontmatter 解析出的 skill 中繼資料。
type skillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// skillEntry 代表 skills/ 目錄下的一個 skill：目錄名、解析出的 meta，以及是否安裝。
type skillEntry struct {
	Dir         string `json:"dir"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	OwnerOnly   bool   `json:"owner_only"`
}

// newSkillsCmd 建立 `4x skills` 父命令，管理 repo skills/ 目錄下的 skill 安裝（symlink 到 ~/.claude/skills/）。
func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "List and install 4x skills into ~/.claude/skills",
		Long: `Manage the skills bundled in this repo's skills/ directory.

Installation is symlink-only — 4x links skills/<name>/ into ~/.claude/skills/<name>,
so a later 'git pull' updates the skill automatically without re-installing.`,
	}
	cmd.AddCommand(
		newSkillsListCmd(),
		newSkillsInstallCmd(),
		newSkillsRemoveCmd(),
	)
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available skills in the repo skills/ directory",
		Args:  cobra.NoArgs,
	}
	cmd.RunE = withJsonError(&jsonOutput, func(_ *cobra.Command, _ []string) error {
		skillsDir, err := findSkillsDir()
		if err != nil {
			return err
		}
		entries, err := listSkills(skillsDir)
		if err != nil {
			return err
		}
		if jsonOutput {
			return printJSON(entries)
		}
		if len(entries) == 0 {
			fmt.Printf("No skills found in %s\n", skillsDir)
			return nil
		}
		for _, e := range entries {
			marker := " "
			if e.Installed {
				marker = "✓"
			}
			fmt.Printf("%s %s\n", marker, e.Name)
			if e.Description != "" {
				fmt.Printf("    %s\n", e.Description)
			}
			if warn, ok := ownerOnlySkills[e.Name]; ok {
				fmt.Printf("    WARNING: %s\n", warn)
			}
		}
		return nil
	})
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func newSkillsInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <name>",
		Short: "Symlink a skill into ~/.claude/skills (idempotent)",
		Args:  cobra.ExactArgs(1),
	}
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		name := args[0]
		skillsDir, err := findSkillsDir()
		if err != nil {
			return err
		}
		src := filepath.Join(skillsDir, name)
		info, err := os.Stat(src)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("skill %q not found in %s", name, skillsDir)
		}
		if _, err := os.Stat(filepath.Join(src, "SKILL.md")); err != nil {
			return fmt.Errorf("skill %q has no SKILL.md", name)
		}

		target, err := skillTargetPath(name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create skills dir: %w", err)
		}

		if li, err := os.Lstat(target); err == nil {
			if li.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("%s exists and is not a symlink; refusing to overwrite", target)
			}
			dest, _ := os.Readlink(target)
			if dest == src {
				fmt.Printf("already installed: %s -> %s\n", target, src)
				printOwnerOnlyWarning(name)
				return nil
			}
			return fmt.Errorf("%s already links to %s; remove it first", target, dest)
		}

		if err := os.Symlink(src, target); err != nil {
			return fmt.Errorf("create symlink: %w", err)
		}
		fmt.Printf("installed: %s -> %s\n", target, src)
		printOwnerOnlyWarning(name)
		return nil
	}
	return cmd
}

func newSkillsRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a skill symlink from ~/.claude/skills",
		Args:  cobra.ExactArgs(1),
	}
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		name := args[0]
		target, err := skillTargetPath(name)
		if err != nil {
			return err
		}
		li, err := os.Lstat(target)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("not installed: %s\n", target)
				return nil
			}
			return err
		}
		if li.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s is not a symlink; refusing to delete real files", target)
		}
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("remove symlink: %w", err)
		}
		fmt.Printf("removed: %s\n", target)
		return nil
	}
	return cmd
}

// printOwnerOnlyWarning 若 name 屬於 owner-only skill，印出 WARNING 提示。
func printOwnerOnlyWarning(name string) {
	if warn, ok := ownerOnlySkills[name]; ok {
		fmt.Printf("WARNING: %q is %s\n", name, warn)
	}
}

// skillTargetPath 回傳 skill 在 ~/.claude/skills/ 下的安裝目標路徑。
func skillTargetPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".claude", "skills", name), nil
}

// findSkillsDir 從 cwd 逐層往上尋找含 skills/ 子目錄的 repo 根目錄，回傳該 skills/ 的絕對路徑。
// 找不到時回傳明確 error，提示需在 4x repo 內執行。
func findSkillsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "skills")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("skills/ directory not found; run this from within the 4x repo")
		}
		dir = parent
	}
}

// listSkills 讀取 skillsDir 下每個含 SKILL.md 的子目錄，解析 meta 並標記是否已安裝，依名稱排序回傳。
func listSkills(skillsDir string) ([]skillEntry, error) {
	dirEntries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	var entries []skillEntry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		skillMdPath := filepath.Join(skillsDir, de.Name(), "SKILL.md")
		if _, err := os.Stat(skillMdPath); err != nil {
			continue
		}
		meta, err := parseSkillMeta(skillMdPath)
		if err != nil {
			return nil, err
		}
		name := meta.Name
		if name == "" {
			name = de.Name()
		}
		installed := false
		if target, err := skillTargetPath(de.Name()); err == nil {
			if li, err := os.Lstat(target); err == nil && li.Mode()&os.ModeSymlink != 0 {
				dest, _ := os.Readlink(target)
				installed = dest == filepath.Join(skillsDir, de.Name())
			}
		}
		_, ownerOnly := ownerOnlySkills[name]
		entries = append(entries, skillEntry{
			Dir:         de.Name(),
			Name:        name,
			Description: meta.Description,
			Installed:   installed,
			OwnerOnly:   ownerOnly,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// parseSkillMeta 從 SKILL.md 開頭的 YAML frontmatter（--- 包夾）解析 name 與 description。
// description 可能為多行折疊（>）或多行文字，解析後壓成單行空白分隔字串。
func parseSkillMeta(path string) (skillMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return skillMeta{}, fmt.Errorf("read %s: %w", path, err)
	}
	front, ok := extractFrontmatter(string(data))
	if !ok {
		return skillMeta{}, fmt.Errorf("%s has no YAML frontmatter", path)
	}
	var meta skillMeta
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return skillMeta{}, fmt.Errorf("parse frontmatter in %s: %w", path, err)
	}
	meta.Description = strings.Join(strings.Fields(meta.Description), " ")
	return meta, nil
}

// extractFrontmatter 取出以 --- 起始、--- 結束的 YAML frontmatter 內容（不含分隔線）。
func extractFrontmatter(content string) (string, bool) {
	content = strings.TrimPrefix(content, "\uFEFF")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[1:i], "\n"), true
		}
	}
	return "", false
}
