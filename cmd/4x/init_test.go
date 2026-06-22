package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestDumpTemplates_CreatesFiles(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	dotDir := filepath.Join(root, protocol.DirName)

	if err := dumpTemplates(dotDir, false); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dotDir, "templates", "designer.md.tmpl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected designer.md.tmpl to exist: %v", err)
	}
	if !strings.Contains(string(data), "You are the Designer") {
		t.Error("expected designer template content")
	}

	localePath := filepath.Join(dotDir, "templates", "locale.tmpl")
	if _, err := os.Stat(localePath); err != nil {
		t.Fatalf("expected locale.tmpl to exist: %v", err)
	}
}

func TestDumpTemplates_SkipsExisting(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	dotDir := filepath.Join(root, protocol.DirName)

	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "designer.md.tmpl"), []byte("MY CUSTOM"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := dumpTemplates(dotDir, false); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(tmplDir, "designer.md.tmpl"))
	if string(data) != "MY CUSTOM" {
		t.Errorf("expected existing file to be preserved, got %q", string(data))
	}
}

func TestDumpTemplates_ForceOverwrites(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	dotDir := filepath.Join(root, protocol.DirName)

	tmplDir := filepath.Join(dotDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "designer.md.tmpl"), []byte("MY CUSTOM"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := dumpTemplates(dotDir, true); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(tmplDir, "designer.md.tmpl"))
	if string(data) == "MY CUSTOM" {
		t.Error("expected force to overwrite existing file")
	}
	if !strings.Contains(string(data), "You are the Designer") {
		t.Error("expected builtin content after force overwrite")
	}
}
