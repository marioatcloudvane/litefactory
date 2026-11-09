package daemon_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
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
	if classifier.calls.Load() != 0 {
		t.Fatalf("Bug consulted the classifier: %d", classifier.calls.Load())
	}
	for _, call := range f.turns.calls() {
		if call.Mode != agent.ModeBug {
			t.Fatalf("Bug launched a substitute mode: %+v", call.Mode)
		}
	}
	bodies, _, _ := f.forge.snapshot()
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "cannot replace or cancel") {
		t.Fatalf("missing visible refusal: %s", joined)
	}
	assertRetentionLabels(t, f, "Mode: Plan")
}
