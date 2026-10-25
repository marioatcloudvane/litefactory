package turn

import (
	"context"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func failureBodies(forge *fakeForge) []string {
	var out []string
	for _, post := range forge.all() {
		if strings.Contains(post.body, "Plan could not finish this step") {
			out = append(out, post.body)
		}
	}
	return out
}

func requireFailureComment(t *testing.T, forge *fakeForge, cause string) {
	t.Helper()
	bodies := failureBodies(forge)
	if len(bodies) != 1 {
		t.Fatalf("expected exactly one failure comment, got %d of %d posts", len(bodies), len(forge.all()))
	}
	for _, want := range []string{"Cause:", cause, "To retry, reply on this issue"} {
		if !strings.Contains(bodies[0], want) {
			t.Fatalf("failure comment lacks %q:\n%s", want, bodies[0])
		}
	}
}

func requireRunFailureReported(t *testing.T, f *fixture, runID int64, want bool) {
	t.Helper()
	got, err := f.db.GraphFailureReported(t.Context(), runID, PlanFailureNode)
	mustProgress(t, err)
	if got != want {
		t.Fatalf("failure reported for run %d = %v, want %v", runID, got, want)
	}
}

// A failed or stopped agent step publishes a comment that states the cause and
// how to retry, through the shipped failure node, exactly once, and replaying the
// failed run neither invokes the agent nor posts a second comment.
func TestPlanFailure_AgentFailureIsPublishedOnceWithCauseAndRetry(t *testing.T) {
	for _, tt := range []struct {
		name   string
		stop   agent.Outcome
		reason string
		cause  string
	}{
		{"timed out", agent.OutcomeTimedOut, "wall-clock limit", "process outcome"},
		{"failed", agent.OutcomeFailed, "the provider refused", "process outcome"},
		{"stopped", agent.OutcomeAbandoned, "daemon stopped", "process outcome"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _, base, calls, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) { a.Stop, a.Reason = tt.stop, tt.reason })
			out, err := Execute(t.Context(), f.opts, base)
			if err == nil || out.Run.Status != store.RunFailed {
				t.Fatalf("a failed agent step completed nominally: %+v err=%v", out.Run, err)
			}
			requireFailureComment(t, forge, tt.cause)
			requireRunFailureReported(t, f, out.Run.ID, true)
			replay, err := Execute(t.Context(), f.opts, base)
			mustProgress(t, err)
			if *calls != 1 || replay.Run.ID != out.Run.ID || !replay.ResearchGraph || len(failureBodies(forge)) != 1 {
				t.Fatalf("replay repeated work: calls=%d run=%d posts=%d", *calls, replay.Run.ID, len(failureBodies(forge)))
			}
		})
	}
}

// A failure comment that cannot be published leaves the owner's comment
// unhandled: the run is not reported and every replay fails, never reposting
// blindly, until the delivery is settled. Once the comment is shown to have
// reached the issue, the replay completes from the retained bytes, once.
func TestPlanFailure_UnpublishedFailureKeepsOwnerCommentUnhandled(t *testing.T) {
	f, _, base, calls, forge := graphPlanFixtureWithForge(t, "agent", func(a *graphPlanAnswer) { a.Stop, a.Reason = agent.OutcomeTimedOut, "wall-clock limit" })
	forge.failWith(500)
	out, err := Execute(t.Context(), f.opts, base)
	if err == nil || out.Run.Status != store.RunFailed {
		t.Fatalf("failure not reported as failed: %+v err=%v", out.Run, err)
	}
	requireRunFailureReported(t, f, out.Run.ID, false)
	forge.recover()
	if _, err = Execute(t.Context(), f.opts, base); err == nil {
		t.Fatal("a replay with no confirmed failure comment reported success")
	}
	requireRunFailureReported(t, f, out.Run.ID, false)
	if attempts := forge.attemptCount(); attempts != 1 {
		t.Fatalf("the uncertain failure comment was posted blindly again: %d attempts", attempts)
	}
	// The lost reply: the comment did reach the issue.
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "plan")
	mustProgress(t, err)
	comments, err := f.db.ListGraphComments(t.Context(), instance.ID, "", 10)
	mustProgress(t, err)
	if len(comments) != 1 || comments[0].Node != PlanFailureNode || comments[0].Kind != "failure" {
		t.Fatalf("the failure comment was not retained before delivery: %+v", comments)
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), comments[0].Key)
	mustProgress(t, err)
	forge.mu.Lock()
	forge.posts = append(forge.posts, forgePost{path: "/repos/" + f.ref().Owner + "/" + f.ref().Repo + "/issues/" + strconv.Itoa(f.ref().Issue) + "/comments", body: delivery.Body, id: 6001})
	forge.mu.Unlock()
	if _, err = Execute(t.Context(), f.opts, base); err != nil {
		t.Fatalf("replay once the comment is confirmed: %v", err)
	}
	requireRunFailureReported(t, f, out.Run.ID, true)
	requireFailureComment(t, forge, "process outcome")
	if *calls != 1 {
		t.Fatalf("the agent ran %d times", *calls)
	}
}

// Every failing outcome of the story effect reaches the failure comment node
// inside the graph: the run itself finishes, the owner is told the cause, and no
// silent stop remains.
func TestPlanFailure_StoryEffectOutcomesReachTheFailureComment(t *testing.T) {
	// A lost create reply is an error of the effect step, not a declared
	// outcome: the attempt fails and is reported from outside the graph.
	t.Run("lost create reply", func(t *testing.T) {
		f, remote, base, _, forge := graphPlanFixtureWithForge(t, "agent")
		remote.loseCreate = true
		out, err := Execute(t.Context(), f.opts, base)
		if err == nil || out.Run.Status != store.RunFailed {
			t.Fatalf("a failed story effect completed nominally: %+v err=%v", out.Run, err)
		}
		requireFailureComment(t, forge, "lost create reply")
		requireRunFailureReported(t, f, out.Run.ID, true)
	})
	t.Run("interrupted attachment", func(t *testing.T) {
		f, remote, base, _, forge := graphPlanFixtureWithForge(t, "agent")
		remote.failAttach = true
		out, err := Execute(t.Context(), f.opts, base)
		if err == nil || out.Run.Status != store.RunFailed {
			t.Fatalf("an interrupted attachment completed nominally: %+v err=%v", out.Run, err)
		}
		requireFailureComment(t, forge, "attach")
		requireRunFailureReported(t, f, out.Run.ID, true)
	})
	t.Run("conflict", func(t *testing.T) {
		f, remote, base, _, forge := graphPlanFixtureWithForge(t, "agent")
		executeGraphPlan(t, f, base)
		before := len(forge.all())
		remote.mu.Lock()
		remote.children[400]["body"] = "an owner edit the draft must not overwrite"
		remote.mu.Unlock()
		out, err := Execute(t.Context(), f.opts, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
		mustProgress(t, err)
		if !out.Succeeded() {
			t.Fatalf("run failed: %+v", out.Run)
		}
		if len(failureBodies(forge)) != 1 || len(forge.all()) != before+1 {
			t.Fatalf("expected exactly one failure comment: %d of %d posts", len(failureBodies(forge)), len(forge.all())-before)
		}
		requireFailureComment(t, forge, "conflict")
		requireRunFailureReported(t, f, out.Run.ID, true)
	})
}

// The failure comment content is Plan's own builder: a blocked, cause-bearing
// result that clears the shared result validation and the failure guard.
func TestPlanFailure_CandidateIsValidAndBounded(t *testing.T) {
	t.Parallel()
	scope := ar.Context{SchemaVersion: ar.SchemaVersion, Mode: "plan", WorkflowVersion: store.WorkflowSeparated, QuestionsAllowed: true}
	for name, cause := range map[string]error{"cause": context.Canceled, "none": nil, "long": errString(strings.Repeat("x", 5000))} {
		candidate := planFailureCandidate(Request{}, cause)
		if d := ar.ValidateResult(candidate, scope); !d.Valid || !d.Complete {
			t.Fatalf("%s: not a valid result: %+v", name, d.Diagnostics)
		}
		if candidate.Outcome == "completed" || candidate.Reason == "" || len(candidate.Questions) != 0 {
			t.Fatalf("%s: fails the failure guard: %+v", name, candidate)
		}
		if len([]rune(candidate.Reason)) > maxFailureReasonRunes+3 {
			t.Fatalf("%s: cause is unbounded", name)
		}
	}
	text := renderPlanFailure(planFailureCandidate(Request{}, context.Canceled), "", naming.Ref{}, "")
	if !strings.Contains(text, "context canceled") || !strings.Contains(text, "To retry, reply on this issue") {
		t.Fatalf("text lacks cause or retry: %s", text)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// The shipped graph declares exactly one failure comment, and every failing
// outcome of an agent or story effect step leads to it.
func TestPlanFailure_ShippedGraphRoutesEveryFailureToTheOneCommentNode(t *testing.T) {
	t.Parallel()
	specs, err := PlanSpecs()
	mustProgress(t, err)
	definition, err := teamsourcetest.CapturePlan(t.Context(), specs)
	mustProgress(t, err)
	node, err := failureCommentNode(definition.Profile)
	mustProgress(t, err)
	if node != PlanFailureNode {
		t.Fatalf("failure node %q, want %q", node, PlanFailureNode)
	}
	for _, stage := range []string{"story", "technical", "acceptance"} {
		for _, edge := range [][2]string{{stage + "_agent", "failed"}, {stage + "_draft", "failed"}, {stage + "_draft", "conflict"}, {stage + "_draft", "uncertain"}} {
			if target, ok := graphexec.Decide(definition.Profile, edge[0], edge[1]); !ok || target != PlanFailureNode {
				t.Errorf("%s/%s reaches %q, want the failure node", edge[0], edge[1], target)
			}
		}
	}
	if _, err := failureCommentNode(&graphexec.Profile{}); err == nil {
		t.Error("a graph with no failure comment node was admitted")
	}
}
