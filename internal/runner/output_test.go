package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanStaleOutputs_RemovesExisting(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "gate-verdicts.json")
	if err := os.WriteFile(stale, []byte(`{"verdicts":[]}`), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	CleanStaleOutputs(stale)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale file removed, stat err = %v", err)
	}
}

func TestCleanStaleOutputs_MissingIsNoop(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.json")

	// 不應 panic 或有副作用
	CleanStaleOutputs(missing)

	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing file should stay missing, stat err = %v", err)
	}
}

func TestCleanStaleOutputs_MultiplePathsAndEmpty(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatalf("setup %s: %v", p, err)
		}
	}

	// 空字串應被略過，其餘全數刪除
	CleanStaleOutputs(a, "", b)

	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, stat err = %v", p, err)
		}
	}
}
