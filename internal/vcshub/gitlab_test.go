package vcshub

import (
	"errors"
	"strings"
	"testing"
)

func TestGlabHub_Preflight_NotInstalled(t *testing.T) {
	origLook := lookPath
	defer func() { lookPath = origLook }()
	lookPath = func(file string) (string, error) { return "", errors.New("not found") }

	if err := (&glabHub{}).Preflight("/repo"); err == nil {
		t.Fatal("expected error when glab not installed")
	}
}

func TestGlabHub_Preflight_NotAuthenticated(t *testing.T) {
	origLook, origExec := lookPath, execCommand
	defer func() { lookPath, execCommand = origLook, origExec }()
	lookPath = func(file string) (string, error) { return "/usr/bin/glab", nil }
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("no token provided"), errors.New("exit status 1")
	}

	if err := (&glabHub{}).Preflight("/repo"); err == nil {
		t.Fatal("expected error when glab not authenticated")
	}
}

func TestGlabHub_Preflight_OK(t *testing.T) {
	origLook, origExec := lookPath, execCommand
	defer func() { lookPath, execCommand = origLook, origExec }()
	lookPath = func(file string) (string, error) { return "/usr/bin/glab", nil }
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("Logged in to gitlab.example.com"), nil
	}

	if err := (&glabHub{}).Preflight("/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGlabHub_CreateIssue(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	var gotArgs []string
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("https://gitlab.example.com/acme/widget/-/issues/7\n"), nil
	}

	id, url, err := (&glabHub{}).CreateIssue("/repo", "Fix login", "body text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
	if url != "https://gitlab.example.com/acme/widget/-/issues/7" {
		t.Errorf("url = %q", url)
	}
	joined := strings.Join(gotArgs, " ")
	if gotArgs[0] != "issue" || gotArgs[1] != "create" {
		t.Errorf("args = %v, want to start with issue create", gotArgs)
	}
	if !strings.Contains(joined, "--title Fix login") || !strings.Contains(joined, "--description body text") || !strings.Contains(joined, "--yes") {
		t.Errorf("args = %v missing --title/--description/--yes", gotArgs)
	}
}

func TestGlabHub_GetIssue_Success(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("Fix login redirect\nhttps://gitlab.example.com/acme/widget/-/issues/7\n"), nil
	}

	id, url, err := (&glabHub{}).GetIssue("/repo", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "7" || url != "https://gitlab.example.com/acme/widget/-/issues/7" {
		t.Errorf("got id=%q url=%q", id, url)
	}
}

func TestGlabHub_GetIssue_NotFound(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("404 Not Found"), errors.New("exit status 1")
	}

	if _, _, err := (&glabHub{}).GetIssue("/repo", "999"); err == nil {
		t.Fatal("expected error for non-existent issue")
	}
}

func TestGlabHub_OpenMR_Success(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	var gotArgs []string
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("https://gitlab.example.com/acme/widget/-/merge_requests/9\n"), nil
	}

	url, err := (&glabHub{}).OpenMR("/repo", "4x/F127", "main", "F127: Issue-first", "Closes #7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://gitlab.example.com/acme/widget/-/merge_requests/9" {
		t.Errorf("url = %q", url)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "--target-branch main") || !strings.Contains(joined, "--yes") {
		t.Errorf("args = %v missing --target-branch/--yes", gotArgs)
	}
}

func TestGlabHub_OpenMR_AlreadyExists(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("error: merge request already exists:\n" +
			"https://gitlab.example.com/acme/widget/-/merge_requests/9\n"), errors.New("exit status 1")
	}

	url, err := (&glabHub{}).OpenMR("/repo", "4x/F127", "main", "title", "body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://gitlab.example.com/acme/widget/-/merge_requests/9" {
		t.Errorf("url = %q", url)
	}
}
