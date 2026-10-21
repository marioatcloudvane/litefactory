package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func standardsTestProposal(t *testing.T, db *DB, id string, base int64) StandardsProposalVersion {
	t.Helper()
	p, err := db.SaveStandardsProposal(t.Context(), StandardsProposalVersion{ProposalID: id, BaseRevision: base, SchemaVersion: 1, Operations: []StandardsOperation{}, ChangeDigest: "change-" + id, ResultDigest: "book-" + id, Author: "42"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ApproveStandardsProposal(t.Context(), StandardsApproval{ProposalID: p.ProposalID, Version: p.Version, BaseRevision: p.BaseRevision, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest, Owner: "42", Provenance: "local-cli"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStandardsPublicationAtomicRetryAndHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	if _, err := db.GetStandardsRevision(ctx, 0); !errors.Is(err, ErrStandardsUnpublished) {
		t.Fatal(err)
	}
	p := standardsTestProposal(t, db, "first", 0)
	snapshot := StandardsSnapshot{Categories: []StandardsCategory{{ID: "cat", Name: "Category"}}, Standards: []StandardsStandard{{ID: "s", Title: "Title", Body: "Body", CategoryIDs: []string{"cat"}}}}
	// Foreign-key failure after the revision/category insert rolls the entire publication back.
	broken := snapshot
	broken.Standards = []StandardsStandard{{ID: "s", Title: "Title", Body: "Body", CategoryIDs: []string{"missing"}}}
	if _, err := db.PublishStandardsProposal(ctx, p, broken, "42"); err == nil {
		t.Fatal("invalid membership published")
	}
	head, err := db.StandardsHead(ctx)
	if err != nil || head != 0 {
		t.Fatalf("partial publication: %d %v", head, err)
	}
	first, err := db.PublishStandardsProposal(ctx, p, snapshot, "42")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := db.PublishStandardsProposal(ctx, p, snapshot, "42")
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatalf("retry: %#v %v", retry, err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO standards_categories(revision_id,id,name,description) VALUES(?,'late','Late','')`, first.ID); err == nil {
		t.Fatal("published snapshot accepted new category")
	}
	next := standardsTestProposal(t, db, "next", first.ID)
	newer := StandardsSnapshot{Categories: snapshot.Categories, Standards: []StandardsStandard{{ID: "s", Title: "Changed", Body: "New", CategoryIDs: []string{"cat"}}}}
	if _, err := db.PublishStandardsProposal(ctx, next, newer, "42"); err != nil {
		t.Fatal(err)
	}
	old, err := db.GetStandardsRevision(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(first, old) {
		t.Fatalf("history changed: %#v %v", old, err)
	}
	retry, err = db.PublishStandardsProposal(ctx, p, snapshot, "42")
	if err != nil || retry.ID != first.ID {
		t.Fatalf("retry after head moved: %#v %v", retry, err)
	}
	if _, err := db.SaveStandardsProposal(ctx, p, p.Version); !errors.Is(err, ErrStandardsConflict) {
		t.Fatalf("published proposal edited: %v", err)
	}
	for _, table := range []string{"standards_revisions", "standards_categories", "standards_standards", "standards_memberships", "standards_proposal_versions", "standards_approvals", "standards_publications"} {
		if _, err := db.sql.ExecContext(ctx, "UPDATE "+table+" SET rowid=rowid"); err == nil {
			t.Errorf("%s allowed update", table)
		}
		if _, err := db.sql.ExecContext(ctx, "DELETE FROM "+table); err == nil {
			t.Errorf("%s allowed deletion", table)
		}
	}
}

func TestStandardsIndependentConnectionsAndStaleApproval(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "standards.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := first.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	})
	p := standardsTestProposal(t, first, "one", 0)
	stale := standardsTestProposal(t, second, "two", 0)
	empty := StandardsSnapshot{Categories: []StandardsCategory{}, Standards: []StandardsStandard{}}
	published, err := first.PublishStandardsProposal(ctx, p, empty, "42")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.PublishStandardsProposal(ctx, stale, empty, "42"); !errors.Is(err, ErrStandardsConflict) {
		t.Fatalf("stale base: %v", err)
	}
	head, err := second.StandardsHead(ctx)
	if err != nil || head != published.ID {
		t.Fatalf("independent read: %d %v", head, err)
	}
	edit := standardsTestProposal(t, second, "edit", head)
	newer, err := second.SaveStandardsProposal(ctx, edit, edit.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.PublishStandardsProposal(ctx, edit, empty, "42"); !errors.Is(err, ErrStandardsConflict) {
		t.Fatalf("stale approved version: %v", err)
	}
	if _, err := second.PublishStandardsProposal(ctx, newer, empty, "42"); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatalf("old approval reused: %v", err)
	}
	if _, err := second.SaveStandardsProposal(ctx, edit, edit.Version); !errors.Is(err, ErrStandardsConflict) {
		t.Fatalf("lost update: %v", err)
	}
}

func TestStandardsCorruptRetainedRecords(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO standards_revisions(id,schema_version,digest,created_at) VALUES(1,1,'digest',?)`, FormatTime(nowUTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetStandardsRevision(ctx, 1); !errors.Is(err, ErrStandardsCorrupt) {
		t.Fatalf("missing provenance: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO standards_proposals(id,current_version) VALUES('broken',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO standards_proposal_versions(proposal_id,version,schema_version,operations_json,change_digest,result_digest,author,created_at) VALUES('broken',1,1,'broken','change','book','42',?)`, FormatTime(nowUTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetStandardsProposal(ctx, "broken", 1); !errors.Is(err, ErrStandardsCorrupt) {
		t.Fatalf("malformed operations: %v", err)
	}
}

func TestStandardsCompetingPublications(t *testing.T) {
	first := newTestDB(t)
	ctx := t.Context()
	second, err := Open(ctx, first.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	})
	p1 := standardsTestProposal(t, first, "one", 0)
	p2 := standardsTestProposal(t, second, "two", 0)
	snapshot := StandardsSnapshot{Categories: []StandardsCategory{{ID: "cat", Name: "Category"}}, Standards: []StandardsStandard{{ID: "standard", Title: "Title", Body: "Body", CategoryIDs: []string{"cat"}}}}
	type result struct {
		revision StandardsRevision
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	// Both goroutines finish when their one local transaction returns; the buffered
	// result channel is drained before this test can close either database.
	for _, candidate := range []struct {
		db *DB
		p  StandardsProposalVersion
	}{{first, p1}, {second, p2}} {
		go func() {
			<-start
			r, err := candidate.db.PublishStandardsProposal(ctx, candidate.p, snapshot, "42")
			results <- result{r, err}
		}()
	}
	close(start)
	successes := 0
	var winningID int64
	for range 2 {
		r := <-results
		if r.err == nil {
			successes++
			winningID = r.revision.ID
		} else if !errors.Is(r.err, ErrStandardsConflict) {
			t.Errorf("unexpected contention error: %v", r.err)
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful publications", successes)
	}
	revisions, err := second.ListStandardsRevisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || revisions[0].ID != winningID || !reflect.DeepEqual(revisions[0].Snapshot, snapshot) {
		t.Fatalf("partial or duplicate history: %#v", revisions)
	}
	head, err := first.StandardsHead(ctx)
	if err != nil || head != winningID {
		t.Fatalf("wrong head: %d %v", head, err)
	}
}
