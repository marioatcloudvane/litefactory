package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStandardsExceptionsTransitionsIndependentConnections(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "exceptions.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	repo, err := db.UpsertRepo(ctx, Repo{Path: t.TempDir(), Forge: "github", Owner: "owner", Name: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	book := standardsTestProposal(t, db, "exceptions-book", 0)
	revision, err := db.PublishStandardsProposal(ctx, book, StandardsSnapshot{Standards: []StandardsStandard{{ID: "s", Title: "Standard", Body: "Text", CategoryIDs: []string{}}}}, "42")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	p := StandardsExceptionProposal{ID: "exception", OperationKey: "operation", SchemaVersion: 1, Digest: "digest", TermsJSON: `{"terms":"exact"}`, CreatedAt: now, Owner: "42", BookRevision: revision.ID, StandardID: "s", RepositoryID: repo.ID, ExpiresAt: now.Add(time.Hour)}
	validate := func(record StandardsExceptionRecord, r StandardsRevision, repository Repo) error {
		if r.ID != p.BookRevision || repository.ID != repo.ID || record.Proposal.Digest != p.Digest {
			return errors.New("bad transactional references")
		}
		return nil
	}
	created, err := db.CreateStandardsException(ctx, p, validate)
	if err != nil {
		t.Fatal(err)
	}
	retry := p
	retry.ID = "discarded-id"
	retry.CreatedAt = now.Add(time.Minute)
	recovered, err := other.CreateStandardsException(ctx, retry, nil)
	if err != nil || !reflect.DeepEqual(created, recovered) {
		t.Fatalf("create retry: %#v %v", recovered, err)
	}
	retry.TermsJSON = `{"terms":"changed"}`
	if _, err := other.CreateStandardsException(ctx, retry, nil); !errors.Is(err, ErrStandardsExceptionConflict) {
		t.Fatalf("different retry: %v", err)
	}
	if _, err := db.RevokeStandardsException(ctx, p.ID, "42", "reason", now); !errors.Is(err, ErrStandardsExceptionApprovalRequired) {
		t.Fatalf("revoke unapproved: %v", err)
	}
	if _, err := db.ApproveStandardsException(ctx, p.ID, "wrong", "42", now, validate); !errors.Is(err, ErrStandardsExceptionDigest) {
		t.Fatalf("digest: %v", err)
	}
	rejected := errors.New("reference changed")
	if _, err := db.ApproveStandardsException(ctx, p.ID, p.Digest, "42", now, func(StandardsExceptionRecord, StandardsRevision, Repo) error { return rejected }); !errors.Is(err, rejected) {
		t.Fatalf("validation: %v", err)
	}
	record, err := db.GetStandardsException(ctx, p.ID)
	if err != nil || record.Approval != nil {
		t.Fatalf("failed approval persisted: %#v %v", record, err)
	}
	approved, err := db.ApproveStandardsException(ctx, p.ID, p.Digest, "42", now, validate)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := other.RevokeStandardsException(ctx, p.ID, "42", "reason", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := db.ApproveStandardsException(ctx, p.ID, p.Digest, "42", now.Add(2*time.Hour), nil)
	if err != nil || !reflect.DeepEqual(revoked, replay) {
		t.Fatalf("approval replay reactivated: %#v %v", replay, err)
	}
	replay, err = other.RevokeStandardsException(ctx, p.ID, "42", "reason", now.Add(2*time.Hour))
	if err != nil || !reflect.DeepEqual(revoked, replay) {
		t.Fatalf("revoke retry: %#v %v", replay, err)
	}
	if !reflect.DeepEqual(approved.Approval, revoked.Approval) {
		t.Fatal("approval changed")
	}
	listed, err := db.ListStandardsExceptions(ctx, 10, 0)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %#v %v", listed, err)
	}
	for _, table := range []string{"standards_exception_proposals", "standards_exception_approvals", "standards_exception_revocations"} {
		if _, err := db.sql.ExecContext(ctx, "UPDATE "+table+" SET rowid=rowid"); err == nil {
			t.Errorf("%s mutable", table)
		}
		if _, err := db.sql.ExecContext(ctx, "DELETE FROM "+table); err == nil {
			t.Errorf("%s deletable", table)
		}
	}
	second := p
	second.ID = "expired"
	second.OperationKey = "expired-key"
	if _, err := db.CreateStandardsException(ctx, second, validate); err != nil {
		t.Fatal(err)
	}
	if _, err := other.ApproveStandardsException(ctx, second.ID, p.Digest, "42", p.ExpiresAt, validate); !errors.Is(err, ErrStandardsExceptionExpired) {
		t.Fatalf("expiry boundary: %v", err)
	}
	// Competing writers either commit or report a rolled-back conflict. Retrying
	// on the independent connection must recover one retained approval.
	third := p
	third.ID, third.OperationKey = "competing", "competing-key"
	if _, err := db.CreateStandardsException(ctx, third, validate); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, connection := range []*DB{db, other} {
		go func(connection *DB) {
			<-start
			_, err := connection.ApproveStandardsException(ctx, third.ID, p.Digest, "42", now, validate)
			results <- err
		}(connection)
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil && !errors.Is(err, ErrStandardsConflict) {
			t.Fatalf("competing approval: %v", err)
		}
	}
	if _, err := other.ApproveStandardsException(ctx, third.ID, p.Digest, "42", now, validate); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM standards_exception_approvals WHERE proposal_id=?`, third.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("approval count: %d %v", count, err)
	}

	// Approval replay competing with revocation cannot undo the terminal event.
	transitions := make(chan error, 2)
	begin := make(chan struct{})
	go func() {
		<-begin
		_, err := db.ApproveStandardsException(ctx, third.ID, p.Digest, "42", now.Add(time.Minute), validate)
		transitions <- err
	}()
	go func() {
		<-begin
		_, err := other.RevokeStandardsException(ctx, third.ID, "42", "complete", now.Add(time.Minute))
		transitions <- err
	}()
	close(begin)
	for range 2 {
		if err := <-transitions; err != nil && !errors.Is(err, ErrStandardsConflict) {
			t.Fatalf("competing transitions: %v", err)
		}
	}
	terminal, err := other.RevokeStandardsException(ctx, third.ID, "42", "complete", now.Add(time.Minute))
	if err != nil || terminal.Revocation == nil {
		t.Fatalf("terminal transition: %+v %v", terminal, err)
	}
	terminal, err = db.ApproveStandardsException(ctx, third.ID, p.Digest, "42", now.Add(time.Hour), nil)
	if err != nil || terminal.Revocation == nil {
		t.Fatalf("terminal approval replay: %+v %v", terminal, err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.GetStandardsException(canceled, p.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
