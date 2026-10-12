package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// CommentForge keeps publication separate from bounded observation. GetIssue
// must reject pull requests, as github.Client.GetIssue does.
type CommentForge interface {
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
	ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error)
}

// IssuePublicationService owns ordinary existing-target comment grants. Creation
// policy is deliberately absent from this authority material.
type IssuePublicationService struct {
	db       *store.DB
	receipts *ReceiptService
	forge    CommentForge
	redactor *redact.Redactor
	targets  []int
	botID    int64
	repo     github.Repo
}

// NewIssuePublicationService binds comment publication to explicit targets and bot identity.
func NewIssuePublicationService(db *store.DB, r *ReceiptService, forge CommentForge, redactor *redact.Redactor, targets []int, botID int64) (*IssuePublicationService, error) {
	if db == nil || r == nil || r.db != db || forge == nil || redactor == nil || botID < 1 || len(targets) > 100 {
		return nil, errors.New("comments require immutable factory dependencies")
	}
	targets = slices.Clone(targets)
	slices.Sort(targets)
	for i, n := range targets {
		if n < 1 || i > 0 && targets[i-1] == n {
			return nil, errors.New("invalid comment targets")
		}
	}
	ref, err := naming.ParseRef(r.binding.Task)
	if err != nil {
		return nil, err
	}
	return &IssuePublicationService{db: db, receipts: r, forge: forge, redactor: redactor, targets: targets, botID: botID, repo: github.Repo{Owner: ref.Owner, Name: ref.Repo}}, nil
}

// Binding returns the immutable attempt scope used by the family dispatcher.
func (s *IssuePublicationService) Binding() store.ResultBinding { return s.receipts.Binding() }

// PermissionMaterial binds authorized comment targets and bot identity into the catalog digest.
func (s *IssuePublicationService) PermissionMaterial() ([]byte, error) {
	return json.Marshal(struct {
		Targets []int
		BotID   int64
	}{s.targets, s.botID})
}

// Registration supplies the ordinary authorized comment handler.
func (s *IssuePublicationService) Registration() FamilyRegistration {
	return FamilyRegistration{Name: "issues_comment", Owner: s, Handle: s.HandleComment}
}

// IssueCommentRequest supplies a repository-local target and content for authorized publication.
type IssueCommentRequest struct {
	IssueNumber int    `json:"issue_number"`
	Message     string `json:"message"`
}

// HandleComment persists prepared intent before electing one comment dispatch owner.
func (s *IssuePublicationService) HandleComment(ctx context.Context, requestID string, arguments []byte) (json.RawMessage, error) {
	reject := func(cause error) (json.RawMessage, error) {
		return nil, errors.Join(cause, s.receipts.Finish(ctx, requestID, "rejected", store.FamilyReference{}))
	}
	var q IssueCommentRequest
	if err := decodeMCPArguments(arguments, &q); err != nil {
		return reject(err)
	}
	if !slices.Contains(s.targets, q.IssueNumber) || q.IssueNumber < 1 || strings.TrimSpace(q.Message) == "" || !utf8.ValidString(q.Message) || len(q.Message) > github.CommentSizeLimit {
		return reject(errors.New("invalid or unauthorized comment"))
	}
	if err := s.db.CheckResultWriter(ctx, s.Binding()); err != nil {
		return nil, err
	}
	issue, err := s.forge.GetIssue(ctx, s.repo, q.IssueNumber)
	if err != nil {
		return reject(err)
	}
	if issue.Number != q.IssueNumber {
		return reject(errors.New("comment target mismatch"))
	}
	correlation, err := json.Marshal(struct {
		Scope   store.FamilyScope
		Request string
	}{s.receipts.scope, requestID})
	if err != nil {
		return reject(err)
	}
	digest := sha256.Sum256(correlation)
	marker := "<!-- lf:mcp-comment v=1 key=" + hex.EncodeToString(digest[:]) + " -->"
	body, err := s.redactor.Apply(ctx, q.Message+"\n\n"+marker, github.CommentSizeLimit)
	if err != nil {
		return reject(err)
	}
	if body.Truncated() || !strings.HasSuffix(body.String(), marker) {
		return reject(errors.New("prepared comment exceeds bounds"))
	}
	if err := bindPublicationDestination(ctx, s.forge, "comment:"+hex.EncodeToString(digest[:]), 0, naming.Ref{Forge: naming.ForgeGitHub, Owner: s.repo.Owner, Repo: s.repo.Name, Issue: q.IssueNumber}, s.botID); err != nil {
		return nil, err
	}
	op, first, err := s.db.ReserveCommentDispatch(ctx, s.Binding(), store.CommentOperation{Scope: s.receipts.scope, RequestID: requestID, IssueNumber: q.IssueNumber, BotID: s.botID, Body: body.String()})
	if err != nil {
		return nil, err
	}
	if first {
		// Last live check is after durable reservation; cancellation burns dispatch
		// rather than allowing a future retry to reclaim uncertain authority.
		if err = s.db.CheckResultWriter(ctx, s.Binding()); err == nil {
			var published github.Comment
			published, err = postCommentEffect(ctx, s.forge, "comment:"+hex.EncodeToString(digest[:]), s.repo, q.IssueNumber, body)
			if err == nil {
				if published.ID < 1 || published.Author.ID != s.botID || published.Body != op.Body {
					err = errors.New("comment POST identity not confirmed")
				} else {
					err = s.db.ConfirmCommentOperation(ctx, op, published.ID)
					if err == nil {
						op.State = "confirmed"
						op.CommentID = published.ID
					}
				}
			}
		}
	}
	finishErr := s.receipts.Finish(ctx, requestID, op.State, store.FamilyReference{Kind: "comment", RequestID: requestID})
	if err != nil || finishErr != nil {
		return nil, errors.Join(err, finishErr)
	}
	result, err := s.receipts.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// CommentReceiptProjection observes retained facts only, including a crash before
// the generic ledger link was persisted. It never contacts the forge.
func CommentReceiptProjection(db *store.DB) FamilyProjection {
	return func(ctx context.Context, r store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error) {
		op, err := db.GetCommentOperation(ctx, r.Scope, r.RequestID)
		if errors.Is(err, store.ErrNotFound) {
			return ReceiptDimensions{}, nil, nil
		}
		if err != nil {
			return ReceiptDimensions{}, nil, err
		}
		states := ReceiptDimensions{Input: "accepted", Effects: op.State}
		if op.CommentID == 0 {
			return states, nil, nil
		}
		return states, []PublicationIdentity{{IssueNumber: op.IssueNumber, CommentID: op.CommentID}}, nil
	}
}

// ReconcileComment is a factory-only observation API. The injected authorization
// callback must grant this exact retained scope and target independently of the
// old agent lease. It cannot create or recover a dispatch right.
func (s *IssuePublicationService) ReconcileComment(ctx context.Context, scope store.FamilyScope, requestID string, page int, authorize func(context.Context, store.CommentOperation) error) (store.CommentOperation, error) {
	op, err := s.db.GetCommentOperation(ctx, scope, requestID)
	if err != nil {
		return op, err
	}
	if authorize == nil || page < 1 || scope.Repository != s.repo.Owner+"/"+s.repo.Name {
		return op, store.ErrResultAuthority
	}
	if err := authorize(ctx, op); err != nil {
		return op, err
	}
	if op.State == "confirmed" {
		return op, nil
	}
	correlation, err := json.Marshal(struct {
		Scope   store.FamilyScope
		Request string
	}{scope, requestID})
	if err != nil {
		return op, err
	}
	digest := sha256.Sum256(correlation)
	if err := bindPublicationDestination(ctx, s.forge, "comment:"+hex.EncodeToString(digest[:]), 0, naming.Ref{Forge: naming.ForgeGitHub, Owner: s.repo.Owner, Repo: s.repo.Name, Issue: op.IssueNumber}, op.BotID); err != nil {
		return op, err
	}
	observed, err := s.forge.ListCommentsPage(ctx, s.repo, op.IssueNumber, page, 100)
	if err != nil {
		return op, err
	}
	if len(observed.Comments) > 100 {
		return op, errors.New("comment observation exceeds bound")
	}
	var id int64
	for _, c := range observed.Comments {
		if c.ID > 0 && c.Author.ID == op.BotID && c.Body == op.Body {
			if id != 0 && id != c.ID {
				return op, nil
			}
			id = c.ID
		}
	}
	if id == 0 {
		return op, nil
	}
	if err := authorize(ctx, op); err != nil {
		return op, err
	}
	if err := s.db.ConfirmCommentOperation(ctx, op, id); err != nil {
		return op, fmt.Errorf("confirm observed comment: %w", err)
	}
	op.State = "confirmed"
	op.CommentID = id
	return op, nil
}
