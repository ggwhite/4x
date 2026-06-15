package server

import (
	"net/http"
	"testing"

	"github.com/ggwhite/4x/internal/protocol"
)

func TestSingleResolver(t *testing.T) {
	ws := protocol.NewCachedWorkspace(setupMultiWorkspace(t, "single-proj"))
	resolver := singleResolver(ws, nil)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	got, pm, bm, err := resolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != ws {
		t.Error("workspace mismatch")
	}
	if pm != nil {
		t.Error("pm should be nil")
	}
	if bm == nil {
		t.Error("bm should be non-nil")
	}
}

func TestMultiResolver_CompatSingleProject(t *testing.T) {
	ws := setupMultiWorkspace(t, "only-one")
	reg := NewProjectRegistry()
	id := reg.Add(ws)

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	got, _, _, err := resolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Root != reg.Get(id).Root {
		t.Error("workspace mismatch")
	}
}

func TestMultiResolver_CompatMultiProject(t *testing.T) {
	reg := NewProjectRegistry()
	reg.Add(setupMultiWorkspace(t, "proj-a"))
	reg.Add(setupMultiWorkspace(t, "proj-b"))

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	if _, _, _, err := resolver(r); err == nil {
		t.Error("expected error for multiple projects")
	}
}

func TestMultiResolver_CompatZeroProject(t *testing.T) {
	reg := NewProjectRegistry()

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	if _, _, _, err := resolver(r); err == nil {
		t.Error("expected error for zero projects")
	}
}

func TestMultiResolver_ContextInjected(t *testing.T) {
	ws := setupMultiWorkspace(t, "ctx-proj")
	reg := NewProjectRegistry()
	id := reg.Add(ws)
	entry := reg.getEntry(id)

	resolver := multiResolver(reg)

	r, _ := http.NewRequest("GET", "/api/tasks", nil)
	r = r.WithContext(withResolvedProject(r.Context(), entry))
	got, pm, bm, err := resolver(r)
	if err != nil {
		t.Fatal(err)
	}
	if got != entry.ws || pm != entry.pm || bm != entry.bm {
		t.Error("resolver should return injected entry's ws/pm/bm")
	}
}
