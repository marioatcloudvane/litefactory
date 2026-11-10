package connector

import (
	"strings"
	"testing"
)

// When the factory could not start brainstorm, the guide says so with the
// reason instead of leaving the client to infer a permissions problem.
func TestGuideSaysWhyBrainstormIsUnavailable(t *testing.T) {
	t.Parallel()
	s := &Server{guidance: guidanceFiles}
	s.SetBrainstormUnavailable(`unsupported judge provider "x"`)
	want := `Brainstorm is not available on this factory: unsupported judge provider "x"`
	for _, topic := range []string{"", "overview", "workflows"} {
		text, err := s.factoryGuide([]string{guideTool}, nil, topic)
		if err != nil || !strings.Contains(text, want) {
			t.Fatalf("topic %q: %v\n%s", topic, err, text)
		}
	}
	// A ready brainstorm service, or no recorded failure, adds no notice.
	text, err := (&Server{guidance: guidanceFiles}).factoryGuide([]string{guideTool}, nil, "")
	if err != nil || strings.Contains(text, "Brainstorm is not available") {
		t.Fatalf("no failure recorded: %v\n%s", err, text)
	}
	ready := &Server{guidance: guidanceFiles}
	ready.SetBrainstormUnavailable("old failure")
	ready.SetBrainstorm(refusalBrainstorms{})
	if text, _ := ready.factoryGuide([]string{guideTool}, nil, ""); strings.Contains(text, "Brainstorm is not available") {
		t.Fatalf("ready service must not be reported unavailable:\n%s", text)
	}
}
