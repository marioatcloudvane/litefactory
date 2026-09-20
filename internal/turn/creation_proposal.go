package turn

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// creationProposalReferenceKind names the receipt reference kind of a retained
// agent proposal; its ID is the run whose presentation row owes the owner it.
const creationProposalReferenceKind = "creation_proposal"

// creationProposalCap is the largest canonical envelope the owner presentation
// can show. A proposal above it could never be shown, so it is refused at call
// time rather than truncated.
const creationProposalCap = 48000

// CreationProposalDraft is one issue the agent proposes. It has no repository,
// source or version: those belong to the factory.
type CreationProposalDraft struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	ParentIssue int    `json:"parent_issue,omitempty"`
	LinkIssues  []int  `json:"link_issues,omitempty"`
}

// CreationProposalRequest is the exact argument shape of issues_create_prepare.
// Strict decoding refuses any field it does not name, including the factory-owned
// repository, source_task and version.
type CreationProposalRequest struct {
	Relationship string                  `json:"relationship,omitempty"`
	Drafts       []CreationProposalDraft `json:"drafts"`
}

// CreationProposalResult is the grant-free reply of a retained proposal. It
// carries no approval command; the owner is told by the factory, not the agent.
type CreationProposalResult struct {
	RequestID    string `json:"request_id"`
	Disposition  string `json:"disposition"`
	Hash         string `json:"hash"`
	Relationship string `json:"relationship"`
	Drafts       int    `json:"drafts"`
	Message      string `json:"message"`
}

// CreationProposalService is the issues_create_prepare family owner. It retains
// a proposal and publishes nothing, so it holds no grant, guard or forge.
type CreationProposalService struct {
	receipts    *ReceiptService
	authorizer  *CreationAuthorizer
	preparer    *CreationPublicationService
	sourceIssue int
}

// NewCreationProposalService binds the drafting tool to one attempt's receipts
// and the loaded creation policy.
func NewCreationProposalService(db *store.DB, r *ReceiptService, a *CreationAuthorizer, redactor *redact.Redactor) (*CreationProposalService, error) {
	if db == nil || r == nil || r.db != db || a == nil || redactor == nil {
		return nil, errors.New("creation proposals require immutable factory dependencies")
	}
	ref, err := naming.ParseRef(r.binding.Task)
	if err != nil {
		return nil, fmt.Errorf("creation proposals: %w", err)
	}
	preparer := &CreationPublicationService{db: db, receipts: r, authorizer: a, redactor: redactor}
	return &CreationProposalService{receipts: r, authorizer: a, preparer: preparer, sourceIssue: ref.Issue}, nil
}

// Binding returns the immutable attempt scope used by the family dispatcher.
func (s *CreationProposalService) Binding() store.ResultBinding { return s.receipts.Binding() }

// PermissionMaterial binds the story-creation gate, bot identity and source
// issue into the catalog digest. It carries no proposal or intent state.
func (s *CreationProposalService) PermissionMaterial() ([]byte, error) {
	return json.Marshal(struct {
		Enabled     bool
		BotID       int64
		SourceIssue int
	}{s.authorizer.enabled, s.authorizer.botID, s.sourceIssue})
}

// Registration supplies the drafting handler. It has no replay check: a replay
// observes the retained receipt and never runs the handler again.
func (s *CreationProposalService) Registration() FamilyRegistration {
	return FamilyRegistration{Name: "issues_create_prepare", Owner: s, Handle: s.HandlePrepare}
}

// creationProposalNonce makes an identical call on one attempt reproduce the same
// marker, envelope and hash. It hashes the bytes the agent sent, not a re-encoding.
func creationProposalNonce(b store.ResultBinding, arguments []byte) [16]byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s:%s", b.Task, b.RunID, b.Attempt, arguments)))
	var nonce [16]byte
	copy(nonce[:], sum[:16])
	return nonce
}

// envelope turns validated-by-shape drafts into the unprepared envelope, filling
// the relationship default and the attach parent, and linking the source issue.
// Repository, source task and version are filled later by the preparer.
func (s *CreationProposalService) envelope(q CreationProposalRequest) (submission.CreationEnvelope, error) {
	relationship := q.Relationship
	if relationship == "" {
		relationship = "linked"
	}
	artifacts := make([]submission.CreationArtifact, len(q.Drafts))
	for i, d := range q.Drafts {
		a := submission.CreationArtifact{Title: d.Title, Body: d.Body, ParentIssue: d.ParentIssue, LinkIssues: d.LinkIssues}
		if relationship == "attach" {
			if a.ParentIssue == 0 {
				a.ParentIssue = s.sourceIssue
			} else if a.ParentIssue != s.sourceIssue {
				return submission.CreationEnvelope{}, fmt.Errorf("draft %d: attach requires the parent issue to be the source issue #%d", i+1, s.sourceIssue)
			}
		}
		artifacts[i] = a
	}
	return submission.CreationEnvelope{Relationship: relationship, Artifacts: linkSourceIssue(artifacts, s.sourceIssue)}, nil
}

// HandlePrepare retains the drafts as this run's proposal. A refusal retains
// nothing and leaves no bookkeeping row: retention is one local transaction, so
// the dispatcher's plain rejection is the truthful receipt of a failure.
func (s *CreationProposalService) HandlePrepare(ctx context.Context, requestID string, arguments []byte) (json.RawMessage, error) {
	if !s.authorizer.enabled {
		return nil, store.ErrCreationAuthority
	}
	var q CreationProposalRequest
	if err := decodeMCPArguments(arguments, &q); err != nil {
		return nil, err
	}
	e, err := s.envelope(q)
	if err != nil {
		return nil, err
	}
	b := s.Binding()
	retain := func(ctx context.Context, e submission.CreationEnvelope) (string, error) {
		return s.authorizer.RetainProposalForRun(ctx, b.RunID, e)
	}
	prepared, err := s.preparer.prepare(ctx, e, creationProposalNonce(b, arguments), creationProposalCap, retain)
	if errors.Is(err, errCreationPresentationLimit) {
		return nil, fmt.Errorf("%w: shorten the drafts or split the work into a smaller proposal", err)
	}
	if err != nil {
		return nil, err
	}
	ref := store.FamilyReference{Kind: creationProposalReferenceKind, ID: b.RunID}
	if err := s.receipts.Finish(ctx, requestID, "confirmed", ref); err != nil {
		return nil, fmt.Errorf("finish creation proposal receipt: %w", err)
	}
	return json.Marshal(CreationProposalResult{
		RequestID:    requestID,
		Disposition:  "confirmed",
		Hash:         prepared.Hash,
		Relationship: prepared.Envelope.Relationship,
		Drafts:       len(prepared.Envelope.Artifacts),
		Message:      "The proposal is retained and nothing has been published. The factory will present it to the owner when this run ends; only the owner can decide whether it is created.",
	})
}

// CreationProposalReceiptProjection reports the input as accepted for a request
// whose receipt references its run's proposal. It observes retained facts only.
// A request that never reached its reference has no retained facts to report.
func CreationProposalReceiptProjection() FamilyProjection {
	return func(_ context.Context, r store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error) {
		if r.Reference.Kind != creationProposalReferenceKind || r.Reference.ID != r.Scope.RunID {
			return ReceiptDimensions{}, nil, nil
		}
		return ReceiptDimensions{Input: "accepted"}, nil, nil
	}
}
