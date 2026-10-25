package turn

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
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
	definition, err := teamsource.CapturePlan(t.Context(), specs)
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

// upstreamAgent is the creation agent with hooks around one story stage, so a
// test can move an upstream story while a dependant's agent is running.
type upstreamAgent struct {
	creationAgent
	// instead, when it reports true, replaces the agent's own work.
	instead func(s planAgentStep) (*agent.RunResult, bool)
	// after runs once the agent has submitted, before the factory accepts.
	after func(s planAgentStep)
}

// upstreamFixture is indepFixture with hooks: story-001 depends on story-002,
// and story-003 on both. It ends with story-002's technical stage approved and
// its acceptance review waiting, so story-001's technical stage is owed.
func upstreamFixture(t *testing.T) (*fixture, *graphPlanHTTP, Request, *fakeForge, *upstreamAgent) {
	t.Helper()
	u := &upstreamAgent{}
	var fx *fixture
	inner := u.behave(t, &fx)
	f, remote, base, forge, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		if u.instead != nil {
			if out, ok := u.instead(s); ok {
				return out, nil
			}
		}
		out, err := inner(s)
		if u.after != nil {
			u.after(s)
		}
		return out, err
	})
	fx = f
	revision := saveProposedSet(t, f, promoteEntries())
	if _, err := Execute(t.Context(), f.opts, promoteTrigger(base, 900, 7, promoteCommand(revision))); err != nil {
		t.Fatalf("promotion: %v logs=%s", err, f.logs.String())
	}
	draftAllStories(t, f, base)
	indepApprove(t, f, base, "story-002", 940)
	if w := indepStory(t, f, "story-002").Wait; w == nil || w.Stage != "acceptance" {
		t.Fatalf("story-002 should wait for its acceptance review: %+v", w)
	}
	state := graphPlanState(t, f)
	if stage, _, ok := state.StoryNextStep("story-001"); stage != "technical" || !ok {
		t.Fatalf("story-001 technical stage is not owed: %q %v", stage, ok)
	}
	return f, remote, base, forge, u
}

// planWorkRun is the factory's own scheduled Plan work for one story task.
func planWorkRun(t *testing.T, f *fixture, base Request, key string) Request {
	t.Helper()
	req := asStoryTask(t, f, base, key)
	req.Trigger = Trigger{CommentID: (store.Schedule{ID: nextStoryTrigger()}).TriggerID(), Body: PlanWorkPayload, Author: "owner", AuthorID: 7}
	req.Command = Command{Kind: KindFreeText, Text: PlanWorkPayload}
	return req
}

// setUpstream changes story-002's saved state, as its own stage completion does.
func setUpstream(t *testing.T, f *fixture, change func(*planbrief.PlanStoryState)) {
	t.Helper()
	mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
		for i := range s.Stories {
			if s.Stories[i].Key == "story-002" {
				change(&s.Stories[i])
			}
		}
		return true, nil
	}))
}

func storyDigest(t *testing.T, item planbrief.PlanStoryState) string {
	t.Helper()
	raw, err := json.Marshal(struct {
		Stage, Candidate string
		Draft            planbrief.PlanDraft
		Approvals        map[string]string
		Wait             *planbrief.PlanWait
	}{item.Stage, item.Candidate, item.Draft, item.Approvals, item.Wait})
	mustProgress(t, err)
	return string(raw)
}

// requireDeferred checks the records a quietly deferred step leaves: no failure
// comment, a run that did not fail, a failed agent attempt carrying the
// publishing diagnostic, a terminal instance that is not parked, and a story
// that is exactly as it was. Replay and recovery post nothing either.
func requireDeferred(t *testing.T, f *fixture, forge *fakeForge, req Request, out Result, err error, before string) {
	t.Helper()
	if err != nil || out.Err != nil {
		t.Fatalf("deferred step failed: err=%v result=%+v logs=%s", err, out, f.logs.String())
	}
	if bodies := failureBodies(forge); len(bodies) != 0 {
		t.Fatalf("a publication window posted a failure comment: %q", bodies)
	}
	if out.Run.ID == 0 || out.Run.Status == store.RunFailed || out.Reason != planUpstreamWait {
		t.Fatalf("deferred run records: %+v reason=%q", out.Run, out.Reason)
	}
	if got := storyDigest(t, indepStory(t, f, "story-001")); got != before {
		t.Fatalf("the deferred result changed the story:\nbefore %s\nafter  %s", before, got)
	}
	instance, ierr := f.db.LatestWorkflowInstance(t.Context(), req.SessionID, "plan")
	mustProgress(t, ierr)
	if instance.Status != store.GraphFailed || !instance.NextDueAt.IsZero() {
		t.Fatalf("deferred instance is running or parked: %+v", instance)
	}
	attempts, aerr := f.db.ListGraphNodeAttemptsByRun(t.Context(), out.Run.ID)
	mustProgress(t, aerr)
	var agentAttempt store.GraphNodeAttempt
	for _, a := range attempts {
		if a.Node == "technical_agent" {
			agentAttempt = a
		}
	}
	diagnostic, derr := PlanAttemptDiagnostics(t.Context(), f.db, agentAttempt.ID)
	mustProgress(t, derr)
	if agentAttempt.Status != store.GraphFailed || !strings.Contains(diagnostic, "plan dependency story-002 is still publishing its stage") {
		t.Fatalf("deferred attempt %+v diagnostic %q", agentAttempt, diagnostic)
	}
	if failed, ferr := PlanLatestAgentFailedUpstream(t.Context(), f.db, req.SessionID); ferr != nil || !failed {
		t.Fatalf("resume rule does not see the upstream failure: %v %v", failed, ferr)
	}
	run, rerr := f.db.GetRun(t.Context(), out.Run.ID)
	mustProgress(t, rerr)
	recovered := run
	recovered.Status, recovered.Error = store.RunFailed, "recovered after restart"
	for _, r := range []store.Run{run, recovered} {
		mustProgress(t, ensurePlanFailureReported(t.Context(), f.opts, req, r))
	}
	if bodies := failureBodies(forge); len(bodies) != 0 {
		t.Fatalf("replay or recovery posted a failure comment: %q", bodies)
	}
	if replay, rerr := Execute(t.Context(), f.opts, req); rerr != nil || replay.Run.ID != out.Run.ID || len(failureBodies(forge)) != 0 {
		t.Fatalf("replay of the deferred run: %v run=%d", rerr, replay.Run.ID)
	}
	if latest, lerr := f.db.LatestWorkflowInstance(t.Context(), req.SessionID, "plan"); lerr != nil || latest.Status == store.GraphRunning {
		t.Fatalf("a Plan instance is left running or parked: %+v %v", latest, lerr)
	}
}

// #1160 c9/c14: in factory-scheduled Plan work, an upstream story that enters
// its publication window while the dependant's agent runs ends the step
// quietly, whether the window opens before acceptance or already refuses the
// agent's submit.
func TestPlanFailure_UpstreamPublicationWindowDefersQuietly(t *testing.T) {
	t.Run("pending before acceptance", func(t *testing.T) {
		f, _, base, forge, u := upstreamFixture(t)
		before := storyDigest(t, indepStory(t, f, "story-001"))
		u.after = func(s planAgentStep) {
			if s.story == "story-001" && s.stage == "technical" {
				setUpstream(t, f, func(st *planbrief.PlanStoryState) { st.Effect, st.EffectRequest = "pending", "plan-request-77-78" })
			}
		}
		req := planWorkRun(t, f, base, "story-001")
		out, err := Execute(t.Context(), f.opts, req)
		u.after = nil
		requireDeferred(t, f, forge, req, out, err, before)
	})
	t.Run("pending at submit", func(t *testing.T) {
		f, _, base, forge, u := upstreamFixture(t)
		before := storyDigest(t, indepStory(t, f, "story-001"))
		refused := false
		u.instead = func(s planAgentStep) (*agent.RunResult, bool) {
			if s.story != "story-001" || s.stage != "technical" {
				return nil, false
			}
			setUpstream(t, f, func(st *planbrief.PlanStoryState) { st.Effect, st.EffectRequest = "pending", "plan-request-77-78" })
			request := integrationCreateRequest(s.story)
			binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), s.story)
			mustProgress(t, err)
			request.Operation, request.ExpectedRevision = "update", binding.Revision
			mustProgress(t, json.Unmarshal([]byte(binding.DraftJSON), &request.Draft))
			request.Draft.Architecture = &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Roles", Decision: "Reuse focused roles", Rationale: "Existing boundaries"}}}
			request.Draft.Guidance = []string{"agent-setup"}
			request.ChangedSections = []string{"architecture", "guidance"}
			good := integrationDone(request, "Drafted "+s.story)
			r, err := s.client.Call(t.Context(), "communication_submit", submitBytes(t, good))
			if err == nil && r.Error == "" {
				var got AgentResultSubmission
				mustProgress(t, json.Unmarshal(r.Result, &got))
				if got.Validation.Valid {
					t.Fatalf("a submit during the publication window was accepted: %+v", got)
				}
			}
			refused = true
			// The upstream's publication lands before the factory accepts.
			setUpstream(t, f, func(st *planbrief.PlanStoryState) { st.Effect = "applied" })
			return agentDone(t, good), true
		}
		req := planWorkRun(t, f, base, "story-001")
		out, err := Execute(t.Context(), f.opts, req)
		u.instead = nil
		if !refused {
			t.Fatal("the agent did not run")
		}
		requireDeferred(t, f, forge, req, out, err, before)
	})
}

// ownerReplyRun is an owner's plain reply on a story's own issue.
func ownerReplyRun(t *testing.T, f *fixture, base Request, key string, id int64, body string) Request {
	t.Helper()
	req := asStoryTask(t, f, base, key)
	binding, err := f.db.GetPlanStoryDraft(t.Context(), f.ref().String(), key)
	mustProgress(t, err)
	reply := &planbrief.PlanOwnerReply{ID: id, OwnerID: 7, ActorID: 7, IssueID: binding.IssueID, Key: key, Body: body}
	req.Trigger = Trigger{CommentID: id, Body: body, Author: "owner", AuthorID: 7, PlanReply: reply}
	req.Command = Command{Kind: KindFreeText, Text: body}
	return req
}

// requireToldOwner checks that the owner got the one existing failure comment
// naming the cause and that the dependant's result was not applied.
func requireToldOwner(t *testing.T, f *fixture, forge *fakeForge, out Result, cause, before string) {
	t.Helper()
	requireFailureComment(t, forge, cause)
	if out.Run.Status != store.RunFailed {
		t.Fatalf("a told failure finished nominally: %+v", out.Run)
	}
	if before != "" {
		if got := storyDigest(t, indepStory(t, f, "story-001")); got != before {
			t.Fatalf("the refused result changed the story:\nbefore %s\nafter  %s", before, got)
		}
	}
}

// #1160 c10: a real hold, an owner-started run, another schedule, or a change
// to the dependant's review basis still tells the owner as before.
func TestPlanFailure_RealUpstreamProblemsStillTellTheOwner(t *testing.T) {
	pending := func(st *planbrief.PlanStoryState) { st.Effect, st.EffectRequest = "pending", "plan-request-77-78" }
	t.Run("held upstream in Plan work", func(t *testing.T) {
		f, _, base, forge, u := upstreamFixture(t)
		before := storyDigest(t, indepStory(t, f, "story-001"))
		u.after = func(s planAgentStep) {
			if s.story == "story-001" {
				setUpstream(t, f, func(st *planbrief.PlanStoryState) { pending(st); st.EffectHold = "the effect is uncertain" })
			}
		}
		out, _ := Execute(t.Context(), f.opts, planWorkRun(t, f, base, "story-001"))
		requireToldOwner(t, f, forge, out, "plan dependency story-002 is inactive or unresolved", before)
	})
	t.Run("owner reply with the upstream publishing", func(t *testing.T) {
		f, _, base, forge, u := upstreamFixture(t)
		before := storyDigest(t, indepStory(t, f, "story-001"))
		ran := false
		u.after = func(s planAgentStep) {
			if s.story == "story-001" {
				ran = true
				setUpstream(t, f, pending)
			}
		}
		out, _ := Execute(t.Context(), f.opts, ownerReplyRun(t, f, base, "story-001", 5101, "Please keep the technical design small."))
		if !ran {
			t.Fatalf("the owner's reply did not run the stage: %+v logs=%s", out, f.logs.String())
		}
		requireToldOwner(t, f, forge, out, "plan dependency story-002 is still publishing its stage", before)
	})
	t.Run("another schedule with the upstream publishing", func(t *testing.T) {
		f, _, base, forge, u := upstreamFixture(t)
		before := storyDigest(t, indepStory(t, f, "story-001"))
		u.after = func(s planAgentStep) {
			if s.story == "story-001" {
				setUpstream(t, f, pending)
			}
		}
		req := planWorkRun(t, f, base, "story-001")
		req.Trigger.Body, req.Command.Text = "Check the plan every morning.", "Check the plan every morning."
		out, _ := Execute(t.Context(), f.opts, req)
		requireToldOwner(t, f, forge, out, "plan dependency story-002 is still publishing its stage", before)
	})
	t.Run("upstream change at the dependant's stage", func(t *testing.T) {
		f, _, base, forge, u := upstreamFixture(t)
		u.after = func(s planAgentStep) {
			if s.story != "story-001" {
				return
			}
			mustProgress(t, f.db.UpdatePlanWorkflowState(t.Context(), f.ref().String(), func(st *planbrief.PlanState) (bool, error) {
				for _, item := range st.Stories {
					if item.Key == "story-002" {
						changed := technicalChange(t, item.Draft)
						return true, st.PutCandidate(changed, "technical", "upstream-technical-change")
					}
				}
				return false, nil
			}))
		}
		out, _ := Execute(t.Context(), f.opts, planWorkRun(t, f, base, "story-001"))
		requireToldOwner(t, f, forge, out, "storyrequest.upstream", "")
		if item := indepStory(t, f, "story-001"); item.Wait != nil {
			t.Fatalf("a stale result opened a review: %+v", item.Wait)
		}
	})
}

// technicalChange returns d with only its technical decisions changed.
func technicalChange(t *testing.T, d planbrief.PlanDraft) planbrief.PlanDraft {
	t.Helper()
	raw, err := json.Marshal(d.Architecture)
	mustProgress(t, err)
	var arch planbrief.PlanArchitecture
	mustProgress(t, json.Unmarshal(raw, &arch))
	if len(arch.General) == 0 {
		t.Fatal("draft has no technical decisions")
	}
	arch.General[0].Decision = "A revised technical decision"
	d.Architecture = &arch
	return d
}
