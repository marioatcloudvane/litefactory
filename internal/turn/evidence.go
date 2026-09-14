package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// CheckpointBinding returns copied factory scope for composing a matching MCP
// attempt. It cannot bind, replace or reactivate authority.
func (s *SubmissionSession) CheckpointBinding() store.SubmissionAttempt {
	b := s.binding
	b.Stories = slices.Clone(b.Stories)
	return b
}

func (s *SubmissionSession) checkCheckpointAuthority(ctx context.Context) error {
	if s.binding.Stage != "engineering" && s.binding.Stage != "legacy" && s.binding.Stage != "validate" {
		return errors.New("checkpoint requires an engineering or assessment session")
	}
	current, err := s.options.DB.GetSubmissionRecovery(ctx, s.binding.RunID, s.binding.Attempt)
	if err != nil {
		return err
	}
	if !current.Active {
		return store.ErrResultAuthority
	}
	return nil
}

// PreflightCheckpoint uses the helper's strict contribution parser and current
// snapshot checks. It records no submission, capture, decision or effect.
func (s *SubmissionSession) PreflightCheckpoint(ctx context.Context, payload []byte) (agent.ContributionDiagnostics, error) {
	if err := s.checkCheckpointAuthority(ctx); err != nil {
		return agent.ContributionDiagnostics{}, err
	}
	raw, err := s.checkpoint(ctx, submission.Request{Method: "preflight", Payload: payload})
	if err != nil {
		return agent.ContributionDiagnostics{}, err
	}
	var result agent.ContributionDiagnostics
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	return result, s.checkCheckpointAuthority(ctx)
}

// SubmitCheckpoint retains the existing helper response: a durable submission
// receipt, or correction diagnostics when no authorized story can be selected.
// Validation is repeated here even after a successful preflight. Replayed stable
// request identities resolve the original receipt and cannot capture twice.
func (s *SubmissionSession) SubmitCheckpoint(ctx context.Context, requestID string, payload []byte) (json.RawMessage, error) {
	if err := s.checkCheckpointAuthority(ctx); err != nil {
		return nil, err
	}
	if requestID == "" || len(requestID) > 128 {
		return nil, errors.New("invalid checkpoint request identity")
	}
	return s.checkpoint(ctx, submission.Request{Method: "submit", RequestID: requestID, Payload: payload})
}

// CheckpointReceipt observes retained state without recovery or effect dispatch.
// Historical readers use ReceiptService's separately granted projection instead.
func (s *SubmissionSession) CheckpointReceipt(ctx context.Context, requestID string) (store.SubmissionReceipt, error) {
	if err := s.checkCheckpointAuthority(ctx); err != nil {
		return store.SubmissionReceipt{}, err
	}
	r, err := s.options.DB.GetSubmissionReceipt(ctx, s.binding.RunID, requestID)
	if err != nil {
		return r, err
	}
	if r.Attempt != s.binding.Attempt {
		return store.SubmissionReceipt{}, store.ErrResultAuthority
	}
	return r, nil
}

// EvidenceService is a stage-bound facade over the checkpoint owner. It neither
// executes tests nor selects the next mode. The dispatcher supplies its live
// AttemptGuard context and reserves the family request before HandleSubmit.
type EvidenceService struct {
	session  *SubmissionSession
	receipts *ReceiptService
}

// NewEvidenceService requires matching checkpoint and receipt scope.
func NewEvidenceService(session *SubmissionSession, receipts *ReceiptService) (*EvidenceService, error) {
	if session == nil || receipts == nil || session.options.DB != receipts.db || session.options.Progress == nil {
		return nil, errors.New("evidence requires shared checkpoint and receipt owners")
	}
	b := receipts.Binding()
	stage := session.binding.Stage
	compatible := b.Mode == "implement" && (stage == "engineering" || stage == "legacy") || b.Mode == "validate" && stage == "validate"
	if !compatible || session.binding.RunID != b.RunID || session.binding.Attempt != b.Attempt || session.binding.Parent != b.Task || session.scope.WorkflowVersion != b.WorkflowVersion || b.SubmissionVersion != 2 {
		return nil, errors.New("evidence requires matching immutable checkpoint scope")
	}
	return &EvidenceService{session: session, receipts: receipts}, nil
}

// Binding returns the immutable attempt scope used by the family dispatcher.
func (s *EvidenceService) Binding() store.ResultBinding { return s.receipts.Binding() }

// PermissionMaterial includes the complete frozen checkpoint selection so that
// the catalog owner can bind it into the immutable lease permission digest.
func (s *EvidenceService) PermissionMaterial() ([]byte, error) {
	return json.Marshal(s.session.CheckpointBinding())
}

// Preflight delegates checkpoint validation without capturing or recording evidence.
func (s *EvidenceService) Preflight(ctx context.Context, payload []byte) (agent.ContributionDiagnostics, error) {
	if err := s.receipts.db.CheckResultWriter(ctx, s.Binding()); err != nil {
		return agent.ContributionDiagnostics{}, err
	}
	return s.session.PreflightCheckpoint(ctx, payload)
}

// HandleSubmit finishes the ledger link using the same underlying request ID.
// Projection can recover a crash between checkpoint recording and this link.
// The public response excludes captured files and retained contribution bytes.
func (s *EvidenceService) HandleSubmit(ctx context.Context, requestID string, payload []byte) (json.RawMessage, error) {
	if err := s.receipts.db.CheckResultWriter(ctx, s.Binding()); err != nil {
		return nil, err
	}
	raw, err := s.session.SubmitCheckpoint(ctx, requestID, payload)
	if err != nil {
		return nil, err
	}
	var receipt store.SubmissionReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, fmt.Errorf("decode checkpoint outcome: %w", err)
	}
	disposition := "rejected"
	var ref store.FamilyReference
	if receipt.ID != 0 {
		disposition = "confirmed"
		if receipt.InputState == "rejected" {
			disposition = "rejected"
		}
		if receipt.EngineeringState == "pending" || receipt.AcceptanceState == "pending" {
			disposition = "uncertain"
		}
		ref = store.FamilyReference{Kind: "submission", ID: receipt.ID, RequestID: requestID}
	}
	if err := s.receipts.Finish(ctx, requestID, disposition, ref); err != nil {
		return nil, err
	}
	result, err := s.receipts.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// EvidenceReceiptProjection also handles an interrupted request before the
// underlying checkpoint receipt was allocated. That absence is still the
// ledger's pending/rejected disposition, not a transport failure.
func EvidenceReceiptProjection(db *store.DB) FamilyProjection {
	project := SubmissionReceiptProjection(db)
	return func(ctx context.Context, r store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error) {
		states, identities, err := project(ctx, r)
		if errors.Is(err, store.ErrNotFound) && r.Reference.Kind == "" {
			if r.Disposition == "rejected" {
				return ReceiptDimensions{Input: "rejected"}, nil, nil
			}
			return ReceiptDimensions{}, nil, nil
		}
		return states, identities, err
	}
}
