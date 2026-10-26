package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func seedOverviewPublication(t *testing.T, db *DB, s Session, key, namespace, purpose string, at *time.Time, receipt, completed bool) {
	t.Helper()
	var when any
	var comment any
	if at != nil {
		when = FormatTime(*at)
	}
	if receipt {
		comment = int64(700)
	}
	_, err := db.sql.ExecContext(t.Context(), `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed,confirmed_at,confirmed_url) VALUES(?,1,?,1,?,?,?,?,9,'Owner-facing result','marker',1,?,?,?,?)`, key, s.ID, s.TaskRef, s.TaskRef, namespace, purpose, comment, completed, when, "https://github.com/acme/api/issues/123#issuecomment-700")
	if err != nil {
		t.Fatal(err)
	}
}

func TestOverviewPublicationsWindowAndEligibility(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 123)
	to := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	from := to.Add(-168 * time.Hour)
	old := from.Add(-time.Millisecond)
	future := to.Add(time.Millisecond)
	middle := to.Add(-time.Hour)
	for _, tc := range []struct {
		key, ns, purpose   string
		at                 *time.Time
		receipt, completed bool
	}{
		{"lower", "comment", "run-report", &from, true, true},
		{"upper", "comment", "run-report", &to, true, true},
		{"confirmed-unsettled", "comment", "run-report", &middle, true, false},
		{"automatic", "automatic", "run-report", &middle, true, true},
		{"schedule", "schedule", "run-report", &middle, true, true},
		{"runless-review", "issue-body", "plan-assembled:candidate", &middle, true, true},
		{"code-review", "code-pr-proposal", "exact-code-approval", &middle, true, true},
		{"old", "comment", "run-report", &old, true, true},
		{"future", "comment", "run-report", &future, true, true},
		{"undated", "comment", "run-report", nil, true, true},
		{"intent-only", "comment", "run-report", &middle, false, false},
		{"navigation", "chat-navigation", "navigation", &middle, true, true},
		{"merge", "merge", "merge", &middle, true, true},
		{"deployment", "deployment", "deployment", &middle, true, true},
		{"part", "report-part", "run-report-part:logical:1/1", &middle, true, true},
	} {
		seedOverviewPublication(t, db, s, tc.key, tc.ns, tc.purpose, tc.at, tc.receipt, tc.completed)
	}
	result, err := db.ListOverviewPublications(t.Context(), from, to)
	got, more, undated := result.Entries, result.HasMore, result.Undated
	if err != nil {
		t.Fatal(err)
	}
	if more || !undated {
		t.Fatalf("coverage more=%v undated=%v", more, undated)
	}
	keys := []string{}
	for _, p := range got {
		keys = append(keys, p.Key)
		if p.ConfirmedAt.Before(from) || p.ConfirmedAt.After(to) || p.RepoID != s.RepoID || p.SessionID != s.ID || p.TaskRef != s.TaskRef || p.URL == "" {
			t.Fatalf("lost context: %+v", p)
		}
		if (p.Key == "runless-review" || p.Key == "code-review") && (p.Kind != "review" || p.RunID != nil) {
			t.Fatalf("runless review=%+v", p)
		}
	}
	want := []string{"upper", "automatic", "code-review", "confirmed-unsettled", "runless-review", "schedule", "lower"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("publications=%v want=%v", keys, want)
	}
}

func TestOverviewPublicationsMultipartCompleteLogicalParent(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 123)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	child := ReportDelivery{Key: "part", SessionID: s.ID, TriggerID: 1, Task: s.TaskRef, Destination: s.TaskRef, TriggerNamespace: "report-part", Purpose: "run-report-part:parent:1/1", BotUserID: 9, Body: "part", Marker: "marker"}
	manifest, _ := json.Marshal(ReportMultipart{Summary: "Logical complete report", Parts: []ReportDelivery{child}, FinalBody: "Published index"})
	seedOverviewPublication(t, db, s, "parent", "report-multipart", "run-report", &now, true, false)
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE report_deliveries SET body=? WHERE delivery_key='parent'`, string(manifest)); err != nil {
		t.Fatal(err)
	}
	if result, err := db.ListOverviewPublications(t.Context(), now.Add(-time.Hour), now); err != nil || len(result.Entries) != 0 {
		t.Fatalf("incomplete logical parent visible: %+v %v", result, err)
	}
	if _, err := db.CompleteReportDelivery(t.Context(), "parent"); err == nil {
		t.Fatal("multipart without completed child accepted")
	}
	seedOverviewPublication(t, db, s, "part", "report-part", child.Purpose, &now, true, true)
	if result, err := db.ListOverviewPublications(t.Context(), now.Add(-time.Hour), now); err != nil || len(result.Entries) != 1 || result.Entries[0].Summary != "Logical complete report" {
		t.Fatalf("confirmed logical publication lost before local settlement: %+v %v", result, err)
	}

	if _, err := db.CompleteReportDelivery(t.Context(), "parent"); err != nil {
		t.Fatal(err)
	}
	result, err := db.ListOverviewPublications(t.Context(), now.Add(-time.Hour), now)
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Key != "parent" {
		t.Fatalf("logical publication=%+v %v", result, err)
	}
}

func TestOverviewPublicationsSentinelStableOrdering(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 123)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for i := 50; i >= 0; i-- {
		seedOverviewPublication(t, db, s, fmt.Sprintf("delivery-%02d", i), "comment", "run-report", &now, true, true)
	}
	result, err := db.ListOverviewPublications(t.Context(), now.Add(-time.Hour), now)
	got, more, undated := result.Entries, result.HasMore, result.Undated
	if err != nil || !more || undated || len(got) != 50 {
		t.Fatalf("sentinel=%d %v %v %v", len(got), more, undated, err)
	}
	for i, p := range got {
		if p.Key != fmt.Sprintf("delivery-%02d", i) {
			t.Fatalf("unstable order %d: %s", i, p.Key)
		}
	}
}

func TestReportConfirmationPopulatedMigrationKeepsHistoricalUndated(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "historical.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "report_confirmation"))
	s := testSession(t, db, 123)
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO report_deliveries(delivery_key,identity_version,session_id,trigger_id,task,destination,trigger_namespace,purpose,bot_user_id,body,marker,possibly_sent,comment_id,completed) VALUES('historical',1,?,1,?,?,'comment','run-report',9,'Historical report','marker',1,700,1)`, s.ID, s.TaskRef, s.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		d, err := db.ConfirmReportDelivery(t.Context(), "historical", 700, "https://github.com/acme/api/issues/123#issuecomment-700")
		if err != nil || !d.ConfirmedAt.IsZero() || d.CommentID != 700 || !d.Completed {
			t.Fatalf("historical confirmation=%+v %v", d, err)
		}
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	result, err := db.ListOverviewPublications(t.Context(), now.Add(-168*time.Hour), now)
	got, more, undated := result.Entries, result.HasMore, result.Undated
	if err != nil || len(got) != 0 || more || !undated {
		t.Fatalf("historical window=%+v more=%v undated=%v err=%v", got, more, undated, err)
	}
}

func TestOverviewPublicationsOversizedMultipartCoverage(t *testing.T) {
	db := newTestDB(t)
	s := testSession(t, db, 123)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	manifest, err := json.Marshal(ReportMultipart{Summary: strings.Repeat("x", 1024*1024+1), Parts: []ReportDelivery{{Key: "part"}}})
	if err != nil {
		t.Fatal(err)
	}
	seedOverviewPublication(t, db, s, "oversized", "report-multipart", "run-report", &now, true, false)
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE report_deliveries SET body=? WHERE delivery_key='oversized'`, string(manifest)); err != nil {
		t.Fatal(err)
	}
	result, err := db.ListOverviewPublications(t.Context(), now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	warned := false
	for _, warning := range result.Warnings {
		if warning == "multipart_publication_decode_budget_exhausted" {
			warned = true
		}
	}
	if len(result.Entries) != 0 || !result.HasMore || !warned {
		t.Fatalf("oversized manifest omission hidden: %+v", result)
	}
	// The completed bit is durable evidence that all parts were settled before
	// the local completion transaction committed; overview need not decode again.
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE report_deliveries SET completed=1 WHERE delivery_key='oversized'`); err != nil {
		t.Fatal(err)
	}
	result, err = db.ListOverviewPublications(t.Context(), now.Add(-time.Hour), now)
	if err != nil || len(result.Entries) != 1 || result.Entries[0].Key != "oversized" || result.Entries[0].Summary == "" || len(result.Entries[0].Summary) > 2048 {
		t.Fatalf("durable multipart evidence lost: %+v %v", result, err)
	}
}
