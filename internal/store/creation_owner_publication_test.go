package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOwnerPublicationAdmissionWindow(t *testing.T) {
	for _, tc := range []struct {
		name          string
		elapsed       time.Duration
		state, reason string
	}{
		{"strictly before", MCPCreationApprovalTTL - time.Nanosecond, "dispatched", ""},
		{"at boundary", MCPCreationApprovalTTL, "refused", OwnerPublicationExpired},
		{"after boundary", MCPCreationApprovalTTL + time.Nanosecond, "refused", OwnerPublicationExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCreationDBFixture(t)
			_, _, id := f.approved(t, "api", 278, 1)
			f.now = f.now.Add(tc.elapsed)
			got, _, err := f.c.ClaimOwnerPublication(t.Context(), id, true)
			if err != nil || got.State != tc.state || got.Reason != tc.reason {
				t.Fatalf("claim = %+v, %v", got, err)
			}
		})
	}
}

func TestOwnerPublicationIndependentOfRepositoryVolume(t *testing.T) {
	f := newCreationDBFixture(t)
	for issue := 100; issue < 105; issue++ {
		_, _, id := f.approved(t, "api", issue, 5)
		got, _, err := f.c.ClaimOwnerPublication(t.Context(), id, true)
		if err != nil || got.State != "dispatched" {
			t.Fatalf("claim %d = %+v, %v", issue, got, err)
		}
		if err = f.c.FinishOwnerPublication(t.Context(), id, "confirmed"); err != nil {
			t.Fatal(err)
		}
	}
	_, _, id := f.approved(t, "api", 278, 5)
	got, _, err := f.c.ClaimOwnerPublication(t.Context(), id, true)
	if err != nil || got.State != "dispatched" {
		t.Fatalf("claim after 25 published issues = %+v, %v", got, err)
	}
}

func TestOwnerPublicationContinuationCurrencyAndHistoricalReplay(t *testing.T) {
	for _, terminal := range []string{"dispatched", "confirmed", "uncertain", "quota"} {
		t.Run(terminal, func(t *testing.T) {
			f := newCreationDBFixture(t)
			_, p, id := f.approved(t, "api", 278, 2)
			original, _, err := f.c.ClaimOwnerPublication(t.Context(), id, true)
			if err != nil {
				t.Fatal(err)
			}
			if err = f.c.CheckOwnerPublicationCurrent(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			f.now = f.now.Add(MCPCreationApprovalTTL + time.Hour)
			if err = f.c.CheckOwnerPublicationCurrent(t.Context(), id); err != nil {
				t.Fatalf("expiry revoked accepted authority: %v", err)
			}
			if terminal == "quota" {
				if _, err = f.db.sql.ExecContext(t.Context(), `UPDATE creation_owner_publications SET state='refused',reason='quota' WHERE intent_id=?`, id); err != nil {
					t.Fatal(err)
				}
				original.State, original.Reason = "refused", OwnerPublicationQuota
			} else if terminal != "dispatched" {
				if err = f.c.FinishOwnerPublication(t.Context(), id, terminal, "retained cause"); err != nil {
					t.Fatal(err)
				}
				original.State, original.Reason = terminal, "retained cause"
			}
			replacement := p
			replacement.Envelope = append(append([]byte(nil), p.Envelope...), ' ')
			replacement.Hash = submissionDigest(replacement.Envelope)
			if err = f.c.RetainProposal(t.Context(), replacement); err != nil {
				t.Fatal(err)
			}
			for _, restore := range []bool{false, true} {
				if restore {
					if err = f.c.RetainProposal(t.Context(), p); err != nil {
						t.Fatal(err)
					}
				}
				if err = f.c.CheckOwnerPublicationCurrent(t.Context(), id); !errors.Is(err, ErrCreationAuthority) {
					t.Fatalf("superseded authority (restored=%v): %v", restore, err)
				}
				replay, proposal, err := f.c.ClaimOwnerPublication(t.Context(), id, false)
				if err != nil || replay != original || proposal.Hash != p.Hash {
					t.Fatalf("historical replay changed = %+v, %v", replay, err)
				}
			}
			if terminal != "dispatched" {
				if err = f.c.FinishOwnerPublication(t.Context(), id, "confirmed", "replacement cause"); err != nil {
					t.Fatal(err)
				}
				replay, _, err := f.c.ClaimOwnerPublication(t.Context(), id, true)
				if err != nil || replay != original {
					t.Fatalf("terminal outcome overwritten = %+v, %v", replay, err)
				}
			}
		})
	}
}

func TestOwnerPublicationAdmissionPreservesEligibility(t *testing.T) {
	for _, tc := range []string{"disabled", "superseded", "restored", "legacy grant"} {
		t.Run(tc, func(t *testing.T) {
			f := newCreationDBFixture(t)
			lease, p, id := f.approved(t, "api", 278, 1)
			enabled := true
			reason := OwnerPublicationStale
			switch tc {
			case "disabled":
				enabled = false
				reason = OwnerPublicationDisabled
			case "legacy grant":
				reason = OwnerPublicationBound
				if _, err := f.db.sql.ExecContext(t.Context(), `INSERT INTO mcp_creation_grants(intent_id,run_id,attempt,instance,expires_ns) VALUES(?,?,?,?,?)`, id, lease.Binding.RunID, lease.Binding.Attempt, lease.Instance, lease.ExpiresAt.UnixNano()); err != nil {
					t.Fatal(err)
				}
			default:
				replacement := p
				replacement.Envelope = append(append([]byte(nil), p.Envelope...), ' ')
				replacement.Hash = submissionDigest(replacement.Envelope)
				if err := f.c.RetainProposal(t.Context(), replacement); err != nil {
					t.Fatal(err)
				}
				if tc == "restored" {
					if err := f.c.RetainProposal(t.Context(), p); err != nil {
						t.Fatal(err)
					}
				}
			}
			got, _, err := f.c.ClaimOwnerPublication(t.Context(), id, enabled)
			if err != nil || got.State != "refused" || got.Reason != reason {
				t.Fatalf("claim = %+v, %v", got, err)
			}
		})
	}
}

func TestLookupOwnerPublicationDoesNotAdmitCancelledApproval(t *testing.T) {
	f := newCreationDBFixture(t)
	_, p, id := f.approved(t, "api", 278, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := f.c.LookupOwnerPublication(context.WithoutCancel(ctx), id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unclaimed lookup = %v", err)
	}
	// Reading an approval did not admit publication: expiry must still refuse it.
	f.now = f.now.Add(MCPCreationApprovalTTL)
	refused, _, err := f.c.ClaimOwnerPublication(t.Context(), id, true)
	if err != nil || refused.Reason != OwnerPublicationExpired {
		t.Fatalf("lookup admitted publication: %+v, %v", refused, err)
	}
	saved, proposal, err := f.c.LookupOwnerPublication(context.WithoutCancel(ctx), id)
	if err != nil || saved != refused || proposal.Hash != p.Hash {
		t.Fatalf("saved lookup = %+v, %v", saved, err)
	}
}

func TestLookupOwnerPublicationReturnsAcceptedAfterCancellation(t *testing.T) {
	f := newCreationDBFixture(t)
	_, p, id := f.approved(t, "api", 278, 1)
	accepted, _, err := f.c.ClaimOwnerPublication(t.Context(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	saved, proposal, err := f.c.LookupOwnerPublication(context.WithoutCancel(ctx), id)
	if err != nil || saved != accepted || proposal.Hash != p.Hash {
		t.Fatalf("saved lookup = %+v, %v", saved, err)
	}
}
