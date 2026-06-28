package verify

import (
	"context"
	"testing"
	"time"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestResolveGroups_VerifyGroupsOnly(t *testing.T) {
	ts := protocol.TestStrategy{
		VerifyGroups: map[string][]string{
			"core":     {"make build", "make test"},
			"sub-repo": {"cd ../sub && make test"},
		},
	}
	groups, err := ResolveGroups(ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	found := false
	for _, g := range groups {
		if g.Name == "core" {
			found = true
			if len(g.Commands) != 2 {
				t.Errorf("core group: expected 2 commands, got %d", len(g.Commands))
			}
		}
	}
	if !found {
		t.Error("core group not found")
	}
}

func TestResolveGroups_GroupsSorted(t *testing.T) {
	ts := protocol.TestStrategy{
		VerifyGroups: map[string][]string{
			"zeta":  {"echo z"},
			"alpha": {"echo a"},
			"mid":   {"echo m"},
		},
	}
	groups, err := ResolveGroups(ts)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, g := range groups {
		if g.Name != want[i] {
			t.Errorf("group[%d]: expected %q, got %q", i, want[i], g.Name)
		}
	}
}

func TestResolveGroups_FallbackVerifyCommands(t *testing.T) {
	ts := protocol.TestStrategy{
		Verify: []string{"make build", "make test"},
	}
	groups, err := ResolveGroups(ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "default" {
		t.Errorf("expected group name 'default', got %q", groups[0].Name)
	}
	if len(groups[0].Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(groups[0].Commands))
	}
}

func TestResolveGroups_BothPresent_Error(t *testing.T) {
	ts := protocol.TestStrategy{
		Verify:       []string{"make test"},
		VerifyGroups: map[string][]string{"core": {"make build"}},
	}
	_, err := ResolveGroups(ts)
	if err == nil {
		t.Error("expected error when both verify_commands and verify_groups present")
	}
}

func TestResolveGroups_NeitherPresent_Error(t *testing.T) {
	ts := protocol.TestStrategy{}
	_, err := ResolveGroups(ts)
	if err == nil {
		t.Error("expected error when no verify commands defined")
	}
}

func TestFallbackGroups_AllPresent(t *testing.T) {
	cfg := protocol.ProjectConfig{
		Build: []string{"make build"},
		Test:  []string{"make test"},
		Lint:  []string{"make lint"},
	}
	groups, err := FallbackGroups(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Name != "fallback" {
		t.Errorf("expected group name 'fallback', got %q", groups[0].Name)
	}
	if len(groups[0].Commands) != 3 {
		t.Errorf("expected 3 commands, got %d", len(groups[0].Commands))
	}
}

func TestFallbackGroups_PartialPresent(t *testing.T) {
	cfg := protocol.ProjectConfig{
		Test: []string{"go test ./..."},
	}
	groups, err := FallbackGroups(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(groups[0].Commands))
	}
	if groups[0].Commands[0] != "go test ./..." {
		t.Errorf("expected 'go test ./...', got %q", groups[0].Commands[0])
	}
}

func TestFallbackGroups_NonePresent(t *testing.T) {
	cfg := protocol.ProjectConfig{}
	_, err := FallbackGroups(cfg)
	if err == nil {
		t.Error("expected error when no build/test/lint commands present")
	}
}

func TestFallbackGroups_CommandOrder(t *testing.T) {
	cfg := protocol.ProjectConfig{
		Build: []string{"build1", "build2"},
		Test:  []string{"test1"},
		Lint:  []string{"lint1", "lint2"},
	}
	groups, err := FallbackGroups(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"build1", "build2", "test1", "lint1", "lint2"}
	if len(groups[0].Commands) != len(want) {
		t.Fatalf("expected %d commands, got %d", len(want), len(groups[0].Commands))
	}
	for i, cmd := range groups[0].Commands {
		if cmd != want[i] {
			t.Errorf("command[%d]: expected %q, got %q", i, want[i], cmd)
		}
	}
}

func TestRunGroups_AllPass(t *testing.T) {
	groups := []Group{
		{Name: "a", Commands: []string{"echo hello"}},
		{Name: "b", Commands: []string{"echo world"}},
	}
	ctx := context.Background()
	result := RunGroups(ctx, groups, ".")
	if !result.Passed {
		t.Errorf("expected pass, got fail: %+v", result.Commands)
	}
	if len(result.Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(result.Commands))
	}
	for _, cmd := range result.Commands {
		if cmd.ExitCode != 0 {
			t.Errorf("command %q: expected exit 0, got %d", cmd.Command, cmd.ExitCode)
		}
		if cmd.Group == "" {
			t.Errorf("command %q: group should not be empty", cmd.Command)
		}
	}
}

func TestRunGroups_GroupFail_SkipsRemainingInGroup(t *testing.T) {
	groups := []Group{
		{Name: "fail-group", Commands: []string{"false", "echo should-skip"}},
		{Name: "ok-group", Commands: []string{"echo still-runs"}},
	}
	ctx := context.Background()
	result := RunGroups(ctx, groups, ".")
	if result.Passed {
		t.Error("expected overall fail")
	}

	var skippedFound bool
	var okFound bool
	for _, cmd := range result.Commands {
		if cmd.Command == "echo should-skip" && cmd.Skipped {
			skippedFound = true
		}
		if cmd.Command == "echo still-runs" && cmd.ExitCode == 0 {
			okFound = true
		}
	}
	if !skippedFound {
		t.Error("expected 'echo should-skip' to be marked skipped")
	}
	if !okFound {
		t.Error("expected 'echo still-runs' in ok-group to still execute")
	}
}

func TestRunGroups_Parallel(t *testing.T) {
	groups := []Group{
		{Name: "slow", Commands: []string{"sleep 0.3"}},
		{Name: "fast", Commands: []string{"sleep 0.3"}},
	}
	ctx := context.Background()
	start := time.Now()
	result := RunGroups(ctx, groups, ".")
	elapsed := time.Since(start)
	if !result.Passed {
		t.Error("expected pass")
	}
	if elapsed > 1*time.Second {
		t.Errorf("groups should run in parallel, took %v", elapsed)
	}
}

func TestRunGroups_ContextTimeout(t *testing.T) {
	groups := []Group{
		{Name: "slow", Commands: []string{"sleep 10"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result := RunGroups(ctx, groups, ".")
	if result.Passed {
		t.Error("expected fail due to timeout")
	}
}
