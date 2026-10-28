package owneraction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type advancingRecheckDomain struct {
	Domain
	advance func()
}

func (d advancingRecheckDomain) Recheck(context.Context, Principal, Facts, time.Time) error {
	d.advance()
	return nil
}

func TestActionAdmissionUsesClockAfterTargetRecheck(t *testing.T) {
	for _, tier := range []string{"conversational", "binding"} {
		t.Run(tier, func(t *testing.T) {
			s, d, p, at := actionServiceFixture(t)
			command := "diagnose"
			if tier == "binding" {
				command = "confirm"
				s.SetDelivery(&actionDeliveryFixture{message: "approval"})
			}
			proposal := proposeDelivered(t, s, p, Invocation{Command: command}, "clock")
			*at = at.Add(5 * time.Second)
			if tier == "binding" {
				if _, err := s.Confirm(t.Context(), p, proposal.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Step(t.Context(), proposal.ID); err != nil {
					t.Fatal(err)
				}
			}
			s.domain = advancingRecheckDomain{Domain: d, advance: func() { *at = proposal.ExpiresAt }}
			var out Proposal
			var err error
			if tier == "binding" {
				out, err = s.Reaction(t.Context(), Reaction{ChannelID: "200", MessageID: "approval", UserID: "100", Emoji: "✅", ReceivedAt: *at})
			} else {
				out, err = s.Confirm(t.Context(), p, proposal.ID)
			}
			if err != nil || out.State != "expired" {
				t.Fatalf("slow target recheck admitted expired action: %+v %v", out, err)
			}
			if _, err = s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("expired admission created journal: %v", err)
			}
		})
	}
}

func TestActionAdmissionRechecksGrantExpiryAfterTargetRecheck(t *testing.T) {
	s, d, discord, at := actionServiceFixture(t)
	base := *at
	// Issue a grant with only six seconds left when this proposal is created.
	*at = base.Add(-24*time.Hour + 6*time.Second)
	p := actionMCPPrincipal(t, s, discord, "near-expiry", "repository:actions")
	*at = base
	proposal, err := s.Propose(t.Context(), p, Invocation{Command: "diagnose"}, "clock")
	if err != nil {
		t.Fatal(err)
	}
	*at = at.Add(5 * time.Second)
	s.domain = advancingRecheckDomain{Domain: d, advance: func() { *at = base.Add(7 * time.Second) }}
	if _, err = s.Confirm(t.Context(), p, proposal.ID); !errors.Is(err, store.ErrConnectorInvalid) {
		t.Fatalf("slow target recheck admitted expired grant: %v", err)
	}
	if _, err = s.db.ReadNaturalExecution(t.Context(), proposal.ExecutionID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired grant created journal: %v", err)
	}
}

func TestActionAvailabilityUsesClockAfterConfirmedDelivery(t *testing.T) {
	s, _, p, at := actionServiceFixture(t)
	proposal, err := s.Propose(t.Context(), p, Invocation{Command: "diagnose"}, "delayed-delivery")
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.AvailableAt.IsZero() {
		t.Fatal("undelivered Discord proposal is available")
	}
	// The sender returns a confirmed identity only after twenty seconds elapse.
	*at = at.Add(20 * time.Second)
	deliveredAt := *at
	if err = s.Available(t.Context(), proposal.ID, "confirmed-delivery"); err != nil {
		t.Fatal(err)
	}
	recorded, err := s.Inspect(t.Context(), p, proposal.ID)
	if err != nil || !recorded.AvailableAt.Equal(deliveredAt) || !recorded.ConfirmNotBefore.Equal(deliveredAt.Add(5*time.Second)) || !recorded.ExpiresAt.Equal(proposal.ExpiresAt) {
		t.Fatalf("delivery clock or original expiry changed: %+v %v", recorded, err)
	}
	*at = at.Add(4 * time.Second)
	out, err := s.Confirm(t.Context(), p, proposal.ID)
	if err != nil || out.State != "awaiting_confirmation" || out.Reason != "too_early" {
		t.Fatalf("delayed delivery skipped minimum review: %+v %v", out, err)
	}
	*at = at.Add(time.Second)
	out, err = s.Confirm(t.Context(), p, proposal.ID)
	if err != nil || out.State != "admitted" || !out.ExpiresAt.Equal(proposal.ExpiresAt) {
		t.Fatalf("full delivery-based review refused: %+v %v", out, err)
	}
}
