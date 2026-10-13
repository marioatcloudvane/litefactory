package turn

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// CreationForge provides writes and bounded, independently authorized observation.
type CreationForge interface {
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	AddSubIssue(context.Context, github.Repo, int, int64) error
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	GetParentIssue(context.Context, github.Repo, int) (github.Issue, error)
	ListIssuesPage(context.Context, github.Repo, string, int, int) (github.IssuePage, error)
}

// CreationPublicationService prepares proposals and observes retained legacy effects.
type CreationPublicationService struct {
	db         *store.DB
	receipts   *ReceiptService
	authorizer *CreationAuthorizer
	forge      CreationForge
	redactor   *redact.Redactor
}

// NewCreationPublicationService binds proposal preparation and historical observation.
func NewCreationPublicationService(db *store.DB, r *ReceiptService, a *CreationAuthorizer, f CreationForge, redactor *redact.Redactor) (*CreationPublicationService, error) {
	if db == nil || r == nil || r.db != db || a == nil || f == nil || redactor == nil {
		return nil, errors.New("creation requires immutable factory dependencies")
	}
	return &CreationPublicationService{db: db, receipts: r, authorizer: a, forge: f, redactor: redactor}, nil
}

// Binding returns the immutable attempt scope used for proposal preparation.
func (s *CreationPublicationService) Binding() store.ResultBinding { return s.receipts.Binding() }

// PreparedCreation is the exact review surface. Hash is content correlation, not
// an approval token or a deduplication key.
type PreparedCreation struct {
	Envelope submission.CreationEnvelope `json:"envelope"`
	Hash     string                      `json:"hash"`
}

// errCreationPresentationLimit refuses a canonical envelope the owner could
// never be shown in one presentation.
var errCreationPresentationLimit = errors.New("creation proposal exceeds exact owner presentation limit")

// creationRetainer retains one fully prepared envelope and returns its hash.
// The owner path retains the proposal alone; the agent tool also records it as
// its run's proposal owed to the owner, atomically.
type creationRetainer func(context.Context, submission.CreationEnvelope) (string, error)

// linkSourceIssue returns artifacts whose link lists each contain the source
// issue exactly once, appended after any targets already present. It copies:
// the input artifacts and their link slices are never modified. It does not
// cap the list; canonical validation refuses more than 16 targets.
func linkSourceIssue(artifacts []submission.CreationArtifact, sourceIssue int) []submission.CreationArtifact {
	out := make([]submission.CreationArtifact, len(artifacts))
	for i, a := range artifacts {
		a.LinkIssues = slices.Clone(a.LinkIssues)
		if !slices.Contains(a.LinkIssues, sourceIssue) {
			a.LinkIssues = append(a.LinkIssues, sourceIssue)
		}
		out[i] = a
	}
	return out
}

func (s *CreationPublicationService) prepare(ctx context.Context, e submission.CreationEnvelope, nonce [16]byte, presentationLimit int, retain creationRetainer) (PreparedCreation, error) {
	return prepareCreation(ctx, e, nonce, presentationLimit, s.receipts.scope.Repository, s.Binding().Task, s.redactor, retain, s.forge)
}

// prepareCreation is shared by agent proposal drafting and owner-comment intake.
// Owner intake has source provenance, but no synthetic run or receipt authority.
func prepareCreation(ctx context.Context, e submission.CreationEnvelope, nonce [16]byte, presentationLimit int, repository, task string, redactor *redact.Redactor, retain creationRetainer, forges ...CreationForge) (PreparedCreation, error) {
	if e.Repository != "" && e.Repository != repository || e.SourceTask != "" && e.SourceTask != task {
		return PreparedCreation{}, rejectCreation("fields", store.ErrCreationAuthority)
	}
	e.Repository = repository
	e.SourceTask = task
	e.Version = 1
	if e.Relationship == "" {
		e.Relationship = "linked"
	}
	// Canonical validation runs before and after preparation; rendering cannot turn
	// malformed source text into a silently accepted proposal.
	if _, _, err := submission.CanonicalCreation(e); err != nil {
		return PreparedCreation{}, rejectCreation("fields", err)
	}
	artifacts := make([]submission.CreationArtifact, len(e.Artifacts))
	for i, a := range e.Artifacts {
		a.LinkIssues = append([]int(nil), a.LinkIssues...)
		var links strings.Builder
		for _, target := range a.LinkIssues {
			ref, err := naming.ParseRef(task)
			if err != nil {
				return PreparedCreation{}, err
			}
			var forge CreationForge
			if len(forges) > 0 {
				forge = forges[0]
			}
			display, err := lookupTicketDisplay(ctx, forge, ref, target)
			if err != nil {
				return PreparedCreation{}, err
			}
			if ref.Forge == "linear" {
				fmt.Fprintf(&links, "\n\nRelated: %s", display.Reference())
			} else {
				fmt.Fprintf(&links, "\n\nRelated: %s", display.URL)
			}
		}
		marker := fmt.Sprintf("<!-- lf:mcp-create v=1 key=%s artifact=%d -->", hex.EncodeToString(nonce[:]), i)
		title, err := redactor.Apply(ctx, a.Title, 256)
		if err != nil {
			return PreparedCreation{}, err
		}
		body, err := redactor.Apply(ctx, a.Body+links.String()+"\n\n"+marker, 65536)
		if err != nil {
			return PreparedCreation{}, err
		}
		if title.Truncated() || body.Truncated() || !strings.HasSuffix(body.String(), marker) {
			return PreparedCreation{}, rejectCreation("fields", errors.New("prepared creation exceeds bounds"))
		}
		a.Title = title.String()
		a.Body = body.String()
		artifacts[i] = a
	}
	e.Artifacts = artifacts
	if presentationLimit > 0 {
		data, _, err := submission.CanonicalCreation(e)
		if err != nil {
			return PreparedCreation{}, rejectCreation("fields", err)
		}
		if len(data) > presentationLimit {
			return PreparedCreation{}, rejectCreation("fields", errCreationPresentationLimit)
		}
	}
	hash, err := retain(ctx, e)
	if err != nil {
		return PreparedCreation{}, err
	}
	return PreparedCreation{Envelope: e, Hash: hash}, nil
}

func (s *CreationPublicationService) matches(issue github.Issue, a submission.CreationArtifact) bool {
	return issue.ID > 0 && issue.Number > 0 && issue.Author.ID == s.authorizer.botID && issue.Title == a.Title && issue.Body == a.Body
}

// CreationPublicationProjection recovers IDs and relationship outcomes through
// scoped retained correlation; it does not perform network observation.
func CreationPublicationProjection(db *store.DB, creation *store.CreationStore) FamilyProjection {
	return func(ctx context.Context, r store.FamilyRequest) (ReceiptDimensions, []PublicationIdentity, error) {
		id, err := db.CreationPublicationRequest(ctx, r.Scope, r.RequestID)
		if errors.Is(err, store.ErrNotFound) {
			return ReceiptDimensions{}, nil, nil
		}
		if err != nil {
			return ReceiptDimensions{}, nil, err
		}
		op, err := creation.GetCreationOperation(ctx, id)
		if err != nil {
			return ReceiptDimensions{}, nil, err
		}
		details, err := db.CreationPublicationArtifacts(ctx, id)
		if err != nil {
			return ReceiptDimensions{}, nil, err
		}
		out := make([]PublicationIdentity, 0, len(details))
		for _, a := range details {
			out = append(out, PublicationIdentity{IssueID: a.IssueID, IssueNumber: a.IssueNumber, Relationship: a.Relationship})
		}
		return ReceiptDimensions{Input: "accepted", Effects: op.State}, out, nil
	}
}

// Reconcile observes one bounded page and already-known relationships under a
// separately supplied factory observation grant. It never resumes the batch.
func (s *CreationPublicationService) Reconcile(ctx context.Context, id int64, state string, page int, authorize func(context.Context, store.CreationProposal) error) (store.CreationOperation, error) {
	p, err := s.db.CreationPublicationContext(ctx, id)
	if err != nil {
		return store.CreationOperation{}, err
	}
	if authorize == nil || p.Repository != s.receipts.scope.Repository || (state != "open" && state != "closed") || page < 1 {
		return store.CreationOperation{}, store.ErrCreationAuthority
	}
	if err := authorize(ctx, p); err != nil {
		return store.CreationOperation{}, err
	}
	op, err := s.authorizer.store.GetCreationOperation(ctx, id)
	if err != nil {
		return op, err
	}
	if op.State == "confirmed" {
		return op, nil
	}
	var e submission.CreationEnvelope
	if err := json.Unmarshal(p.Envelope, &e); err != nil {
		return store.CreationOperation{}, err
	}
	source, err := naming.ParseRef(p.SourceTask)
	if err != nil {
		return store.CreationOperation{}, err
	}
	repo := github.RepoFromRef(source)
	observed, err := s.forge.ListIssuesPage(ctx, repo, state, page, 100)
	if err != nil {
		return store.CreationOperation{}, err
	}
	if len(observed.Items) > 100 {
		return store.CreationOperation{}, errors.New("creation observation exceeds bound")
	}
	details, err := s.db.CreationPublicationArtifacts(ctx, id)
	if err != nil {
		return store.CreationOperation{}, err
	}
	confirmed := 0
	for _, d := range details {
		a := e.Artifacts[d.Ordinal]
		if d.IssueID == 0 {
			var match github.Issue
			ambiguous := false
			for _, issue := range observed.Items {
				if s.matches(issue, a) {
					if match.ID != 0 && match.ID != issue.ID {
						ambiguous = true
					}
					match = issue
				}
			}
			if ambiguous || match.ID == 0 {
				continue
			}
			d.IssueID = match.ID
			d.IssueNumber = match.Number
		} else {
			issue, err := s.forge.GetIssue(ctx, repo, d.IssueNumber)
			if err != nil {
				return store.CreationOperation{}, err
			}
			if issue.ID != d.IssueID || !s.matches(issue, a) {
				continue
			}
		}
		d.Relationship = "linked"
		if a.ParentIssue > 0 {
			d.Relationship = "uncertain"
			parent, err := s.forge.GetParentIssue(ctx, repo, d.IssueNumber)
			if err != nil {
				return store.CreationOperation{}, err
			}
			expected, err := s.forge.GetIssue(ctx, repo, a.ParentIssue)
			if err != nil {
				return store.CreationOperation{}, err
			}
			if parent.ID > 0 && parent.ID == expected.ID && parent.Number == a.ParentIssue {
				d.Relationship = "attached"
			}
		}
		if err := authorize(ctx, p); err != nil {
			return store.CreationOperation{}, err
		}
		if err := s.db.SaveCreationArtifact(ctx, id, d); err != nil {
			return store.CreationOperation{}, err
		}
		if d.Relationship == "linked" || d.Relationship == "attached" {
			confirmed++
		}
	}
	outcome := "uncertain"
	if confirmed == len(e.Artifacts) {
		outcome = "confirmed"
	}
	if err := authorize(ctx, p); err != nil {
		return store.CreationOperation{}, err
	}
	return s.authorizer.ReconcileOperation(ctx, id, outcome)
}
