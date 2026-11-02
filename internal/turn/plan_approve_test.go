package turn

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"testing/fstest"
)

// The shipped Plan graph gates every stage. These tests run the same flow on a
// copy of it in which the technical stage has no gate: its applied draft goes to
// lf_plan_approve, with the stage's own gate as the owner fallback.

const ungatedTechnicalNode = "technical_approve"

// ungatedTechnicalPlan returns the packaged Plan files with the technical stage's
// applied edge sent through lf_plan_approve.
func ungatedTechnicalPlan(t *testing.T) fstest.MapFS {
	t.Helper()
	source, err := teamsourcetest.Files(t.Context(), teamsourcetest.Go)
	if err != nil {
		t.Fatal(err)
	}
	out := fstest.MapFS{}
	for name, raw := range source {
		if rest, ok := strings.CutPrefix(name, "plan/"); ok {
			out[rest] = &fstest.MapFile{Data: raw}
		}
	}
	dot := string(out["plan.dot"].Data)
	// The applied draft reaches the gate through the advisory judge, which the
	// fixture keeps on the review outcome only.
	const applied = `technical_draft -> technical_check [lf_on="applied"];`
	if !strings.Contains(dot, applied) {
		t.Fatal("shipped technical stage no longer routes its applied draft to the judge before the gate")
	}
	node := ` ` + ungatedTechnicalNode + ` [lf_action="lf_plan_approve", lf_source="technical_agent", lf_outcomes="approved,review,superseded,blocked,failed", lf_limit="8"];
`
	edges := `technical_draft -> ` + ungatedTechnicalNode + ` [lf_on="applied"];
 ` + ungatedTechnicalNode + ` -> waiting [lf_on="approved"];
 ` + ungatedTechnicalNode + ` -> technical_check [lf_on="review"];
 ` + ungatedTechnicalNode + ` -> stopped [lf_on="superseded"];
 ` + ungatedTechnicalNode + ` -> failed [lf_on="blocked"];
 ` + ungatedTechnicalNode + ` -> failed [lf_on="failed"];`
	dot = strings.Replace(dot, applied, edges, 1)
	dot = strings.Replace(dot, " // ---- Stage 4", node+" // ---- Stage 4", 1)
	out["plan.dot"] = &fstest.MapFile{Data: []byte(dot)}
	return out
}

// ungatedTechnicalFixture is the Plan fixture whose captured graph has no gate on
// the technical stage. The returned arm function installs a change to the saved
// Plan state that is applied exactly once, after the technical draft is applied
// and before the approval node runs.
func ungatedTechnicalFixture(t *testing.T) (f *fixture, base Request, forge *fakeForge, arm func(func(*planbrief.PlanState))) {
	t.Helper()
	f, _, base, _, forge = graphPlanFixtureWithForge(t, "agent")
	plan := ungatedTechnicalPlan(t)
	f.opts.WorkflowSource = func(ctx context.Context, _ store.Repo, w agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		return fixtureInstalled(teamsource.CaptureWorkflowIdentity(ctx, plan, "plan.dot", teamsourcetest.Go, w, specs))
	}
	var change atomic.Pointer[func(*planbrief.PlanState)]
	// The driver reads the clock before it executes each node, so the clock is the
	// moment between the effect's completion and the approval node's start.
	f.opts.MCP.Now = func() time.Time {
		if apply := change.Load(); apply != nil {
			if current, ok := newestGraphInstance(f); ok {
				if current.CurrentNode == ungatedTechnicalNode && change.CompareAndSwap(apply, nil) {
					if err := f.db.UpdatePlanWorkflowState(context.Background(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
						(*apply)(s)
						return true, nil
					}); err != nil {
						t.Errorf("change saved state before approval: %v", err)
					}
				}
			}
		}
		return time.Now()
	}
	arm = func(fn func(*planbrief.PlanState)) { change.Store(&fn) }
	return f, base, forge, arm
}

func postsContaining(forge *fakeForge, text string) int {
	n := 0
	for _, post := range forge.all() {
		if strings.Contains(post.body, text) {
			n++
		}
	}
	return n
}

// newestGraphInstance is the most recently created graph instance. An owner reply
// runs under the story's own task, so it is not the session's latest instance.
func newestGraphInstance(f *fixture) (store.GraphInstance, bool) {
	var newest store.GraphInstance
	found := false
	for id := int64(1); ; id++ {
		instance, err := f.db.GetGraphInstance(context.Background(), id)
		if err != nil {
			return newest, found
		}
		newest, found = instance, true
	}
}

func executedUngated(t *testing.T, f *fixture) string {
	t.Helper()
	instance, ok := newestGraphInstance(f)
	if !ok {
		t.Fatal("no graph instance")
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	var nodes []string
	for _, a := range attempts {
		nodes = append(nodes, a.Node)
	}
	return strings.Join(nodes, " ")
}

func TestPlanApprove_GraphApprovalRecordsTechnicalAndOffersAcceptance(t *testing.T) {
	f, base, forge, _ := ungatedTechnicalFixture(t)
	executeGraphPlan(t, f, base)
	before := len(forge.all())
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))

	if !strings.Contains(executedUngated(t, f), ungatedTechnicalNode) || strings.Contains(executedUngated(t, f), "technical_review") {
		t.Fatalf("technical stage did not end at the graph approval: %s", executedUngated(t, f))
	}
	s := graphPlanState(t, f)
	item := s.Stories[0]
	want, err := s.ReviewRevision(item.Key, "technical")
	if err != nil {
		t.Fatal(err)
	}
	if item.Approvals["technical"] != want || item.ApprovedBy["technical"] != planbrief.ApprovalByGraph {
		t.Fatalf("technical approval not recorded by the graph at the current revision: approvals=%v by=%v want %q", item.Approvals, item.ApprovedBy, want)
	}
	if item.ApprovedBy["story"] != planbrief.ApprovalByOwner && item.ApprovedBy["story"] != "" {
		t.Fatalf("the owner's story approval was re-attributed: %v", item.ApprovedBy)
	}
	if item.Wait != nil {
		t.Fatalf("a wait is open for a stage the graph approved: %+v", item.Wait)
	}
	if n := postsContaining(forge, "/approve-plan story-001 technical"); n != 0 {
		t.Fatalf("technical review command posted %d times for a graph-approved stage", n)
	}
	if after := len(forge.all()); after != before {
		for _, post := range forge.all()[before:] {
			t.Logf("post: %s", post.body)
		}
		t.Fatalf("graph approval posted %d comments, want none", after-before)
	}
	if bodies := failureBodies(forge); len(bodies) != 0 {
		t.Fatalf("failure notice posted: %q", bodies)
	}
	if stage, create, ok := s.StoryNextStep(item.Key); !ok || create || stage != "acceptance" {
		t.Fatalf("next step = %q create=%v ok=%v, want the acceptance stage", stage, create, ok)
	}
	if got := s.GraphApprovedStages(); len(got) != 1 || got[0].Key != item.Key || got[0].Stage != "technical" {
		t.Fatalf("graph-approved stages = %+v", got)
	}
}

func TestPlanApprove_UpstreamApprovalWithdrawnAsksTheOwner(t *testing.T) {
	f, base, forge, arm := ungatedTechnicalFixture(t)
	executeGraphPlan(t, f, base)
	// A story this one depends on, not approved at the technical stage. The story
	// stage approval is kept current so only the upstream guard is in doubt.
	arm(func(s *planbrief.PlanState) {
		s.Stories = append(s.Stories, planbrief.PlanStoryState{Key: "dep-001", Stage: "story", Approvals: map[string]string{}, Draft: planbrief.PlanDraft{Key: "dep-001"}})
		s.Stories[0].Draft.Dependencies.StoryKeys = []string{"dep-001"}
		ref, err := s.ReviewRevision("story-001", "story")
		if err != nil {
			t.Errorf("story revision: %v", err)
		}
		s.Stories[0].Approvals["story"] = ref
	})
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))

	if !strings.HasSuffix(executedUngated(t, f), ungatedTechnicalNode+" technical_check technical_advice technical_review") {
		t.Fatalf("review outcome did not reach the stage gate: %s", executedUngated(t, f))
	}
	item := graphPlanState(t, f).Stories[0]
	if item.Approvals["technical"] != "" || item.ApprovedBy["technical"] != "" {
		t.Fatalf("technical was approved despite the doubt: %v %v", item.Approvals, item.ApprovedBy)
	}
	if item.Wait == nil || item.Wait.Kind != "review" || item.Wait.Stage != "technical" {
		t.Fatalf("technical review wait not saved: %+v", item.Wait)
	}
	if want := "/approve-plan story-001 technical " + item.Candidate; postsContaining(forge, want) == 0 {
		t.Fatalf("review does not show %q", want)
	}
	if bodies := failureBodies(forge); len(bodies) != 0 {
		t.Fatalf("failure notice posted: %q", bodies)
	}
}

func TestPlanApprove_HoldRaisedBeforeRecordingFailsWithCause(t *testing.T) {
	f, base, forge, arm := ungatedTechnicalFixture(t)
	executeGraphPlan(t, f, base)
	arm(func(s *planbrief.PlanState) { s.Stories[0].Hold = "owner paused this story" })
	// The run reports the failure; the executed outcome is checked on the saved state.
	_, _ = Execute(t.Context(), f.opts, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))

	item := graphPlanState(t, f).Stories[0]
	if item.Approvals["technical"] != "" || item.ApprovedBy["technical"] != "" || item.Wait != nil {
		t.Fatalf("held story was approved or waits: approvals=%v by=%v wait=%+v", item.Approvals, item.ApprovedBy, item.Wait)
	}
	if !strings.HasSuffix(executedUngated(t, f), ungatedTechnicalNode+" failed") {
		t.Fatalf("blocked outcome did not reach the failure comment: %s", executedUngated(t, f))
	}
	bodies := failureBodies(forge)
	if len(bodies) != 1 || !strings.Contains(bodies[0], "held") {
		t.Fatalf("failure comment does not name the hold: %q", bodies)
	}
	if postsContaining(forge, "/approve-plan story-001 technical") != 0 {
		t.Fatal("a review was shown for a held story")
	}
}

func TestPlanApprove_StageRewoundBeforeRecordingEndsQuietly(t *testing.T) {
	f, base, forge, arm := ungatedTechnicalFixture(t)
	executeGraphPlan(t, f, base)
	before := len(forge.all())
	arm(func(s *planbrief.PlanState) {
		s.Stories[0].Stage = "story"
		delete(s.Stories[0].Approvals, "story")
		s.Stories[0].ApprovedBy = nil
	})
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))

	s := graphPlanState(t, f)
	item := s.Stories[0]
	if item.Approvals["technical"] != "" || item.ApprovedBy["technical"] != "" || item.Wait != nil {
		t.Fatalf("rewound story was approved or waits: approvals=%v by=%v wait=%+v", item.Approvals, item.ApprovedBy, item.Wait)
	}
	if !strings.HasSuffix(executedUngated(t, f), ungatedTechnicalNode) {
		t.Fatalf("superseded outcome did not end at the terminal: %s", executedUngated(t, f))
	}
	if after := len(forge.all()); after != before {
		for _, post := range forge.all()[before:] {
			t.Logf("post: %s", post.body)
		}
		t.Fatalf("rewound stage posted %d comments, want none", after-before)
	}
	if stage, create, ok := s.StoryNextStep(item.Key); !ok || create || stage != "story" {
		t.Fatalf("next step = %q create=%v ok=%v, want the earlier story stage", stage, create, ok)
	}
}
