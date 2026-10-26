package store

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func doctorInboxInput(repo Repo, issue int, comment int64, op string) DoctorCommand {
	return DoctorCommand{RepoID: repo.ID, TaskRef: "github:acme/api#" + strconv.Itoa(issue), IssueID: int64(issue + 10000), IssueNumber: issue, CommentID: comment, ActorID: 7, CreatedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Operation: op, Instructions: "explain"}
}

func TestDoctorInboxOrderingStopAndReplay(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "acme", "api")
	input := doctorInboxInput(repo, 406, 10, "diagnose")
	input.TaskRef = "github:acme/api#406"
	first, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Instructions = "edited input"
	same, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil || same.ID != first.ID || same.Instructions != first.Instructions {
		t.Fatalf("replay changed authority: %+v %v", same, err)
	}
	input.CommentID = 11
	second, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimDoctorCommand(ctx, "", false, "test-instance")
	if err != nil || claimed.ID != first.ID {
		t.Fatalf("order: %+v %v", claimed, err)
	}
	if _, err := db.ClaimDoctorCommand(ctx, "", false, "test-instance"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("same issue interleaved: %v", err)
	}
	input.CommentID = 12
	input.Operation = "stop"
	stop, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginDoctorAttempt(ctx, input.TaskRef, DoctorAttempt{ID: "late", SourceID: claimed.SourceID(), OwnerText: "late", PromptVersion: "p", GuideVersion: "g"}); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("stopped claim reopened conversation: %v", err)
	}
	if err := db.CompleteDoctorCommand(ctx, second.ID, "late result", 0); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("stopped queue completed: %v", err)
	}
	control, err := db.ClaimDoctorCommand(ctx, "", true, "test-instance")
	if err != nil || control.ID != stop.ID {
		t.Fatalf("stop blocked: %+v %v", control, err)
	}
	if err := db.CompleteDoctorCommand(ctx, control.ID, "Stopped", 0); err != nil {
		t.Fatal(err)
	}
	input.CommentID = 9
	input.Operation = "diagnose"
	delayed, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil || delayed.State != "stopped" {
		t.Fatalf("delayed pre-stop input admitted: %+v %v", delayed, err)
	}
	input.CommentID = 13
	later, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.ClaimDoctorCommand(ctx, "", false, "test-instance")
	if err != nil || got.ID != later.ID {
		t.Fatalf("new instruction unavailable: %+v %v", got, err)
	}
}

func TestDoctorPresentationRequiresAllConfirmedParts(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "acme", "api")
	input := doctorInboxInput(repo, 406, 10, "diagnose")
	input.TaskRef = "github:acme/api#406"
	_, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.ClaimDoctorCommand(ctx, "", false, "test-instance")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteDoctorCommand(ctx, c.ID, "two-part result", 0); err != nil {
		t.Fatal(err)
	}
	pending, err := db.DoctorCommandsNeedingReports(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending %+v %v", pending, err)
	}
	c = pending[0]
	if err := db.EnsureDoctorDeliveries(ctx, c, []string{"first part", "second part"}, 99); err != nil {
		t.Fatal(err)
	}
	deliveries, err := db.PendingDoctorDeliveries(ctx)
	if err != nil || len(deliveries) != 3 {
		t.Fatalf("deliveries %+v %v", deliveries, err)
	}
	var ordered []ReportDelivery
	var index ReportDelivery
	for _, delivery := range deliveries {
		if delivery.Purpose == "doctor-response-index" {
			index = delivery
		} else {
			ordered = append(ordered, delivery)
		}
	}
	if _, ready, err := db.PrepareDoctorDelivery(ctx, index.Key); err != nil || ready {
		t.Fatalf("index ready before content receipts: %v %v", ready, err)
	}
	deliveries = ordered
	deliveries = append(deliveries, index)
	for i, d := range deliveries {
		if complete, err := db.DoctorPresentationComplete(ctx, c.ID); err != nil || complete {
			t.Fatalf("premature presentation %v %v", complete, err)
		}
		if d.Purpose == "doctor-response-index" {
			prepared, ready, err := db.PrepareDoctorDelivery(ctx, d.Key)
			if err != nil || !ready || !strings.Contains(prepared.Body, "#issuecomment-100") || !strings.Contains(prepared.Body, "#issuecomment-101") {
				t.Fatalf("index missing complete part receipts: %+v %v %v", prepared, ready, err)
			}
			replayed, ready, err := db.PrepareDoctorDelivery(ctx, d.Key)
			if err != nil || !ready || replayed.Body != prepared.Body {
				t.Fatalf("index body changed on replay: %+v %v %v", replayed, ready, err)
			}
		}
		if ok, err := db.MarkReportDeliverySent(ctx, d.Key); err != nil || !ok {
			t.Fatalf("claim %v %v", ok, err)
		}
		if _, err := db.ConfirmReportDelivery(ctx, d.Key, int64(100+i)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CompleteReportDelivery(ctx, d.Key); err == nil {
			t.Fatal("workflow completion accepted doctor owner")
		}
		if err := db.CompleteDoctorDelivery(ctx, d.Key); err != nil {
			t.Fatal(err)
		}
	}
	if complete, err := db.DoctorPresentationComplete(ctx, c.ID); err != nil || !complete {
		t.Fatalf("complete %v %v", complete, err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM watermarks`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("doctor advanced workflow watermarks: %d %v", count, err)
	}
}

func TestDoctorScanFixedWindowAndActivation(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "acme", "api")
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	first, err := db.DoctorScanWindow(ctx, repo.ID, at)
	if err != nil || !first.ActivatedAt.Equal(at) || !first.CompletedAt.Equal(at) {
		t.Fatalf("activation %+v %v", first, err)
	}
	next, err := db.DoctorScanWindow(ctx, repo.ID, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AdvanceDoctorScan(ctx, next, 2); err != nil {
		t.Fatal(err)
	}
	resumed, err := db.DoctorScanWindow(ctx, repo.ID, at.Add(time.Hour))
	if err != nil || resumed.Page != 2 || !resumed.WindowEnd.Equal(next.WindowEnd) || !resumed.CompletedAt.Equal(at) {
		t.Fatalf("partial page skipped %+v %v", resumed, err)
	}
	if err := db.AdvanceDoctorScan(ctx, resumed, 0); err != nil {
		t.Fatal(err)
	}
	done, err := db.DoctorScanWindow(ctx, repo.ID, at.Add(time.Hour))
	if err != nil || !done.CompletedAt.Equal(next.WindowEnd) {
		t.Fatalf("completed %+v %v", done, err)
	}
}

func TestDoctorMigrationPreservesWorkflowDelivery(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, t.TempDir()+"/upgrade.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.ensureMigrationTable(ctx); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version < 84 {
			if err := db.applyMigration(ctx, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	session := testSession(t, db, 406)
	intent, err := db.EnsureReportDelivery(ctx, ReportDelivery{Key: "retained-workflow", SessionID: session.ID, TriggerID: 100, Task: session.TaskRef, Destination: "acme/api#406", TriggerNamespace: "issue-comment", Purpose: "reply", BotUserID: 99, Body: "Existing workflow report", Marker: "existing-marker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MarkReportDeliverySent(ctx, intent.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConfirmReportDelivery(ctx, intent.Key, 101); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := db.GetReportDelivery(ctx, intent.Key)
	if err != nil || after.SessionID != session.ID || after.Body != intent.Body || after.CommentID != 101 {
		t.Fatalf("migration changed workflow report: %+v %v", after, err)
	}
	pending, err := db.ListPendingReportDeliveries(ctx)
	if err != nil || len(pending) != 1 || pending[0].Key != intent.Key {
		t.Fatalf("workflow recovery lost: %+v %v", pending, err)
	}
}
