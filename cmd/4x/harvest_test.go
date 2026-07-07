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

func TestHarvestLearnings_MultipleRolesSameRound(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	roundDir := ws.RoundDir(featureID, 1)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRoleLearningsHelper(t, filepath.Join(roundDir, "coder-learnings.json"),
		"coder", learning.CategoryCodeQuality, "coder learning content")
	writeRoleLearningsHelper(t, filepath.Join(roundDir, "reviewer-learnings.json"),
		"reviewer", learning.CategoryReview, "reviewer learning content")

	prompt.HarvestLearnings(ws, featureID)

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	// 兩個角色的檔案都應被收割，而非只留最後寫入者。
	if len(store.Entries) != 2 {
		t.Errorf("expected 2 entries (coder + reviewer), got %d", len(store.Entries))
	}
}

func TestHarvestLearnings_LegacyRoleLearningsFile(t *testing.T) {
	root := t.TempDir()
	if err := protocol.Init(root, protocol.Config{}); err != nil {
		t.Fatal(err)
	}
	ws := &protocol.Workspace{Root: root}
	featureID := "F042-test"
	if err := ws.InitFeatureDir(featureID); err != nil {
		t.Fatal(err)
	}

	roundDir := ws.RoundDir(featureID, 1)
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 舊版單一固定檔名 role-learnings.json 仍應被 glob 收割（向下相容）。
	writeRoleLearningsHelper(t, filepath.Join(roundDir, protocol.RoleLearningsFileName),
		"coder", learning.CategoryCodeQuality, "legacy learning content")

	prompt.HarvestLearnings(ws, featureID)

	storePath := filepath.Join(ws.DotDir(), protocol.LearningsFile)
	store, err := learning.LoadStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Entries) != 1 {
		t.Errorf("expected 1 entry from legacy role-learnings.json, got %d", len(store.Entries))
	}
}

func writeRoleLearningsHelper(t *testing.T, path, role string, cat learning.Category, content string) {
	t.Helper()
	rf := learning.RoleLearningsFile{
		Role:      role,
		Learnings: []learning.RetroLearning{{Category: cat, Content: content}},
	}
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFileHelper(t, path, string(data))
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
