package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSkill 在 skillsDir 下建立一個含 SKILL.md 的 skill 目錄。
func writeSkill(t *testing.T, skillsDir, name, frontmatter string) {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "\n---\n\n# body\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupSkillsRepo 建一個含 skills/ 的臨時 repo，chdir 進去並把 HOME 指到獨立臨時目錄，
// 回傳 skillsDir 與 home。
func setupSkillsRepo(t *testing.T) (skillsDir, home string) {
	t.Helper()
	repo := t.TempDir()
	skillsDir = filepath.Join(repo, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(repo)
	return skillsDir, home
}

func TestExtractFrontmatter(t *testing.T) {
	got, ok := extractFrontmatter("---\nname: a\ndescription: b\n---\nbody")
	if !ok {
		t.Fatal("expected frontmatter found")
	}
	if got != "name: a\ndescription: b" {
		t.Errorf("unexpected frontmatter: %q", got)
	}

	if _, ok := extractFrontmatter("no frontmatter here"); ok {
		t.Error("expected no frontmatter")
	}
}

func TestParseSkillMeta_FoldedDescription(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: my-skill\ndescription: >\n  line one\n  line two\n---\n# body\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := parseSkillMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "my-skill" {
		t.Errorf("name = %q", meta.Name)
	}
	if meta.Description != "line one line two" {
		t.Errorf("description = %q", meta.Description)
	}
}

func TestListSkills_SortedAndInstalledFlag(t *testing.T) {
	skillsDir, _ := setupSkillsRepo(t)
	writeSkill(t, skillsDir, "zebra", "name: zebra\ndescription: z")
	writeSkill(t, skillsDir, "alpha", "name: alpha\ndescription: a")
	// 非 skill 目錄（無 SKILL.md）應被略過
	if err := os.MkdirAll(filepath.Join(skillsDir, "notaskill"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := listSkills(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(entries))
	}
	if entries[0].Name != "alpha" || entries[1].Name != "zebra" {
		t.Errorf("expected sorted order, got %v", entries)
	}
	if entries[0].Installed {
		t.Error("alpha should not be installed yet")
	}

	// 安裝 alpha 後，Installed 應為 true
	if err := newSkillsInstallCmd().RunE(nil, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	entries, err = listSkills(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	if !entries[0].Installed {
		t.Error("alpha should be installed after install")
	}
}

func TestSkillsInstall_Idempotent(t *testing.T) {
	skillsDir, home := setupSkillsRepo(t)
	writeSkill(t, skillsDir, "alpha", "name: alpha\ndescription: a")

	install := newSkillsInstallCmd()
	if err := install.RunE(nil, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "skills", "alpha")
	li, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if li.Mode()&os.ModeSymlink == 0 {
		t.Fatal("target is not a symlink")
	}
	dest, _ := os.Readlink(target)
	if dest != filepath.Join(skillsDir, "alpha") {
		t.Errorf("symlink points to %q", dest)
	}

	// 二次安裝應冪等、不報錯
	if err := install.RunE(nil, []string{"alpha"}); err != nil {
		t.Fatalf("second install should be idempotent: %v", err)
	}
}

func TestSkillsInstall_NotFound(t *testing.T) {
	setupSkillsRepo(t)
	if err := newSkillsInstallCmd().RunE(nil, []string{"nope"}); err == nil {
		t.Fatal("expected error for missing skill")
	}
}

func TestSkillsRemove_Symlink(t *testing.T) {
	skillsDir, home := setupSkillsRepo(t)
	writeSkill(t, skillsDir, "alpha", "name: alpha\ndescription: a")
	if err := newSkillsInstallCmd().RunE(nil, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "skills", "alpha")

	if err := newSkillsRemoveCmd().RunE(nil, []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Error("symlink should be removed")
	}

	// 未安裝時 remove 不報錯
	if err := newSkillsRemoveCmd().RunE(nil, []string{"alpha"}); err != nil {
		t.Errorf("remove of not-installed should be no-op: %v", err)
	}
}

func TestSkillsRemove_RefusesRealDir(t *testing.T) {
	_, home := setupSkillsRepo(t)
	// 在 ~/.claude/skills/ 下放一個真實目錄（非 symlink），remove 應拒絕刪除
	target := filepath.Join(home, ".claude", "skills", "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := newSkillsRemoveCmd().RunE(nil, []string{"real"}); err == nil {
		t.Fatal("expected error refusing to delete real dir")
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("real dir should not be deleted")
	}
}

func TestFindSkillsDir_WalksUp(t *testing.T) {
	skillsDir, _ := setupSkillsRepo(t)
	// 從 skills/alpha 子目錄往上找也應找到 skills/
	sub := filepath.Join(skillsDir, "alpha", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	got, err := findSkillsDir()
	if err != nil {
		t.Fatal(err)
	}
	// skills/ 是往上第一個含 skills 子目錄的：此處 sub 下無 skills，往上到 repo 才有。
	// macOS 的 /var → /private/var symlink 會讓路徑字串不同，改以 EvalSymlinks 正規化後比對。
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(skillsDir)
	if gotResolved != wantResolved {
		t.Errorf("findSkillsDir = %q, want %q", gotResolved, wantResolved)
	}
}
