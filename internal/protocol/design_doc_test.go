package protocol

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/feature"
)

func TestResolveDesignDoc_YAMLPathFirst(t *testing.T) {
	root := t.TempDir()
	yamlSpecPath := "custom/my-spec.md"
	abs := filepath.Join(root, yamlSpecPath)
	os.MkdirAll(filepath.Dir(abs), 0o755)
	os.WriteFile(abs, []byte("yaml spec content"), 0o644)

	// 也放一份到 docs/design/ 確認 YAML 優先
	docsPath := filepath.Join(root, "docs", "design", "F054-test-spec.md")
	os.MkdirAll(filepath.Dir(docsPath), 0o755)
	os.WriteFile(docsPath, []byte("docs spec content"), 0o644)

	f := feature.Feature{ID: "F054-test", Spec: yamlSpecPath}
	doc := ResolveDesignDoc(root, f, "spec")

	if doc.Content != "yaml spec content" {
		t.Errorf("Content = %q, want yaml spec content", doc.Content)
	}
	if doc.Source != yamlSpecPath {
		t.Errorf("Source = %q, want %q", doc.Source, yamlSpecPath)
	}
}

func TestResolveDesignDoc_DocsDesignFallback(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "design", "F054-test-spec.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("docs content"), 0o644)

	f := feature.Feature{ID: "F054-test"}
	doc := ResolveDesignDoc(root, f, "spec")

	if doc.Content != "docs content" {
		t.Errorf("Content = %q, want docs content", doc.Content)
	}
	want := filepath.Join("docs", "design", "F054-test-spec.md")
	if doc.Source != want {
		t.Errorf("Source = %q, want %q", doc.Source, want)
	}
}

func TestResolveDesignDoc_StripPrefixFallback(t *testing.T) {
	root := t.TempDir()
	// 只有 strip prefix 版本的檔案
	path := filepath.Join(root, "docs", "design", "test-feature-plan.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("stripped content"), 0o644)

	f := feature.Feature{ID: "F054-test-feature"}
	doc := ResolveDesignDoc(root, f, "plan")

	if doc.Content != "stripped content" {
		t.Errorf("Content = %q, want stripped content", doc.Content)
	}
	want := filepath.Join("docs", "design", "test-feature-plan.md")
	if doc.Source != want {
		t.Errorf("Source = %q, want %q", doc.Source, want)
	}
}

func TestResolveDesignDoc_NotFound(t *testing.T) {
	root := t.TempDir()
	f := feature.Feature{ID: "F099-missing"}
	doc := ResolveDesignDoc(root, f, "spec")

	if doc.Content != "" {
		t.Errorf("Content should be empty, got %q", doc.Content)
	}
	if doc.Source != "" {
		t.Errorf("Source should be empty, got %q", doc.Source)
	}
}

func TestResolveDesignDoc_PlanField(t *testing.T) {
	root := t.TempDir()
	planPath := "docs/design/F054-test-plan.md"
	abs := filepath.Join(root, planPath)
	os.MkdirAll(filepath.Dir(abs), 0o755)
	os.WriteFile(abs, []byte("plan content"), 0o644)

	f := feature.Feature{ID: "F054-test", Plan: planPath}
	doc := ResolveDesignDoc(root, f, "plan")

	if doc.Content != "plan content" {
		t.Errorf("Content = %q, want plan content", doc.Content)
	}
	if doc.Source != planPath {
		t.Errorf("Source = %q, want %q", doc.Source, planPath)
	}
}

func TestResolveDesignDoc_ExtraDirs(t *testing.T) {
	root := t.TempDir()
	customDir := filepath.Join(root, "docs", "feature")
	os.MkdirAll(customDir, 0o755)
	os.WriteFile(filepath.Join(customDir, "ws-074-spec.md"), []byte("custom spec"), 0o644)

	f := feature.Feature{ID: "ws-074"}
	doc := ResolveDesignDoc(root, f, "spec", "docs/feature")

	if doc.Content != "custom spec" {
		t.Errorf("Content = %q, want custom spec", doc.Content)
	}
	want := filepath.Join("docs", "feature", "ws-074-spec.md")
	if doc.Source != want {
		t.Errorf("Source = %q, want %q", doc.Source, want)
	}
}

func TestResolveDesignDoc_ExtraDirsPriorityOverDefault(t *testing.T) {
	root := t.TempDir()

	customDir := filepath.Join(root, "docs", "feature")
	os.MkdirAll(customDir, 0o755)
	os.WriteFile(filepath.Join(customDir, "F054-test-spec.md"), []byte("custom"), 0o644)

	defaultDir := filepath.Join(root, "docs", "design")
	os.MkdirAll(defaultDir, 0o755)
	os.WriteFile(filepath.Join(defaultDir, "F054-test-spec.md"), []byte("default"), 0o644)

	f := feature.Feature{ID: "F054-test"}
	doc := ResolveDesignDoc(root, f, "spec", "docs/feature")

	if doc.Content != "custom" {
		t.Errorf("Content = %q, want custom (extraDirs should take priority)", doc.Content)
	}
}

func TestResolveDesignDoc_ExtraDirsStripPrefix(t *testing.T) {
	root := t.TempDir()
	customDir := filepath.Join(root, "docs", "feature")
	os.MkdirAll(customDir, 0o755)
	os.WriteFile(filepath.Join(customDir, "test-feature-plan.md"), []byte("stripped custom"), 0o644)

	f := feature.Feature{ID: "F054-test-feature"}
	doc := ResolveDesignDoc(root, f, "plan", "docs/feature")

	if doc.Content != "stripped custom" {
		t.Errorf("Content = %q, want stripped custom", doc.Content)
	}
}

func TestStripFeaturePrefix(t *testing.T) {
	cases := map[string]string{
		"F054-test":  "test",
		"F022-multi": "multi",
		"test":       "test",
		"F0-x":       "F0-x",
		"abcd-x":     "abcd-x",
	}
	for in, want := range cases {
		if got := stripFeaturePrefix(in); got != want {
			t.Errorf("stripFeaturePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
