package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestBugLifecycleJourneyConfirmedClosureAndReports(t *testing.T) {
	db, _, _, h, in := closeJourneySetup(t)
	m, e := db.GetBugMerge(t.Context(), in.SessionID, in.MergeOperation)
	if e != nil {
		t.Fatal(e)
	}
	child := m.Binding.Review.Evidence.Child.Request
	life, e := db.EnsureBugLifecycle(t.Context(), in.SessionID, child.ParentInstance, child.ChildDefinition)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.CompleteBugLifecycle(t.Context(), in.SessionID); e == nil {
		t.Fatal("lifecycle completed before independently observed closure")
	}
	closed, e := h.Close(t.Context(), db, in)
	if e != nil || closed.Outcome != "confirmed" {
		t.Fatalf("closure %+v %v", closed, e)
	}
	report := store.BugLifecycleReport{SessionID: in.SessionID, AttemptID: child.ParentAttempt, Event: "final", Kind: "notice", Body: "Confirmed issue closure with retained authorized integration.", DeliveryKey: "bug-final-fixture"}
	if e = db.RetainBugLifecycleReport(t.Context(), report); e != nil {
		t.Fatal(e)
	}
	if e = db.CompleteBugLifecycle(t.Context(), in.SessionID); e == nil {
		t.Fatal("pending report counted as delivered")
	}
	if e = db.ConfirmBugLifecycleReport(t.Context(), in.SessionID, "final"); e == nil {
		t.Fatal("nonexistent delivery confirmed")
	}
	identity := m.Binding.Review.Evidence.Identity
	task := fmt.Sprintf("github:%s#%d", identity.Repository, identity.IssueNumber)
	_, e = db.EnsureReportDelivery(t.Context(), store.ReportDelivery{Key: report.DeliveryKey, Version: 1, SessionID: in.SessionID, Task: task, Destination: task, TriggerNamespace: "fixture", Purpose: "final", BotUserID: 99, Body: report.Body, Marker: report.DeliveryKey})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.MarkReportDeliverySent(t.Context(), report.DeliveryKey); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ConfirmReportDelivery(t.Context(), report.DeliveryKey, 900); e != nil {
		t.Fatal(e)
	}
	if e = db.ConfirmBugLifecycleReport(t.Context(), in.SessionID, "final"); e == nil {
		t.Fatal("receipt without completed delivery accepted")
	}
	if _, e = db.CompleteReportDelivery(t.Context(), report.DeliveryKey); e != nil {
		t.Fatal(e)
	}
	if e = db.ConfirmBugLifecycleReport(t.Context(), in.SessionID, "final"); e != nil {
		t.Fatal(e)
	}
	if e = db.CompleteBugLifecycle(t.Context(), in.SessionID); e != nil {
		t.Fatal(e)
	}
	after, e := db.GetBugLifecycle(t.Context(), in.SessionID)
	if e != nil || after.Disposition != "completed" || after.ParentInstance != life.ParentInstance {
		t.Fatalf("completion %+v %v", after, e)
	}
	if e = db.RecordBugLifecycleDisposition(t.Context(), in.SessionID, life.ParentInstance, "record", "running", ""); e == nil {
		t.Fatal("completed lifecycle restarted")
	}
}
func TestBugLifecycleJourneyCancellationAndSourceProvenance(t *testing.T) {
	db, _, _, _, in := closeJourneySetup(t)
	m, e := db.GetBugMerge(t.Context(), in.SessionID, in.MergeOperation)
	if e != nil {
		t.Fatal(e)
	}
	child := m.Binding.Review.Evidence.Child.Request
	life, e := db.EnsureBugLifecycle(t.Context(), in.SessionID, child.ParentInstance, child.ChildDefinition)
	if e != nil {
		t.Fatal(e)
	}
	bad := store.BugLifecycleReport{SessionID: in.SessionID, AttemptID: child.ParentAttempt, Event: "bad", Kind: "failure", Body: "Failure", DeliveryKey: "bad"}
	if e = db.RetainBugLifecycleReport(t.Context(), bad); e == nil {
		t.Fatal("successful domain notice fabricated failure")
	}
	bad.Kind = "pre-dispatch"
	if e = db.RetainBugLifecycleReport(t.Context(), bad); e == nil {
		t.Fatal("pre-dispatch invented attempt")
	}
	bad.AttemptID = 0
	if e = db.RetainBugLifecycleReport(t.Context(), bad); e != nil {
		t.Fatal(e)
	}
	bad.Body = "changed"
	if e = db.RetainBugLifecycleReport(t.Context(), bad); e == nil {
		t.Fatal("report identity accepted changed content")
	}
	if e = db.CancelBugLifecycle(t.Context(), in.SessionID); e != nil {
		t.Fatal(e)
	}
	after, e := db.GetBugLifecycle(t.Context(), in.SessionID)
	if e != nil || !after.Cancelled {
		t.Fatalf("cancellation %+v %v", after, e)
	}
	if _, e = db.ClaimBugContinuation(context.Background(), store.BugParentContinuation{SessionID: in.SessionID, Operation: "cancelled", Cause: "merge-reconcile", Reference: in.MergeOperation, ExpectedRevision: after.Revision, At: time.Now()}); e == nil {
		t.Fatal("cancelled lifecycle restarted")
	}
	if e = db.RecordBugLifecycleDisposition(t.Context(), in.SessionID, life.ParentInstance, "record", "running", ""); e == nil {
		t.Fatal("cancelled disposition started fresh work")
	}
}
