package store

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func resultRun(t *testing.T, db *DB, session Session, mode, attempt string, trigger int64) ResultAttempt {
	t.Helper()
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "application-results", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: mode, Agent: AgentClaude, TriggerCommentID: trigger, LogPath: func(n int) string { return fmt.Sprintf("/logs/result-%d", n) }})
	if err != nil {
		t.Fatal(err)
	}
	return ResultAttempt{Task: session.TaskRef, Mode: mode, WorkContext: "authorized-context", Attempt: attempt, RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}
}
func resultRecord(b ResultBinding, id string, expected int64) ResultRecord {
	return ResultRecord{Binding: b, RequestID: id, ExpectedRevision: expected, Payload: []byte(fmt.Sprintf(`{"comment":"request %s"}`, id)), Diagnostics: []byte(`{"valid":true}`), Snapshot: []byte(fmt.Sprintf(`{"retained":"%s"}`, id)), Retired: []byte(`{"collection":["retired-1"]}`), Accepted: true}
}
func resultCounts(t *testing.T, db *DB) [4]int {
	t.Helper()
	var counts [4]int
	for i, query := range []string{`SELECT count(*) FROM application_result_requests`, `SELECT count(*) FROM application_result_revisions`, `SELECT count(*) FROM application_result_observations`, `SELECT COALESCE(sum(revision),0) FROM application_results`} {
		if err := db.sql.QueryRowContext(t.Context(), query).Scan(&counts[i]); err != nil {
			t.Fatal(err)
		}
	}
	return counts
}
func TestResultBinding_AllModesAndContextContinuation(t *testing.T) {
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			session := testSession(t, db, 701)
			a := resultRun(t, db, session, mode, "first", 1)
			b, err := db.BindResultAttempt(t.Context(), a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.RecordResult(t.Context(), resultRecord(b, "create", 0)); err != nil {
				t.Fatal(err)
			}
			fresh := resultRun(t, db, session, mode, "fresh", 2)
			next, err := db.BindResultAttempt(t.Context(), fresh)
			if err != nil {
				t.Fatal(err)
			}
			if b.ResultID != next.ResultID {
				t.Fatal("same context did not continue")
			}
			if err = db.CheckResultWriter(t.Context(), b); !errors.Is(err, ErrResultAuthority) {
				t.Fatalf("old writer retained: %v", err)
			}
			if _, err = db.BindResultAttempt(t.Context(), a); !errors.Is(err, ErrResultAuthority) {
				t.Fatal("revoked attempt rebound")
			}
			if _, err = db.GetResultRevision(t.Context(), b, 1); err != nil {
				t.Fatal("historical read revoked", err)
			}
			before := resultCounts(t, db)
			if _, err = db.RecordResult(t.Context(), resultRecord(b, "forbidden", 1)); !errors.Is(err, ErrResultAuthority) {
				t.Fatal("revoked append accepted", err)
			}
			if resultCounts(t, db) != before {
				t.Fatal("revoked write changed persistence")
			}
			fresh.Attempt = "changed-scope"
			fresh.FrozenStories = []ResultStoryRevision{{Story: "US-001", Revision: submissionTestDigest([]byte("new frozen requirements"))}}
			changed, err := db.BindResultAttempt(t.Context(), fresh)
			if err != nil {
				t.Fatal(err)
			}
			if changed.ResultID == next.ResultID {
				t.Fatal("changed frozen scope reused result")
			}
			var generations int
			if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM managed_features`).Scan(&generations); err != nil {
				t.Fatal(err)
			}
			if generations != 0 {
				t.Fatal("fabricated managed generation")
			}
		})
	}
}
func TestResultBinding_ModeTaskAndVersionIsolation(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 702)
	a := resultRun(t, db, session, "research", "first", 1)
	b, err := db.BindResultAttempt(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"mode", "task", "run", "schema", "workflow", "submission"} {
		t.Run(fault, func(t *testing.T) {
			bad := a
			bad.Attempt = "bad-" + fault
			switch fault {
			case "mode":
				bad.Mode = "spike"
			case "task":
				bad.Task = "other"
			case "run":
				bad.RunID = 999
			case "schema":
				bad.SchemaVersion = 2
			case "workflow":
				bad.WorkflowVersion = 3
			case "submission":
				bad.SubmissionVersion = 1
			}
			if _, err := db.BindResultAttempt(t.Context(), bad); err == nil {
				t.Fatal("invalid binding admitted")
			}
		})
	}
	fresh := resultRun(t, db, session, "spike", "spike", 2)
	other, err := db.BindResultAttempt(t.Context(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	if other.ResultID == b.ResultID {
		t.Fatal("mode change reused result")
	}
	forged := other
	forged.ResultID = b.ResultID
	if _, err = db.GetResultRevision(t.Context(), forged, 1); !errors.Is(err, ErrResultAuthority) {
		t.Fatal("forged reader crossed identity", err)
	}
}
func TestResultRecord_ConcurrentCASAndRollback(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 703)
	a := resultRun(t, db, session, "research", "writer", 1)
	b, err := db.BindResultAttempt(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordResult(t.Context(), resultRecord(b, "create", 0)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"left", "right"} {
		wg.Go(func() { _, err := db.RecordResult(t.Context(), resultRecord(b, id, 1)); results <- err })
	}
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			var stale *ResultRevisionConflict
			if !errors.As(err, &stale) || stale.Expected != 1 || stale.Current != 2 {
				t.Fatalf("unexpected competitor result: %v", err)
			}
			conflicts++
		}
	}
	if winners != 1 || conflicts != 1 || resultCounts(t, db) != [4]int{2, 2, 4, 2} {
		t.Fatal("CAS did not retain one complete winner")
	}
	before := resultCounts(t, db)
	if _, err = db.sql.ExecContext(t.Context(), `CREATE TRIGGER reject_result_revision BEFORE INSERT ON application_result_revisions WHEN NEW.revision=3 BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordResult(t.Context(), resultRecord(b, "rollback", 2)); err == nil {
		t.Fatal("injected snapshot failure ignored")
	}
	if resultCounts(t, db) != before {
		t.Fatal("failed snapshot left request or observations")
	}
	if _, err = db.sql.ExecContext(t.Context(), `DROP TRIGGER reject_result_revision`); err != nil {
		t.Fatal(err)
	}
	bad := resultRecord(b, "bad", 2)
	bad.Snapshot = []byte(`{`)
	if _, err = db.RecordResult(t.Context(), bad); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	if resultCounts(t, db) != before {
		t.Fatal("invalid candidate left partial writes")
	}
}
func TestResultRequestReferences_OrderedBoundedAndScoped(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 705)
	a := resultRun(t, db, session, "research", "writer", 1)
	b, err := db.BindResultAttempt(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordResult(t.Context(), resultRecord(b, "first", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordResult(t.Context(), resultRecord(b, "second", 1)); err != nil {
		t.Fatal(err)
	}
	bad := resultRecord(b, "third", 2)
	bad.Accepted, bad.Snapshot, bad.Retired = false, nil, nil
	if _, err = db.RecordResult(t.Context(), bad); err != nil {
		t.Fatal(err)
	}
	refs, err := db.ResultRequestReferences(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	want := []ResultRequestReference{
		{RunID: b.RunID, RequestID: "third", InputState: "rejected", AppliedRevision: 0},
		{RunID: b.RunID, RequestID: "second", InputState: "accepted", AppliedRevision: 2},
		{RunID: b.RunID, RequestID: "first", InputState: "accepted", AppliedRevision: 1},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("references: got=%+v want=%+v", refs, want)
	}
	// The inventory is bounded to the most recently retained 32 correlations.
	for i := range 40 {
		bulk := resultRecord(b, fmt.Sprintf("bulk-%02d", i), 2)
		bulk.Accepted, bulk.Snapshot, bulk.Retired = false, nil, nil
		if _, err = db.RecordResult(t.Context(), bulk); err != nil {
			t.Fatal(err)
		}
	}
	bounded, err := db.ResultRequestReferences(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded) != 32 {
		t.Fatalf("bound not enforced: %d", len(bounded))
	}
	if bounded[0].RequestID != "bulk-39" || bounded[31].RequestID != "bulk-08" {
		t.Fatalf("bound did not retain the most recent requests: first=%s last=%s", bounded[0].RequestID, bounded[31].RequestID)
	}
	// Binding a new context on the same task (here, a different mode) revokes
	// this result's writer, but a historical reader still recovers its exact
	// retained inventory, and a sibling result gains no visibility into it.
	fresh := resultRun(t, db, session, "spike", "other", 2)
	other, err := db.BindResultAttempt(t.Context(), fresh)
	if err != nil {
		t.Fatal(err)
	}
	if other.ResultID == b.ResultID {
		t.Fatal("sibling mode reused this result")
	}
	revoked, err := db.ResultRequestReferences(t.Context(), b)
	if err != nil || !reflect.DeepEqual(revoked, bounded) {
		t.Fatalf("revoked writer lost historical inventory: %+v %v", revoked, err)
	}
	siblingRefs, err := db.ResultRequestReferences(t.Context(), other)
	if err != nil {
		t.Fatal(err)
	}
	if len(siblingRefs) != 0 {
		t.Fatal("sibling result leaked request references")
	}
	// A forged binding masquerading as this result cannot read its inventory.
	forged := other
	forged.ResultID = b.ResultID
	if _, err = db.ResultRequestReferences(t.Context(), forged); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("forged reader crossed identity: %v", err)
	}
}
func TestResultHistory_ReopenReplayCollisionAndObservations(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 704)
	a := resultRun(t, db, session, "implement", "writer", 1)
	b, err := db.BindResultAttempt(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	q := resultRecord(b, "original", 0)
	r, err := db.RecordResult(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(q.Snapshot)
	q.Snapshot[0] = '!'
	q.Retired[0] = '!'
	r.Payload[0] = '!'
	if _, err = db.RecordResult(t.Context(), resultRecord(b, "newer", 1)); err != nil {
		t.Fatal(err)
	}
	observed, err := db.ObserveResult(t.Context(), b, b.RunID, "original", "engineering", "recorded")
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.Observations) != 3 || observed.Latest.Input != "accepted" || observed.Latest.Engineering != "recorded" || observed.Latest.Acceptance != "pending" || observed.Latest.Publication != "pending" {
		t.Fatal("receipt dimensions conflated")
	}
	if _, err = db.ObserveResult(t.Context(), b, b.RunID, "original", "acceptance", "accepted"); err == nil {
		t.Fatal("separated Implement granted acceptance observation")
	}
	if err = db.EndResultAttempt(t.Context(), b); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ObserveResult(t.Context(), b, b.RunID, "original", "publication", "delivered"); !errors.Is(err, ErrResultAuthority) {
		t.Fatal("revoked observer accepted")
	}
	path := db.Path()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	replay, err := reopened.ReplayResultRequest(t.Context(), b, "original", resultRecord(b, "original", 0).Payload)
	if err != nil {
		t.Fatal(err)
	}
	if replay.AppliedRevision != 1 || len(replay.Observations) != 3 {
		t.Fatal("replay lost original receipt")
	}
	snapshot, err := reopened.GetResultRevision(t.Context(), b, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snapshot.Snapshot, original) {
		t.Fatal("retained bytes aliased or changed")
	}
	if _, err = reopened.ReplayResultRequest(t.Context(), b, "original", []byte(`{"different":true}`)); !errors.Is(err, ErrResultCorrelation) {
		t.Fatal("collision not rejected")
	}
	revisions, err := reopened.ResultRevisionNumbers(t.Context(), b)
	if err != nil || !reflect.DeepEqual(revisions, []int64{1, 2}) {
		t.Fatal("history did not survive", revisions, err)
	}
}
func TestResultRecord_RejectionAndV2Isolation(t *testing.T) {
	db, a := submissionFixture(t, "engineering")
	proof := submissionSnapshot(a, "prior-proof")
	if err := db.RetainSubmissionSnapshot(t.Context(), proof); err != nil {
		t.Fatal(err)
	}
	check := SubmissionCheck{RunID: a.RunID, Attempt: a.Attempt, Reference: "prior-check", SnapshotRef: proof.Reference, Name: "synthetic retained fact", Executed: true, Passed: true, Provenance: []byte(`{"source":"synthetic fixture"}`), Result: []byte(`{"result":"fixture"}`)}
	if err := db.RetainSubmissionCheck(t.Context(), check); err != nil {
		t.Fatal(err)
	}
	frozenBefore, err := db.ListApprovedStoryRevisions(t.Context(), a.Parent, a.Generation)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "v2-original"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.BindResultAttempt(t.Context(), ResultAttempt{Task: a.Parent, Mode: "implement", WorkContext: "separate", Attempt: "application", RunID: a.RunID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	beforePending, err := db.ListPendingSubmissions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	q := resultRecord(b, "accepted", 0)
	q.SubmissionReceiptID = v2.ID
	r, err := db.RecordResult(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	if r.SubmissionReceiptID != v2.ID {
		t.Fatal("factory receipt association lost")
	}
	rejected := resultRecord(b, "rejected", 1)
	rejected.Accepted = false
	rejected.Snapshot = nil
	rejected.Retired = nil
	rejected.Payload = []byte(`malformed input`)
	rejected.Diagnostics = []byte(`{"valid":false}`)
	rr, err := db.RecordResult(t.Context(), rejected)
	if err != nil {
		t.Fatal(err)
	}
	if rr.AppliedRevision != 0 || rr.Latest.Input != "rejected" {
		t.Fatal("rejection became applied")
	}
	if _, err = db.ObserveResult(t.Context(), b, b.RunID, "rejected", "engineering", "recorded"); err == nil {
		t.Fatal("rejected input acquired successful observation")
	}
	after, err := db.GetSubmissionReceipt(t.Context(), a.RunID, "v2-original")
	if err != nil || !reflect.DeepEqual(v2, after) {
		t.Fatal("v2 receipt changed", err)
	}
	pending, err := db.ListPendingSubmissions(t.Context())
	if err != nil || !reflect.DeepEqual(beforePending, pending) {
		t.Fatal("application submissions entered v2 queue", err)
	}
	retainedProof, err := db.GetSubmissionSnapshot(t.Context(), a.RunID, proof.Reference)
	if err != nil || !reflect.DeepEqual(proof, retainedProof) {
		t.Fatal("retained proof changed", err)
	}
	retainedCheck, err := db.GetSubmissionCheck(t.Context(), a.RunID, check.Reference)
	if err != nil || !reflect.DeepEqual(check, retainedCheck) {
		t.Fatal("retained check changed", err)
	}
	frozenAfter, err := db.ListApprovedStoryRevisions(t.Context(), a.Parent, a.Generation)
	if err != nil || !reflect.DeepEqual(frozenBefore, frozenAfter) {
		t.Fatal("frozen scope changed", err)
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), b)
	if err != nil || !reflect.DeepEqual(revisions, []int64{1}) {
		t.Fatal("rejection changed revisions")
	}
}
