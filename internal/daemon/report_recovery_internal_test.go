package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/naming"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func recoveryUnitFixture() (*Daemon, *bytes.Buffer, store.ReportRecovery) {
	var logs bytes.Buffer
	d := &Daemon{interval: time.Minute, log: slog.New(slog.NewJSONHandler(&logs, nil)), diagnosticRedactor: redact.New(redact.Options{Environ: []string{"TEST_API_KEY=private-recovery-value"}})}
	return d, &logs, store.ReportRecovery{Key: recoveryKey(42, 12), SessionID: 42, TriggerID: 12, Task: "github:owner/repo#1", RunID: 99}
}
func recoveryUnitContext(t *testing.T, at time.Time) context.Context {
	t.Helper()
	return context.WithValue(t.Context(), recoveryTickKey{}, at)
}
func recoveryLogRows(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestReportRecovery_CadenceAndSuppressedTicks(t *testing.T) {
	d, logs, source := recoveryUnitFixture()
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	calls := 0
	attempt := func() (turn.Posted, error) {
		calls++
		return turn.Posted{}, &turn.DeliveryError{Key: "delivery-1", State: "uncertain", Cause: errors.New("private-recovery-value unavailable")}
	}
	delays := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for failed, delay := range delays {
		posted, err := d.reportAttempt(recoveryUnitContext(t, at), source, attempt)
		if !isRecoveryPending(err) || posted.CommentID != 0 {
			t.Fatalf("failure %d returned %+v, %v", failed, posted, err)
		}
		row := d.recoveries.items[source.Key]
		if row.Attempts != failed || !row.NextAt.Equal(at.Add(delay)) || row.Escalated != (failed >= 3) {
			t.Fatalf("failure %d: %+v", failed, row)
		}
		if !d.recoveries.dirty[source.Key] || row.DeliveryKey != "delivery-1" || strings.Contains(row.Reason, "private-recovery-value") {
			t.Fatalf("outage debt not safely retained: %+v", row)
		}
		beforeLogs := logs.Len()
		beforeCalls := calls
		posted, err = d.reportAttempt(recoveryUnitContext(t, row.NextAt.Add(-time.Nanosecond)), source, attempt)
		if !isRecoveryPending(err) || !posted.Skipped || calls != beforeCalls || logs.Len() != beforeLogs || d.recoveries.items[source.Key].Attempts != failed {
			t.Fatal("ineligible tick had retry side effects")
		}
		at = row.NextAt
	}
	rows := recoveryAlertRows(t, logs)
	if len(rows) != 2 || rows[0]["recovery_attempts"] != float64(0) || rows[1]["recovery_attempts"] != float64(3) {
		t.Fatalf("first/third recovery alerts=%v", rows)
	}
	if calls != len(delays) {
		t.Fatalf("eligible attempts=%d", calls)
	}
}

func TestReportRecovery_StateChangeAndResolution(t *testing.T) {
	d, logs, source := recoveryUnitFixture()
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for i, state := range []string{"uncertain", "uncertain", "confirmed"} {
		_, err := d.reportAttempt(recoveryUnitContext(t, at), source, func() (turn.Posted, error) {
			return turn.Posted{}, &turn.DeliveryError{Key: "delivery-1", State: state, Cause: errors.New("private-recovery-value unavailable")}
		})
		if !isRecoveryPending(err) {
			t.Fatal(err)
		}
		wantLogs := 1
		if i == 2 {
			wantLogs = 2
		}
		if got := len(recoveryAlertRows(t, logs)); got != wantLogs {
			t.Fatalf("state %s logs=%d", state, got)
		}
		at = d.recoveries.items[source.Key].NextAt
	}
	result, err := d.reportAttempt(recoveryUnitContext(t, at), source, func() (turn.Posted, error) { return turn.Posted{CommentID: 77, Skipped: false}, nil })
	if err != nil || result.CommentID != 77 {
		t.Fatalf("resolved result=%+v err=%v", result, err)
	}
	row := d.recoveries.items[source.Key]
	if row.State != "resolved" || !d.recoveries.dirty[source.Key] || d.sessionRecoveryHeld(42, at) {
		t.Fatalf("resolved metadata=%+v", row)
	}
	d.recoverySucceeded(t.Context(), source.Key)
	rows := recoveryAlertRows(t, logs)
	if len(rows) != 3 || rows[2]["msg"] != "report recovery resolved" || strings.Contains(logs.String(), "private-recovery-value") {
		t.Fatalf("diagnostics=%s", logs.String())
	}
	for _, row := range rows[:2] {
		for _, field := range []string{"task", "delivery", "run_id", "state", "reason", "next_eligible"} {
			if _, ok := row[field]; !ok {
				t.Fatalf("missing diagnostic %s", field)
			}
		}
	}
}

func TestReportRecovery_GlobalHoldAndTaskIsolation(t *testing.T) {
	for _, offset := range []time.Duration{time.Minute, 9 * time.Minute} {
		t.Run(offset.String(), func(t *testing.T) {
			d, logs, source := recoveryUnitFixture()
			at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
			d.recoveryFailure(t.Context(), source, at, errors.New("unavailable"))
			if !d.sessionRecoveryHeld(42, at) || d.sessionRecoveryHeld(43, at) {
				t.Fatal("blocked delivery held unrelated task")
			}
			d.backoff.fail(at, 10*time.Minute)
			before := logs.Len()
			calls := 0
			fn := func() (turn.Posted, error) { calls++; return turn.Posted{CommentID: 77, Skipped: false}, nil }
			posted, err := d.reportAttempt(recoveryUnitContext(t, at.Add(offset)), source, fn)
			if !isRecoveryPending(err) || !posted.Skipped || calls != 0 || logs.Len() != before {
				t.Fatal("global hold allowed retry effects")
			}
			posted, err = d.reportAttempt(recoveryUnitContext(t, at.Add(10*time.Minute)), source, fn)
			if err != nil || posted.CommentID == 0 || calls != 1 || d.recoveries.items[source.Key].State != "resolved" {
				t.Fatalf("hold release result=%+v err=%v calls=%d", posted, err, calls)
			}
		})
	}
}

// Existing escalation assertions count alerts independently of per-attempt INFO.
func recoveryAlertRows(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var rows []map[string]any
	for _, row := range recoveryLogRows(t, logs) {
		if row["msg"] == "report recovery pending" || row["msg"] == "report recovery resolved" {
			rows = append(rows, row)
		}
	}
	return rows
}

func TestCandidateHoldDiagnostics(t *testing.T) {
	d, logs, source := recoveryUnitFixture()
	at := time.Date(2026, 10, 12, 12, 0, 0, 123, time.UTC)
	ref, err := naming.ParseRef(source.Task)
	if err != nil {
		t.Fatal(err)
	}
	c := &candidate{session: store.Session{ID: source.SessionID}, ref: ref, log: d.log}
	source.NextAt, source.State = at.Add(time.Minute), "pending"
	d.initRecoveries()
	d.recoveries.items[source.Key] = source
	other := source
	other.Key = "z-last"
	d.recoveries.items[other.Key] = other
	d.prepFailures.record(source.SessionID, "redacted preparation failure", at, time.Minute)
	count := 0
	check := func(kind, key, state, reason string, due time.Time) {
		t.Helper()
		for range 10 {
			if got := d.offer(t.Context(), c, at, &TickResult{}); got != offerSkipped {
				t.Fatalf("offer=%v", got)
			}
		}
		count++
		rows := recoveryLogRows(t, logs)
		if len(rows) != count {
			t.Fatalf("hold events=%d want=%d: %s", len(rows), count, logs)
		}
		row := rows[len(rows)-1]
		if row["level"] != "INFO" || row["msg"] != "session admission held" || row["session_id"] != float64(c.session.ID) || row["task"] != source.Task || row["hold_kind"] != kind || row["recovery"] != key || row["state"] != state || row["reason"] != reason || row["next_eligible"] != due.Format(time.RFC3339Nano) {
			t.Fatalf("hold=%v", row)
		}
	}
	check("report-recovery", source.Key, source.State, "", source.NextAt)
	source.State = "uncertain"
	d.recoveries.items[source.Key] = source
	check("report-recovery", source.Key, source.State, "", source.NextAt)
	source.NextAt = source.NextAt.Add(time.Second)
	d.recoveries.items[source.Key] = source
	check("report-recovery", source.Key, source.State, "", source.NextAt)
	delete(d.recoveries.items, source.Key)
	check("report-recovery", other.Key, other.State, "", other.NextAt)
	delete(d.recoveries.items, other.Key)
	check("preparation", "", "", "redacted preparation failure", at.Add(time.Minute))
	d.prepFailures.items[source.SessionID] = prepareFailure{Reason: "changed reason", NextAt: at.Add(time.Minute)}
	check("preparation", "", "", "changed reason", at.Add(time.Minute))
	// Observing release forgets suppression, even if the next hold is identical.
	if !d.inflight.enter(c.session.ID) {
		t.Fatal("fixture admission")
	}
	if got := d.offer(t.Context(), c, at.Add(time.Minute), &TickResult{}); got != offerSkipped || len(d.holdDiagnostics) != 0 {
		t.Fatal("released hold was not forgotten")
	}
	d.inflight.leave(c.session.ID)
	check("preparation", "", "", "changed reason", at.Add(time.Minute))
	c.session.ID++
	d.prepFailures.record(c.session.ID, "changed reason", at, time.Minute)
	check("preparation", "", "", "changed reason", at.Add(time.Minute))
	c.session.ID--
	if !d.observeCandidateHold(t.Context(), c, at) || len(recoveryLogRows(t, logs)) != count {
		t.Fatal("another session changed suppression")
	}
}

func assertRecoveryAttempt(t *testing.T, logs *bytes.Buffer, source store.ReportRecovery, outcome string) {
	t.Helper()
	var events []map[string]any
	for _, row := range recoveryLogRows(t, logs) {
		if row["msg"] == "report recovery attempt" || row["msg"] == "report recovery attempt outcome" {
			events = append(events, row)
		}
	}
	if len(events) != 2 || events[0]["msg"] != "report recovery attempt" || events[1]["outcome"] != outcome {
		t.Fatalf("attempt events=%v", events)
	}
	for _, row := range events {
		if row["level"] != "INFO" || row["session_id"] != float64(source.SessionID) || row["task"] != source.Task || row["recovery"] != source.Key {
			t.Fatalf("uncorrelated event=%v", row)
		}
	}
	if outcome == "failed" && (events[1]["reason"] != source.Reason || events[1]["next_eligible"] != source.NextAt.Format(time.RFC3339Nano)) {
		t.Fatalf("failure event=%v source=%+v", events[1], source)
	}
	if strings.Contains(logs.String(), "private-recovery-value") {
		t.Fatal("unredacted reason")
	}
}

func TestReportRecoveryAttemptDiagnostics(t *testing.T) {
	d, logs, source := recoveryUnitFixture()
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	// An initial failure is not a pending retry.
	_, err := d.reportAttempt(recoveryUnitContext(t, at), source, func() (turn.Posted, error) { return turn.Posted{}, errors.New("private-recovery-value unavailable") })
	if !isRecoveryPending(err) || strings.Contains(logs.String(), "report recovery attempt") {
		t.Fatal("initial attempt reported as retry")
	}
	logs.Reset()
	at = d.recoveries.items[source.Key].NextAt
	_, err = d.reportAttempt(recoveryUnitContext(t, at), source, func() (turn.Posted, error) { return turn.Posted{}, errors.New("private-recovery-value unavailable") })
	if !isRecoveryPending(err) {
		t.Fatal(err)
	}
	assertRecoveryAttempt(t, logs, d.recoveries.items[source.Key], "failed")
	logs.Reset()
	at = d.recoveries.items[source.Key].NextAt
	_, err = d.reportAttempt(recoveryUnitContext(t, at), source, func() (turn.Posted, error) { return turn.Posted{Skipped: true}, &recoveryPending{} })
	if !isRecoveryPending(err) {
		t.Fatal(err)
	}
	assertRecoveryAttempt(t, logs, source, "pending")
	logs.Reset()
	_, err = d.reportAttempt(recoveryUnitContext(t, at), source, func() (turn.Posted, error) { return turn.Posted{CommentID: 77}, nil })
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryAttempt(t, logs, source, "resolved")
}

type recoveryDiagnosticReporter struct {
	Reporter
	calls   int
	pending bool
	err     error
}

func (r *recoveryDiagnosticReporter) ReportPending(context.Context, int64, int64) (turn.Posted, bool, error) {
	r.calls++
	return turn.Posted{}, r.pending, r.err
}

func TestResumeReportAttemptDiagnostics(t *testing.T) {
	d, logs, source := recoveryUnitFixture()
	db, _, session := recoveryDatabase(t)
	d.db = db
	source.SessionID, source.RunID, source.Task = session.ID, 0, session.TaskRef
	source.Key = recoveryKey(session.ID, source.TriggerID)
	reporter := &recoveryDiagnosticReporter{}
	d.reporter = reporter
	ref, err := naming.ParseRef(source.Task)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	work := &task{session: store.Session{ID: source.SessionID}, ref: ref, tickAt: at}
	if pending, err := d.resumeReport(t.Context(), work, source.TriggerID); err != nil || pending || reporter.calls != 1 || logs.Len() != 0 {
		t.Fatalf("ordinary polling: %v %v %s", pending, err, logs)
	}
	d.recoveryFailure(t.Context(), source, at, errors.New("unavailable"))
	logs.Reset()
	if _, err := d.resumeReport(t.Context(), work, source.TriggerID); !isRecoveryPending(err) || reporter.calls != 1 || logs.Len() != 0 {
		t.Fatal("ineligible retry had effects")
	}
	work.tickAt = d.recoveries.items[source.Key].NextAt
	if pending, err := d.resumeReport(t.Context(), work, source.TriggerID); err != nil || pending {
		t.Fatalf("no delivery: %v %v", pending, err)
	}
	assertRecoveryAttempt(t, logs, source, "no-delivery-found")
	logs.Reset()
	reporter.err = errors.New("private-recovery-value unavailable")
	if _, err := d.resumeReport(t.Context(), work, source.TriggerID); !isRecoveryPending(err) {
		t.Fatal(err)
	}
	assertRecoveryAttempt(t, logs, d.recoveries.items[source.Key], "failed")
	logs.Reset()
	reporter.err, reporter.pending = nil, true
	work.tickAt = d.recoveries.items[source.Key].NextAt
	if pending, err := d.resumeReport(t.Context(), work, source.TriggerID); err != nil || !pending {
		t.Fatalf("resolved: %v %v", pending, err)
	}
	assertRecoveryAttempt(t, logs, source, "resolved")
}
