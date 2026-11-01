package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// A Linear story shell is stored with its italic line as *...* (#1298); the
// real shell body must still correlate, and every real difference is named.
func TestCreationDifferencePlanShellOnLinear(t *testing.T) {
	a := PlanStoryAuthority{ParentID: 4503599629137800}
	title, body := planStoryShell(a, 61, 1, "story-001", planbrief.PlanStorySetEntry{Name: "Search box in the Invoices tab", Intent: "A live search box filters the month's invoice rows."}, "MAR-61")
	if !strings.Contains(body, "\n_First draft in progress.") {
		t.Fatalf("fixture lost the shell line: %q", body)
	}
	stored := strings.Replace(strings.Replace(body, "\n_First", "\n*First", 1), "ready._", "ready.*", 1)
	origin := ticket.Origin("https://api.linear.app")
	bot := ticket.Identity{Provider: ticket.Linear, Origin: origin, RemoteID: "bot"}
	uuid := "c445fd81-e40a-4dd2-9d8e-31bd8edefbf4"
	created := func(edit func(*ticket.Snapshot)) ticket.Snapshot {
		s := ticket.Snapshot{Title: title, Body: stored, Representation: "linear-markdown/v1"}
		s.Ref.Identity = ticket.Identity{Provider: ticket.Linear, Origin: origin, RemoteID: uuid}
		s.Author.Identity = bot
		if edit != nil {
			edit(&s)
		}
		return s
	}
	if field := creationDifference(created(nil), uuid, title, body, "linear-markdown/v1", bot); field != "" {
		t.Fatalf("Linear-stored shell did not correlate: %s", field)
	}
	for want, edit := range map[string]func(*ticket.Snapshot){
		"uuid":           func(s *ticket.Snapshot) { s.Ref.Identity.RemoteID = "other" },
		"title":          func(s *ticket.Snapshot) { s.Title = "MAR-61-001: Another story" },
		"body":           func(s *ticket.Snapshot) { s.Body = strings.Replace(s.Body, "invoice rows", "payment rows", 1) },
		"representation": func(s *ticket.Snapshot) { s.Representation = "other/v1" },
		"author":         func(s *ticket.Snapshot) { s.Author.Identity.RemoteID = "someone" },
	} {
		if got := creationDifference(created(edit), uuid, title, body, "linear-markdown/v1", bot); got != want {
			t.Errorf("difference = %q, want %q", got, want)
		}
	}
}
