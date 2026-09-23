package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// OwnerGrantCommand is an exact public content fingerprint, not a secret.
type OwnerGrantCommand struct{ ProposalHash, Relationship string }

// ParseOwnerGrantCommand accepts only the whole exact public approval syntax.
func ParseOwnerGrantCommand(body string) (OwnerGrantCommand, error) {
	var command OwnerGrantCommand
	parts := strings.Split(body, " ")
	if len(body) > 128 || len(parts) != 3 || parts[0] != "/mcp-create" || len(parts[1]) != 64 || strings.Trim(parts[1], "0123456789abcdef") != "" || (parts[2] != "linked" && parts[2] != "attach") {
		return command, rejectCreation("syntax", errors.New("use exactly /mcp-create <proposal SHA256> linked|attach"))
	}
	return OwnerGrantCommand{ProposalHash: parts[1], Relationship: parts[2]}, nil
}

// OwnerGrantControl is metadata supplied by the daemon's authenticated comment
// ingestion, never decoded from MCP arguments or inferred from comment prose.
type OwnerGrantControl struct {
	Repository, SourceTask, Mode, Body string
	CommentID, ActorID                 int64
	CreatedAt, UpdatedAt               time.Time
	Bot                                bool
}

// CreationAuthorizerOptions fixes the loaded gate and trusted numeric control identities.
type CreationAuthorizerOptions struct {
	Store          *store.CreationStore
	SourceOutcomes CreationSourceOutcomes
	Enabled        bool
	OwnerID, BotID int64
}

// CreationSourceOutcomes observes admitted history without authorizing a new
// mutation. Keeping observation separate makes unavailable storage retryable.
type CreationSourceOutcomes interface {
	GetOwnerIntent(context.Context, store.CreationSource) (int64, error)
	GetPreparation(context.Context, store.CreationSource) (store.CreationProposal, error)
}

// CreationAuthorizer is factory-owned. Configuration/owner identities are copied
// at construction; no method changes them and no model-facing mint API exists.
type CreationAuthorizer struct {
	store          *store.CreationStore
	sourceOutcomes CreationSourceOutcomes
	enabled        bool
	ownerID, botID int64
}

// NewCreationAuthorizer constructs immutable factory-owned creation policy.
func NewCreationAuthorizer(o CreationAuthorizerOptions) (*CreationAuthorizer, error) {
	if o.Store == nil || o.OwnerID < 1 || o.BotID < 1 || o.OwnerID == o.BotID {
		return nil, store.ErrCreationAuthority
	}
	return &CreationAuthorizer{store: o.Store, sourceOutcomes: o.SourceOutcomes, enabled: o.Enabled, ownerID: o.OwnerID, botID: o.BotID}, nil
}

func (a *CreationAuthorizer) outcomes() CreationSourceOutcomes {
	if a.sourceOutcomes != nil {
		return a.sourceOutcomes
	}
	return a.store
}
func creationProposal(e submission.CreationEnvelope) (store.CreationProposal, error) {
	data, hash, err := submission.CanonicalCreation(e)
	if err != nil {
		return store.CreationProposal{}, err
	}
	return store.CreationProposal{Hash: hash, Repository: e.Repository, SourceTask: e.SourceTask, Relationship: e.Relationship, Artifacts: len(e.Artifacts), Envelope: data}, nil
}

// RetainProposal is called by the factory proposal presenter before owner
// approval; it grants nothing and publishes nothing. Preflight never calls it.
func (a *CreationAuthorizer) RetainProposal(ctx context.Context, e submission.CreationEnvelope) (string, error) {
	p, err := creationProposal(e)
	if err != nil {
		return "", err
	}
	if err := a.store.RetainProposal(ctx, p); err != nil {
		return "", err
	}
	return p.Hash, nil
}

// RetainProposalForRun retains the proposal and records it as the run's single
// proposal still owed to the owner, atomically. It grants nothing and publishes
// nothing.
func (a *CreationAuthorizer) RetainProposalForRun(ctx context.Context, runID int64, e submission.CreationEnvelope) (string, error) {
	p, err := creationProposal(e)
	if err != nil {
		return "", err
	}
	if err := a.store.RetainProposalForRun(ctx, p, store.CreationPresentation{RunID: runID}); err != nil {
		return "", err
	}
	return p.Hash, nil
}

// AdmitOwnerGrantIntent verifies control provenance before recording exact owner intent.
func (a *CreationAuthorizer) AdmitOwnerGrantIntent(ctx context.Context, c OwnerGrantControl) (int64, error) {
	if a == nil || a.store == nil {
		return 0, fmt.Errorf("creation owner intake unavailable: %w", store.ErrCreationAuthority)
	}
	if c.Bot || c.ActorID == a.botID || c.ActorID != a.ownerID || c.CommentID < 1 || c.CreatedAt.IsZero() {
		return 0, rejectCreation("controls", store.ErrCreationAuthority)
	}
	// A recorded admission is history, not a new request for authority. Observe
	// it before current configuration, syntax, proposal freshness or edits.
	if id, err := a.outcomes().GetOwnerIntent(ctx, creationSource(c)); err == nil {
		return id, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	if !a.enabled {
		return 0, rejectCreation("disabled", store.ErrCreationAuthority)
	}
	if c.Mode != "research" {
		return 0, rejectCreation("mode", store.ErrCreationAuthority)
	}

	command, err := ParseOwnerGrantCommand(c.Body)
	if err != nil {
		return 0, err
	}
	return a.store.AdmitOwnerIntent(ctx, store.CreationApproval{Repository: c.Repository, SourceTask: c.SourceTask, ProposalHash: command.ProposalHash, CommentID: c.CommentID, OwnerID: a.ownerID, ActorID: c.ActorID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Bot: c.Bot}, command.Relationship)
}

// CreationVisible checks the loaded gate and Research profile for creation.
// It does not imply a grant; the exact owner intent is checked independently.
func (a *CreationAuthorizer) CreationVisible(ctx context.Context, guard *AttemptGuard) bool {
	return a.enabled && guard != nil && guard.Binding().Mode == "research" && guard.Check(ctx) == nil
}

// BindCreationGrant consumes an eligible intent into one live Research launch.
func (a *CreationAuthorizer) BindCreationGrant(ctx context.Context, guard *AttemptGuard, intent int64) (int64, error) {
	if !a.CreationVisible(ctx, guard) {
		return 0, store.ErrCreationAuthority
	}
	live, release, err := guard.Context(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	return a.store.BindGrant(live, guard.Lease(), intent)
}

// ReserveCreation recomputes canonical content and atomically reserves authorized volume.
func (a *CreationAuthorizer) ReserveCreation(ctx context.Context, guard *AttemptGuard, e submission.CreationEnvelope) (store.CreationReservation, error) {
	if !a.CreationVisible(ctx, guard) {
		return store.CreationReservation{}, store.ErrCreationAuthority
	}
	p, err := creationProposal(e)
	if err != nil {
		return store.CreationReservation{}, err
	}
	live, release, err := guard.Context(ctx)
	if err != nil {
		return store.CreationReservation{}, err
	}
	defer release()
	return a.store.ReserveCreation(live, guard.Lease(), p)
}

// BeginDispatch rechecks the loaded gate and consumes the original internal permit.
func (a *CreationAuthorizer) BeginDispatch(ctx context.Context, permit *store.CreationPermit) (store.CreationOperation, error) {
	if !a.enabled {
		return store.CreationOperation{}, store.ErrCreationAuthority
	}
	return a.store.BeginDispatch(ctx, permit)
}

// RecordOperationOutcome retains a known effect after caller authority may have ended.
func (a *CreationAuthorizer) RecordOperationOutcome(ctx context.Context, permit *store.CreationPermit, outcome string) (store.CreationOperation, error) {
	return a.store.RecordOperationOutcome(ctx, permit, outcome)
}

// ReconcileOperation records factory-observed history without issuing dispatch authority.
func (a *CreationAuthorizer) ReconcileOperation(ctx context.Context, id int64, outcome string) (store.CreationOperation, error) {
	return a.store.ReconcileOperation(ctx, id, outcome)
}

func creationSource(c OwnerGrantControl) store.CreationSource {
	return store.CreationSource{Repository: c.Repository, SourceTask: c.SourceTask, CommentID: c.CommentID, OwnerID: c.ActorID}
}
