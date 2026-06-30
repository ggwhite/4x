package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestGenerateLearningsContext_GroupsByCategory(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryOps, Content: "set GOWORK=off in worktree", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "reuse existing structures", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L003", Category: learning.CategoryOps, Content: "check docker daemon first", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L004", Category: learning.CategoryTesting, Content: "test edge cases", Status: learning.StatusActive, CreatedAt: time.Now()},
	}}
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "## design") {
		t.Error("missing design section")
	}
	if !strings.Contains(content, "## ops") {
		t.Error("missing ops section")
	}
	if !strings.Contains(content, "## testing") {
		t.Error("missing testing section")
	}

	designIdx := strings.Index(content, "## design")
	opsIdx := strings.Index(content, "## ops")
	testingIdx := strings.Index(content, "## testing")
	if designIdx > opsIdx || opsIdx > testingIdx {
		t.Errorf("categories not sorted: design=%d ops=%d testing=%d", designIdx, opsIdx, testingIdx)
	}

	opsSection := content[opsIdx:]
	if !strings.Contains(opsSection, "- set GOWORK=off in worktree") {
		t.Error("missing ops entry 1")
	}
	if !strings.Contains(opsSection, "- check docker daemon first") {
		t.Error("missing ops entry 2")
	}
}

func TestGenerateLearningsContext_OnlyActive(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	store := learning.Store{Version: 1, Entries: []learning.Entry{
		{ID: "L001", Category: learning.CategoryDesign, Content: "active entry", Status: learning.StatusActive, CreatedAt: time.Now()},
		{ID: "L002", Category: learning.CategoryDesign, Content: "stale entry", Status: learning.StatusStale, CreatedAt: time.Now()},
		{ID: "L003", Category: learning.CategoryDesign, Content: "promoted entry", Status: learning.StatusPromoted, CreatedAt: time.Now()},
	}}
	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	if err := store.Save(storePath); err != nil {
		t.Fatal(err)
	}

	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "active entry") {
		t.Error("active entry should be included")
	}
	if strings.Contains(content, "stale entry") {
		t.Error("stale entry should not be included")
	}
	if strings.Contains(content, "promoted entry") {
		t.Error("promoted entry should not be included")
	}
}

func TestGenerateLearningsContext_EmptyStore(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}

	if err := GenerateLearningsContext(ws); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(ws.DotDir(), protocol.LearningsContextFile)
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "# Learnings") {
		t.Error("missing header")
	}
	if !strings.Contains(content, "No active learnings.") {
		t.Error("missing empty message")
	}
}
