package daemon_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestWorkflowBoundaryBugStickyAndModeSwitch(t *testing.T) {
	f := newFixture(t)
	classifier := &retentionClassifier{}
	f.daemon = retentionDaemon(t, f, classifier)
	f.forge.onAddLabels = func(_ http.ResponseWriter, _ *http.Request) bool {
		t.Error("Bug attempted label addition")
		return true
	}
	f.forge.onRemoveLabel = func(_ http.ResponseWriter, _ *http.Request) bool { t.Error("Bug attempted label removal"); return true }
	f.plantCursor()
	f.issueWithLabels("Bug admission", "bug")
	f.settle()
	f.forge.addComment(testIssue, 501, owner(), "investigate")
	f.settle()
	a, err := f.db.GetTaskWorkflowAdmission(t.Context(), f.session().ID)
	if err != nil || a.Workflow != "bug" || a.Family != "standalone" {
		t.Fatalf("admission=%+v err=%v", a, err)
	}
	f.issueWithLabels("Bug relabelled", "Mode: Plan")
	f.daemon = retentionDaemon(t, f, classifier)
	if _, err := f.daemon.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.forge.addComment(testIssue, 502, owner(), "/plan replace workflow")
	f.settle()
	f.forge.addComment(testIssue, 503, owner(), "continue investigation")
	f.settle()
	again, err := f.db.GetTaskWorkflowAdmission(t.Context(), f.session().ID)
	if err != nil || again != a {
		t.Fatalf("sticky admission changed: %+v %v", again, err)
	}
	if classifier.calls.Load() != 0 || len(f.turns.calls()) != 0 {
		t.Fatalf("Bug launched substitute: classifier=%d turns=%d", classifier.calls.Load(), len(f.turns.calls()))
	}
	bodies, _, _ := f.forge.snapshot()
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "cannot replace or cancel") || !strings.Contains(joined, "admission is retained") {
		t.Fatalf("missing visible refusal: %s", joined)
	}
	assertRetentionLabels(t, f, "Mode: Plan")
	if len(f.runs()) != 0 {
		t.Fatalf("unavailable Bug manufactured runs: %+v", f.runs())
	}
}

func TestWorkflowBoundaryBugReportFailureRetainsAdmission(t *testing.T) {
	f := newFixture(t)
	classifier := &retentionClassifier{}
	f.daemon = retentionDaemon(t, f, classifier)
	f.plantCursor()
	f.issueWithLabels("Bug unavailable", "bug")
	f.settle()
	f.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "injected publication failure"})
		return true
	}
	f.forge.addComment(testIssue, 501, owner(), "investigate")
	f.settle()
	a, err := f.db.GetTaskWorkflowAdmission(t.Context(), f.session().ID)
	if err != nil || a.Workflow != "bug" {
		t.Fatalf("lost admission: %+v %v", a, err)
	}
	pending, found, err := f.db.FindPendingReportDelivery(t.Context(), a.SessionID, 501)
	if err != nil || !found {
		t.Fatalf("failure lost report debt: %+v %v %v", pending, found, err)
	}
	f.forge.onPostComment = nil
	f.restorePendingReport(501)
	f.daemon = retentionDaemon(t, f, classifier)
	if _, err := f.daemon.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.settle()
	if pending, found, err := f.db.FindPendingReportDelivery(t.Context(), a.SessionID, 501); err != nil || found {
		t.Fatalf("report debt not cleared: %+v %v %v", pending, found, err)
	}
	if classifier.calls.Load() != 0 || len(f.turns.calls()) != 0 {
		t.Fatal("publication recovery launched substitute")
	}
	again, err := f.db.GetTaskWorkflowAdmission(t.Context(), a.SessionID)
	if err != nil || again != a {
		t.Fatalf("recovery lost admission: %+v %v", again, err)
	}
	if _, err := f.db.GetModeSelection(t.Context(), a.SessionID, 501); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Bug created mode journal: %v", err)
	}
}
