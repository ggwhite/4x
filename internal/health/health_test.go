package health

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestRunHealthCheck_AllPass(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db", "check-redis"},
	}
	var calls []string
	executor := func(cmd string) error {
		calls = append(calls, cmd)
		return nil
	}

	if err := RunHealthCheck(cfg, executor); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	if len(calls) != 2 {
		t.Errorf("expected 2 commands run, got %v", calls)
	}
}

func TestRunHealthCheck_FailNoRecovery(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db"},
	}
	executor := func(cmd string) error {
		return fmt.Errorf("exit 1")
	}

	err := RunHealthCheck(cfg, executor)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "check-db") {
		t.Errorf("error should name the failed command, got %v", err)
	}
}

func TestRunHealthCheck_FailRecoverySuccess(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db"},
		Recovery: []string{"restart-db"},
	}
	attempt := 0
	var recovered bool
	executor := func(cmd string) error {
		switch cmd {
		case "check-db":
			attempt++
			if attempt == 1 {
				return fmt.Errorf("exit 1")
			}
			return nil
		case "restart-db":
			recovered = true
			return nil
		}
		return nil
	}

	if err := RunHealthCheck(cfg, executor); err != nil {
		t.Fatalf("expected pass after recovery, got %v", err)
	}
	if !recovered {
		t.Error("expected recovery command to run")
	}
	if attempt != 2 {
		t.Errorf("expected 2 check attempts, got %d", attempt)
	}
}

func TestRunHealthCheck_FailRecoveryFail(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db"},
		Recovery: []string{"restart-db"},
	}
	executor := func(cmd string) error {
		if cmd == "restart-db" {
			return fmt.Errorf("recovery boom")
		}
		return fmt.Errorf("exit 1")
	}

	err := RunHealthCheck(cfg, executor)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "recovery") {
		t.Errorf("error should mention recovery failure, got %v", err)
	}
}

// recovery 成功但重跑 health check 仍失敗，最多重試一次後仍回傳 error
func TestRunHealthCheck_RecheckStillFails(t *testing.T) {
	cfg := protocol.HealthCheck{
		Commands: []string{"check-db"},
		Recovery: []string{"restart-db"},
	}
	checkAttempts := 0
	executor := func(cmd string) error {
		if cmd == "check-db" {
			checkAttempts++
			return fmt.Errorf("exit 1")
		}
		return nil // recovery 成功
	}

	err := RunHealthCheck(cfg, executor)
	if err == nil {
		t.Fatal("expected error")
	}
	if checkAttempts != 2 {
		t.Errorf("expected exactly 2 check attempts (no infinite retry), got %d", checkAttempts)
	}
	if !strings.Contains(err.Error(), "after recovery") {
		t.Errorf("error should indicate failure after recovery, got %v", err)
	}
}

func TestRunHealthCheck_NoCommands(t *testing.T) {
	cfg := protocol.HealthCheck{}
	executor := func(cmd string) error {
		t.Error("executor should not be called when no commands")
		return nil
	}

	if err := RunHealthCheck(cfg, executor); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}
