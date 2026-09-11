package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	rows := recoveryLogRows(t, logs)
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
		if got := len(recoveryLogRows(t, logs)); got != wantLogs {
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
	rows := recoveryLogRows(t, logs)
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
