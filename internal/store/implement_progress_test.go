package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func progressWorker(t *testing.T, db *DB, issue int, key string) ResultBinding {
	t.Helper()
	s := testSession(t, db, issue)
	b, err := db.BindResultAttempt(t.Context(), resultRun(t, db, s, "implement", "worker", 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureImplementStories(t.Context(), "github:o/r#1", 1, []ImplementStorySeed{{Key: key, Branch: "lf/" + key}}, implementT0); err != nil {
		t.Fatal(err)
	}
	if _, err := move(t, db, key, ImplementWaiting, ImplementRunning, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, key, s.ID, implementT0); err != nil {
		t.Fatal(err)
	}
	return b
}

func beginProgress(t *testing.T, db *DB, b ResultBinding, id string, seq int64, kind, message string) {
	t.Helper()
	raw, err := json.Marshal(struct {
		Sequence      int64
		Kind, Message string
	}{seq, kind, message})
	if err != nil {
		t.Fatal(err)
	}
	repo := strings.TrimPrefix(strings.Split(b.Task, "#")[0], "github:")
	_, _, err = db.BeginFamilyRequest(t.Context(), b, FamilyRequest{Scope: FamilyScope{Repository: repo, Task: b.Task, RunID: b.RunID, Attempt: b.Attempt}, RequestID: id, Tool: "activity_update", PayloadHash: submissionDigest(raw)})
	if err != nil {
		t.Fatal(err)
	}
}
func writeProgress(t *testing.T, db *DB, b ResultBinding, id string, seq int64, kind, message string) ImplementProgressOutcome {
	t.Helper()
	beginProgress(t, db, b, id, seq, kind, message)
	out, err := db.RecordImplementProgress(t.Context(), b, id, seq, kind, message)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func progressReceipt(t *testing.T, db *DB, b ResultBinding, id string) FamilyRequest {
	t.Helper()
	repo := strings.TrimPrefix(strings.Split(b.Task, "#")[0], "github:")
	r, err := db.ReadFamilyRequest(t.Context(), b, []FamilyScope{{Repository: repo, Task: b.Task, RunID: b.RunID, Attempt: b.Attempt}}, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestImplementProgressSequenceValidationAndReceipt(t *testing.T) {
	db := newTestDB(t)
	b := progressWorker(t, db, 801, "a")
	cases := []struct {
		id                           string
		seq                          int64
		kind, msg, disposition, code string
	}{
		{"missing", 1, "", "ok", "rejected", "kind_required"},
		{"kind", 1, "unknown", "ok", "rejected", "invalid_progress"},
		{"zero", 0, "testing", "ok", "rejected", "invalid_progress"},
		{"blank", 1, "testing", " \t", "rejected", "invalid_progress"},
		{"utf8", 1, "testing", string([]byte{255}), "rejected", "invalid_progress"},
		{"bytes", 1, "testing", strings.Repeat("é", 1025), "rejected", "message_limit"},
		{"first", 3, "implementation", strings.Repeat("é", 1024), "accepted", ""},
		{"replay", 3, "implementation", strings.Repeat("é", 1024), "replayed", ""},
		{"conflict-text", 3, "implementation", "changed", "rejected", "sequence_conflict"},
		{"conflict-kind", 3, "repair", strings.Repeat("é", 1024), "rejected", "sequence_conflict"},
		{"stale", 2, "testing", "old", "stale", ""},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			out := writeProgress(t, db, b, c.id, c.seq, c.kind, c.msg)
			if out.Disposition != c.disposition || out.Code != c.code {
				t.Fatalf("outcome %+v", out)
			}
			r := progressReceipt(t, db, b, c.id)
			want := "confirmed"
			if c.disposition == "rejected" {
				want = "rejected"
			}
			if r.Disposition != want {
				t.Fatalf("receipt %+v", r)
			}
			if out.Report.ID > 0 && (r.Reference.Kind != "activity" || r.Reference.ID != out.Report.ID) {
				t.Fatalf("unlinked receipt %+v", r)
			}
		})
	}
	_, n, err := db.ImplementProgressForAttempt(t.Context(), b)
	if err != nil || n != 1 {
		t.Fatalf("count %d %v", n, err)
	}
}

func TestImplementProgressAtomicFailureAndLostReply(t *testing.T) {
	db := newTestDB(t)
	b := progressWorker(t, db, 802, "a")
	beginProgress(t, db, b, "lost", 1, "testing", "checking")
	// Failure after insertion but before receipt confirmation rolls both back.
	_, err := db.sql.ExecContext(t.Context(), `CREATE TRIGGER progress_fail BEFORE UPDATE ON family_requests WHEN NEW.reference_kind='activity' BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordImplementProgress(t.Context(), b, "lost", 1, "testing", "checking"); err == nil {
		t.Fatal("failure not injected")
	}
	_, n, err := db.ImplementProgressForAttempt(t.Context(), b)
	if err != nil || n != 0 {
		t.Fatalf("partial report %d %v", n, err)
	}
	if r := progressReceipt(t, db, b, "lost"); r.Disposition != "pending" {
		t.Fatalf("partial receipt %+v", r)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DROP TRIGGER progress_fail`); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishFamilyRequest(t.Context(), b, "lost", "uncertain", FamilyReference{}); err != nil {
		t.Fatal(err)
	}
	out, err := db.RecordImplementProgress(t.Context(), b, "lost", 1, "testing", "checking")
	if err != nil || out.Disposition != "accepted" {
		t.Fatalf("retry %+v %v", out, err)
	}
	// Simulate loss of the reply, then repeat the known identity.
	out, err = db.RecordImplementProgress(t.Context(), b, "lost", 1, "testing", "checking")
	if err != nil || out.Disposition != "replayed" {
		t.Fatalf("lost reply %+v %v", out, err)
	}
	// Recovery also repairs a historical uncertain receipt when the row exists.
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE family_requests SET disposition='uncertain',reference_kind='',reference_id=0 WHERE request_id='lost'`); err != nil {
		t.Fatal(err)
	}
	out, err = db.RecordImplementProgress(t.Context(), b, "lost", 1, "testing", "checking")
	if err != nil || out.Disposition != "replayed" {
		t.Fatalf("recovery %+v %v", out, err)
	}
	if r := progressReceipt(t, db, b, "lost"); r.Disposition != "confirmed" || r.Reference.ID != out.Report.ID {
		t.Fatalf("recovery receipt %+v", r)
	}
	_, n, err = db.ImplementProgressForAttempt(t.Context(), b)
	if err != nil || n != 1 {
		t.Fatalf("duplicate %d %v", n, err)
	}
}

func TestImplementProgressAuthority(t *testing.T) {
	for _, change := range []string{"replaced", "wrong-session", "parked", "revoked"} {
		t.Run(change, func(t *testing.T) {
			db := newTestDB(t)
			b := progressWorker(t, db, 803, "a")
			beginProgress(t, db, b, "report", 1, "repair", "fix")
			switch change {
			case "replaced", "wrong-session":
				if _, err := db.sql.ExecContext(t.Context(), `UPDATE implement_stories SET worker_session=0`); err != nil {
					t.Fatal(err)
				}
			case "parked":
				if _, err := move(t, db, "a", ImplementRunning, ImplementParked, 2, func(in *ImplementTransition) { in.Reason = "park" }); err != nil {
					t.Fatal(err)
				}
			case "revoked":
				if err := db.EndResultAttempt(t.Context(), b); err != nil {
					t.Fatal(err)
				}
			}
			out, err := db.RecordImplementProgress(t.Context(), b, "report", 1, "repair", "fix")
			if change == "revoked" {
				if !errors.Is(err, ErrResultAuthority) {
					t.Fatalf("authority %v", err)
				}
			} else if err != nil || out.Code != "authority" {
				t.Fatalf("authority %+v %v", out, err)
			}
			var n int
			if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM implement_progress_reports`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("unauthorized write %d %v", n, err)
			}
		})
	}
}

func TestImplementProgressBudgetAndConcurrentRetry(t *testing.T) {
	db := newTestDB(t)
	b := progressWorker(t, db, 804, "a")
	writeProgress(t, db, b, "first", 1, "testing", "check")
	_, err := db.sql.ExecContext(t.Context(), `WITH RECURSIVE seq(n) AS (SELECT 2 UNION ALL SELECT n+1 FROM seq WHERE n<1024) INSERT INTO implement_progress_reports(id,parent,generation,story_key,run_id,attempt,sequence,kind,message,message_hash,story_attempts,recorded_at) SELECT n,parent,generation,story_key,run_id,attempt,n,kind,message,message_hash,story_attempts,recorded_at FROM implement_progress_reports CROSS JOIN seq WHERE sequence=1`)
	if err != nil {
		t.Fatal(err)
	}
	if out := writeProgress(t, db, b, "over", 1025, "testing", "check"); out.Code != "update_limit" {
		t.Fatalf("budget %+v", out)
	}
	if out := writeProgress(t, db, b, "repeat", 1, "testing", "check"); out.Disposition != "replayed" {
		t.Fatalf("replay at limit %+v", out)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		id := fmt.Sprintf("concurrent-%d", i)
		beginProgress(t, db, b, id, 1, "testing", "check")
		wg.Go(func() {
			out, err := db.RecordImplementProgress(t.Context(), b, id, 1, "testing", "check")
			if err != nil || out.Disposition != "replayed" {
				t.Errorf("concurrent %+v %v", out, err)
			}
		})
	}
	wg.Wait()
	_, n, err := db.ImplementProgressForAttempt(t.Context(), b)
	if err != nil || n != 1024 {
		t.Fatalf("budget count %d %v", n, err)
	}
}

// Snapshot every workflow table so advisory words cannot mutate engineering,
// acceptance, scheduling, result submissions or remote operation queues.
func progressWorkflowSnapshot(t *testing.T, db *DB) map[string]string {
	t.Helper()
	rows, err := db.sql.QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type='table' AND name NOT IN ('implement_progress_reports','family_requests') ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	result := map[string]string{}
	for _, name := range names {
		r, err := db.sql.QueryContext(t.Context(), `SELECT * FROM "`+name+`"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := r.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for r.Next() {
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := r.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&text, "%#v\n", values)
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
		r.Close()
		result[name] = text.String()
	}
	return result
}
func TestImplementProgressLatestRestartAndAdvisoryOnly(t *testing.T) {
	db := newTestDB(t)
	a := progressWorker(t, db, 805, "a")
	b := progressWorker(t, db, 806, "b")
	if _, err := db.RecordImplementFailure(t.Context(), "github:o/r#1", 1, "a", "repair needed", implementT0); err != nil {
		t.Fatal(err)
	}
	before := progressWorkflowSnapshot(t, db)
	for i, kind := range []string{"implementation", "testing", "repair"} {
		writeProgress(t, db, a, kind, int64(i+1), kind, "done")
	}
	writeProgress(t, db, a, "new", 4, "implementation", "newer")
	writeProgress(t, db, b, "other", 1, "testing", "other story")
	after := progressWorkflowSnapshot(t, db)
	if !reflect.DeepEqual(before, after) {
		for k, v := range before {
			if after[k] != v {
				t.Errorf("workflow table changed: %s", k)
			}
		}
	}
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reports, err := reopened.LatestImplementProgress(t.Context(), "github:o/r#1", 1)
	if err != nil || len(reports) != 4 {
		t.Fatalf("latest %+v %v", reports, err)
	}
	for _, r := range reports {
		if r.StoryKey == "a" && (r.StoryAttempts != 1 || r.Kind == "implementation" && r.Sequence != 4) {
			t.Fatalf("latest repair %+v", r)
		}
	}
	for _, scope := range []struct {
		parent     string
		generation int64
	}{{"github:o/r#2", 1}, {"github:o/r#1", 2}} {
		r, err := reopened.LatestImplementProgress(t.Context(), scope.parent, scope.generation)
		if err != nil || len(r) != 0 {
			t.Fatalf("scope leak %+v %v", r, err)
		}
	}
	r, n, err := reopened.ImplementProgressForAttempt(t.Context(), a)
	if err != nil || n != 4 || r.Sequence != 4 {
		t.Fatalf("restart %+v %d %v", r, n, err)
	}
}

func TestImplementProgressMembershipWithRetainedGeneration(t *testing.T) {
	db := newTestDB(t)
	b := progressWorker(t, db, 807, "a")
	run, err := db.GetRun(t.Context(), b.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureImplementStories(t.Context(), "github:o/r#1", 2, []ImplementStorySeed{{Key: "a", Branch: "lf/a-new"}}, implementT0); err != nil {
		t.Fatal(err)
	}
	// The old generation remains advisory history. Membership means a matching
	// ledger row exists, rather than requiring every historical row to disappear.
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE implement_stories SET worker_session=? WHERE generation=2`, run.SessionID); err != nil {
		t.Fatal(err)
	}
	worker, err := db.IsImplementProgressWorker(t.Context(), b)
	if err != nil || !worker {
		t.Fatalf("retained generation hides worker: %v %v", worker, err)
	}
	out := writeProgress(t, db, b, "active", 1, "testing", "working")
	if out.Disposition != "accepted" || out.Report.Generation != 1 {
		t.Fatalf("running generation %+v", out)
	}
}

func TestImplementProgressReceiptSurvivesCompaction(t *testing.T) {
	db := newTestDB(t)
	a := progressWorker(t, db, 808, "a")
	b := progressWorker(t, db, 809, "b")
	writeProgress(t, db, a, "first", 1, "implementation", "first")
	accepted := writeProgress(t, db, b, "retained", 1, "testing", "retained")
	// Create a physical rowid gap, as removal of an archived family can do.
	if _, err := db.sql.ExecContext(t.Context(), `DELETE FROM implement_progress_reports WHERE run_id=?`, a.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	replay, err := db.RecordImplementProgress(t.Context(), b, "retained", 1, "testing", "retained")
	if err != nil || replay.Disposition != "replayed" || replay.Report.ID != accepted.Report.ID {
		t.Fatalf("compacted replay %+v %v", replay, err)
	}
	receipt := progressReceipt(t, db, b, "retained")
	if receipt.Disposition != "confirmed" || receipt.Reference.ID != accepted.Report.ID {
		t.Fatalf("compacted receipt %+v", receipt)
	}
}
