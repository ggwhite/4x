package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestPrepareLiveAuth(t *testing.T) {
	t.Run("authOn=false returns empty, writes nothing", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		token, err := prepareLiveAuth(false)
		if err != nil {
			t.Fatalf("prepareLiveAuth(false): %v", err)
		}
		if token != "" {
			t.Fatalf("token = %q, want empty", token)
		}
		if _, err := os.Stat(filepath.Join(home, protocol.UserConfigDir, "live-token")); !os.IsNotExist(err) {
			t.Fatalf("live-token should not exist, stat err = %v", err)
		}
	})

	t.Run("authOn=true fail-hard when WriteLiveToken fails", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		// 在 ~/.4x 位置放一個檔案，使 WriteLiveToken 的 MkdirAll 失敗。
		if err := os.WriteFile(filepath.Join(home, protocol.UserConfigDir), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		token, err := prepareLiveAuth(true)
		if err == nil {
			t.Fatal("prepareLiveAuth(true) should fail-hard when WriteLiveToken fails")
		}
		if token != "" {
			t.Fatalf("token = %q, want empty on error", token)
		}
	})

	t.Run("authOn=true clean HOME produces 0600 token file", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		token, err := prepareLiveAuth(true)
		if err != nil {
			t.Fatalf("prepareLiveAuth(true): %v", err)
		}
		if len(token) != 64 {
			t.Fatalf("token len = %d, want 64", len(token))
		}
		path := filepath.Join(home, protocol.UserConfigDir, "live-token")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 600", perm)
		}
	})
}
