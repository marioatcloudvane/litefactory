package turn

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type commentObservation struct {
	CommentForge
	next int
}

func (f *commentObservation) ListCommentsPage(ctx context.Context, r github.Repo, issue, page, limit int) (github.CommentPage, error) {
	out, err := f.CommentForge.ListCommentsPage(ctx, r, issue, page, limit)
	f.next = out.NextPage
	return out, err
}

type creationObservation struct {
	CreationForge
	next int
}

func (f *creationObservation) ListIssuesPage(ctx context.Context, r github.Repo, state string, page, limit int) (github.IssuePage, error) {
	out, err := f.CreationForge.ListIssuesPage(ctx, r, state, page, limit)
	f.next = out.NextPage
	return out, err
}

func (r *MCPRuntime) observationReceipts(s store.FamilyScope) (*ReceiptService, error) {
	return NewReceiptService(r.Authority.db, store.ResultBinding{Task: s.Task, RunID: s.RunID, Attempt: s.Attempt}, nil, nil)
}

// ObserveComment performs one factory-owned, observation-only recovery page.
// Scope comes from the durable intent; obsolete leases never regain authority.
func (r *MCPRuntime) ObserveComment(ctx context.Context, o store.CommentOperation, page int) (store.CommentOperation, int, error) {
	if r == nil || r.Comments == nil || o.BotID != r.BotID {
		return o, 0, store.ErrResultAuthority
	}
	ref, err := naming.ParseRef(o.Scope.Task)
	if err != nil {
		return o, 0, err
	}
	if o.Scope.Repository != ref.Owner+"/"+ref.Repo || o.IssueNumber != ref.Issue {
		return o, 0, store.ErrResultAuthority
	}
	receipts, err := r.observationReceipts(o.Scope)
	if err != nil {
		return o, 0, err
	}
	forge := &commentObservation{CommentForge: r.Comments}
	service, err := NewIssuePublicationService(r.Authority.db, receipts, forge, r.Redactor, []int{o.IssueNumber}, r.BotID)
	if err != nil {
		return o, 0, err
	}
	observed, err := service.ReconcileComment(ctx, o.Scope, o.RequestID, page, func(_ context.Context, current store.CommentOperation) error {
		if current.Scope != o.Scope || current.RequestID != o.RequestID || current.Body != o.Body || current.IssueNumber != o.IssueNumber || current.BotID != r.BotID {
			return store.ErrResultAuthority
		}
		return nil
	})
	return observed, forge.next, err
}

// ObserveCreation observes retained dispatched artifacts without binding a grant,
// including when new creation is disabled. It never redispatches a batch.
func (r *MCPRuntime) ObserveCreation(ctx context.Context, o store.PendingCreationOperation, state string, page int) (store.CreationOperation, int, error) {
	if r == nil || r.Creation == nil || r.CreationAuthorizer == nil {
		return store.CreationOperation{}, 0, store.ErrCreationAuthority
	}
	receipts, err := r.observationReceipts(o.Scope)
	if err != nil {
		return store.CreationOperation{}, 0, err
	}
	forge := &creationObservation{CreationForge: r.Creation}
	service, err := NewCreationPublicationService(r.Authority.db, receipts, r.CreationAuthorizer, forge, r.Redactor, r.Paths.StoryProgressLocksDir())
	if err != nil {
		return store.CreationOperation{}, 0, err
	}
	observed, err := service.Reconcile(ctx, o.ID, state, page, func(ctx context.Context, p store.CreationProposal) error {
		if p.SourceTask != o.Scope.Task || p.Repository != o.Scope.Repository {
			return store.ErrCreationAuthority
		}
		session, err := r.Authority.db.GetSessionByTaskRef(ctx, p.SourceTask)
		if err != nil {
			return err
		}
		if session.TaskRef != o.Scope.Task {
			return store.ErrCreationAuthority
		}
		return nil
	})
	return observed, forge.next, err
}

// AdmitCreationApproval is reachable only from authenticated owner control intake.
func (r *MCPRuntime) AdmitCreationApproval(ctx context.Context, c OwnerGrantControl) (int64, error) {
	if r == nil || r.CreationAuthorizer == nil {
		return 0, errors.New("creation owner intake unavailable")
	}
	return r.CreationAuthorizer.AdmitOwnerGrantIntent(ctx, c)
}

// PrepareCreationProposal admits proposal data only from authenticated owner
// controls. Preparation retains exact redacted bytes but grants no publication.
func (r *MCPRuntime) PrepareCreationProposal(ctx context.Context, c OwnerGrantControl) (PreparedCreation, error) {
	if r == nil || r.CreationAuthorizer == nil || !r.CreationAuthorizer.enabled || c.Mode != "research" || c.Bot || c.ActorID != r.CreationAuthorizer.ownerID || c.ActorID == r.BotID || c.CommentID < 1 || c.CreatedAt.IsZero() || (!c.UpdatedAt.IsZero() && !c.UpdatedAt.Equal(c.CreatedAt)) {
		return PreparedCreation{}, store.ErrCreationAuthority
	}
	if !strings.HasPrefix(c.Body, "/mcp-create prepare ") {
		return PreparedCreation{}, store.ErrCreationAuthority
	}
	var envelope submission.CreationEnvelope
	if err := decodeMCPArguments([]byte(strings.TrimPrefix(c.Body, "/mcp-create prepare ")), &envelope); err != nil {
		return PreparedCreation{}, err
	}
	if envelope.Version != 0 && envelope.Version != 1 {
		return PreparedCreation{}, errors.New("unsupported creation envelope version")
	}
	ref, err := naming.ParseRef(c.SourceTask)
	if err != nil {
		return PreparedCreation{}, err
	}
	if c.Repository != ref.Owner+"/"+ref.Repo {
		return PreparedCreation{}, store.ErrCreationAuthority
	}

	envelope.Artifacts = linkSourceIssue(envelope.Artifacts, ref.Issue)
	// No live result/lease is created by this owner-only presenter.
	receipts, err := r.observationReceipts(store.FamilyScope{Task: c.SourceTask, Repository: c.Repository, RunID: c.CommentID, Attempt: "owner-proposal"})
	if err != nil {
		return PreparedCreation{}, err
	}
	service, err := NewCreationPublicationService(r.Authority.db, receipts, r.CreationAuthorizer, r.Creation, r.Redactor, r.Paths.StoryProgressLocksDir())
	if err != nil {
		return PreparedCreation{}, err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", c.SourceTask, c.CommentID, c.Body)))
	var nonce [16]byte
	copy(nonce[:], digest[:16])
	prepared, err := service.prepare(ctx, envelope, nonce, 48000, r.CreationAuthorizer.RetainProposal)
	if err != nil {
		return PreparedCreation{}, err
	}

	return prepared, nil
}
