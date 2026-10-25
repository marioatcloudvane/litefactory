package turn

import (
	"html"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestImplementDependencyDecisionGuidance(t *testing.T) {
	f := newImplementFixture(t, implementStories(5, map[string][]string{"story-004": {"story-001", "story-002", "story-003"}, "story-005": {"story-004"}})...)
	rows := []store.ImplementStory{
		{Key: "story-001", State: store.ImplementParked, Reason: strings.Repeat("long reason ", 500)},
		{Key: "story-002", State: store.ImplementRunning},
		{Key: "story-003", State: store.ImplementDone},
		{Key: "story-004", State: store.ImplementWaiting},
	}
	destination := "https://example.com/confirmed-decision"
	body, counts := renderImplementHandoffNotes(f.auth, rows, nil, nil, map[string]implementDisplay{"story-001": {ReplyURL: destination}})
	if counts != (implementHandoffCounts{parked: 1, waiting: 4}) {
		t.Fatal(counts)
	}
	var parked, dependent string
	for _, line := range strings.Split(html.UnescapeString(body), "\n") {
		if strings.HasPrefix(line, "| story-001 ") {
			parked = line
		}
		if strings.HasPrefix(line, "| story-004 ") {
			dependent = line
		}
	}
	for _, want := range []string{destination, "reply on the parent issue naming story-001", "does not approve or merge", "Must merge before story-004"} {
		if !strings.Contains(parked, want) {
			t.Errorf("parked guidance missing %q: %s", want, parked)
		}
	}
	for _, want := range []string{destination, "first resolve", "story-002 is running", "story-003 is queued for automatic merge", "After every prerequisite merges", "reply alone does not start it"} {
		if !strings.Contains(dependent, want) {
			t.Errorf("dependent guidance missing %q: %s", want, dependent)
		}
	}
	body, _ = renderImplementHandoffNotes(f.auth, rows, nil, nil)
	if !strings.Contains(body, "confirmed report destination unavailable") || strings.Contains(body, destination) {
		t.Fatal(body)
	}
}
