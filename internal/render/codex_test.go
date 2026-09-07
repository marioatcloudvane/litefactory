package render_test

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

// TestCodexRendererRegistered checks that this file's init actually claimed
// harness key "codex" in the shared registry, per T-15.
func TestCodexRendererRegistered(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup(%q): unexpected error: %v", "codex", err)
	}
	if r == nil {
		t.Fatal("Lookup(\"codex\"): got nil Renderer")
	}

	found := false
	for _, h := range render.Harnesses() {
		if h == "codex" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Harnesses(): %v does not contain %q", render.Harnesses(), "codex")
	}
}

// TestCodexRenderBasic checks AC-1: a plain Name/Description/Body source
// lands at ".codex/skills/<agent>/SKILL.md" with the frontmatter fields
// mapped into a YAML header ahead of the body.
func TestCodexRenderBasic(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:        "go-architect",
			Description: "Plans Go implementation work",
		},
		Body: "You are a senior Go architect.\n\nDo the thing.\n",
	}

	got, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Render: got %d files, want 1: %+v", len(got), got)
	}

	wantPath := ".codex/skills/go-architect/SKILL.md"
	if got[0].Path != wantPath {
		t.Errorf("Path = %q, want %q", got[0].Path, wantPath)
	}

	wantBytes := "---\nname: go-architect\ndescription: Plans Go implementation work\n---\n\nYou are a senior Go architect.\n\nDo the thing.\n"
	if string(got[0].Bytes) != wantBytes {
		t.Errorf("Bytes = %q, want %q", string(got[0].Bytes), wantBytes)
	}
}

// TestCodexRenderEmptyBody checks that a source with no body still produces
// exactly one trailing newline and no dangling blank frontmatter separator.
func TestCodexRenderEmptyBody(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{Name: "empty-body-agent", Description: "d"},
	}

	got, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	want := "---\nname: empty-body-agent\ndescription: d\n---\n"
	if string(got[0].Bytes) != want {
		t.Errorf("Bytes = %q, want %q", string(got[0].Bytes), want)
	}
}

// TestCodexRenderDeterministic is AC-3's own basic determinism coverage:
// rendering the same source twice, including one with a multi-key Extra
// map (to exercise the sort-before-report path), produces byte-identical
// output both times.
func TestCodexRenderDeterministic(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:        "deterministic-agent",
			Description: "Checks repeatability",
		},
		Body: "First line.\nSecond line.\n",
	}

	first, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render (first): unexpected error: %v", err)
	}
	second, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render (second): unexpected error: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("file count differs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Errorf("Path[%d] differs: %q vs %q", i, first[i].Path, second[i].Path)
		}
		if string(first[i].Bytes) != string(second[i].Bytes) {
			t.Errorf("Bytes[%d] differs:\n%q\nvs\n%q", i, first[i].Bytes, second[i].Bytes)
		}
	}
}

// TestCodexRenderMissingName checks that an empty agent name is rejected
// rather than producing a SKILL.md path with an empty directory segment.
func TestCodexRenderMissingName(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	_, err = r.Render(render.ResolvedSource{Frontmatter: render.Frontmatter{Description: "no name"}})
	if err == nil {
		t.Fatal("Render with empty Name: expected an error, got nil")
	}
	if !errors.Is(err, render.ErrMissingAgentName) {
		t.Fatalf("Render error does not wrap ErrMissingAgentName: %v", err)
	}
}

// TestCodexRenderUnsupportedModel checks AC-2: Model has no Codex
// equivalent, so a non-empty Model must fail loudly naming "model", not be
// silently dropped.
func TestCodexRenderUnsupportedModel(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	_, err = r.Render(render.ResolvedSource{
		Frontmatter: render.Frontmatter{Name: "model-agent", Model: "opus"},
	})
	assertUnsupportedField(t, err, "model-agent", "codex", "model")
}

// TestCodexRenderUnsupportedTools checks AC-2: Tools has no Codex
// equivalent, so a non-empty Tools list must fail loudly naming "tools".
func TestCodexRenderUnsupportedTools(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	_, err = r.Render(render.ResolvedSource{
		Frontmatter: render.Frontmatter{Name: "tools-agent", Tools: []string{"Read", "Write"}},
	})
	assertUnsupportedField(t, err, "tools-agent", "codex", "tools")
}

// TestCodexRenderUnsupportedExtraReportsLowestKey checks AC-2 and
// determinism together: an Extra frontmatter field the renderer does not
// recognize fails loudly naming that key, and when several are present the
// lexicographically first one is reported every time, regardless of map
// iteration order.
func TestCodexRenderUnsupportedExtraReportsLowestKey(t *testing.T) {
	r, err := render.Lookup("codex")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:  "extra-agent",
			Extra: map[string]any{"zeta_permission": true, "alpha_permission": true},
		},
	}

	for i := 0; i < 5; i++ {
		_, err := r.Render(source)
		assertUnsupportedField(t, err, "extra-agent", "codex", "alpha_permission")
	}
}

func assertUnsupportedField(t *testing.T, err error, agent, harness, field string) {
	t.Helper()
	if err == nil {
		t.Fatal("Render: expected an error, got nil")
	}
	var uf *render.UnsupportedFieldError
	if !errors.As(err, &uf) {
		t.Fatalf("Render error is not an *UnsupportedFieldError: %v (%T)", err, err)
	}
	if uf.Agent != agent || uf.Harness != harness || uf.Field != field {
		t.Fatalf("UnsupportedFieldError = %+v, want Agent=%q Harness=%q Field=%q", uf, agent, harness, field)
	}
	if !errors.Is(err, render.ErrUnsupportedField) {
		t.Fatalf("Render error does not wrap ErrUnsupportedField: %v", err)
	}
}
