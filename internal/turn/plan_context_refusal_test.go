package turn

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// A Plan agent that abbreviates the context's parent (as run 4 of #273 did)
// must be told so by its own run, and the next turn must refuse with an
// explanation rather than stay unconsumed on every tick (#289).
func TestMalformedPlanningContextIsReportedThenRefused(t *testing.T) {
	var f *fixture
	launches := 0
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		launches++
		writePlanOutcome(t, req, "discussion")
		s := f.reloadSession()
		writePlanJSON(t, filepath.Join(req.WorktreePath, req.PlanContextPath), planbrief.WorkingContext{Version: 1, Parent: fmt.Sprint(req.Ref.Issue), Branch: s.Branch, Feature: s.FeatureSlug, Scope: "Audit stories", Decisions: "None", Assumptions: "None", Questions: "None"})
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	setupOutcomeForge(t, f)
	req := planRequest(f)
	turn := func(body string) (Result, error) {
		req.Trigger.Body = body
		req.Command = ParseCommand(body)
		_, err := f.db.RecordComment(t.Context(), store.Comment{SessionID: f.session.ID, ForgeCommentID: req.Trigger.CommentID, AuthorHandle: "owner", AuthorUserID: 42, Body: body, CreatedAt: time.Now(), Classification: store.ClassificationInstruction})
		mustProgress(t, err)
		result, err := Execute(t.Context(), f.opts, req)
		req.Trigger.CommentID++
		return result, err
	}

	written, err := turn("/plan audit the stories")
	if err != nil {
		t.Fatalf("writing run: %v\nlogs:\n%s", err, f.logs)
	}
	if written.Err == nil || !strings.Contains(written.Err.Error(), `parent is "`) || !strings.Contains(written.Err.Error(), planbrief.ContextFileName) {
		t.Fatalf("writing run did not report its malformed context: err=%v", written.Err)
	}

	refused, err := turn("Check the assessment")
	if err != nil {
		t.Fatalf("malformed context left the trigger unconsumed instead of refusing: %v", err)
	}
	if launches != 1 {
		t.Fatalf("refused context launched an agent: launches=%d", launches)
	}
	if refused.Disposition != DispositionPrepareFailed || !strings.Contains(refused.Reason, planbrief.ContextFileName) || !strings.Contains(refused.Reason, "No agent launched") || !strings.Contains(refused.Reason, `expected "`+f.ref().String()+`"`) {
		t.Fatalf("refusal explanation: disposition=%s reason=%q", refused.Disposition, refused.Reason)
	}
	if refused.Run.ID == 0 || refused.Run.Status != store.RunFailed || refused.Run.SemanticOutcome != "refused" {
		t.Fatalf("refused run not terminal: %+v", refused.Run)
	}
}

func TestPlanningContextRefusalClassification(t *testing.T) {
	working := planbrief.WorkingContext{Parent: "273", Branch: "lf/273-x", Feature: "x"}
	err := checkPlanningIdentity(working, "github:o/r#273", "lf/273-x", "x")
	if err == nil || !strings.Contains(err.Error(), `parent is "273", expected "github:o/r#273"`) || strings.Contains(err.Error(), "branch is") {
		t.Fatalf("identity mismatch detail: %v", err)
	}
	if checkPlanningIdentity(working, "273", "lf/273-x", "x") != nil {
		t.Fatal("matching identity refused")
	}
	long := clipIdentity(strings.Repeat("é", 100))
	if len(long) > 84 || !strings.HasSuffix(long, "…") {
		t.Fatalf("unbounded identity value: %d bytes", len(long))
	}
	var refusal *planContextRefusal
	if !errors.As(fmt.Errorf("prepare: %w", &planContextRefusal{path: "p", err: err}), &refusal) {
		t.Fatal("refusal not recognizable through wrapping")
	}
}
