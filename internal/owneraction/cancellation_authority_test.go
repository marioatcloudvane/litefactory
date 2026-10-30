package owneraction

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestActionDiscordOwnerCancelsRevokedMCPProposal(t *testing.T) {
	s, _, discord, at := actionServiceFixture(t)
	origin := actionMCPPrincipal(t, s, discord, "origin", "repository:actions")
	replacement := actionMCPPrincipal(t, s, discord, "replacement", "repository:actions")
	proposal, err := s.Propose(t.Context(), origin, Invocation{Command: "confirm"}, "binding")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ApprovalChannel != discord.ChannelID {
		t.Fatal("proposal is not visible in configured owner channel")
	}
	if err = s.db.RevokeConnectorGrant(t.Context(), 42, origin.GrantID, *at); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Confirm(t.Context(), replacement, proposal.ID); !errors.Is(err, store.ErrConnectorInvalid) {
		t.Fatalf("replacement grant adopted confirmation: %v", err)
	}
	if _, err = s.Cancel(t.Context(), origin, proposal.ID); !errors.Is(err, store.ErrConnectorInvalid) {
		t.Fatalf("revoked MCP grant retained cancellation authority: %v", err)
	}
	if _, err = s.Cancel(t.Context(), replacement, proposal.ID); !errors.Is(err, store.ErrConnectorInvalid) {
		t.Fatalf("replacement grant adopted MCP cancellation: %v", err)
	}
	visible, err := s.Inspect(t.Context(), discord, proposal.ID)
	if err != nil || visible.State != "awaiting_confirmation" {
		t.Fatalf("configured owner cannot inspect pending work: %+v %v", visible, err)
	}
	cancelled, err := s.Cancel(t.Context(), discord, proposal.ID)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatalf("configured owner cannot cancel revoked-origin work: %+v %v", cancelled, err)
	}
	if cancelled.Principal.GrantID != origin.GrantID {
		t.Fatal("cancellation rewrote originating provenance")
	}
	if _, err = s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cancellation created execution: %v", err)
	}
	recorded, err := s.Inspect(t.Context(), discord, proposal.ID)
	if err != nil || len(recorded.Attempts) != 4 || recorded.Attempts[3].Method != "cancel_discord" {
		t.Fatalf("cancellation decision missing: %+v %v", recorded.Attempts, err)
	}
}
