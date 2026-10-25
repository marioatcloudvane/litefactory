package daemon

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// approvedPlanState holds one story with the given task owner and, when
// approve is set, the owner's approval of all three stages.
func approvedPlanState(t *testing.T, owner string, approve bool) planbrief.PlanState {
	t.Helper()
	d := planbrief.PlanDraft{Key: "story-a", Name: "Shape a story", UserStory: planbrief.UserStoryContent{Actor: "owner", Want: "clear intent", Benefit: "shared understanding"}, Problem: "Unclear requirements", InScope: []string{"Story conversation"}, NotInScope: []string{"Implementation"},
		Architecture: &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "Ownership", Decision: "Separate roles", Rationale: "Focused responsibilities"}}}, Guidance: []string{"agent-setup"},
		Criteria: []planbrief.PlanCriterion{{ID: "AC-001", Text: "Draft retains intent", ProofMethod: "Unit test"}},
		Tasks:    []planbrief.TaskContent{{ID: "T-001", Description: "Implement draft", OwnerRole: owner, CriterionIDs: []string{"AC-001"}}}}
	s := planbrief.PlanState{SharedRevision: "shared-1"}
	if err := s.PutCandidate(d, "story", "candidate"); err != nil {
		t.Fatal(err)
	}
	if !approve {
		return s
	}
	for n := range 3 {
		st := s.Stories[0]
		if err := s.OpenPlanWait(st.Key, "wait-"+st.Stage, "review", 100); err != nil {
			t.Fatal(err)
		}
		r := planbrief.PlanOwnerReply{ID: int64(10 + n), OwnerID: 7, ActorID: 7, IssueID: 100, Key: st.Key, Stage: st.Stage, Candidate: st.Candidate, WaitID: "wait-" + st.Stage, Body: "/approve-plan " + st.Key + " " + st.Stage + " " + st.Candidate}
		if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
			t.Fatalf("approve %s: %v %v", r.Stage, ok, err)
		}
	}
	return s
}

func TestAssemblyBlocked(t *testing.T) {
	scope := planbrief.ReleaseScope([]string{"implementation-engineer"}, []string{"agent-setup"})
	t.Run("approved but invalid is reported", func(t *testing.T) {
		s := approvedPlanState(t, "frontend-engineer", true)
		if _, due := parentWorkIDWithScope(s, scope); due {
			t.Fatal("assembly due for an unknown owner")
		}
		if err := assemblyBlocked(s, scope); err == nil || !strings.Contains(err.Error(), "authorized role") {
			t.Fatal("blocked assembly not explained:", err)
		}
	})
	t.Run("awaiting approval is silent", func(t *testing.T) {
		if err := assemblyBlocked(approvedPlanState(t, "frontend-engineer", false), scope); err != nil {
			t.Fatal("waiting plan reported as blocked:", err)
		}
	})
	t.Run("go- owner of a release role assembles", func(t *testing.T) {
		s := approvedPlanState(t, "go-implementation-engineer", true)
		if _, due := parentWorkIDWithScope(s, scope); !due {
			t.Fatal("assembly not due")
		}
		if err := assemblyBlocked(s, scope); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("empty plan is silent", func(t *testing.T) {
		if err := assemblyBlocked(planbrief.PlanState{}, scope); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStoryWorkIDRetainedEffectHolds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		story planbrief.PlanStoryState
		want  bool
	}{
		{"request hold", planbrief.PlanStoryState{Effect: "uncertain", EffectRequest: "original", EffectHolds: map[string]string{"original": "content conflict"}}, false},
		{"legacy hold", planbrief.PlanStoryState{Effect: "uncertain", EffectRequest: "original", EffectHold: "content conflict"}, false},
		{"independent hold", planbrief.PlanStoryState{Effect: "pending", EffectRequest: "newer", EffectHolds: map[string]string{"original": "content conflict"}}, false},
		{"unheld uncertain", planbrief.PlanStoryState{Effect: "uncertain", EffectRequest: "original"}, true},
		{"unheld pending", planbrief.PlanStoryState{Effect: "pending", EffectRequest: "original"}, true},
		{"ordinary hold with reconciliation debt", planbrief.PlanStoryState{Effect: "pending", EffectRequest: "original", Hold: "waiting for owner"}, true},
		{"owner wait", planbrief.PlanStoryState{Hold: "waiting for owner"}, false},
		{"upstream wait", planbrief.PlanStoryState{Draft: planbrief.PlanDraft{Dependencies: planbrief.DependencyContent{StoryKeys: []string{"upstream"}}}}, false},
		{"healthy draft", planbrief.PlanStoryState{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.story.Key = "story-001"
			state := planbrief.PlanState{SharedRevision: "shared", StorySet: &planbrief.PlanStorySetState{Promoted: true}, Stories: []planbrief.PlanStoryState{tc.story}}
			id, ok := storyWorkID(state, tc.story.Key)
			if ok != tc.want {
				t.Fatalf("work=%d eligible=%v want %v", id, ok, tc.want)
			}
			if ok && tc.story.Effect != "" && id != planWorkID("effect", tc.story.Key, tc.story.EffectRequest) {
				t.Fatal("reconciliation identity changed")
			}
		})
	}
	story := planbrief.PlanStoryState{Key: "story-001", Effect: "uncertain"}
	story.SetEffectHold("original", "content conflict")
	story.ClearEffectHold("original")
	story.Effect = "applied"
	state := planbrief.PlanState{SharedRevision: "shared", StorySet: &planbrief.PlanStorySetState{Promoted: true}, Stories: []planbrief.PlanStoryState{story}}
	if _, ok := storyWorkID(state, story.Key); !ok {
		t.Fatal("legitimately cleared hold blocked draft")
	}
}

// dependantPlanState has story-002 depend on story-001, whose story stage is
// approved, so story-002's story stage is owed.
func dependantPlanState(t *testing.T) planbrief.PlanState {
	t.Helper()
	s := planbrief.PlanState{SharedRevision: "shared-1"}
	for _, key := range []string{"story-001", "story-002"} {
		d := planbrief.PlanDraft{Key: key, Name: "Story " + key}
		if key == "story-002" {
			d.Dependencies.StoryKeys = []string{"story-001"}
		}
		if err := s.PutCandidate(d, "story", key+"-candidate"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.OpenPlanWait("story-001", "wait-story", "review", 100); err != nil {
		t.Fatal(err)
	}
	r := planbrief.PlanOwnerReply{ID: 10, OwnerID: 7, ActorID: 7, IssueID: 100, Key: "story-001", Stage: "story", Candidate: "story-001-candidate", WaitID: "wait-story", Body: "/approve-plan story-001 story story-001-candidate"}
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("approve story-001: %v %v", ok, err)
	}
	if stage, _, ok := s.StoryNextStep("story-002"); stage != "story" || !ok {
		t.Fatalf("story-002 story stage not owed: %q %v", stage, ok)
	}
	return s
}

// #1160 c2/c3/c11: only a story whose latest agent attempt failed on an
// upstream story gets a work identity that names its upstream's candidate and
// effect; every other story keeps today's identity.
func TestStoryWorkIDAfterUpstreamFailure(t *testing.T) {
	s := dependantPlanState(t)
	plain, ok := storyWorkID(s, "story-002")
	if !ok {
		t.Fatal("no work")
	}
	if same, _ := storyWorkIDAfter(s, "story-002", false); same != plain {
		t.Fatal("an unflagged story changed identity")
	}
	flagged, _ := storyWorkIDAfter(s, "story-002", true)
	if again, _ := storyWorkIDAfter(s, "story-002", true); again != flagged {
		t.Fatal("the same state gave a different identity")
	}
	moves := map[string]func(*planbrief.PlanStoryState){
		"effect request": func(st *planbrief.PlanStoryState) { st.EffectRequest = "plan-request-9-9" },
		"candidate":      func(st *planbrief.PlanStoryState) { st.Candidate = "story-001-next" },
	}
	for name, move := range moves {
		t.Run(name, func(t *testing.T) {
			moved := s
			moved.Stories = slices.Clone(s.Stories)
			move(&moved.Stories[0])
			if id, ok := storyWorkID(moved, "story-002"); !ok || id != plain {
				t.Fatal("an upstream move changed the identity of a story that did not fail on it")
			}
			if id, ok := storyWorkIDAfter(moved, "story-002", true); !ok || id == flagged {
				t.Fatal("an upstream move did not offer the story again")
			}
		})
	}
	// While the upstream publishes, the dependant's stage is not owed at all.
	publishing := s
	publishing.Stories = slices.Clone(s.Stories)
	publishing.Stories[0].Effect = "pending"
	if _, ok := storyWorkIDAfter(publishing, "story-002", true); ok {
		t.Fatal("work offered while the upstream publishes")
	}
}

// planStoryFixture binds story-002 of parent 278 to issue 200 and saves state.
func planStoryFixture(t *testing.T, f *publicationIO, state planbrief.PlanState) (*task, store.GraphPlanStory) {
	t.Helper()
	ctx := t.Context()
	parent, err := naming.ParseRef(f.session.TaskRef)
	publicationMust(t, err)
	repo, err := f.db.GetRepo(ctx, f.session.RepoID)
	publicationMust(t, err)
	_, err = f.db.AdmitPlanWorkflowPath(ctx, f.session.ID, "graph")
	publicationMust(t, err)
	_, err = f.db.ReservePlanStory(ctx, parent.String(), "story-002")
	publicationMust(t, err)
	publicationMust(t, f.db.BindPlanStoryShell(ctx, parent.String(), "story-002", 2000, 200, "original-content"))
	_, err = f.db.SavePlanWorkflowState(ctx, parent.String(), 0, state)
	publicationMust(t, err)
	child, err := f.db.GetOrCreateSession(ctx, repo, 200)
	publicationMust(t, err)
	ref, err := naming.ParseRef(child.TaskRef)
	publicationMust(t, err)
	work := &task{repo: repo, session: child, ref: ref, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, issue: github.Issue{ID: 2000, Number: 200, State: "open"}, log: f.d.log, tickAt: f.now}
	story, owned, err := f.db.GraphPlanStoryAt(ctx, repo.ID, 200)
	publicationMust(t, err)
	if !owned {
		t.Fatal("story-002 is not a story task")
	}
	return work, story
}

// failPlanAgent records a failed Plan agent attempt with diagnostic on session.
func failPlanAgent(t *testing.T, f *publicationIO, sessionID int64, status, diagnostic string) {
	t.Helper()
	ctx := t.Context()
	definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "plan", ProfileVersion: 1, Path: "core/plan.dot", Digest: strings.Repeat("c", 64), CreatedAt: f.now})
	publicationMust(t, err)
	instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: sessionID, DefinitionID: definition.ID, CurrentNode: "story_agent", StartedAt: f.now})
	publicationMust(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "story_agent", StartedAt: f.now})
	publicationMust(t, err)
	completion := store.GraphCompletion{Status: status, FinishedAt: f.now, InstanceStatus: store.GraphFailed}
	if status == store.GraphSucceeded {
		completion = store.GraphCompletion{Status: status, Outcome: "done", FinishedAt: f.now, InstanceStatus: store.GraphRunning}
	}
	if diagnostic != "" {
		completion.Outputs = []store.GraphOutput{{Kind: store.GraphOutputDiagnostic, Payload: []byte(diagnostic)}}
	}
	publicationMust(t, f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, completion))
}

// attempt marks a work identity as already attempted on the story task.
func attemptWork(t *testing.T, f *publicationIO, work *task, id int64) {
	t.Helper()
	scheduled, _, err := f.db.CreateOrGetSchedule(t.Context(), store.CreateScheduleParams{SessionID: work.session.ID, SourceCommentID: id, OwnerHandle: "owner", Payload: planWorkPayload, DueAt: f.now})
	publicationMust(t, err)
	reserved, ok, err := f.db.ReserveSchedule(t.Context(), scheduled.ID, f.now)
	publicationMust(t, err)
	if !ok {
		t.Fatal("schedule not reserved")
	}
	_, err = f.db.FinishSchedule(t.Context(), reserved.ID, store.ScheduleSucceeded, 0, f.now)
	publicationMust(t, err)
}

func offeredStories(t *testing.T, f *publicationIO, work *task) int {
	t.Helper()
	queue := &repoQueue{repo: work.repo, forgeRepo: work.forgeRepo, log: f.d.log}
	publicationMust(t, f.d.graphPlanStories(t.Context(), map[int64]*repoQueue{work.repo.ID: queue}, map[int64]*candidate{}))
	return len(queue.candidates)
}

// #1160 c2/c3/c11 against saved records: an attempted stage stays attempted
// until its upstream moves, and only after an upstream-dependency failure.
func TestPlanWorkResumesOnlyAfterAnUpstreamFailure(t *testing.T) {
	for _, tc := range []struct {
		name, diagnostic string
		resumes          bool
	}{
		{"publishing", "execute graph activity \"lf_agent\": plan dependency story-001 is still publishing its stage", true},
		{"inactive", "resolve Plan upstream references: plan dependency story-001 is inactive or unresolved", true},
		{"stale binding", "retained application result is not complete: $.result.payload.plan.storyrequest.upstream stale_binding: changed", true},
		{"other failure", "research process outcome: failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationIO(t)
			state := dependantPlanState(t)
			work, _ := planStoryFixture(t, f, state)
			first, ok, err := f.d.planWorkFor(t.Context(), work)
			publicationMust(t, err)
			if !ok || first != planWorkID("story", "story-002", "story", "story-002-candidate", "false") {
				t.Fatalf("first work identity changed: %d %v", first, ok)
			}
			attemptWork(t, f, work, first)
			failPlanAgent(t, f, work.session.ID, store.GraphFailed, tc.diagnostic)
			if n := offeredStories(t, f, work); n != 0 && !tc.resumes {
				t.Fatalf("a failed stage was offered again without an owner reply: %d", n)
			}
			// The upstream publishes its next stage result.
			parent := f.session.TaskRef
			publicationMust(t, f.db.UpdatePlanWorkflowState(t.Context(), parent, func(s *planbrief.PlanState) (bool, error) {
				s.Stories[0].EffectRequest = "plan-request-31-32"
				return true, nil
			}))
			id, ok, err := f.d.planWorkFor(t.Context(), work)
			publicationMust(t, err)
			if !ok {
				t.Fatal("no work after the upstream published")
			}
			if tc.resumes != (id != first) {
				t.Fatalf("resume=%v but identity changed=%v", tc.resumes, id != first)
			}
			if got := offeredStories(t, f, work); (got == 1) != tc.resumes {
				t.Fatalf("offered %d story tasks, resume=%v", got, tc.resumes)
			}
			if !tc.resumes {
				return
			}
			// The resumed identity is stable: once attempted it is not offered again.
			attemptWork(t, f, work, id)
			if got := offeredStories(t, f, work); got != 0 {
				t.Fatalf("an attempted resume was offered again: %d", got)
			}
			// An owner reply that ran the stage leaves no automatic retry behind:
			// its run succeeded and opened a review, so no stage is owed.
			failPlanAgent(t, f, work.session.ID, store.GraphSucceeded, "")
			publicationMust(t, f.db.UpdatePlanWorkflowState(t.Context(), parent, func(s *planbrief.PlanState) (bool, error) {
				s.Stories[0].EffectRequest = "plan-request-41-42"
				return true, s.OpenPlanWait("story-002", "wait-reply", "review", 2000)
			}))
			if _, ok, err := f.d.planWorkFor(t.Context(), work); err != nil || ok {
				t.Fatalf("an automatic retry is still owed after the owner's reply ran the stage: %v %v", ok, err)
			}
		})
	}
}

// #1160 c14: a Plan-work run deferred while an upstream story published owes no
// failure comment, even when it was recovered as failed; any other failed run
// of a schedule still does.
func TestRequirePlanFailureReportedSkipsUpstreamDeferral(t *testing.T) {
	for _, tc := range []struct {
		name, payload, diagnostic string
		owed                      bool
	}{
		{"deferred Plan work", planWorkPayload, "plan dependency story-001 is still publishing its stage", false},
		{"held upstream", planWorkPayload, "plan dependency story-001 is inactive or unresolved", true},
		{"another schedule", "Check the plan every morning.", "plan dependency story-001 is still publishing its stage", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationIO(t)
			ctx := t.Context()
			work, _ := planStoryFixture(t, f, dependantPlanState(t))
			scheduled, _, err := f.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: work.session.ID, SourceCommentID: 77, OwnerHandle: "owner", Payload: tc.payload, DueAt: f.now})
			publicationMust(t, err)
			segment, err := f.db.GetOrCreateSegment(ctx, work.session.ID, 1, work.session.TaskRef, store.AgentClaude)
			publicationMust(t, err)
			run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: work.session.ID, SegmentID: segment.ID, Mode: "plan", Agent: store.AgentClaude, TriggerCommentID: scheduled.TriggerID(), StartedAt: f.now, LogPath: func(int) string { return "deferred.jsonl" }})
			publicationMust(t, err)
			definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "plan", ProfileVersion: 1, Path: "core/plan.dot", Digest: strings.Repeat("d", 64), CreatedAt: f.now})
			publicationMust(t, err)
			instance, err := f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: work.session.ID, DefinitionID: definition.ID, CurrentNode: "story_agent", StartedAt: f.now})
			publicationMust(t, err)
			attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "story_agent", RunID: run.ID, StartedAt: f.now})
			publicationMust(t, err)
			publicationMust(t, f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: f.now, InstanceStatus: store.GraphFailed, Outputs: []store.GraphOutput{{Kind: store.GraphOutputDiagnostic, Payload: []byte(tc.diagnostic)}}}))
			// Recovered as failed after a restart.
			run, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, Error: "recovered after restart", FinishedAt: f.now})
			publicationMust(t, err)
			err = f.d.requirePlanFailureReported(ctx, turn.Result{ResearchGraph: true, Run: run})
			if tc.owed != (err != nil) {
				t.Fatalf("failure comment owed=%v, want %v (%v)", err != nil, tc.owed, err)
			}
		})
	}
}

// reactionForge accepts the acknowledgement reaction; nothing else is called.
type reactionForge struct{ Forge }

func (reactionForge) AddCommentReaction(context.Context, github.Repo, int64, github.ReactionContent) error {
	return nil
}

// #1160 c8, story-issue path: a reply on a story saved with a later-stage wait
// behind a stale earlier approval is answered, the wait is gone, and the story
// stage is the work offered next, so its review shows the working command.
func TestHandlePlanStoryCommentAnswersAStaleEarlierApproval(t *testing.T) {
	f := newPublicationIO(t)
	ctx := t.Context()
	state := dependantPlanState(t)
	if err := state.OpenPlanWait("story-002", "wait-story", "review", 2000); err != nil {
		t.Fatal(err)
	}
	r := planbrief.PlanOwnerReply{ID: 11, OwnerID: 7, ActorID: 7, IssueID: 2000, Key: "story-002", Stage: "story", Candidate: "story-002-candidate", WaitID: "wait-story", Body: "/approve-plan story-002 story story-002-candidate"}
	if ok, err := state.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatalf("approve story-002 story: %v %v", ok, err)
	}
	if err := state.OpenPlanWait("story-002", "wait-technical", "review", 2000); err != nil || state.Stories[1].Wait == nil {
		t.Fatalf("technical wait: %v %+v", err, state.Stories[1].Wait)
	}
	state.Stories[1].Approvals["story"] = "stale-story-approval"
	work, story := planStoryFixture(t, f, state)
	f.d.forge = reactionForge{}
	refused := "/approve-plan story-002 technical story-002-candidate"
	comment := github.Comment{ID: 7001, Body: refused, Author: github.User{ID: 10}, CreatedAt: f.now}
	publicationMust(t, f.d.handlePlanStoryComment(ctx, work, comment, story, f.d.log))
	f.mu.Lock()
	bodies, paths := slices.Clone(f.bodies), slices.Clone(f.postPaths)
	f.mu.Unlock()
	if len(bodies) != 1 || paths[0] != "/repos/owner/api/issues/200/comments" {
		t.Fatalf("explanation posts %v %q", paths, bodies)
	}
	if !strings.Contains(bodies[0], "its next review will show the command to use") || strings.Contains(bodies[0], refused) {
		t.Fatalf("the explanation leaves no way forward: %q", bodies[0])
	}
	saved, _, err := f.db.GetPlanWorkflowState(ctx, f.session.TaskRef)
	publicationMust(t, err)
	if saved.Stories[1].Wait != nil || saved.Stories[1].Stage != "technical" {
		t.Fatalf("stale wait survived: %+v", saved.Stories[1])
	}
	id, ok, err := f.d.planWorkFor(ctx, work)
	publicationMust(t, err)
	if !ok || id != planWorkID("story", "story-002", "story", "story-002-candidate", "false") {
		t.Fatalf("the story stage is not the work offered next: %d %v", id, ok)
	}
}
