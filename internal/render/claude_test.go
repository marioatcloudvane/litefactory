package render_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/render"
)

// TestClaudeRenderStandaloneFile checks AC-1: the standalone-file output
// shape has a YAML frontmatter block with name and description, tools
// rendered as a comma-separated list, and the body content following it.
func TestClaudeRenderStandaloneFile(t *testing.T) {
	r, err := render.Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(\"claude\"): unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:        "go-architect",
			Description: "Plans Go implementation work",
			Tools:       []string{"Read", "Write", "Bash"},
			Model:       "sonnet",
		},
		Body: "You are the Go architect.\n\nDo the thing.\n",
	}

	files, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Render: got %d files, want 1", len(files))
	}

	got := files[0]
	if got.Path != ".claude/agents/go-architect.md" {
		t.Errorf("Path = %q, want %q", got.Path, ".claude/agents/go-architect.md")
	}

	out := string(got.Bytes)
	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("output does not start with a frontmatter delimiter: %q", out)
	}
	for _, want := range []string{
		"name: go-architect\n",
		"description: Plans Go implementation work\n",
		"tools: Read, Write, Bash\n",
		"model: sonnet\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "You are the Go architect.") {
		t.Errorf("output missing body content, got:\n%s", out)
	}
	if !strings.HasSuffix(out, "Do the thing.\n") {
		t.Errorf("output does not end with exactly one trailing newline after the body, got:\n%q", out)
	}
	if strings.HasSuffix(out, "\n\n") {
		t.Errorf("output has more than one trailing newline: %q", out)
	}
}

// TestClaudeRenderOmitsUnsetOptionalFields checks that tools and model are
// omitted from the frontmatter block entirely when the source does not set
// them, rather than being rendered as empty keys.
func TestClaudeRenderOmitsUnsetOptionalFields(t *testing.T) {
	r, err := render.Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(\"claude\"): unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:        "reviewer",
			Description: "Reviews pull requests",
		},
		Body: "Review the diff.\n",
	}

	files, err := r.Render(source)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	out := string(files[0].Bytes)
	if strings.Contains(out, "tools:") {
		t.Errorf("output should omit an empty tools key, got:\n%s", out)
	}
	if strings.Contains(out, "model:") {
		t.Errorf("output should omit an empty model key, got:\n%s", out)
	}
}

// TestClaudeRenderDeterministic checks AC-2: rendering the same
// ResolvedSource twice, including from independently constructed values,
// produces byte-identical output.
func TestClaudeRenderDeterministic(t *testing.T) {
	r, err := render.Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(\"claude\"): unexpected error: %v", err)
	}

	newSource := func() render.ResolvedSource {
		return render.ResolvedSource{
			Frontmatter: render.Frontmatter{
				Name:        "test-engineer",
				Description: "Writes and runs tests",
				Tools:       []string{"Read", "Bash", "Edit"},
				Model:       "opus",
			},
			Body: "Write the tests.\n",
		}
	}

	first, err := r.Render(newSource())
	if err != nil {
		t.Fatalf("Render (first): unexpected error: %v", err)
	}
	second, err := r.Render(newSource())
	if err != nil {
		t.Fatalf("Render (second): unexpected error: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("got %d files first render, %d second", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Errorf("file %d: Path differs: %q vs %q", i, first[i].Path, second[i].Path)
		}
		if string(first[i].Bytes) != string(second[i].Bytes) {
			t.Errorf("file %d: Bytes differ:\n--- first ---\n%s\n--- second ---\n%s", i, first[i].Bytes, second[i].Bytes)
		}
	}
}

// TestClaudeRenderUnsupportedExtraField checks that a frontmatter field this
// renderer does not know how to express (anything landing in
// Frontmatter.Extra) fails loudly, naming the agent, the harness, and the
// field, and that no RenderedFile is returned alongside the error.
func TestClaudeRenderUnsupportedExtraField(t *testing.T) {
	r, err := render.Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(\"claude\"): unexpected error: %v", err)
	}

	source := render.ResolvedSource{
		Frontmatter: render.Frontmatter{
			Name:        "go-architect",
			Description: "Plans Go implementation work",
			Extra:       map[string]any{"color": "blue"},
		},
		Body: "Body text.\n",
	}

	files, err := r.Render(source)
	if err == nil {
		t.Fatal("Render: expected an error for an unsupported extra field, got nil")
	}
	if files != nil {
		t.Errorf("Render: expected no files alongside an error, got %+v", files)
	}

	var unsupported *render.UnsupportedFieldError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Render error is not an *UnsupportedFieldError: %v (%T)", err, err)
	}
	if unsupported.Agent != "go-architect" {
		t.Errorf("UnsupportedFieldError.Agent = %q, want %q", unsupported.Agent, "go-architect")
	}
	if unsupported.Harness != "claude" {
		t.Errorf("UnsupportedFieldError.Harness = %q, want %q", unsupported.Harness, "claude")
	}
	if unsupported.Field != "color" {
		t.Errorf("UnsupportedFieldError.Field = %q, want %q", unsupported.Field, "color")
	}
	if !errors.Is(err, render.ErrUnsupportedField) {
		t.Errorf("Render error does not wrap ErrUnsupportedField: %v", err)
	}
}

// TestClaudeRenderMissingName checks that a ResolvedSource with no agent
// name fails rather than producing a broken ".claude/agents/.md" path.
func TestClaudeRenderMissingName(t *testing.T) {
	r, err := render.Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(\"claude\"): unexpected error: %v", err)
	}

	_, err = r.Render(render.ResolvedSource{
		Frontmatter: render.Frontmatter{Description: "no name given"},
		Body:        "Body text.\n",
	})
	if err == nil {
		t.Fatal("Render: expected an error for a missing agent name, got nil")
	}
	if !errors.Is(err, render.ErrMissingAgentName) {
		t.Fatalf("Render error does not wrap ErrMissingAgentName: %v", err)
	}
}
