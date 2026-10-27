package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func boundedStandardsFixture(t *testing.T, description string, operations []StandardsOperation) (*DB, StandardsRevision) {
	t.Helper()
	db := newTestDB(t)
	p, err := db.SaveStandardsProposal(t.Context(), StandardsProposalVersion{ProposalID: "bounded", SchemaVersion: 1, Operations: operations, ChangeDigest: "change", ResultDigest: "result", Author: "42"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ApproveStandardsProposal(t.Context(), StandardsApproval{ProposalID: p.ProposalID, Version: p.Version, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest, Owner: "42", Provenance: "local-cli"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := StandardsSnapshot{Categories: []StandardsCategory{{ID: "c", Name: "C"}}, Standards: []StandardsStandard{{ID: "s", CategoryID: "c", Rule: "MUST Rule", Description: description, Evidence: "Evidence.", Scope: "All.", Check: "manual", AppliesTo: []string{"story-worker"}}}}
	revision, err := db.PublishStandardsProposal(t.Context(), p, snapshot, "42")
	if err != nil {
		t.Fatal(err)
	}
	return db, revision
}

func TestBoundedStandardsReadPreservesExactVersions(t *testing.T) {
	db, revision := boundedStandardsFixture(t, "Description.", nil)
	for _, id := range []int64{0, revision.ID} {
		got, err := db.GetStandardsRevisionBounded(t.Context(), id, 1<<20)
		if err != nil || !reflect.DeepEqual(got, revision) {
			t.Fatalf("bounded revision %+v %v", got, err)
		}
	}
	for _, version := range []int64{0, 1} {
		got, err := db.GetStandardsProposalBounded(t.Context(), "bounded", version, 1<<20)
		want, werr := db.GetStandardsProposal(t.Context(), "bounded", version)
		if err != nil || werr != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("bounded proposal %+v %v", got, err)
		}
	}
	if got, err := db.GetStandardsRevisionBounded(t.Context(), revision.ID+1, 1<<20); !errors.Is(err, ErrNotFound) || got.ID != 0 {
		t.Fatalf("missing revision substituted %+v %v", got, err)
	}
	if _, err := db.GetStandardsProposalBounded(t.Context(), "missing", 0, 1<<20); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := db.GetStandardsRevisionBounded(t.Context(), revision.ID, 0); !errors.Is(err, ErrStandardsReadTooLarge) {
		t.Fatal(err)
	}
	if _, err := db.GetStandardsProposalBounded(t.Context(), "bounded", 1, 0); !errors.Is(err, ErrStandardsReadTooLarge) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.GetStandardsRevisionBounded(ctx, revision.ID, 1<<20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestBoundedStandardsReadCountsUTF8BytesAndRows(t *testing.T) {
	db, revision := boundedStandardsFixture(t, strings.Repeat("🙂", 1000), nil)
	got, err := db.GetStandardsRevisionBounded(t.Context(), revision.ID, 3000)
	if !errors.Is(err, ErrStandardsReadTooLarge) || got.ID != 0 {
		t.Fatalf("UTF-8 source allocated: %+v %v", got, err)
	}
	got, err = db.GetStandardsRevisionBounded(t.Context(), revision.ID, 8000)
	if err != nil || len(got.Snapshot.Standards) != 1 {
		t.Fatalf("within bound refused %+v %v", got, err)
	}
	// Even short rows carry retained object/slice overhead rather than a budget
	// based only on characters in their text.
	db, revision = boundedStandardsFixture(t, "x", nil)
	if got, err = db.GetStandardsRevisionBounded(t.Context(), revision.ID, 256); !errors.Is(err, ErrStandardsReadTooLarge) || got.ID != 0 {
		t.Fatalf("row overhead omitted: %+v %v", got, err)
	}
}

func TestBoundedStandardsRefusesProposalBeforeDecoding(t *testing.T) {
	db, revision := boundedStandardsFixture(t, "Description.", nil)
	// A deliberately corrupt private fixture proves the scalar size query runs
	// before either reader decodes operations_json. Ordinary writes stay sealed.
	err := db.withTx(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), `DROP TRIGGER standards_proposal_versions_immutable_update`); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), `UPDATE standards_proposal_versions SET operations_json=? WHERE proposal_id='bounded'`, strings.Repeat("x", 8192))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetStandardsRevisionBounded(t.Context(), revision.ID, 4096); !errors.Is(err, ErrStandardsReadTooLarge) || got.ID != 0 {
		t.Fatalf("revision decoded oversized proposal: %+v %v", got, err)
	}
	if got, err := db.GetStandardsProposalBounded(t.Context(), "bounded", 1, 4096); !errors.Is(err, ErrStandardsReadTooLarge) || got.ProposalID != "" {
		t.Fatalf("proposal allocated before bound: %+v %v", got, err)
	}
	if _, err := db.GetStandardsRevision(t.Context(), revision.ID); !errors.Is(err, ErrStandardsCorrupt) {
		t.Fatalf("unrestricted compatibility path did not reach corrupt decoder: %v", err)
	}
}
