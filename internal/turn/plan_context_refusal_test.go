package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// A Plan agent that abbreviates the context's parent (as run 4 of #273 did)
// must be told so by its own run. The next request continues without admitting
// or replacing the invalid source.
func TestMalformedPlanningContextIsReportedThenContinues(t *testing.T) {
	var f *fixture
	launches := 0
	var originalPath string
	var originalWorktree string
	var originalBytes []byte
	runner := newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		launches++
		writePlanOutcome(t, req, "discussion")
		if launches == 2 {
			if req.PlanContextPath == originalPath || req.PlanWorkingContext != "" || !strings.Contains(req.Prompt, "Optional planning context is unavailable") {
				t.Fatal("unavailable context was admitted or destination reused")
			}
			writePlanContext(t, f, req)
			return succeeded(req, f.paths.Home()), nil
		}
		originalPath = req.PlanContextPath
		originalWorktree = req.WorktreePath
		s := f.reloadSession()
		writePlanJSON(t, filepath.Join(req.WorktreePath, req.PlanContextPath), planbrief.WorkingContext{Version: 1, Parent: fmt.Sprint(req.Ref.Issue), Branch: s.Branch, Feature: s.FeatureSlug, Scope: "Audit stories", Decisions: "None", Assumptions: "None", Questions: "None"})
		originalBytes, _ = os.ReadFile(filepath.Join(req.WorktreePath, originalPath))
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

	continued, err := turn("Check the assessment")
	if err != nil || !continued.Succeeded() {
		t.Fatalf("request did not continue: %+v err=%v logs=%s", continued, err, f.logs)
	}
	if launches != 2 {
		t.Fatalf("expected same request to launch: launches=%d", launches)
	}
	actual, err := os.ReadFile(filepath.Join(originalWorktree, originalPath))
	mustProgress(t, err)
	if string(actual) != string(originalBytes) {
		t.Fatal("unusable original context modified")
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
