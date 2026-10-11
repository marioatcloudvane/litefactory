package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestQualifiedClassificationRejectsOriginAndProviderCollisions(t *testing.T) {
	t.Parallel()
	filter := &Filter{identities: Identities{OwnerUserID: 42, BotUserID: 99, TicketOrigin: "https://enterprise.example/api/v3"}}
	for _, tc := range []struct {
		name     string
		provider ticket.Provider
		origin   ticket.Origin
		id       string
		want     store.Classification
	}{
		{"owner", ticket.GitHub, "https://enterprise.example/api/v3", "42", store.ClassificationInstruction},
		{"bot", ticket.GitHub, "https://enterprise.example/api/v3", "99", store.ClassificationSelf},
		{"foreign owner", ticket.GitHub, "https://other.example/api/v3", "42", store.ClassificationThirdParty},
		{"foreign bot", ticket.GitHub, "https://other.example/api/v3", "99", store.ClassificationThirdParty},
		{"other provider", "other", "https://enterprise.example/api/v3", "42", store.ClassificationThirdParty},
		{"deleted", ticket.GitHub, "https://enterprise.example/api/v3", "", store.ClassificationThirdParty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := ticket.Reply{Author: ticket.Actor{Identity: ticket.Identity{Provider: tc.provider, Origin: tc.origin, RemoteID: tc.id}, Handle: "owner"}, Body: "/implement continue"}
			if got := filter.ClassifyReply(reply); got.Classification != tc.want {
				t.Fatalf("classification %+v, want %s", got, tc.want)
			}
		})
	}
	marker := ticket.Reply{Author: ticket.Actor{Identity: filter.actorIdentity(42)}, Body: MarkerPrefix + "report -->"}
	if got := filter.ClassifyReply(marker); got.SelfReason != SelfReasonMarker || got.Triggers() {
		t.Fatalf("marker ordering changed: %+v", got)
	}
}
