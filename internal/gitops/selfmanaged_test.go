package gitops

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

// selfManagedFixture 是三個 4x 自管路徑之一的測試內容：相對路徑、乾淨版本、pipeline 改寫後版本。
// 內容刻意寫成各自格式合法（feature YAML 可被 feature.Feature 解析、learnings.json 為合法 JSON），
// 讓測試貼近真實 pipeline 產出，而非任意字串。
type selfManagedFixture struct {
	rel   string
	clean string
	dirty string
}

// selfManagedFixtures 回傳 root 內三個 4x 自管檔的 fixture，供測試種檔與改髒共用。
func selfManagedFixtures(featureID string) []selfManagedFixture {
	return []selfManagedFixture{
		{
			rel:   filepath.Join(protocol.DirName, protocol.FeaturesDir, featureID+".yaml"),
			clean: "id: " + featureID + "\nname: seed\nstatus: in-progress\n",
			dirty: "id: " + featureID + "\nname: seed\nstatus: ready-for-review\ndescription: rewritten by 4x pipeline\n",
		},
		{
			rel:   filepath.Join(protocol.DirName, protocol.LearningsFile),
			clean: "{\"learnings\": []}\n",
			dirty: "{\"learnings\": [{\"id\": \"L1\"}]}\n",
		},
		{
			rel:   filepath.Join(protocol.DirName, protocol.LearningsContextFile),
			clean: "# Learnings\n",
			dirty: "# Learnings\n\n- harvested by 4x pipeline\n",
		},
	}
}

// seedSelfManaged 在 root 建立三個 4x 自管檔並以明確路徑 commit，讓它們成為 tracked 且乾淨。
func seedSelfManaged(t *testing.T, root, featureID string) {
	t.Helper()
	var rels []string
	for _, f := range selfManagedFixtures(featureID) {
		writeFile(t, filepath.Join(root, f.rel), f.clean)
		rels = append(rels, f.rel)
	}
	runGit(t, root, append([]string{"add", "--"}, rels...)...)
	runGit(t, root, "commit", "-m", "seed 4x self-managed files")
}

// dirtySelfManaged 把三個已 tracked 的 4x 自管檔改成未 commit 的髒狀態，
// 模擬 pipeline 期間 4x 自己對主工作區的寫入。
func dirtySelfManaged(t *testing.T, root, featureID string) {
	t.Helper()
	for _, f := range selfManagedFixtures(featureID) {
		writeFile(t, filepath.Join(root, f.rel), f.dirty)
	}
}

// TestSelfManagedPathspecs 驗證 AC-1：pathspec 清單長度、順序與字面值。
func TestSelfManagedPathspecs(t *testing.T) {
	got := selfManagedPathspecs()
	want := []string{".4x/features", ".4x/learnings.json", ".4x/learnings-context.md"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pathspec[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestIsSelfManagedPath 驗證 AC-2：只有 feature YAML 與兩個 learnings 檔算自管路徑。
func TestIsSelfManagedPath(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{".4x/features/F1.yaml", true},
		{".4x/features/nested/F1.yaml", true},
		{".4x/features/README.md", false},
		{".4x/learnings.json", true},
		{".4x/learnings-context.md", true},
		{".4x/settings.json", false},
		{".4x/candidates.json", false},
		{"internal/gitops/gitops.go", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isSelfManagedPath(c.rel); got != c.want {
			t.Errorf("isSelfManagedPath(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

// TestCommitSelfManaged_NoDirtyPathsNoCommit 驗證 AC-7(a)：三個自管路徑皆乾淨時回傳 false
// 且不產生任何 commit。
func TestCommitSelfManaged_NoDirtyPathsNoCommit(t *testing.T) {
	root, _, _ := setupMonoWorkspace(t)
	seedSelfManaged(t, root, "feat-clean")

	before := gitOutput(root, "rev-list", "--count", "HEAD")
	if commitSelfManaged(root, "feat-clean") {
		t.Error("commitSelfManaged should return false when nothing is dirty")
	}
	if after := gitOutput(root, "rev-list", "--count", "HEAD"); after != before {
		t.Errorf("commit count changed: before %s after %s", before, after)
	}
}

// TestCommitSelfManaged_UntrackedNotCommitted 驗證 AC-7(b)：自管路徑為 untracked 時不被
// 收進 commit，呼叫後仍維持 "??" 狀態。
func TestCommitSelfManaged_UntrackedNotCommitted(t *testing.T) {
	root, _, _ := setupMonoWorkspace(t)
	// 只把 feature YAML 與 learnings.json 種成 tracked，learnings-context.md 留 untracked。
	yamlPath := filepath.Join(root, protocol.DirName, protocol.FeaturesDir, "feat-untracked.yaml")
	jsonPath := filepath.Join(root, protocol.DirName, protocol.LearningsFile)
	writeFile(t, yamlPath, "seed\n")
	writeFile(t, jsonPath, "{}\n")
	runGit(t, root, "add", "--", ".4x/features/feat-untracked.yaml", ".4x/learnings.json")
	runGit(t, root, "commit", "-m", "seed tracked self-managed files")

	ctxPath := filepath.Join(root, protocol.DirName, protocol.LearningsContextFile)
	writeFile(t, ctxPath, "# Learnings\n")

	if commitSelfManaged(root, "feat-untracked") {
		t.Error("commitSelfManaged should return false for untracked-only dirt")
	}

	status := gitOutput(root, "status", "--porcelain", "--", ".4x/learnings-context.md")
	if !strings.HasPrefix(status, "??") {
		t.Errorf("learnings-context.md should stay untracked, git status = %q", status)
	}
}
