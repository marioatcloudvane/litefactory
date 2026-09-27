package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// checksTableSchema returns the live CREATE TABLE text for submission_checks,
// which is the only reliable way to assert on a composite PRIMARY KEY and a
// FOREIGN KEY clause: PRAGMA table_info does not expose either directly.
func checksTableSchema(t *testing.T, db *DB) string {
	t.Helper()
	var schema string
	if err := db.sql.QueryRowContext(t.Context(), `SELECT sql FROM sqlite_master WHERE type='table' AND name='submission_checks'`).Scan(&schema); err != nil {
		t.Fatalf("read submission_checks schema: %v", err)
	}
	return schema
}

// TestMigration0047FreshDatabaseScopesChecksBySnapshot is AC-001's fresh-
// database half: a brand-new database's submission_checks table is already
// keyed by (run_id,snapshot_ref,reference) and still references
// submission_snapshots.
func TestMigration0047FreshDatabaseScopesChecksBySnapshot(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)

	schema := checksTableSchema(t, db)
	if !strings.Contains(schema, "PRIMARY KEY(run_id,snapshot_ref,reference)") {
		t.Fatalf("fresh submission_checks is not keyed by (run_id,snapshot_ref,reference): %s", schema)
	}
	if !strings.Contains(schema, "FOREIGN KEY(run_id,snapshot_ref) REFERENCES submission_snapshots(run_id,reference)") {
		t.Fatalf("fresh submission_checks lost its snapshot foreign key: %s", schema)
	}
}

// TestMigration0047PreservesPopulatedEvidenceAndIsIdempotent is AC-001's
// populated-database half. A database is stood up on the schema exactly as
// it was one migration before 0047, a real check is retained under it (the
// same shape production would have written), and then the rest of the
// sequence — including 0047 — is applied. The retained evidence, its
// ownership, provenance and the run's retained-byte accounting must survive
// byte for byte; the widened key and the foreign key must both be present
// afterward; and applying the full migration sequence again must change
// nothing.
func TestMigration0047PreservesPopulatedEvidenceAndIsIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pre-scope.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "submission_check_snapshot_scope"))

	// Every table submission_checks depends on (managed features/stories,
	// approvals, submission attempts/snapshots) already exists one migration
	// earlier, so their fixtures use the real store API; the run uses the historical
	// column set because AllocateRun now requires newer additive columns.
	session := testSession(t, db, 741)
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	key := "US-1"
	story := ManagedStory{ParentTask: session.TaskRef, Generation: 1, Key: key, IssueNumber: 500, IssueID: 5000, BodyHash: "old", State: "planning"}
	if err := db.PutManagedStory(t.Context(), story); err != nil {
		t.Fatal(err)
	}
	revision := ApprovedStoryRevision{ManagedStory: story, ParentID: 1230, Title: "Title", Body: "Exact body", FetchedAt: time.Now().UTC()}
	revision.Hash = submissionTestDigest([]byte(revision.Title + "\x00" + revision.Body))
	if err := db.SetManagedFeatureReady(t.Context(), session.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFeatureContract(t.Context(), FeatureContract{Parent: session.TaskRef, Generation: 1, ContentVersion: 2, SubmissionVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), session.TaskRef, 1, []ApprovedStoryRevision{revision}); err != nil {
		t.Fatal(err)
	}
	run := historicalRun(t, db, session.ID, 1, "implement")
	attempt := SubmissionAttempt{RunID: run.ID, Attempt: "attempt-1", Parent: session.TaskRef, Generation: 1, Stage: "engineering", Stories: []SubmissionStoryBinding{{Story: key, IssueID: story.IssueID, Revision: revision.Hash}}}
	if err := db.BindSubmissionAttempt(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}

	snapshot := submissionSnapshot(attempt, "pre-migration-snapshot")
	if err := db.RetainSubmissionSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	check := SubmissionCheck{RunID: run.ID, Attempt: attempt.Attempt, Reference: "build", SnapshotRef: snapshot.Reference, Name: "go build", Executed: true, Passed: true, Provenance: []byte(`{"cmd":"go build ./..."}`), Result: []byte("ok")}
	if err := db.RetainSubmissionCheck(t.Context(), check); err != nil {
		t.Fatal(err)
	}

	beforeBytes, err := db.SubmissionRetainedBytes(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion, err := db.SchemaVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("apply migration 0047 and beyond: %v", err)
	}

	schema := checksTableSchema(t, db)
	if !strings.Contains(schema, "PRIMARY KEY(run_id,snapshot_ref,reference)") {
		t.Fatalf("migration did not widen the key: %s", schema)
	}
	if !strings.Contains(schema, "FOREIGN KEY(run_id,snapshot_ref) REFERENCES submission_snapshots(run_id,reference)") {
		t.Fatalf("migration dropped the snapshot foreign key: %s", schema)
	}

	got, err := db.GetSubmissionCheck(t.Context(), run.ID, snapshot.Reference, check.Reference)
	if err != nil {
		t.Fatalf("pre-migration check did not survive: %v", err)
	}
	if !reflect.DeepEqual(got, check) {
		t.Fatalf("migration altered retained evidence: got %+v, want %+v", got, check)
	}

	afterBytes, err := db.SubmissionRetainedBytes(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if beforeBytes != afterBytes {
		t.Fatalf("migration changed retained-byte accounting: %d -> %d", beforeBytes, afterBytes)
	}

	// The foreign key relationship still holds after migration: a check
	// bound to a nonexistent snapshot is still rejected.
	if err := db.RetainSubmissionCheck(t.Context(), SubmissionCheck{RunID: run.ID, Attempt: attempt.Attempt, Reference: "orphan", SnapshotRef: "no-such-snapshot", Name: "go build", Executed: true, Passed: true, Provenance: []byte("x"), Result: []byte("y")}); err == nil {
		t.Fatal("a check bound to a nonexistent snapshot was accepted after migration")
	}

	// The behavior the migration exists to fix: a second snapshot may now
	// reuse the exact same check reference the pre-migration row used,
	// without disturbing it.
	second := submissionSnapshot(attempt, "post-migration-snapshot")
	if err := db.RetainSubmissionSnapshot(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	reused := SubmissionCheck{RunID: run.ID, Attempt: attempt.Attempt, Reference: check.Reference, SnapshotRef: second.Reference, Name: "go build", Executed: true, Passed: false, Provenance: []byte(`{"cmd":"go build ./... (second)"}`), Result: []byte("fail")}
	if err := db.RetainSubmissionCheck(t.Context(), reused); err != nil {
		t.Fatalf("a second snapshot could not reuse the pre-migration check reference: %v", err)
	}
	untouched, err := db.GetSubmissionCheck(t.Context(), run.ID, snapshot.Reference, check.Reference)
	if err != nil || !reflect.DeepEqual(untouched, check) {
		t.Fatalf("reusing the reference under a new snapshot mutated the original: %+v", untouched)
	}

	// Idempotent: re-running the full sequence changes nothing further.
	for i := range 2 {
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatalf("re-migrate %d: %v", i, err)
		}
	}
	afterVersion, err := db.SchemaVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if afterVersion == beforeVersion {
		t.Fatal("migration never actually applied")
	}
	var applied int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != afterVersion {
		t.Fatalf("schema_migrations holds %d rows for version %d: a migration was recorded twice", applied, afterVersion)
	}
	final, err := db.GetSubmissionCheck(t.Context(), run.ID, snapshot.Reference, check.Reference)
	if err != nil || !reflect.DeepEqual(final, check) {
		t.Fatal("re-migration altered retained evidence")
	}
}

// TestSubmissionCheckScopedBySnapshotReference is AC-002: two snapshots in
// the same run and attempt may retain a check under the identical
// human-supplied reference ("build") without colliding, each is read back
// under its own snapshot only, a lookup under the wrong snapshot misses
// cleanly rather than resolving to the other snapshot's check, an exact
// replay is idempotent, and changed content under the identical
// (run,snapshot,reference) tuple still conflicts.
func TestSubmissionCheckScopedBySnapshotReference(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "engineering")

	snapA := submissionSnapshot(a, "snapshot-a")
	if err := db.RetainSubmissionSnapshot(t.Context(), snapA); err != nil {
		t.Fatal(err)
	}
	snapB := submissionSnapshot(a, "snapshot-b")
	snapB.Files[0].Content = []byte("package other\n")
	snapB.Files[0].SHA256 = submissionTestDigest(snapB.Files[0].Content)
	var manifest strings.Builder
	for _, f := range snapB.Files {
		fmt.Fprintf(&manifest, "%s\x00%s\x00%t\n", f.Path, f.SHA256, f.Deleted)
	}
	snapB.Digest = submissionTestDigest([]byte(manifest.String()))
	if err := db.RetainSubmissionSnapshot(t.Context(), snapB); err != nil {
		t.Fatal(err)
	}

	checkA := SubmissionCheck{RunID: a.RunID, Attempt: a.Attempt, Reference: "build", SnapshotRef: snapA.Reference, Name: "go build", Executed: true, Passed: true, Provenance: []byte(`{"cmd":"go build ./... (a)"}`), Result: []byte("ok a")}
	if err := db.RetainSubmissionCheck(t.Context(), checkA); err != nil {
		t.Fatal(err)
	}
	checkB := SubmissionCheck{RunID: a.RunID, Attempt: a.Attempt, Reference: "build", SnapshotRef: snapB.Reference, Name: "go build", Executed: true, Passed: false, Provenance: []byte(`{"cmd":"go build ./... (b)"}`), Result: []byte("fail b")}
	if err := db.RetainSubmissionCheck(t.Context(), checkB); err != nil {
		t.Fatalf("a second snapshot in the same run/attempt could not reuse the same check reference: %v", err)
	}

	gotA, err := db.GetSubmissionCheck(t.Context(), a.RunID, snapA.Reference, "build")
	if err != nil || !reflect.DeepEqual(gotA, checkA) {
		t.Fatalf("snapshot A's check lost or mutated: %+v (err=%v)", gotA, err)
	}
	gotB, err := db.GetSubmissionCheck(t.Context(), a.RunID, snapB.Reference, "build")
	if err != nil || !reflect.DeepEqual(gotB, checkB) {
		t.Fatalf("snapshot B's check lost or mutated: %+v (err=%v)", gotB, err)
	}

	if _, err := db.GetSubmissionCheck(t.Context(), a.RunID, "no-such-snapshot", "build"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup under a wrong/nonexistent snapshot did not miss cleanly: %v", err)
	}

	// Exact same-tuple replay is idempotent.
	if err := db.RetainSubmissionCheck(t.Context(), checkA); err != nil {
		t.Fatalf("exact replay of an identical check was rejected: %v", err)
	}

	// Changed content under the identical tuple still conflicts, and the
	// original is left untouched.
	conflicting := checkA
	conflicting.Passed = false
	conflicting.Result = []byte("now fails")
	if err := db.RetainSubmissionCheck(t.Context(), conflicting); err == nil {
		t.Fatal("changed content under the same (run,snapshot,reference) tuple was accepted")
	}
	stillA, err := db.GetSubmissionCheck(t.Context(), a.RunID, snapA.Reference, "build")
	if err != nil || !reflect.DeepEqual(stillA, checkA) {
		t.Fatal("a rejected conflicting retention mutated the original check")
	}
}
