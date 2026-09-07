package render_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

// TestRegisterLookupRoundTrip is a minimal smoke test for this task's own
// verification, not T-17's shared cross-harness contract suite (which
// belongs to the story that lands once the three concrete renderers exist).
// It only exercises that a fake harness, added as a plain map entry via
// Register, is retrievable via Lookup and callable through the Renderer
// interface — the shape AC-1 depends on.
func TestRegisterLookupRoundTrip(t *testing.T) {
	const harness = "render-smoke-fake"
	want := render.RenderedFile{Path: "AGENT.fake", Bytes: []byte("hello\n")}

	render.Register(harness, render.RendererFunc(func(source render.ResolvedSource) ([]render.RenderedFile, error) {
		return []render.RenderedFile{want}, nil
	}))

	r, err := render.Lookup(harness)
	if err != nil {
		t.Fatalf("Lookup(%q): unexpected error: %v", harness, err)
	}

	got, err := r.Render(render.ResolvedSource{Frontmatter: render.Frontmatter{Name: "agent-x"}})
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Path != want.Path || string(got[0].Bytes) != string(want.Bytes) {
		t.Fatalf("Render: got %+v, want [%+v]", got, want)
	}

	names := render.Harnesses()
	found := false
	for _, n := range names {
		if n == harness {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Harnesses(): %v does not contain %q", names, harness)
	}
}

// TestLookupUnknownHarness checks that an unregistered harness name comes
// back as a typed, sentinel-wrapped error rather than a bare string or a
// zero Renderer.
func TestLookupUnknownHarness(t *testing.T) {
	_, err := render.Lookup("render-smoke-does-not-exist")
	if err == nil {
		t.Fatal("Lookup of an unregistered harness: expected an error, got nil")
	}
	var unknown *render.UnknownHarnessError
	if !errors.As(err, &unknown) {
		t.Fatalf("Lookup error is not an *UnknownHarnessError: %v (%T)", err, err)
	}
	if !errors.Is(err, render.ErrUnknownHarness) {
		t.Fatalf("Lookup error does not wrap ErrUnknownHarness: %v", err)
	}
}

// TestUnsupportedFieldErrorNamesAgentHarnessAndField checks AC-2's
// requirement in the type itself: the error message names the agent key,
// the harness, and the dotted field path, and it wraps ErrUnsupportedField
// so a caller can branch on it with errors.Is.
func TestUnsupportedFieldErrorNamesAgentHarnessAndField(t *testing.T) {
	err := &render.UnsupportedFieldError{Agent: "go-architect", Harness: "codex", Field: "tools.network_access"}
	msg := err.Error()
	for _, want := range []string{"go-architect", "codex", "tools.network_access"} {
		if !strings.Contains(msg, want) {
			t.Errorf("UnsupportedFieldError.Error() = %q, want it to contain %q", msg, want)
		}
	}
	if !errors.Is(err, render.ErrUnsupportedField) {
		t.Fatalf("UnsupportedFieldError does not wrap ErrUnsupportedField: %v", err)
	}
}
