package vcshub

import (
	"errors"
	"testing"
)

func TestNew_GitHub(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("git@github.com:acme/widget.git\n"), nil
	}
	if _, ok := New("/repo").(*githubHub); !ok {
		t.Errorf("New() did not return *githubHub for github.com remote")
	}
}

func TestNew_GitLab(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return []byte("git@gitlab.example.com:acme/widget.git\n"), nil
	}
	if _, ok := New("/repo").(*glabHub); !ok {
		t.Errorf("New() did not return *glabHub for self-hosted GitLab remote")
	}
}

func TestNew_NoRemote(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()
	execCommand = func(dir, name string, args ...string) ([]byte, error) {
		return nil, errors.New("fatal: No such remote 'origin'")
	}
	if _, ok := New("/repo").(*glabHub); !ok {
		t.Errorf("New() did not fall back to *glabHub when no remote")
	}
}

func TestIssueIDFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"42", "42"},
		{"https://github.com/acme/widget/issues/42", "42"},
		{"https://gitlab.example.com/acme/widget/-/issues/7", "7"},
	}
	for _, c := range cases {
		if got := issueIDFromURL(c.in); got != c.want {
			t.Errorf("issueIDFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://github.com/acme/widget/issues/42", "https://github.com/acme/widget/issues/42"},
		{"Created issue: https://github.com/acme/widget/issues/42.", "https://github.com/acme/widget/issues/42"},
		{"see (https://gitlab.example.com/acme/widget/-/merge_requests/7)", "https://gitlab.example.com/acme/widget/-/merge_requests/7"},
		{"no url here", ""},
	}
	for _, c := range cases {
		if got := extractURL(c.in); got != c.want {
			t.Errorf("extractURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
