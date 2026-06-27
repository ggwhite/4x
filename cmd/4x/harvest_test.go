package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/learning"
	"github.com/ggwhite/4x/internal/prompt"
	"github.com/ggwhite/4x/internal/protocol"
)

func TestHarvestLearnings_Success(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	retro := learning.RetroFile{
		Learnings: []learning.RetroLearning{
			{Category: learning.CategoryCodeQuality, Content: "always wrap errors"},
			{Category: learning.CategoryTesting, Content: "test edge cases"},
		},
	}
	data, _ := json.Marshal(retro)
	retroPath := filepath.Join(ws.FeatureDir(featureID), protocol.RetroLearningsFile)
	writeTestFileHelper(t, retroPath, string(data))

	prompt.HarvestLearnings(ws, featureID)

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(store.Entries))
	}
}

func TestHarvestLearnings_NoRetroFile(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	prompt.HarvestLearnings(ws, featureID)

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Error("expected no learnings.json when no retro file")
	}
}

func TestHarvestLearnings_EmptyLearnings(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	retroPath := filepath.Join(ws.FeatureDir(featureID), protocol.RetroLearningsFile)
	writeTestFileHelper(t, retroPath, `{"learnings":[]}`)

	prompt.HarvestLearnings(ws, featureID)

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Error("expected no learnings.json when learnings empty")
	}
}
