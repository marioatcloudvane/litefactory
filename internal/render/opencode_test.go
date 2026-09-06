package render_test

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

// TestOpencodeRendererRegistered checks that this file's init actually
// claimed harness key "opencode" in the shared registry, per T-16.
func TestOpencodeRendererRegistered(t *testing.T) {
	r, err := render.Lookup("opencode")
	if err != nil {
		t.Fatalf("Lookup(%q): unexpected error: %v", "opencode", err)
	}
	if r == nil {
		t.Fatal("Lookup(\"opencode\"): got nil Renderer")
	}

	found := false
	for _, h := range render.Harnesses() {
		if h == "opencode" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Harnesses(): %v does not contain %q", render.Harnesses(), "opencode")
	}
}

// TestOpencodeRenderBasic checks AC-1: a Name/Description/Tools/Model/Body
// source lands at ".opencode/agents/<agent>.md" with the frontmatter fields
// mapped into a YAML header ahead of the body.
func TestOpencodeRenderBasic(t *testing.T) {
	r, err := render.Lookup("opencode")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:        "go-architect",
			Description: "Designs the plan",
			Tools:       []string{"Read", "Write"},
			Model:       "opus",
		},
		Body: "You are a senior Go architect.\n",
	}

	got, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Render: got %d files, want 1: %+v", len(got), got)
	}

	wantPath := ".opencode/agents/go-architect.md"
	if got[0].Path != wantPath {
		t.Errorf("Path = %q, want %q", got[0].Path, wantPath)
	}

	wantBytes := "---\n" +
		"name: go-architect\n" +
		"description: Designs the plan\n" +
		"tools:\n" +
		"    - Read\n" +
		"    - Write\n" +
		"model: opus\n" +
		"---\n" +
		"\n" +
		"You are a senior Go architect.\n"
	if string(got[0].Bytes) != wantBytes {
		t.Errorf("Bytes = %q, want %q", string(got[0].Bytes), wantBytes)
	}
}

// TestOpencodeRenderEmptyBody checks that a source with no body, and no
// Tools/Model set, still produces exactly one trailing newline, no dangling
// blank frontmatter separator, and omits the optional fields.
func TestOpencodeRenderEmptyBody(t *testing.T) {
	r, err := render.Lookup("opencode")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{Name: "bare-agent", Description: "d"},
	}

	got, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	want := "---\nname: bare-agent\ndescription: d\n---\n"
	if string(got[0].Bytes) != want {
		t.Errorf("Bytes = %q, want %q", string(got[0].Bytes), want)
	}
}

// TestOpencodeRenderDeterministic renders the same source twice and checks
// the bytes are identical — AC-3's minimal determinism coverage for this
// run, since T-17's shared cross-harness golden suite does not exist yet.
func TestOpencodeRenderDeterministic(t *testing.T) {
	r, err := render.Lookup("opencode")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:        "determinism-check",
			Description: "Checks repeated renders match",
			Tools:       []string{"Bash", "Read", "Grep"},
			Model:       "sonnet",
		},
		Body: "Body content that should round-trip unchanged.\n",
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

// TestOpencodeRenderMissingName checks that an empty agent name is rejected
// rather than producing a path with an empty file-name segment.
func TestOpencodeRenderMissingName(t *testing.T) {
	r, err := render.Lookup("opencode")
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

// TestOpencodeRenderUnsupportedExtraReportsLowestKey checks AC-2 and
// determinism together: an Extra frontmatter field this renderer's mapping
// does not recognize fails loudly naming that key, and when several are
// present the lexicographically first one is reported every time,
// regardless of map iteration order.
func TestOpencodeRenderUnsupportedExtraReportsLowestKey(t *testing.T) {
	r, err := render.Lookup("opencode")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:  "picky-agent",
			Extra: map[string]any{"zeta_permission": true, "alpha_permission": true},
		},
	}

	for i := 0; i < 5; i++ {
		got, err := r.Render(source)
		if got != nil {
			t.Fatalf("Render: expected no files alongside an error, got: %+v", got)
		}
		assertOpencodeUnsupportedField(t, err, "picky-agent", "opencode", "alpha_permission")
	}
}

func assertOpencodeUnsupportedField(t *testing.T, err error, agent, harness, field string) {
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
