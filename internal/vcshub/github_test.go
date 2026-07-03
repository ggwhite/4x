package vcshub

import (
	"errors"
	"strings"
	"testing"
)

func TestGithubHub_Preflight_NotInstalled(t *testing.T) {
	origLook := lookPath
	defer func() { lookPath = origLook }()
	lookPath = func(file string) (string, error) { return "", errors.New("not found") }

	if err := (&githubHub{}).Preflight("/repo"); err == nil {
		t.Fatal("expected error when gh not installed")
	}
}

func TestGithubHub_Preflight_NotAuthenticated(t *testing.T) {
	origLook, origExec := lookPath, execCommand
	defer func() { lookPath, execCommand = origLook, origExec }()
	lookPath = func(file string) (string, error) { return "/usr/bin/gh", nil }
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("You are not logged into any GitHub hosts"), errors.New("exit status 1")
	}

	if err := (&githubHub{}).Preflight("/repo"); err == nil {
		t.Fatal("expected error when gh not authenticated")
	}
}

func TestGithubHub_Preflight_OK(t *testing.T) {
	origLook, origExec := lookPath, execCommand
	defer func() { lookPath, execCommand = origLook, origExec }()
	lookPath = func(file string) (string, error) { return "/usr/bin/gh", nil }
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("Logged in to github.com as octocat"), nil
	}

	if err := (&githubHub{}).Preflight("/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGithubHub_CreateIssue(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	var gotArgs []string
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("https://github.com/acme/widget/issues/42\n"), nil
	}

	id, url, err := (&githubHub{}).CreateIssue("/repo", "Fix login", "body text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
	if url != "https://github.com/acme/widget/issues/42" {
		t.Errorf("url = %q", url)
	}
	joined := strings.Join(gotArgs, " ")
	if gotArgs[0] != "issue" || gotArgs[1] != "create" {
		t.Errorf("args = %v, want to start with issue create", gotArgs)
	}
	if !strings.Contains(joined, "--title Fix login") || !strings.Contains(joined, "--body body text") {
		t.Errorf("args = %v missing --title/--body", gotArgs)
	}
}

func TestGithubHub_GetIssue_Success(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte(`{"number":42,"url":"https://github.com/acme/widget/issues/42"}`), nil
	}

	id, url, err := (&githubHub{}).GetIssue("/repo", "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "42" || url != "https://github.com/acme/widget/issues/42" {
		t.Errorf("got id=%q url=%q", id, url)
	}
}

func TestGithubHub_GetIssue_NotFound(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("GraphQL: Could not resolve to an issue"), errors.New("exit status 1")
	}

	if _, _, err := (&githubHub{}).GetIssue("/repo", "999"); err == nil {
		t.Fatal("expected error for non-existent issue")
	}
}

func TestGithubHub_OpenMR_Success(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	var gotArgs []string
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("https://github.com/acme/widget/pull/7\n"), nil
	}

	url, err := (&githubHub{}).OpenMR("/repo", "4x/F127", "main", "F127: Issue-first", "Closes #42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/acme/widget/pull/7" {
		t.Errorf("url = %q", url)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "--base main") || !strings.Contains(joined, "--head 4x/F127") {
		t.Errorf("args = %v missing --base/--head", gotArgs)
	}
}

func TestGithubHub_OpenMR_AlreadyExists(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("a pull request for branch \"4x/F127\" into branch \"main\" already exists:\n" +
			"https://github.com/acme/widget/pull/7\n"), errors.New("exit status 1")
	}

	url, err := (&githubHub{}).OpenMR("/repo", "4x/F127", "main", "title", "body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/acme/widget/pull/7" {
		t.Errorf("url = %q", url)
	}
}
