package turn

import (
	"strings"
	"testing"
)

// An application result is identified by task, mode and work context. Sharing
// one work context across every run on a task meant the first result was an
// authorship and every later one had to be a revision — so an agent that
// delivered only a terminal result, which the instructions permitted, worked
// exactly once per task and was refused forever after. A graph instance is one
// conversation with one outcome, so it gets its own.
func TestGraphRunsAuthorTheirOwnResult(t *testing.T) {
	t.Parallel()
	const slug = "expose-the-creation-proposer"
	first := researchWorkContext(slug, 7)
	second := researchWorkContext(slug, 8)
	if first == second {
		t.Fatalf("two graph instances share a work context: %q", first)
	}
	for _, got := range []string{first, second} {
		if !strings.HasPrefix(got, slug) {
			t.Errorf("%q no longer names the feature it belongs to", got)
		}
	}
	// A run that is not a graph run keeps the task-wide context it always had.
	if plain := researchWorkContext(slug, 0); plain != slug {
		t.Errorf("a non-graph run must keep its feature slug, got %q", plain)
	}
}
