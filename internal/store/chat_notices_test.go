package store

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNoticeAdmissionRollbackAndReplay(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	if err := db.InitNoticePositions(ctx, "test", at); err != nil {
		t.Fatal(err)
	}
	before, _, err := db.ListNoticeRecords(ctx, "test", "transitions", at)
	if err != nil {
		t.Fatal(err)
	}
	after := before
	after.LastID = 12
	n := ChatNotice{TaskRef: "github:acme/repo#1", ChatReply: ChatReply{Transport: "test", MessageID: "transition:12", Command: "stage changed", ChannelRef: "3", CreatedAt: at, ReplyText: "hello"}}
	bad := n
	bad.MessageID = ""
	if err := db.AdmitNotices(ctx, "test", before, after, []ChatNotice{n, bad}); err == nil {
		t.Fatal("invalid insert committed")
	}
	got, _, err := db.ListNoticeRecords(ctx, "test", "transitions", at)
	if err != nil || !reflect.DeepEqual(got, before) {
		t.Fatalf("rollback position=%+v err=%v", got, err)
	}
	if _, err := db.ReadChatNotice(ctx, "test", n.MessageID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial notice survived: %v", err)
	}
	if err := db.AdmitNotices(ctx, "test", before, after, []ChatNotice{n, n}); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitNotices(ctx, "test", before, after, []ChatNotice{n}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale position accepted: %v", err)
	}
	got, _, err = db.ListNoticeRecords(ctx, "test", "transitions", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitNotices(ctx, "test", got, got, []ChatNotice{n}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM chat_notices`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestNoticeLeaseFenceAndRetention(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	if err := db.InitNoticePositions(ctx, "test", at); err != nil {
		t.Fatal(err)
	}
	p, _, err := db.ListNoticeRecords(ctx, "test", "transitions", at)
	if err != nil {
		t.Fatal(err)
	}
	notices := []ChatNotice{}
	for _, key := range []string{"confirmed", "failed"} {
		notices = append(notices, ChatNotice{TaskRef: "github:acme/repo#1", ChatReply: ChatReply{Transport: "test", MessageID: key, Command: "started", ChannelRef: "3", CreatedAt: at, ReplyText: "private text"}})
	}
	if err := db.AdmitNotices(ctx, "test", p, p, notices); err != nil {
		t.Fatal(err)
	}
	first, err := db.ClaimChatNotice(ctx, "test", at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.ClaimChatNotice(ctx, "test", at)
	if err != nil || first.MessageID == second.MessageID {
		t.Fatalf("claim second: %+v %v", second, err)
	}
	if _, err := db.ClaimChatNotice(ctx, "test", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lease ignored: %v", err)
	}
	reclaimed, err := db.ClaimChatNotice(ctx, "test", at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishChatNotice(ctx, first, false, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale worker finished: %v", err)
	}
	if err := db.FinishChatNotice(ctx, reclaimed, false, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishChatNotice(ctx, second, true, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, n := range notices {
		row, err := db.ReadChatNotice(ctx, "test", n.MessageID)
		if err != nil || row.ReplyText != "" || row.FinishedAt.IsZero() {
			t.Fatalf("terminal text: %+v %v", row, err)
		}
	}
	if err := db.PurgeChatNotices(ctx, at.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadChatNotice(ctx, "test", first.MessageID); err != nil {
		t.Fatalf("purged too early: %v", err)
	}
	if err := db.PurgeChatNotices(ctx, at.Add(24*time.Hour+time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, n := range notices {
		if _, err := db.ReadChatNotice(ctx, "test", n.MessageID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("not purged: %v", err)
		}
	}
	if _, _, err := db.ListNoticeRecords(ctx, "test", "transitions", at); err != nil {
		t.Fatalf("positions purged: %v", err)
	}
}

func TestNoticeQuestionUsesReferencedDiagnosis(t *testing.T) {
	t.Parallel()
	db, _, instance, _ := persistentBugStopFixture(t)
	ctx := t.Context()
	at := graphTestTime()
	attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "repair", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishGraphNodeAttempt(ctx, attempt.ID, GraphSucceeded, "done", at); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"record":"diagnosis","revision":1,"digest":"original","analysis":{"questions":[{"id":"Q1","text":"Which boundary?","material":true}],"summary":"Do not post analysis prose"}}`, `{"record":"diagnosis","revision":2,"digest":"newer","analysis":{"questions":[{"id":"Q2","text":"New unrelated question?","material":true}]}}`} {
		if _, err := db.RecordGraphOutput(ctx, GraphOutput{NodeAttemptID: attempt.ID, Kind: "json", Payload: []byte(raw), CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.InitNoticePositions(ctx, "test", at); err != nil {
		t.Fatal(err)
	}
	if err := db.ParkGraphNode(ctx, GraphStop{Key: "diagnosis-wait", InstanceID: instance.ID, Origin: "repair", Cause: "diagnosis_question", Reason: "Whole analysis and questions", Candidate: []byte(`{"diagnosis":{"revision":1,"digest":"original"},"reason":"Whole analysis and questions"}`), At: at, AllowRunless: true}); err != nil {
		t.Fatal(err)
	}
	p, records, err := db.ListNoticeRecords(ctx, "test", "owner_waits", at)
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if p.InitialID != 0 {
		t.Fatal("baseline moved after initialization")
	}
	candidate := string(records[0].Candidate)
	if !strings.Contains(candidate, "Which boundary?") || strings.Contains(candidate, "unrelated") || strings.Contains(candidate, "analysis prose") {
		t.Fatalf("incorrect revision or report prose: %s", candidate)
	}
}

func TestNoticePositionFindsRecycledGraphIDs(t *testing.T) {
	t.Parallel()
	db, _, instance, session := persistentBugStopFixture(t)
	ctx := t.Context()
	at := graphTestTime()
	old, err := db.RecordGraphTransition(ctx, GraphTransition{InstanceID: instance.ID, FromNode: "a", ToNode: "b", OnOutcome: "done", At: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InitNoticePositions(ctx, "test", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	before, rows, err := db.ListNoticeRecords(ctx, "test", "transitions", at.Add(time.Second))
	if err != nil || len(rows) != 0 || before.InitialID != old.ID {
		t.Fatalf("first enable replayed or saved wrong baseline: %+v %v", rows, err)
	}
	// Archival removes append-only graph history while preserving read positions.
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM graph_transitions WHERE id=?`, old.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.RecordGraphTransition(ctx, GraphTransition{InstanceID: instance.ID, FromNode: "b", ToNode: "c", OnOutcome: "done", At: at.Add(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID != old.ID {
		t.Fatal("fixture did not recycle the graph ID")
	}
	before, rows, err = db.ListNoticeRecords(ctx, "test", "transitions", at.Add(2*time.Second))
	if err != nil || len(rows) != 1 {
		t.Fatalf("fresh recycled record lost: %+v %v", rows, err)
	}
	after := before
	after.Seen = map[string]string{strconv.FormatInt(fresh.ID, 10): session.TaskRef + "@" + FormatTime(fresh.At)}
	if err := db.AdmitNotices(ctx, "test", before, after, nil); err != nil {
		t.Fatal(err)
	}
	_, rows, err = db.ListNoticeRecords(ctx, "test", "transitions", at.Add(3*time.Second))
	if err != nil || len(rows) != 0 {
		t.Fatalf("recycled record replayed: %+v %v", rows, err)
	}
}

func TestNoticeRunStartsReadPreviousMode(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 675)
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	previous := ""
	for i, mode := range []string{"research", "plan", "implement", "implement"} {
		run, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: mode, Agent: AgentClaude, TriggerCommentID: int64(100 + i), LogPath: func(n int) string { return "/logs/" + strconv.Itoa(n) + ".jsonl" }})
		if err != nil {
			t.Fatal(err)
		}
		want[strconv.FormatInt(run.ID, 10)] = previous
		previous = mode
	}
	// A prelaunch allocation never crossed the execution boundary: not a previous run.
	if _, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "validate", Agent: AgentClaude, TriggerCommentID: 200, Prelaunch: true, LogPath: func(n int) string { return "/logs/" + strconv.Itoa(n) + ".jsonl" }}); err != nil {
		t.Fatal(err)
	}
	last, err := db.AllocateRun(ctx, AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "validate", Agent: AgentClaude, TriggerCommentID: 201, LogPath: func(n int) string { return "/logs/" + strconv.Itoa(n) + ".jsonl" }})
	if err != nil {
		t.Fatal(err)
	}
	want[strconv.FormatInt(last.ID, 10)] = "implement"
	for id, previous := range want {
		n, _ := strconv.ParseInt(id, 10, 64)
		r, err := db.readNoticeRecord(ctx, "run_starts", n)
		if err != nil || r.PreviousMode != previous {
			t.Fatalf("run %s previous=%q want %q err=%v", id, r.PreviousMode, previous, err)
		}
	}
}

func TestNoticeOwnerWaitsReadStateAndPublication(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 675)
	at := graphTestTime()
	definition, err := db.CreateGraphDefinition(ctx, GraphDefinition{Team: "notices", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(key, kind string, comment int64) int64 {
		t.Helper()
		instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "build", StartedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := db.StartGraphNodeAttempt(ctx, GraphAttemptStart{InstanceID: instance.ID, Node: "build", StartedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.FinishGraphNodeAttempt(ctx, attempt.ID, GraphSucceeded, kind, at); err != nil {
			t.Fatal(err)
		}
		if err := db.RetainGraphComment(ctx, GraphCommentIntent{Key: key, InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "build", Kind: kind, Candidate: []byte(`{"questions":[{"text":"Which release?"}]}`)}); err != nil {
			t.Fatal(err)
		}
		if comment > 0 {
			if _, err := db.EnsureReportDelivery(ctx, ReportDelivery{Key: key, SessionID: session.ID, Task: session.TaskRef, Destination: "acme/repo#675", TriggerNamespace: "issue-comment", Purpose: "implement-" + kind, BotUserID: 99, Body: kind, Marker: "marker-" + key}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.MarkReportDeliverySent(ctx, key); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ConfirmReportDelivery(ctx, key, comment); err != nil {
				t.Fatal(err)
			}
			if _, err := db.CompleteReportDelivery(ctx, key); err != nil {
				t.Fatal(err)
			}
		}
		var id int64
		if err := db.sql.QueryRowContext(ctx, `SELECT rowid FROM graph_owner_waits WHERE delivery_key=?`, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	read := func(id int64) NoticeRecord {
		t.Helper()
		r, err := db.readNoticeRecord(ctx, "owner_waits", id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, tc := range []struct {
		key, kind string
		comment   int64
	}{{"question", "question", 91}, {"review", "review", 92}} {
		r := read(wait(tc.key, tc.kind, tc.comment))
		if r.Ignore || !r.Ready || !r.Published || r.State != "waiting" || r.Kind != tc.kind || r.CommentID != tc.comment {
			t.Fatalf("%s published wait=%+v", tc.kind, r)
		}
	}
	// An unpublished wait stays pending until it is published or stops waiting.
	for _, state := range []string{"cancelled", "allocated"} {
		id := wait("unpublished-"+state, "question", 0)
		if r := read(id); r.Ready || r.Published || r.State != "waiting" {
			t.Fatalf("undelivered wait=%+v", r)
		}
		if _, err := db.sql.ExecContext(ctx, `UPDATE graph_owner_waits SET state=? WHERE rowid=?`, state, id); err != nil {
			t.Fatal(err)
		}
		if r := read(id); !r.Ready || r.Published || r.State != state {
			t.Fatalf("%s wait=%+v", state, r)
		}
	}
}
