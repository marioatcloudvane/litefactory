package turn

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// Quote and backslash characters are built from their codes, as analysis
// prose about literal-text spans carries them.
var (
	quoteDQ = string(rune(34))
	quoteBS = string(rune(92))
)

// An analysis whose cause mentions the pre tag and continues with an unpaired
// double quote (and a table-cell backslash escape) is published once, and
// recovering the same question again posts nothing new.
func TestGraphBugConversationPublishesAnalysisWithStrayQuote(t *testing.T) {
	f, forge, p, inst := conversationFixture(t)
	activities := ActivityRegistry{}
	mustProgress(t, activities.Register(graphexec.ResearchLoopAction, GraphBugConversation{Profile: p, Reporter: f.opts.ResearchReporter}))
	_, e := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: f.opts.MCP.Now()})
	mustProgress(t, e)
	cause := "The " + "<" + "pre> " + quoteDQ + "unclosed value stops the report"
	remedy := "Copy " + "<" + "pre> " + quoteDQ + "a " + quoteBS + "| b" + quoteDQ + " cells unchanged"
	completeConversationAnalystWith(t, f, inst, "done", 0, "No panic", func(a *buganalysis.BugAnalysis) {
		a.Cause = cause
		a.Remedy = remedy
	})
	parked, e := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: f.opts.MCP.Now()})
	mustProgress(t, e)
	if parked.Status != "waiting_owner" {
		t.Fatalf("review did not park: %+v", parked)
	}
	post := forge.only(t)
	for _, want := range []string{cause, remedy, "revision 1", "/approve-bug analysis 1"} {
		if !strings.Contains(post.body, want) {
			t.Fatalf("posted analysis lacks %q: %q", want, post.body)
		}
	}
	wait, e := f.db.GetGraphOwnerWait(t.Context(), inst.ID)
	mustProgress(t, e)
	if wait.State != "waiting" {
		t.Fatalf("owner wait not waiting: %+v", wait)
	}
	delivery, e := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, e)
	if !delivery.Completed || delivery.CommentID == 0 {
		t.Fatalf("delivery not completed: %+v", delivery)
	}
	// Replay: the same question recovers to the same comment and bytes.
	if _, e = f.opts.ResearchReporter.RecoverGraphStopQuestion(t.Context(), wait.DeliveryKey); e != nil {
		t.Fatal(e)
	}
	if n := len(forge.all()); n != 1 {
		t.Fatalf("replay posted again: %d comments", n)
	}
	again, e := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, e)
	if again.Body != delivery.Body || again.CommentID != delivery.CommentID {
		t.Fatalf("replay changed the stored delivery: %+v", again)
	}
	rerendered, e := f.opts.ResearchReporter.renderer.RenderReportDelivery(t.Context(), delivery.Body, "")
	mustProgress(t, e)
	if rerendered.String() != delivery.Body {
		t.Fatal("re-rendering the stored body changed its bytes")
	}
}

// A secret hidden as a unicode-escaped JSON value beside a stray double quote
// after a tag mention is still refused: nothing is stored or posted.
func TestGraphBugConversationRefusesEncodedSecretBesideStrayQuote(t *testing.T) {
	f, forge, p, inst := conversationFixture(t)
	activities := ActivityRegistry{}
	mustProgress(t, activities.Register(graphexec.ResearchLoopAction, GraphBugConversation{Profile: p, Reporter: f.opts.ResearchReporter}))
	_, e := ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: f.opts.MCP.Now()})
	mustProgress(t, e)
	var encoded strings.Builder
	encoded.WriteString(quoteDQ)
	for _, c := range testBotToken {
		fmt.Fprintf(&encoded, "%su%04x", quoteBS, c)
	}
	encoded.WriteString(quoteDQ)
	completeConversationAnalystWith(t, f, inst, "done", 0, "No panic", func(a *buganalysis.BugAnalysis) {
		a.Cause = "The " + "<" + "pre> " + quoteDQ + " " + encoded.String() + " tail"
	})
	_, _ = ExecuteNodeAction(t.Context(), f.opts, GraphNodeRequest{Profile: p, Activities: activities, InstanceID: inst.ID, At: f.opts.MCP.Now()})
	if n := len(forge.all()); n != 0 {
		t.Fatalf("refused analysis was posted: %d comments", n)
	}
	if wait, e := f.db.GetGraphOwnerWait(t.Context(), inst.ID); e == nil {
		if _, e = f.db.GetReportDelivery(t.Context(), wait.DeliveryKey); !errors.Is(e, store.ErrNotFound) {
			t.Fatalf("refused analysis stored a delivery: %v", e)
		}
	}
}
