package turn

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestWorkflowDecisionDestinationUsesOnlyCurrentConfirmedWait(t *testing.T) {
	f, forge, _, instance := persistentStopReporterFixture(t)
	key := retainPersistentStopQuestion(t, f, instance)
	reporter := newReporter(t, f, forge, nil)
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	link, err := reporter.workflowDecisionURL(t.Context(), ref, f.session.ID, report.ReplyBugCommentNotApplied)
	mustProgress(t, err)
	if link != "" {
		t.Fatal("unpublished wait offered as current destination")
	}
	posted, err := reporter.RecoverGraphStopQuestion(t.Context(), key)
	mustProgress(t, err)
	link, err = reporter.workflowDecisionURL(t.Context(), ref, f.session.ID, report.ReplyBugCommentNotApplied)
	mustProgress(t, err)
	if link == "" || !strings.Contains(link, "#issuecomment-") || posted.CommentID == 0 {
		t.Fatalf("confirmed wait destination missing: %q", link)
	}
	// A newer graph with no published wait cannot inherit the old question.
	_, err = f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: instance.DefinitionID, CurrentNode: "analyst", StartedAt: time.Now().UTC()})
	mustProgress(t, err)
	link, err = reporter.workflowDecisionURL(t.Context(), ref, f.session.ID, report.ReplyBugCommentNotApplied)
	mustProgress(t, err)
	if link != "" {
		t.Fatalf("new current graph inherited historical question %q", link)
	}
}

// Final delivery refresh changes the revision and resolves the previous owner
// display. Its confirmed result must remain reachable without a new decision.
func TestWorkflowCompletedValidateResultNeedsNoPendingDisplay(t *testing.T) {
	o := newOwnerFixture(t)
	o.mixed()
	o.say(o.ship())
	st := o.state()
	if !effectDone(st.Effects.Close) {
		t.Fatal("fixture did not complete delivery")
	}
	saved, err := o.db.GetValidateReport(t.Context(), o.instance.ID)
	mustProgress(t, err)
	reporter := o.opts.ResearchReporter
	head := o.head()
	reporter.validateHead = func(context.Context, store.Session) (string, error) { return head, nil }
	session, err := o.db.GetSession(t.Context(), o.instance.SessionID)
	mustProgress(t, err)
	ref, err := naming.ParseRef(session.TaskRef)
	mustProgress(t, err)
	old, err := o.db.LatestOwnerDecisionDisplay(t.Context(), ref.String(), ValidateShipCommand)
	mustProgress(t, err)
	if !old.Resolved || old.Revision == saved.Revision {
		t.Fatal("fixture retained a current pending display")
	}
	link, err := reporter.currentValidateReportURL(t.Context(), ref, session.ID, saved, st)
	mustProgress(t, err)
	if link == "" {
		t.Fatal("confirmed final result became unreachable after resolving decisions")
	}
	// A later branch head is different work, so this is no current-result claim.
	head = strings.Repeat("f", 40)
	link, err = reporter.currentValidateReportURL(t.Context(), ref, session.ID, saved, st)
	mustProgress(t, err)
	if link != "" {
		t.Fatalf("head drift retained current-result link %q", link)
	}
}
