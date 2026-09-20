package turn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

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

// CreationPublicationService consumes the existing authorizer; it owns no grant,
// quota or approval engine. Production launch and recovery scheduling are external.
type CreationPublicationService struct {
	db           *store.DB
	receipts     *ReceiptService
	authorizer   *CreationAuthorizer
	forge        CreationForge
	redactor     *redact.Redactor
	lockDir      string
	mu           sync.Mutex
	guard        *AttemptGuard
	intentID     int64
	proposalHash string
}

// NewCreationPublicationService composes publication with the existing creation authorizer.
func NewCreationPublicationService(db *store.DB, r *ReceiptService, a *CreationAuthorizer, f CreationForge, redactor *redact.Redactor, lockDir string) (*CreationPublicationService, error) {
	if db == nil || r == nil || r.db != db || a == nil || f == nil || redactor == nil || lockDir == "" {
		return nil, errors.New("creation requires immutable factory dependencies")
	}
	return &CreationPublicationService{db: db, receipts: r, authorizer: a, forge: f, redactor: redactor, lockDir: lockDir}, nil
}

// Binding returns the immutable attempt scope used by the family dispatcher.
func (s *CreationPublicationService) Binding() store.ResultBinding { return s.receipts.Binding() }

// PermissionMaterial binds creation gate visibility and bot identity into the catalog digest.
func (s *CreationPublicationService) PermissionMaterial() ([]byte, error) {
	return json.Marshal(struct {
		Enabled      bool
		BotID        int64
		IntentID     int64  `json:"IntentID,omitempty"`
		ProposalHash string `json:"ProposalHash,omitempty"`
	}{s.authorizer.enabled, s.authorizer.botID, s.intentID, s.proposalHash})
}

// Registration supplies creation handling and fresh authorization checks for replay.
func (s *CreationPublicationService) Registration() FamilyRegistration {
	return FamilyRegistration{Name: "issues_create_batch", Owner: s, Handle: s.HandleCreate, ReplayCheck: s.CheckReplay}
}

// BindGuard is factory-only, one-time construction after the catalog digest and
// lease exist. No tool arguments can install or replace this capability.
func (s *CreationPublicationService) BindGuard(g *AttemptGuard) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g == nil || g.Binding() != s.Binding() || s.guard != nil {
		return store.ErrCreationAuthority
	}
	s.guard = g
	return nil
}
func (s *CreationPublicationService) liveGuard() *AttemptGuard {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.guard
}

// PreparedCreation is the exact review surface. Hash is content correlation, not
// an approval token or a deduplication key.
type PreparedCreation struct {
	Envelope submission.CreationEnvelope `json:"envelope"`
	Hash     string                      `json:"hash"`
}

// Prepare is a factory-only presenter. It defaults to linking, prepares all
// redaction/links/markers before hashing, and retains no approval or grant.
func (s *CreationPublicationService) Prepare(ctx context.Context, e submission.CreationEnvelope) (PreparedCreation, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return PreparedCreation{}, err
	}
	return s.prepare(ctx, e, nonce, 0, s.authorizer.RetainProposal)
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
	b := s.Binding()
	if e.Repository != "" && e.Repository != s.receipts.scope.Repository || e.SourceTask != "" && e.SourceTask != b.Task {
		return PreparedCreation{}, store.ErrCreationAuthority
	}
	e.Repository = s.receipts.scope.Repository
	e.SourceTask = b.Task
	e.Version = 1
	if e.Relationship == "" {
		e.Relationship = "linked"
	}
	// Canonical validation runs before and after preparation; rendering cannot turn
	// malformed source text into a silently accepted proposal.
	if _, _, err := submission.CanonicalCreation(e); err != nil {
		return PreparedCreation{}, err
	}
	artifacts := make([]submission.CreationArtifact, len(e.Artifacts))
	for i, a := range e.Artifacts {
		a.LinkIssues = append([]int(nil), a.LinkIssues...)
		var links strings.Builder
		for _, target := range a.LinkIssues {
			fmt.Fprintf(&links, "\n\nRelated: https://github.com/%s/issues/%d", e.Repository, target)
		}
		marker := fmt.Sprintf("<!-- lf:mcp-create v=1 key=%s artifact=%d -->", hex.EncodeToString(nonce[:]), i)
		title, err := s.redactor.Apply(ctx, a.Title, 256)
		if err != nil {
			return PreparedCreation{}, err
		}
		body, err := s.redactor.Apply(ctx, a.Body+links.String()+"\n\n"+marker, 65536)
		if err != nil {
			return PreparedCreation{}, err
		}
		if title.Truncated() || body.Truncated() || !strings.HasSuffix(body.String(), marker) {
			return PreparedCreation{}, errors.New("prepared creation exceeds bounds")
		}
		a.Title = title.String()
		a.Body = body.String()
		artifacts[i] = a
	}
	e.Artifacts = artifacts
	if presentationLimit > 0 {
		data, _, err := submission.CanonicalCreation(e)
		if err != nil {
			return PreparedCreation{}, err
		}
		if len(data) > presentationLimit {
			return PreparedCreation{}, errCreationPresentationLimit
		}
	}
	hash, err := retain(ctx, e)
	if err != nil {
		return PreparedCreation{}, err
	}
	return PreparedCreation{Envelope: e, Hash: hash}, nil
}

func (s *CreationPublicationService) decode(arguments []byte) (submission.CreationEnvelope, error) {
	var e submission.CreationEnvelope
	if err := decodeMCPArguments(arguments, &e); err != nil {
		return e, err
	}
	if e.Repository != s.receipts.scope.Repository || e.SourceTask != s.Binding().Task {
		return e, store.ErrCreationAuthority
	}
	_, _, err := submission.CanonicalCreation(e)
	return e, err
}

// CheckReplay reuses the primitive's live-grant check and existing reservation;
// it never consumes a permit or publishes. An unreserved rejected request must
// not acquire a fresh reservation merely by replaying its generic journal entry.
func (s *CreationPublicationService) CheckReplay(ctx context.Context, requestID string, args []byte) error {
	e, err := s.decode(args)
	if err != nil {
		return err
	}
	if _, err := s.db.CreationPublicationRequest(ctx, s.receipts.scope, requestID); err != nil {
		return store.ErrCreationAuthority
	}
	r, err := s.authorizer.ReserveCreation(ctx, s.liveGuard(), e)
	if err != nil {
		return err
	}
	if r.Permit != nil {
		return store.ErrCreationAuthority
	}
	return nil
}

// HandleCreate publishes reviewed artifacts only through the authorizer's dispatch permit.
func (s *CreationPublicationService) HandleCreate(ctx context.Context, requestID string, args []byte) (result json.RawMessage, err error) {
	reject := func(cause error) (json.RawMessage, error) {
		return nil, errors.Join(cause, s.receipts.Finish(ctx, requestID, "rejected", store.FamilyReference{}))
	}
	e, err := s.decode(args)
	if err != nil {
		return reject(err)
	}
	guard := s.liveGuard()
	if guard == nil {
		return reject(store.ErrCreationAuthority)
	}
	live, release, err := guard.Context(ctx)
	if err != nil {
		return reject(err)
	}
	defer release()
	admitted, err := s.db.ReadFamilyRequest(live, s.Binding(), []store.FamilyScope{s.receipts.scope}, requestID)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(args)
	if admitted.Tool != "issues_create_batch" || admitted.PayloadHash != hex.EncodeToString(hash[:]) || admitted.Disposition != "pending" {
		return nil, store.ErrResultCorrelation
	}
	reservation, err := s.authorizer.ReserveCreation(live, guard, e)
	if err != nil {
		return reject(err)
	}
	ref := store.FamilyReference{Kind: "creation_publication", ID: reservation.Operation.ID}
	// Scoped operation correlation is retained before remote dispatch, so the
	// receipt projection survives a crash before the generic ledger is finished.
	if err := s.db.LinkCreationPublication(ctx, s.Binding(), s.receipts.scope, requestID, reservation.Operation.ID); err != nil {
		return nil, err
	}
	defer func() {
		disposition := "uncertain"
		if err == nil {
			op, readErr := s.authorizer.store.GetCreationOperation(ctx, reservation.Operation.ID)
			err = errors.Join(err, readErr)
			if readErr == nil && op.State == "confirmed" {
				disposition = "confirmed"
			}
		}
		err = errors.Join(err, s.receipts.Finish(ctx, requestID, disposition, ref))
	}()
	if reservation.Permit != nil {
		var unlock func() error
		unlock, err = s.db.LockStoryProgress(live, e.SourceTask, s.lockDir)
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, unlock()) }()
		if _, err = s.authorizer.BeginDispatch(live, reservation.Permit); err != nil {
			return nil, err
		}
		err = s.publish(live, guard, reservation.Operation.ID, e)
		state := "confirmed"
		if err != nil {
			state = "uncertain"
		}
		_, recordErr := s.authorizer.RecordOperationOutcome(ctx, reservation.Permit, state)
		if err != nil || recordErr != nil {
			return nil, errors.Join(err, recordErr)
		}
	}
	receipt, err := s.receipts.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	receipt.Reference = ref
	if receipt.States.Effects == "confirmed" {
		receipt.Disposition = "confirmed"
	} else {
		receipt.Disposition = "uncertain"
	}

	return json.Marshal(receipt)
}

func (s *CreationPublicationService) publish(ctx context.Context, guard *AttemptGuard, id int64, e submission.CreationEnvelope) error {
	source, err := naming.ParseRef(e.SourceTask)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(source)
	for i, a := range e.Artifacts {
		// Reapplying redaction is hygiene only. Any changed byte demands new review.
		title, err := s.redactor.Apply(ctx, a.Title, 256)
		if err != nil {
			return err
		}
		body, err := s.redactor.Apply(ctx, a.Body, 65536)
		if err != nil {
			return err
		}
		if title.String() != a.Title || body.String() != a.Body || title.Truncated() || body.Truncated() {
			return errors.New("reviewed creation content changed")
		}
		if err := guard.Check(ctx); err != nil {
			return err
		}
		// Primitive replay verifies expiry on each additional effect without minting
		// another owner; only this original consumed permit path can reach this loop.
		if _, err := s.authorizer.ReserveCreation(ctx, guard, e); err != nil {
			return err
		}
		if err := s.db.BeginCreationArtifact(ctx, id, i); err != nil {
			return err
		}
		issue, err := s.forge.CreateIssue(ctx, repo, title, body)
		if err != nil {
			return err
		}
		if !s.matches(issue, a) {
			return errors.New("created issue content or identity mismatch")
		}
		detail := store.CreationPublicationArtifact{Ordinal: i, Dispatched: true, IssueID: issue.ID, IssueNumber: issue.Number, Relationship: "linked"}
		if a.ParentIssue > 0 {
			detail.Relationship = "uncertain"
		}
		if err := s.db.SaveCreationArtifact(ctx, id, detail); err != nil {
			return err
		}
		if a.ParentIssue > 0 {
			if err := guard.Check(ctx); err != nil {
				return err
			}
			if _, err := s.authorizer.ReserveCreation(ctx, guard, e); err != nil {
				return err
			}
			if err := s.forge.AddSubIssue(ctx, repo, a.ParentIssue, issue.ID); err != nil {
				return err
			}
			parent, err := s.forge.GetParentIssue(ctx, repo, issue.Number)
			if err != nil {
				return err
			}
			expected, err := s.forge.GetIssue(ctx, repo, a.ParentIssue)
			if err != nil {
				return err
			}
			if parent.ID < 1 || parent.ID != expected.ID || parent.Number != a.ParentIssue {
				return errors.New("creation attachment not confirmed")
			}
			detail.Relationship = "attached"
			if err := s.db.SaveCreationArtifact(ctx, id, detail); err != nil {
				return err
			}
		}
	}
	return nil
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
