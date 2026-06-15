package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/feature"
	"gopkg.in/yaml.v3"
)

func setupCachedTestWorkspace(t *testing.T) (*CachedWorkspace, string) {
	t.Helper()
	tmp := t.TempDir()
	dotDir := filepath.Join(tmp, DirName)
	if err := os.MkdirAll(filepath.Join(dotDir, FeaturesDir), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Project: ProjectConfig{Name: "test"}, Default: "claude"}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dotDir, ConfigFile), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := &Workspace{Root: tmp}
	return NewCachedWorkspace(ws), tmp
}

func writeFeatureFile(t *testing.T, tmp string, f feature.Feature) string {
	t.Helper()
	data, err := yaml.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tmp, DirName, FeaturesDir, f.ID+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCachedReadConfig_CacheHit(t *testing.T) {
	cws, _ := setupCachedTestWorkspace(t)

	cfg1, err := cws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg1.Project.Name != "test" {
		t.Errorf("Name = %q, want test", cfg1.Project.Name)
	}

	cfg2, err := cws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Default != cfg1.Default {
		t.Error("second call should return same data")
	}
}

func TestCachedReadConfig_InvalidateOnChange(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)

	if _, err := cws.ReadConfig(); err != nil {
		t.Fatal(err)
	}

	// 確保 mtime 不同（某些 FS 精度只到秒）
	time.Sleep(10 * time.Millisecond)
	newCfg := Config{Project: ProjectConfig{Name: "updated"}, Default: "codex"}
	data, _ := json.MarshalIndent(newCfg, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, DirName, ConfigFile), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := cws.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project.Name != "updated" {
		t.Errorf("Name = %q, want updated (should invalidate on mtime change)", cfg.Project.Name)
	}
}

func TestCachedListFeatures_CacheHit(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)
	writeFeatureFile(t, tmp, feature.Feature{ID: "F001-test", Name: "Test", Status: feature.StatusNotStarted})

	list1, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list1) != 1 {
		t.Fatalf("len = %d, want 1", len(list1))
	}

	list2, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 1 || list2[0].ID != list1[0].ID {
		t.Error("second call should return cached data")
	}

	// 回傳的是副本，修改不應影響 cache
	list2[0].Name = "mutated"
	list3, _ := cws.ListFeatures()
	if list3[0].Name != "Test" {
		t.Errorf("cache should be immune to caller mutation, got %q", list3[0].Name)
	}
}

func TestCachedListFeatures_InvalidateOnNewFile(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)
	writeFeatureFile(t, tmp, feature.Feature{ID: "F001-a", Name: "A", Status: feature.StatusNotStarted})

	list1, _ := cws.ListFeatures()
	if len(list1) != 1 {
		t.Fatalf("len = %d, want 1", len(list1))
	}

	writeFeatureFile(t, tmp, feature.Feature{ID: "F002-b", Name: "B", Status: feature.StatusNotStarted})

	list2, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 2 {
		t.Errorf("len = %d, want 2 (should detect new file)", len(list2))
	}
}

func TestCachedListFeatures_InvalidateOnDelete(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)
	writeFeatureFile(t, tmp, feature.Feature{ID: "F001-a", Name: "A", Status: feature.StatusNotStarted})
	p2 := writeFeatureFile(t, tmp, feature.Feature{ID: "F002-b", Name: "B", Status: feature.StatusNotStarted})

	list1, _ := cws.ListFeatures()
	if len(list1) != 2 {
		t.Fatalf("len = %d, want 2", len(list1))
	}

	if err := os.Remove(p2); err != nil {
		t.Fatal(err)
	}

	list2, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list2) != 1 {
		t.Errorf("len = %d, want 1 (should detect deletion)", len(list2))
	}
}

func TestCachedListFeatures_InvalidateOnModify(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)
	path := writeFeatureFile(t, tmp, feature.Feature{ID: "F001-x", Name: "Original", Status: feature.StatusNotStarted})

	list1, _ := cws.ListFeatures()
	if list1[0].Name != "Original" {
		t.Fatalf("Name = %q, want Original", list1[0].Name)
	}

	time.Sleep(10 * time.Millisecond)
	data, _ := yaml.Marshal(feature.Feature{ID: "F001-x", Name: "Modified", Status: feature.StatusNotStarted})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	list2, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if list2[0].Name != "Modified" {
		t.Errorf("Name = %q, want Modified", list2[0].Name)
	}
}

func TestCachedListFeatures_EmptyDir(t *testing.T) {
	cws, _ := setupCachedTestWorkspace(t)
	list, err := cws.ListFeatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("len = %d, want 0", len(list))
	}
}

func TestCachedLoadFeature_CacheHit(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)
	writeFeatureFile(t, tmp, feature.Feature{ID: "F010-load", Name: "LoadTest", Status: feature.StatusNotStarted})

	f1, err := cws.LoadFeature("F010-load")
	if err != nil {
		t.Fatal(err)
	}
	if f1.Name != "LoadTest" {
		t.Errorf("Name = %q, want LoadTest", f1.Name)
	}

	f2, err := cws.LoadFeature("F010-load")
	if err != nil {
		t.Fatal(err)
	}
	if f2.Name != f1.Name {
		t.Error("second call should return cached data")
	}
}

func TestCachedLoadFeature_InvalidateOnModify(t *testing.T) {
	cws, tmp := setupCachedTestWorkspace(t)
	path := writeFeatureFile(t, tmp, feature.Feature{ID: "F010-load2", Name: "Before", Status: feature.StatusNotStarted})

	if _, err := cws.LoadFeature("F010-load2"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)
	data, _ := yaml.Marshal(feature.Feature{ID: "F010-load2", Name: "After", Status: feature.StatusNotStarted})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := cws.LoadFeature("F010-load2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "After" {
		t.Errorf("Name = %q, want After", got.Name)
	}
}

func TestCachedLoadFeature_NotFound(t *testing.T) {
	cws, _ := setupCachedTestWorkspace(t)
	if _, err := cws.LoadFeature("nonexistent"); err == nil {
		t.Error("expected error for nonexistent feature")
	}
}
