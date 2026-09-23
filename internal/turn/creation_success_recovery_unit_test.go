package turn_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/turn"
)

type sourceOutcomeFake struct {
	id       int64
	proposal store.CreationProposal
	err      error
	sources  []store.CreationSource
}

func (f *sourceOutcomeFake) GetOwnerIntent(_ context.Context, source store.CreationSource) (int64, error) {
	f.sources = append(f.sources, source)
	return f.id, f.err
}
func (f *sourceOutcomeFake) GetPreparation(_ context.Context, source store.CreationSource) (store.CreationProposal, error) {
	f.sources = append(f.sources, source)
	return f.proposal, f.err
}
func recoveryUnitControl() turn.OwnerGrantControl {
	return turn.OwnerGrantControl{Repository: "owner/repo", SourceTask: "github:owner/repo#314", ActorID: 10, CommentID: 71, CreatedAt: time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC), Mode: "research"}
}
func recoveryUnitRuntime(t *testing.T, f *sourceOutcomeFake, enabled bool) *turn.MCPRuntime {
	t.Helper()
	a, err := turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: &store.CreationStore{}, SourceOutcomes: f, Enabled: enabled, OwnerID: 10, BotID: 20})
	if err != nil {
		t.Fatal(err)
	}
	return &turn.MCPRuntime{CreationAuthorizer: a, BotID: 20}
}
func TestCreationSuccessRecovery_OriginalResultBeforeMutableControls(t *testing.T) {
	t.Parallel()
	envelope := submission.CreationEnvelope{Version: 1, Repository: "owner/repo", SourceTask: "github:owner/repo#314", Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Original title", Body: "Original body"}}}
	bytes, hash, err := submission.CanonicalCreation(envelope)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"approval", "preparation"} {
		for _, change := range []string{"edited source", "changed mode", "disabled capability", "all changes"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				t.Parallel()
				f := &sourceOutcomeFake{id: 817, proposal: store.CreationProposal{Hash: hash, Envelope: bytes}}
				c := recoveryUnitControl()
				c.Body = "/mcp-create malformed"
				enabled := true
				switch change {
				case "edited source":
					c.Body = "prefix removed"
					c.UpdatedAt = c.CreatedAt.Add(time.Hour)
				case "changed mode":
					c.Mode = "implement"
				case "disabled capability":
					enabled = false
				case "all changes":
					c.Body = "prefix removed"
					c.UpdatedAt = c.CreatedAt.Add(time.Hour)
					c.Mode = "validate"
					enabled = false
				}
				r := recoveryUnitRuntime(t, f, enabled)
				if kind == "approval" {
					id, err := r.AdmitCreationApproval(t.Context(), c)
					if err != nil || id != 817 {
						t.Fatalf("original intent=%d err=%v", id, err)
					}
					// Successful historical observation is not launch authority.
					if _, err := r.CreationAuthorizer.BindCreationGrant(t.Context(), nil, id); !errors.Is(err, store.ErrCreationAuthority) {
						t.Fatalf("recovery acquired grant: %v", err)
					}
				} else {
					got, err := r.PrepareCreationProposal(t.Context(), c)
					if err != nil || got.Hash != hash || !reflect.DeepEqual(got.Envelope, envelope) {
						t.Fatalf("original proposal=%+v err=%v", got, err)
					}
				}
				want := []store.CreationSource{{Repository: c.Repository, SourceTask: c.SourceTask, CommentID: c.CommentID, OwnerID: c.ActorID}}
				if !reflect.DeepEqual(f.sources, want) {
					t.Fatalf("trusted lookup=%+v want=%+v", f.sources, want)
				}
			})
		}
	}
}
func TestCreationSuccessRecovery_ObservationFailureRemainsPending(t *testing.T) {
	t.Parallel()
	failed := errors.New("observation unavailable")
	for _, kind := range []string{"approval", "preparation"} {
		for _, cause := range []error{failed, fmt.Errorf("%w: %w", store.ErrCreationAuthority, failed), context.Canceled} {
			t.Run(kind+"/"+cause.Error(), func(t *testing.T) {
				t.Parallel()
				f := &sourceOutcomeFake{err: cause}
				r := recoveryUnitRuntime(t, f, false)
				c := recoveryUnitControl()
				c.Mode = "validate"
				c.Body = "edited text"
				c.UpdatedAt = c.CreatedAt.Add(time.Hour)
				var err error
				if kind == "approval" {
					var id int64
					id, err = r.AdmitCreationApproval(t.Context(), c)
					if id != 0 {
						t.Fatalf("intent=%d", id)
					}
				} else {
					var p turn.PreparedCreation
					p, err = r.PrepareCreationProposal(t.Context(), c)
					if p.Hash != "" {
						t.Fatalf("proposal=%+v", p)
					}
				}
				if !errors.Is(err, cause) {
					t.Fatalf("lost observation error: %v", err)
				}
				if reason, rejected := turn.CreationRejectionReason(err); rejected {
					t.Fatalf("false rejection %q", reason)
				}
			})
		}
	}
}
func TestCreationSuccessRecovery_IdentityBeforeRetainedSuccess(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"approval", "preparation"} {
		for _, actor := range []string{"nonowner", "bot identity", "bot flag"} {
			t.Run(kind+"/"+actor, func(t *testing.T) {
				t.Parallel()
				f := &sourceOutcomeFake{id: 817}
				r := recoveryUnitRuntime(t, f, true)
				c := recoveryUnitControl()
				switch actor {
				case "nonowner":
					c.ActorID = 11
				case "bot identity":
					c.ActorID = 20
				case "bot flag":
					c.Bot = true
				}
				var err error
				if kind == "approval" {
					var id int64
					id, err = r.AdmitCreationApproval(t.Context(), c)
					if id != 0 {
						t.Fatalf("untrusted recovered id=%d", id)
					}
				} else {
					var p turn.PreparedCreation
					p, err = r.PrepareCreationProposal(t.Context(), c)
					if p.Hash != "" {
						t.Fatalf("untrusted recovered proposal=%+v", p)
					}
				}
				reason, rejected := turn.CreationRejectionReason(err)
				if !rejected || reason != "controls" || len(f.sources) != 0 {
					t.Fatalf("identity refusal=%q %v err=%v observed=%+v", reason, rejected, err, f.sources)
				}
			})
		}
	}
}
