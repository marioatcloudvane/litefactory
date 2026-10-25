package standardsexceptions

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func exceptionFixture(t *testing.T, policy *standards.ExceptionPolicy) (*Service, *store.DB, store.Repo, Request, *time.Time) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "exceptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "owner", Name: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	book := standards.Service{Store: db, OwnerID: 42}
	p, err := book.Create(ctx, "book", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "category", Name: "Category"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "standard", Rule: "MUST Standard", Description: "Guidance", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "category", ExceptionPolicy: policy, AppliesTo: []string{"story-worker"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Approve(ctx, p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	revision, err := book.Publish(ctx, p.ProposalID, p.Version, p.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	service := &Service{Store: db, OwnerID: 42, Now: func() time.Time { return now }}
	request := Request{BookRevision: revision.ID, StandardID: "standard", RepositoryID: repo.ID, TaskRef: "github:owner/repo#42", Departure: "Use the temporary alternative", Reason: "Migration in progress", ExpiresAt: now.Add(time.Hour)}
	return service, db, repo, request, &now
}
func allowPolicy() *standards.ExceptionPolicy {
	return &standards.ExceptionPolicy{Version: 1, Mode: "allow-task", AllRegisteredRepositories: true, MaxLifetimeSeconds: 3600}
}

func TestExceptionClockApprovalAndRetainedHistory(t *testing.T) {
	s, db, repo, request, now := exceptionFixture(t, allowPolicy())
	ctx := t.Context()
	proposed, err := s.Propose(ctx, "operation", request)
	if err != nil {
		t.Fatal(err)
	}
	if proposed.Effective || proposed.Approval != nil || proposed.Status != "proposed" {
		t.Fatalf("proposal grants permission: %+v", proposed)
	}
	if _, err := s.Approve(ctx, proposed.Proposal.ID, "wrong"); !errors.Is(err, store.ErrStandardsExceptionDigest) {
		t.Fatalf("digest mismatch: %v", err)
	}
	*now = now.Add(30 * time.Minute)
	approved, err := s.Approve(ctx, proposed.Proposal.ID, proposed.Proposal.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if !approved.Effective || !approved.Proposal.Terms.ExpiresAt.Equal(request.ExpiresAt) {
		t.Fatalf("delayed approval changed expiry: %+v", approved)
	}
	*now = request.ExpiresAt
	expired, err := s.Show(ctx, proposed.Proposal.ID)
	if err != nil || expired.Effective || !expired.Expired || expired.Approval == nil || expired.Status != "expired" {
		t.Fatalf("expiry boundary: %+v %v", expired, err)
	}
	revoked, err := s.Revoke(ctx, proposed.Proposal.ID, "Finished")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Approve(ctx, proposed.Proposal.ID, proposed.Proposal.Digest)
	if err != nil || replay.Effective || replay.Revocation == nil || replay.Status != "revoked" {
		t.Fatalf("replay revived permission: %+v %v", replay, err)
	}
	if !revoked.Approval.CreatedAt.Equal(approved.Approval.CreatedAt) {
		t.Fatal("approval history changed")
	}
	repo.Name = "renamed"
	if _, err := db.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}
	unavailable, err := s.Show(ctx, proposed.Proposal.ID)
	if err != nil || unavailable.Effective || unavailable.Status != "unavailable" || unavailable.Error == "" || unavailable.Approval == nil || unavailable.Revocation == nil || unavailable.Proposal.Terms.RepositoryName != "repo" {
		t.Fatalf("renamed repository history: %+v %v", unavailable, err)
	}
}

func TestExceptionPolicyAndLifetimeFailClosed(t *testing.T) {
	cases := []struct {
		name   string
		policy *standards.ExceptionPolicy
		mutate func(*Request)
	}{
		{name: "missing policy"},
		{name: "denied policy", policy: &standards.ExceptionPolicy{Version: 1, Mode: "deny"}},
		{name: "repository ineligible", policy: &standards.ExceptionPolicy{Version: 1, Mode: "allow-task", RepositoryIDs: []int64{999}, MaxLifetimeSeconds: 3600}},
		{name: "task ineligible", policy: &standards.ExceptionPolicy{Version: 1, Mode: "allow-task", AllRegisteredRepositories: true, TaskRefs: []string{"github:owner/repo#99"}, MaxLifetimeSeconds: 3600}},
		{name: "task repository differs", policy: allowPolicy(), mutate: func(r *Request) { r.TaskRef = "github:other/repo#42" }},
		{name: "lifetime one millisecond too long", policy: allowPolicy(), mutate: func(r *Request) { r.ExpiresAt = r.ExpiresAt.Add(time.Millisecond) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, db, _, request, _ := exceptionFixture(t, tc.policy)
			if tc.mutate != nil {
				tc.mutate(&request)
			}
			if _, err := s.Propose(t.Context(), "operation", request); !errors.Is(err, ErrPolicy) {
				t.Fatalf("request allowed or wrong error: %v", err)
			}
			rows, err := db.ListStandardsExceptions(t.Context(), 10, 0)
			if err != nil || len(rows) != 0 {
				t.Fatalf("denial persisted proposal: %+v %v", rows, err)
			}
		})
	}
}

func TestExceptionExpiryRefusesFirstApprovalAndRetryPreservesTerms(t *testing.T) {
	s, _, _, request, now := exceptionFixture(t, allowPolicy())
	ctx := t.Context()
	first, err := s.Propose(ctx, "operation", request)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	retry, err := s.Propose(ctx, "operation", request)
	if err != nil || retry.Proposal.ID != first.Proposal.ID || retry.Proposal.Digest != first.Proposal.Digest || !retry.Proposal.CreatedAt.Equal(first.Proposal.CreatedAt) {
		t.Fatalf("retry drift: %+v %v", retry, err)
	}
	changed := request
	changed.Reason = "Different justification"
	if _, err := s.Propose(ctx, "operation", changed); err == nil {
		t.Fatal("same operation changed terms")
	}
	*now = request.ExpiresAt
	if _, err := s.Approve(ctx, first.Proposal.ID, first.Proposal.Digest); !errors.Is(err, store.ErrStandardsExceptionExpired) {
		t.Fatalf("expired approval: %v", err)
	}
	view, err := s.Show(ctx, first.Proposal.ID)
	if err != nil || view.Approval != nil || view.Effective {
		t.Fatalf("expired approval persisted: %+v %v", view, err)
	}
}

func TestExceptionDigestBindsCreationTime(t *testing.T) {
	s, db, _, request, _ := exceptionFixture(t, allowPolicy())
	view, err := s.Propose(t.Context(), "operation", request)
	if err != nil {
		t.Fatal(err)
	}
	record, err := db.GetStandardsException(t.Context(), view.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Proposal.CreatedAt = record.Proposal.CreatedAt.Add(-time.Minute)
	if _, err := decode(record.Proposal); !errors.Is(err, ErrDigest) {
		t.Fatalf("altered creation time accepted: %v", err)
	}
}
