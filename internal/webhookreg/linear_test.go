package webhookreg

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestLinearSubscriptionSelectionDoesNotChangeGitHubEvents(t *testing.T) {
	linear := subscriptionEvents(ticket.Linear)
	if !slices.Equal(linear, []string{"Issue", "Comment"}) || !validProviderSubscriptionEvents(ticket.Linear, linear) {
		t.Fatal("Linear resource events missing")
	}
	if validProviderSubscriptionEvents(ticket.Linear, []string{"issues", "issue_comment"}) || validProviderSubscriptionEvents(ticket.Linear, []string{"Issue", "Issue"}) {
		t.Fatal("invalid Linear events accepted")
	}
	github := subscriptionEvents(ticket.GitHub)
	if !slices.Equal(github, Events) || !validProviderSubscriptionEvents(ticket.GitHub, github) {
		t.Fatal("GitHub event compatibility changed")
	}
	github[0] = "altered"
	if Events[0] == "altered" {
		t.Fatal("event configuration aliases shared defaults")
	}
}
