package daemon

import (
	"github.com/cloudvane/litefactory/internal/ticket"
	"testing"
)

func TestLegacyTicketObservationRejectsForeignActors(t *testing.T) {
	t.Parallel()
	origin := ticket.Origin("https://api.github.com")
	snapshot := ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: "owner/repo", Key: "7"}}, Assignees: []ticket.Actor{{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "42"}, Handle: "renamed"}}}
	observed, err := legacyTicketSnapshot(snapshot)
	if err != nil || observed.ID != 700 || observed.Number != 7 || !observed.AssignedTo(42) {
		t.Fatalf("compatible observation: %+v %v", observed, err)
	}
	snapshot.Assignees[0].Identity.Origin = "https://other.example"
	if _, err := legacyTicketSnapshot(snapshot); err == nil {
		t.Fatal("colliding actor from another origin admitted")
	}
	snapshot.Assignees = nil
	snapshot.Ref.Identity.Provider = "other"
	snapshot.Ref.Locator.Provider = "other"
	if _, err := legacyTicketSnapshot(snapshot); err == nil {
		t.Fatal("another provider entered GitHub compatibility workflow")
	}
}
