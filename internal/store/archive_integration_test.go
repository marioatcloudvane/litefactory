package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/archive"
)

func archiveITSeed(t *testing.T, db *DB) ArchiveFamily {
	t.Helper()
	repo := testRepo(t, db, "acme", "api")
	s, err := db.GetOrCreateSession(t.Context(), repo, 213)
	if err != nil {
		t.Fatal(err)
	}
	for i, body := range []any{"one", []byte{0, 255, 1}, nil} {
		var user any = int64(i)
		if i == 1 {
			user = 2.5
		}
		if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO comments(session_id,forge_comment_id,author_handle,author_user_id,author_is_bot,body,created_at,classification,tag,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			s.ID, 100+i, "a", user, 0, body, "t", "instruction", nil, "r"); err != nil && body != nil {
			t.Fatal(err) // body is NOT NULL, the nil row is expected to be refused
		}
	}
	return ArchiveFamily{SessionID: s.ID, RepoID: repo.ID, TaskRef: s.TaskRef}
}

func archiveITTrailer(t *testing.T, db *DB, f ArchiveFamily) archive.Trailer {
	t.Helper()
	d := archive.NewDigester()
	if err := db.StreamArchiveRows(t.Context(), f, func(r archive.Row) error { _, err := d.Add(r); return err }); err != nil {
		t.Fatal(err)
	}
	return d.Trailer()
}

func archiveITCount(t *testing.T, db *DB) int64 {
	t.Helper()
	var n int64
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM comments`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestArchiveIntegrationRegistryIsCompleteOnMigratedDatabase(t *testing.T) {
	db := newTestDB(t)
	problems, err := db.ArchiveRegistryProblems(t.Context())
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems %v %v", problems, err)
	}
}

func TestArchiveIntegrationDeleteRequiresExactDigest(t *testing.T) {
	db := newTestDB(t)
	f := archiveITSeed(t, db)
	want := archiveITTrailer(t, db, f)
	total := archiveITCount(t, db)
	if want.Counts["comments"] != total || total != 2 {
		t.Fatalf("counts %v total %d", want.Counts, total)
	}
	now := time.Now()

	// Wrong digest, wrong count: rolled back, nothing deleted.
	bad := want
	bad.Digest = "0000"
	if _, err := db.DeleteArchivedFamily(t.Context(), f, bad, now); !errors.Is(err, ErrArchiveDigestMismatch) {
		t.Fatalf("digest err %v", err)
	}
	bad = archive.Trailer{Kind: want.Kind, Digest: want.Digest, Digests: want.Digests, Counts: map[string]int64{"comments": total + 1}}
	if _, err := db.DeleteArchivedFamily(t.Context(), f, bad, now); !errors.Is(err, ErrArchiveDigestMismatch) {
		t.Fatalf("count err %v", err)
	}
	if archiveITCount(t, db) != total {
		t.Fatal("rows deleted by a refused transaction")
	}

	// A row changed after the trailer was taken.
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE comments SET author_user_id=3.0000001 WHERE forge_comment_id=101`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DeleteArchivedFamily(t.Context(), f, want, now); !errors.Is(err, ErrArchiveDigestMismatch) {
		t.Fatalf("changed row err %v", err)
	}
	if archiveITCount(t, db) != total {
		t.Fatal("rows deleted despite changed row")
	}

	// Exact match deletes exactly what the file holds, and only that.
	want = archiveITTrailer(t, db, f)
	other, err := db.GetOrCreateSession(t.Context(), testRepo(t, db, "acme", "web"), 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO comments(session_id,forge_comment_id,author_handle,author_is_bot,body,created_at,classification,recorded_at) VALUES(?,1,'z',0,'keep','t','self','r')`, other.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.DeleteArchivedFamily(t.Context(), f, want, now)
	if err != nil || !reflect.DeepEqual(deleted, map[string]int64{"comments": 2}) {
		t.Fatalf("deleted %v %v", deleted, err)
	}
	if archiveITCount(t, db) != 1 {
		t.Fatal("unrelated task's row was touched")
	}
	// Second delete of the now-empty family is a digest mismatch, never a silent no-op.
	if _, err := db.DeleteArchivedFamily(t.Context(), f, want, now); !errors.Is(err, ErrArchiveDigestMismatch) {
		t.Fatalf("repeat err %v", err)
	}
	// Session skeleton is still there.
	if _, err := db.GetSession(t.Context(), f.SessionID); err != nil {
		t.Fatalf("skeleton lost: %v", err)
	}
}

func TestArchiveIntegrationSweepStateRoundTripsAndRejectsInvalid(t *testing.T) {
	db := newTestDB(t)
	if s, err := db.GetArchiveSweep(t.Context()); err != nil || s.ID != "" {
		t.Fatalf("initial %+v %v", s, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	in := ArchiveSweep{ID: "p1", StartedAt: now, Boundary: 9, Cursor: 4, Owners: 4, Counts: map[string]int{"archived": 2}, Deferred: 1, DeletedAny: true, Complete: true, NextDue: now.Add(24 * time.Hour)}
	if err := db.SaveArchiveSweep(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	out, err := db.GetArchiveSweep(t.Context())
	if err != nil || !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip %+v %v", out, err)
	}
	for _, bad := range []ArchiveSweep{{}, {ID: "x"}, {ID: "x", StartedAt: now, Cursor: 5, Boundary: 4}} {
		if err := db.SaveArchiveSweep(t.Context(), bad); err == nil {
			t.Fatalf("accepted invalid %+v", bad)
		}
	}
}

func TestArchiveIntegrationCompactOnEmptyWorkIsHarmless(t *testing.T) {
	db := newTestDB(t)
	r, err := db.Compact(t.Context())
	if err != nil || r.Before <= 0 || r.After <= 0 {
		t.Fatalf("compact %+v %v", r, err)
	}
}

// A closed issue whose bookkeeping is still pending (here an undelivered
// report) was kept by the sweep forever; #385, #406, #430 and #453 held about
// 235 MB this way. Once the forge confirms the issue closed, only active work
// blocks the delete; the archive file keeps every row. A finished run alone is
// no longer pending bookkeeping (#600).
func TestArchiveIntegrationClosedIssueIgnoresPendingBookkeeping(t *testing.T) {
	db := newTestDB(t)
	f := archiveITSeed(t, db)
	segment, err := db.GetOrCreateSegment(t.Context(), f.SessionID, 1, "s", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: f.SessionID, SegmentID: segment.ID, Mode: "research", Agent: AgentClaude, TriggerCommentID: 100, LogPath: func(int) string { return "/logs/1.jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: "pending-report", Version: 1, SessionID: f.SessionID, TriggerID: 100, Task: f.TaskRef, Destination: f.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 99, Body: "report", Marker: "marker"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	want := archiveITTrailer(t, db, f)
	if _, err := db.DeleteArchivedFamily(t.Context(), f, want, now); !errors.Is(err, ErrArchiveNotSettled) {
		t.Fatalf("pending family deleted without a closed issue: %v", err)
	}
	f.IssueClosed = true
	if _, err := db.DeleteArchivedFamily(t.Context(), f, want, now); err != nil {
		t.Fatalf("closed issue with only pending bookkeeping not deleted: %v", err)
	}
	if archiveITCount(t, db) != 0 {
		t.Fatal("archived comments still present")
	}
}

func TestArchiveOwnerInputHistoryOrdersForeignKeysAndKeepsUndeliveredLive(t *testing.T) {
	db := newTestDB(t)
	i := graphTestInstance(t, db)
	session, err := db.GetSession(t.Context(), i.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	family := ArchiveFamily{SessionID: session.ID, RepoID: session.RepoID, TaskRef: session.TaskRef, IssueClosed: true}
	// A terminal family still owns its stranded instruction; closure does not
	// withdraw one-time direction that has not reached a logical step.
	recordOwnerInput(t, db, session.ID, 20, "Keep the explanation for the next worker.")
	if _, err = db.RetainOwnerInstruction(t.Context(), session.ID, 20, 42, "Keep the explanation for the next worker.", OwnerInputTarget{}); err != nil {
		t.Fatal(err)
	}
	if err = db.SetGraphInstanceStatus(t.Context(), i.ID, GraphSucceeded, time.Time{}, graphTestTime()); err != nil {
		t.Fatal(err)
	}
	owner, err := db.GetArchiveOwner(t.Context(), session.ID, graphTestTime())
	if err != nil || owner.UndeletableReason != "owner_instruction_undelivered" {
		t.Fatalf("pending input not visible archive debt: %+v %v", owner, err)
	}
	trailer := archiveITTrailer(t, db, family)
	if _, err = db.DeleteArchivedFamily(t.Context(), family, trailer, graphTestTime()); !errors.Is(err, ErrArchiveNotSettled) {
		t.Fatalf("closed family discarded pending input: %v", err)
	}
	pending, err := db.ListOwnerInstructions(t.Context(), session.ID, 0)
	if err != nil || len(pending) != 1 {
		t.Fatalf("stranded input lost: %+v %v", pending, err)
	}

	// A later eligible instance takes the input; completed history may then move
	// as a whole, with bindings before displays and snapshots before guidance.
	next, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: i.DefinitionID, CurrentNode: "work", StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, session.ID, 30)
	attempt, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: next.ID, Node: "work", RunID: run.ID, AcceptOwnerInput: true, StartedAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	recordOwnerInput(t, db, session.ID, 40, "/guide Use ISO dates.")
	if _, err = db.SetOwnerGuidance(t.Context(), session.ID, 40, 42, "Use ISO dates."); err != nil {
		t.Fatal(err)
	}
	if _, err = db.OwnerGuidanceForRun(t.Context(), run.ID); err != nil {
		t.Fatal(err)
	}
	display := OwnerDecisionDisplay{TaskRef: session.TaskRef, Action: "/return", Target: "reviewed", Revision: "revision", InstanceID: next.ID, PublicationID: 50, CreatedAt: graphTestTime()}
	if err = db.PutOwnerDecisionDisplay(t.Context(), display); err != nil {
		t.Fatal(err)
	}
	display, err = db.LatestOwnerDecisionDisplay(t.Context(), session.TaskRef, "/return")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PutOwnerDecisionBinding(t.Context(), OwnerDecisionBinding{TaskRef: session.TaskRef, CommentID: 60, AuthorID: 42, Body: "/return -- change date", Normalized: "/return revision -- change date", Display: display, CreatedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.PutValidateReturn(t.Context(), ValidateReturn{InstanceID: next.ID, Parent: session.TaskRef, Generation: 1, Commit: "abc", ReportRevision: "revision", CommentID: 60, AuthorID: 42, Explanation: "change date", Context: "full reviewed context", CreatedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "done", FinishedAt: graphTestTime(), InstanceStatus: GraphSucceeded}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded, FinishedAt: graphTestTime()}); err != nil {
		t.Fatal(err)
	}
	trailer = archiveITTrailer(t, db, family)
	for _, table := range []string{"owner_instruction_inputs", "owner_run_guidance", "issue_owner_guidance", "owner_decision_bindings", "owner_decision_displays", "validate_returns"} {
		if trailer.Counts[table] != 1 {
			t.Fatalf("missing %s from archive: %v", table, trailer.Counts)
		}
	}
	deleted, err := db.DeleteArchivedFamily(t.Context(), family, trailer, graphTestTime())
	if err != nil {
		t.Fatalf("completed owner history failed FK-safe archive: %v", err)
	}
	for _, table := range []string{"owner_instruction_inputs", "owner_run_guidance", "issue_owner_guidance", "owner_decision_bindings", "owner_decision_displays", "validate_returns"} {
		if deleted[table] != 1 {
			t.Fatalf("history %s remained live: %v", table, deleted)
		}
	}
}
