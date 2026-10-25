package messaging

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestInboxOrganizedRequestDoesNotRetargetRevokedPosition(t *testing.T) {
	ticket := inboxOrganizerTicket{Decisions: []store.InboxDecision{{Key: "surviving", Ordinal: 7}}}
	row := store.InboxOrganizedTicket{DecisionKeys: []string{"", "surviving"}}
	for _, position := range []int{-1, 0, 2} {
		if _, ok := inboxOrganizedDecision(ticket, row, position); ok {
			t.Fatalf("unavailable position %d retargeted", position)
		}
	}
	decision, ok := inboxOrganizedDecision(ticket, row, 1)
	if !ok || decision.Key != "surviving" || decision.Ordinal != 7 {
		t.Fatalf("surviving reference changed: %+v %v", decision, ok)
	}
}
