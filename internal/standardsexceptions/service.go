// Package standardsexceptions records exact-task human agreements without changing factory authority.
package standardsexceptions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Exception errors distinguish input, authority, policy, reference and expiry failures.
var (
	ErrInvalid           = errors.New("exceptions: invalid input")
	ErrOwner             = errors.New("exceptions: configured owner required")
	ErrPolicy            = errors.New("exceptions: policy denied")
	ErrReference         = errors.New("exceptions: missing or unverifiable reference")
	ErrDigest            = errors.New("exceptions: digest mismatch")
	ErrPolicyMissing     = fmt.Errorf("%w: policy_missing", ErrPolicy)
	ErrPolicyUnsupported = fmt.Errorf("%w: policy_unsupported", ErrPolicy)
	ErrExpired           = errors.New("exceptions: expired")
)

// Request names one task and one exact published standard. Expiry is absolute.
type Request struct {
	BookRevision int64     `json:"book_revision"`
	StandardID   string    `json:"standard_id"`
	RepositoryID int64     `json:"repository_id"`
	TaskRef      string    `json:"task_ref"`
	Departure    string    `json:"departure"`
	Reason       string    `json:"reason"`
	ExpiresAt    time.Time `json:"expires_at"`
	Replaces     string    `json:"replaces,omitempty"`
}

// Terms retain all references instead of following mutable repository names or latest revisions.
type Terms struct {
	Request
	BookDigest      string `json:"book_digest"`
	PolicyDigest    string `json:"policy_digest"`
	Forge           string `json:"forge"`
	RepositoryOwner string `json:"repository_owner"`
	RepositoryName  string `json:"repository_name"`
}

// Proposal is the immutable reviewed agreement and its exact digest.
type Proposal struct {
	ID            string    `json:"id"`
	OperationKey  string    `json:"operation_key"`
	SchemaVersion int       `json:"schema_version"`
	Digest        string    `json:"digest"`
	Terms         Terms     `json:"terms"`
	CreatedAt     time.Time `json:"created_at"`
	Owner         string    `json:"owner"`
}

// View separates retained events from permission at EvaluatedAt. Error retains damaged history.
type View struct {
	Proposal    Proposal                            `json:"proposal"`
	Approval    *store.StandardsExceptionApproval   `json:"approval,omitempty"`
	Revocation  *store.StandardsExceptionRevocation `json:"revocation,omitempty"`
	Effective   bool                                `json:"effective"`
	Expired     bool                                `json:"expired"`
	Status      string                              `json:"status"`
	EvaluatedAt time.Time                           `json:"evaluated_at"`
	Error       string                              `json:"error,omitempty"`
	Standard    *store.StandardsStandard            `json:"standard,omitempty"`
}

type persistence interface {
	CreateStandardsException(context.Context, store.StandardsExceptionProposal, store.StandardsExceptionValidator) (store.StandardsExceptionProposal, error)
	GetStandardsException(context.Context, string) (store.StandardsExceptionRecord, error)
	ListStandardsExceptions(context.Context, int, int) ([]store.StandardsExceptionRecord, error)
	ApproveStandardsException(context.Context, string, string, string, time.Time, store.StandardsExceptionValidator) (store.StandardsExceptionRecord, error)
	RevokeStandardsException(context.Context, string, string, string, time.Time) (store.StandardsExceptionRecord, error)
	GetRepo(context.Context, int64) (store.Repo, error)
	GetStandardsRevision(context.Context, int64) (store.StandardsRevision, error)
}

// Service receives trusted CLI authority and a clock; it makes no remote task existence claim.
type Service struct {
	Store   persistence
	OwnerID int64
	Now     func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func proposalDigest(p store.StandardsExceptionProposal) string {
	return hash(struct {
		Schema                int
		ID, Key, Owner, Terms string
		CreatedAt             time.Time
	}{p.SchemaVersion, p.ID, p.OperationKey, p.Owner, p.TermsJSON, p.CreatedAt.UTC().Truncate(time.Millisecond)})
}
func normalize(r Request) (Request, error) {
	r.Departure = strings.TrimSpace(r.Departure)
	r.Reason = strings.TrimSpace(r.Reason)
	r.ExpiresAt = r.ExpiresAt.UTC().Truncate(time.Millisecond)
	if r.BookRevision <= 0 || r.RepositoryID <= 0 || strings.TrimSpace(r.StandardID) == "" || r.Departure == "" || r.Reason == "" || r.ExpiresAt.IsZero() {
		return r, ErrInvalid
	}
	if _, err := naming.ParseRef(r.TaskRef); err != nil {
		return r, fmt.Errorf("%w: task_ref: %w", ErrInvalid, err)
	}
	return r, nil
}
func decode(p store.StandardsExceptionProposal) (Terms, error) {
	var terms Terms
	if err := json.Unmarshal([]byte(p.TermsJSON), &terms); err != nil {
		return terms, fmt.Errorf("%w: terms: %w", ErrReference, err)
	}
	if p.SchemaVersion != 1 || proposalDigest(p) != p.Digest {
		return terms, ErrDigest
	}
	b, _ := json.Marshal(terms)
	if string(b) != p.TermsJSON {
		return terms, ErrDigest
	}
	if _, err := normalize(terms.Request); err != nil {
		return terms, err
	}
	if terms.BookRevision != p.BookRevision || terms.StandardID != p.StandardID || terms.RepositoryID != p.RepositoryID || !terms.ExpiresAt.Equal(p.ExpiresAt) {
		return terms, ErrDigest
	}
	return terms, nil
}
func standard(r store.StandardsRevision, id string) (store.StandardsStandard, error) {
	for _, v := range r.Snapshot.Standards {
		if v.ID == id {
			return v, nil
		}
	}
	return store.StandardsStandard{}, ErrReference
}
func validate(rec store.StandardsExceptionRecord, r store.StandardsRevision, repo store.Repo) error {
	p := rec.Proposal
	t, err := decode(p)
	if err != nil {
		return err
	}
	v, err := standard(r, t.StandardID)
	if err != nil {
		return err
	}
	if v.ExceptionPolicy == nil {
		return ErrPolicyMissing
	}
	if v.ExceptionPolicy.Version != 1 {
		return ErrPolicyUnsupported
	}
	if err = standards.VerifyRevision(r); err != nil {
		return fmt.Errorf("%w: %w", ErrReference, err)
	}
	if r.ID != t.BookRevision || r.Digest != t.BookDigest || repo.ID != t.RepositoryID || repo.Forge != t.Forge || repo.Owner != t.RepositoryOwner || repo.Name != t.RepositoryName {
		return ErrReference
	}
	ref, err := naming.ParseRef(t.TaskRef)
	if err != nil {
		return err
	}
	if ref.Forge != repo.Forge || ref.Owner != repo.Owner || ref.Repo != repo.Name {
		return fmt.Errorf("%w: task repository differs", ErrPolicy)
	}
	policy := v.ExceptionPolicy
	if policy == nil || policy.Version != 1 || policy.Mode != "allow-task" || standards.PolicyDigest(policy) != t.PolicyDigest {
		return ErrPolicy
	}
	if !policy.AllRegisteredRepositories && !slices.Contains(policy.RepositoryIDs, repo.ID) {
		return fmt.Errorf("%w: repository ineligible", ErrPolicy)
	}
	if len(policy.TaskRefs) > 0 && !slices.Contains(policy.TaskRefs, t.TaskRef) {
		return fmt.Errorf("%w: task ineligible", ErrPolicy)
	}
	if policy.MaxLifetimeSeconds <= 0 || policy.MaxLifetimeSeconds > 9223372036 || !t.ExpiresAt.After(p.CreatedAt) || t.ExpiresAt.After(p.CreatedAt.Add(time.Duration(policy.MaxLifetimeSeconds)*time.Second)) {
		return fmt.Errorf("%w: lifetime exceeds published maximum", ErrPolicy)
	}

	if rec.Approval != nil && (rec.Approval.Digest != p.Digest || rec.Approval.Owner == "" || !store.StandardsProvenanceValid(rec.Approval.Provenance) || rec.Approval.CreatedAt.Before(p.CreatedAt) || !rec.Approval.CreatedAt.Before(t.ExpiresAt)) {
		return ErrDigest
	}
	return nil
}

// Propose retains canonical terms; the operation key recovers identical retries.
func (s *Service) Propose(ctx context.Context, key string, request Request) (View, error) {
	if s.OwnerID <= 0 {
		return View{}, ErrOwner
	}
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return View{}, ErrInvalid
	}
	request, err := normalize(request)
	if err != nil {
		return View{}, err
	}
	owner := strconv.FormatInt(s.OwnerID, 10)
	id := "exc-" + hash(struct{ Owner, Key string }{owner, key})[:32]
	if existing, e := s.Store.GetStandardsException(ctx, id); e == nil {
		t, e := decode(existing.Proposal)
		if e != nil {
			return View{}, e
		}
		if t.Request != request || existing.Proposal.Owner != owner || existing.Proposal.OperationKey != key {
			return View{}, store.ErrStandardsConflict
		}
		return s.finish(ctx, existing, s.now())
	} else if !errors.Is(e, store.ErrNotFound) {
		return View{}, e
	}
	now := s.now()
	if !request.ExpiresAt.After(now) {
		return View{}, ErrExpired
	}
	r, err := s.Store.GetStandardsRevision(ctx, request.BookRevision)
	if err != nil {
		return View{}, fmt.Errorf("%w: %w", ErrReference, err)
	}
	repo, err := s.Store.GetRepo(ctx, request.RepositoryID)
	if err != nil {
		return View{}, fmt.Errorf("%w: %w", ErrReference, err)
	}
	v, err := standard(r, request.StandardID)
	if err != nil {
		return View{}, err
	}
	terms := Terms{Request: request, BookDigest: r.Digest, PolicyDigest: standards.PolicyDigest(v.ExceptionPolicy), Forge: repo.Forge, RepositoryOwner: repo.Owner, RepositoryName: repo.Name}
	raw, _ := json.Marshal(terms)
	p := store.StandardsExceptionProposal{ID: id, OperationKey: key, SchemaVersion: 1, TermsJSON: string(raw), Owner: owner, CreatedAt: now, BookRevision: request.BookRevision, StandardID: request.StandardID, RepositoryID: request.RepositoryID, ExpiresAt: request.ExpiresAt}
	p.Digest = proposalDigest(p)
	if request.Replaces != "" {
		if _, e := s.Store.GetStandardsException(ctx, request.Replaces); e != nil {
			return View{}, fmt.Errorf("%w: replaces: %w", ErrReference, e)
		}
	}
	p, err = s.Store.CreateStandardsException(ctx, p, validate)
	if errors.Is(err, store.ErrStandardsExceptionConflict) || errors.Is(err, store.ErrStandardsConflict) {
		existing, e := s.Store.GetStandardsException(ctx, id)
		if e == nil {
			t, e := decode(existing.Proposal)
			if e == nil && t.Request == request && existing.Proposal.Owner == owner {
				return s.finish(ctx, existing, s.now())
			}
		}
	}
	if err != nil {
		return View{}, err
	}
	return s.Show(ctx, p.ID)
}

// Show returns retained history and effectiveness at the current clock time.
func (s *Service) Show(ctx context.Context, id string) (View, error) {
	r, err := s.Store.GetStandardsException(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.finish(ctx, r, s.now())
}

// List projects a bounded page without treating historical approval as current permission.
func (s *Service) List(ctx context.Context, limit, offset int) ([]View, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, ErrInvalid
	}
	rs, err := s.Store.ListStandardsExceptions(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(rs))
	now := s.now()
	for _, r := range rs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, s.project(ctx, r, now))
	}
	return out, ctx.Err()
}

// Approve records a separate trusted-owner decision on the exact ID and digest.
func (s *Service) Approve(ctx context.Context, id, digest string) (View, error) {
	if s.OwnerID <= 0 {
		return View{}, ErrOwner
	}
	if id == "" || digest == "" {
		return View{}, ErrInvalid
	}
	r, err := s.Store.ApproveStandardsException(ctx, id, digest, strconv.FormatInt(s.OwnerID, 10), s.now(), validate)
	if err != nil {
		return View{}, err
	}
	return s.finish(ctx, r, s.now())
}

// Revoke retains a terminal event, including when the agreement has expired.
func (s *Service) Revoke(ctx context.Context, id, reason string) (View, error) {
	if s.OwnerID <= 0 {
		return View{}, ErrOwner
	}
	r, err := s.Store.RevokeStandardsException(ctx, id, strconv.FormatInt(s.OwnerID, 10), strings.TrimSpace(reason), s.now())
	if err != nil {
		return View{}, err
	}
	return s.finish(ctx, r, s.now())
}
func (s *Service) project(ctx context.Context, rec store.StandardsExceptionRecord, now time.Time) View {
	p := rec.Proposal
	t, err := decode(p)
	out := View{Proposal: Proposal{ID: p.ID, OperationKey: p.OperationKey, SchemaVersion: p.SchemaVersion, Digest: p.Digest, Terms: t, CreatedAt: p.CreatedAt, Owner: p.Owner}, Approval: rec.Approval, Revocation: rec.Revocation, EvaluatedAt: now, Expired: !now.Before(p.ExpiresAt), Status: "proposed"}
	if err == nil {
		var r store.StandardsRevision
		var repo store.Repo
		r, err = s.Store.GetStandardsRevision(ctx, p.BookRevision)
		if err == nil {
			repo, err = s.Store.GetRepo(ctx, p.RepositoryID)
		}
		if err == nil {
			err = validate(rec, r, repo)
		}
		if err == nil {
			v, e := standard(r, p.StandardID)
			if e == nil {
				out.Standard = &v
			}
		}
	}
	if err != nil {
		out.Status = "unavailable"
		out.Error = err.Error()
		return out
	}
	if rec.Revocation != nil {
		out.Status = "revoked"
		return out
	}
	if out.Expired {
		out.Status = "expired"
		return out
	}
	if rec.Approval != nil {
		out.Status = "approved"
		out.Effective = !now.Before(rec.Approval.CreatedAt)
	}
	return out
}

func (s *Service) finish(ctx context.Context, r store.StandardsExceptionRecord, now time.Time) (View, error) {
	v := s.project(ctx, r, now)
	return v, ctx.Err()
}
