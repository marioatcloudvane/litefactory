package daemon

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

type scheduledExecutor func(context.Context, turn.Request) (turn.Result, error)

func (e scheduledExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	return e(ctx, req)
}

func TestScheduledLinearStoryRetainsIdentityAndPublishesOnce(t *testing.T) {
	testScheduledLinearStory(t, false)
}

func TestScheduledLinearHeldEffectSettlesAcrossRestart(t *testing.T) {
	testScheduledLinearStory(t, true)
}

func testScheduledLinearStory(t *testing.T, held bool) {
	f, remote, v, sub, q := linearObservationFixture(t)
	ctx := t.Context()
	db := f.d.db
	handoffLinearObservation(t, f, v, sub)
	seen := map[int64]*candidate{}
	linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, seen))
	for _, c := range seen {
		if c.discoveryReservation != nil {
			c.discoveryReservation.finish()
		}
	}
	parent, err := db.GetSessionByTicket(ctx, ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"})
	linearWorkflowCheck(t, err)
	parentSnapshot, err := db.GetExecutionSnapshot(ctx, parent.ID)
	linearWorkflowCheck(t, err)
	_, err = db.AdmitPlanWorkflowPath(ctx, parent.ID, "graph")
	linearWorkflowCheck(t, err)
	state := planbrief.PlanState{SharedRevision: "scope"}
	rev, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "item", Name: "Scheduled story", Intent: "Review child"}})
	linearWorkflowCheck(t, err)
	_, err = state.PromoteStorySet(rev)
	linearWorkflowCheck(t, err)
	key := state.StorySet.Keys["item"]
	_, err = db.ReservePlanStory(ctx, parent.TaskRef, key)
	linearWorkflowCheck(t, err)
	if held {
		state.Stories[0].Effect = "uncertain"
		state.Stories[0].SetEffectHold("plan-request-692-6680", "owner edits conflict with expected content revision")
	}
	revision, err := db.SavePlanWorkflowState(ctx, parent.TaskRef, 0, state)
	linearWorkflowCheck(t, err)
	if held {
		for revision < 10 {
			revision, err = db.SavePlanWorkflowState(ctx, parent.TaskRef, revision, state)
			linearWorkflowCheck(t, err)
		}
	}
	clients, err := f.d.connections.Clients(ctx, v, parentSnapshot.ActorID)
	linearWorkflowCheck(t, err)
	bridge, err := turn.NewTicketWorkflow(ctx, db, clients.Linear, parentSnapshot.ActorID)
	linearWorkflowCheck(t, err)
	redactor := redact.New(redact.Options{Environ: []string{}})
	title, err := redactor.Apply(ctx, "Scheduled story", 256)
	linearWorkflowCheck(t, err)
	body, err := redactor.Apply(ctx, fmt.Sprintf("<!-- lf:plan-story parent=%d key=%s -->\nDraft", parent.IssueNumber, key), 4096)
	linearWorkflowCheck(t, err)
	child, err := bridge.CreateIssueEffect(ctx, "scheduled-shell", q.forgeRepo, parent.IssueNumber, title, body)
	linearWorkflowCheck(t, err)
	// Retain a child admitted under a later revision of the same source.
	v, err = db.DisableConnection(ctx, v.RepoID, store.TicketConnection, v.Revision)
	linearWorkflowCheck(t, err)
	v.Enabled = true
	v, err = db.SetConnection(ctx, v, v.Revision)
	linearWorkflowCheck(t, err)
	_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: q.repo.ID, ConnectionVersionID: v.ID, Ref: child.QualifiedSnapshot.Ref, Enabled: true, Provenance: "discovery"})
	linearWorkflowCheck(t, err)
	childSession, err := AdmitProjectTicket(ctx, db, f.d.connections, q.repo, child.QualifiedSnapshot.Ref)
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, db.BindPlanStoryShell(ctx, parent.TaskRef, key, child.ID, child.Number, turn.PlanIssueRevision(child.Title, child.Body)))
	managed, err := f.d.managedChildSession(ctx, q.repo, parent.TaskRef, child.Number, child.ID)
	linearWorkflowCheck(t, err)
	snapshot, err := db.GetExecutionSnapshot(ctx, managed.ID)
	linearWorkflowCheck(t, err)
	if managed.ID != childSession.ID || snapshot.TicketVersionID != v.ID || snapshot.TicketVersionID == parentSnapshot.TicketVersionID {
		t.Fatal("fixture did not retain separate child revision")
	}
	binding, err := db.GetSessionTicket(ctx, managed.ID)
	linearWorkflowCheck(t, err)
	// This is the pre-fix caller destination, rejected before resolving Linear.
	_, err = f.d.readIssue(sessionContext(ctx, managed.ID), github.Repo{Owner: q.repo.Owner, Name: q.repo.Name}, managed.IssueNumber)
	if !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("old destination error: %v", err)
	}
	// A changed registry row must not replace either retained task destination.
	changedRepo := q.repo
	changedRepo.Owner, changedRepo.Name, changedRepo.Path = "other", "code", t.TempDir()
	prepared := &task{repo: changedRepo, forgeRepo: github.Repo{Owner: changedRepo.Owner, Name: changedRepo.Name}, session: managed}
	resolved, err := f.d.readScheduledIssue(sessionContext(ctx, managed.ID), prepared)
	linearWorkflowCheck(t, err)
	if prepared.repo != snapshot.CodeTarget || prepared.forgeRepo.String() != binding.Locator.Container || resolved.ID != child.ID {
		t.Fatalf("scheduled task destinations changed: repo=%+v ticket=%s issue=%d", prepared.repo, prepared.forgeRepo, resolved.ID)
	}
	renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: f.d.log})
	linearWorkflowCheck(t, err)
	reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: f.d.log, BotUserID: snapshot.ActorID})
	linearWorkflowCheck(t, err)
	var failedGraph store.GraphInstance
	if held {
		definition, e := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "plan", ProfileVersion: 1, Path: "core/plan.dot", Digest: strings.Repeat("b", 64), CreatedAt: f.now})
		linearWorkflowCheck(t, e)
		failedGraph, e = db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: managed.ID, DefinitionID: definition.ID, CurrentNode: "story_draft", StartedAt: f.now})
		linearWorkflowCheck(t, e)
		attempt, e := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: failedGraph.ID, Node: "story_draft", StartedAt: f.now})
		linearWorkflowCheck(t, e)
		linearWorkflowCheck(t, db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphFailed, "", f.now))
		linearWorkflowCheck(t, db.SetGraphInstanceStatus(ctx, failedGraph.ID, store.GraphFailed, time.Time{}, f.now))
		failedGraph, e = db.GetGraphInstance(ctx, failedGraph.ID)
		linearWorkflowCheck(t, e)
	}
	locked := !held
	calls := 0
	executor := scheduledExecutor(func(ctx context.Context, req turn.Request) (turn.Result, error) {
		if locked {
			return turn.Result{Disposition: turn.DispositionLocked}, nil
		}
		calls++
		if req.Mode != agent.ModePlan || req.SessionID != managed.ID || req.Issue.Number != child.Number {
			return turn.Result{}, fmt.Errorf("wrong scheduled request: %+v", req)
		}
		if held {
			return turn.Result{ResearchGraph: true, Reason: "waiting: owner edits conflict with expected content revision"}, nil
		}
		segment, e := db.GetOrCreateSegment(ctx, managed.ID, 1, managed.TaskRef, store.AgentClaude)
		if e != nil {
			return turn.Result{}, e
		}
		run, e := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: managed.ID, SegmentID: segment.ID, Mode: string(agent.ModePlan), Agent: store.AgentClaude, TriggerCommentID: req.Trigger.CommentID, StartedAt: f.now, LogPath: func(int) string { return "scheduled-test.jsonl" }})
		if e != nil {
			return turn.Result{}, e
		}
		run, e = db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: f.now.Add(time.Second)})
		if e != nil {
			return turn.Result{}, e
		}
		ref, e := naming.ParseRef(managed.TaskRef)
		if e != nil {
			return turn.Result{}, e
		}
		_, e = reporter.ReportRun(ctx, turn.RunReport{Run: report.Run{Ref: ref, Ordinal: run.Ordinal, Result: &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "Scheduled Linear story review"}}, SessionID: managed.ID})
		return turn.Result{Run: run, Outcome: agent.OutcomeSucceeded, ResearchGraph: true}, e
	})
	options := Options{DB: db, Forge: f.client, Filter: f.d.filter, Connections: f.d.connections, Turn: executor, Reporter: reporter, InstanceID: "scheduled-linear", Logger: f.d.log, Now: func() time.Time { return f.now }, DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): explicitWorkflowClassifier{t}}, ToolProfiles: config.DefaultToolProfiles().Permissions}
	d, err := New(options)
	linearWorkflowCheck(t, err)
	sourceID, _ := storyWorkID(state, key)
	if held {
		sourceID = planWorkID("effect", key, "plan-request-692-6680")
	}
	scheduled, _, err := db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: managed.ID, SourceCommentID: sourceID, OwnerHandle: "human-uuid", Payload: planWorkPayload, DueAt: f.now})
	linearWorkflowCheck(t, err)
	d.prepFailures.record(managed.ID, "previous refusal", f.now.Add(-time.Hour), time.Second)
	if locked {
		first := TickResult{}
		linearWorkflowCheck(t, d.dispatchSchedules(ctx, map[int64]store.Repo{q.repo.ID: q.repo}, f.now, &first))
		linearWorkflowCheck(t, d.Quiesce(ctx))
		pending, e := db.GetSchedule(ctx, scheduled.ID)
		linearWorkflowCheck(t, e)
		if pending.Status != store.SchedulePending || pending.RunID != 0 || calls != 0 {
			t.Fatalf("lock contention not retried: %+v", pending)
		}
		locked = false
	}
	result := TickResult{}
	linearWorkflowCheck(t, d.dispatchSchedules(ctx, map[int64]store.Repo{q.repo.ID: q.repo}, f.now, &result))
	linearWorkflowCheck(t, d.Quiesce(ctx))
	if result.Dispatched != 1 || calls != 1 {
		t.Fatalf("dispatch=%d calls=%d logs=%s", result.Dispatched, calls, f.logs.String())
	}
	stored, err := db.GetSchedule(ctx, scheduled.ID)
	linearWorkflowCheck(t, err)
	if stored.Status != store.ScheduleSucceeded {
		t.Fatalf("schedule: %+v logs=%s", stored, f.logs.String())
	}
	if row, _ := d.prepFailures.hold(managed.ID, f.now); row.Attempts != 0 {
		t.Fatalf("success retained spell: %+v", row)
	}
	linearWorkflowCheck(t, d.dispatchSchedules(ctx, map[int64]store.Repo{q.repo.ID: q.repo}, f.now, &result))
	linearWorkflowCheck(t, d.Quiesce(ctx))
	if calls != 1 {
		t.Fatal("duplicate scheduled execution")
	}
	if held {
		d, err = New(options)
		linearWorkflowCheck(t, err)
		restarted := TickResult{}
		linearWorkflowCheck(t, d.dispatchSchedules(ctx, map[int64]store.Repo{q.repo.ID: q.repo}, f.now, &restarted))
		linearWorkflowCheck(t, d.Quiesce(ctx))
		storyQueue := &repoQueue{repo: q.repo, forgeRepo: q.forgeRepo, log: f.d.log}
		linearWorkflowCheck(t, d.graphPlanStories(ctx, map[int64]*repoQueue{q.repo.ID: storyQueue}, map[int64]*candidate{}))
		if calls != 1 || restarted.Dispatched != 0 || len(storyQueue.candidates) != 0 || stored.RunID != 0 {
			t.Fatal("held work repeated or allocated a run")
		}
		_, exists, e := db.GetRunByTrigger(ctx, managed.ID, scheduled.TriggerID())
		linearWorkflowCheck(t, e)
		if exists {
			t.Fatal("held work created a run")
		}
		latest, e := db.LatestSessionGraphInstance(ctx, managed.ID)
		linearWorkflowCheck(t, e)
		attempts, e := db.ListGraphNodeAttempts(ctx, failedGraph.ID)
		linearWorkflowCheck(t, e)
		if !reflect.DeepEqual(latest, failedGraph) || len(attempts) != 1 || attempts[0].Status != store.GraphFailed {
			t.Fatal("failed graph or attempts changed")
		}
		retained, rev, e := db.GetPlanWorkflowState(ctx, parent.TaskRef)
		linearWorkflowCheck(t, e)
		if rev != 10 || !reflect.DeepEqual(retained, state) {
			t.Fatalf("held state changed: %+v", retained)
		}
		logs := f.logs.String()
		for _, want := range []string{"Plan work returned without a run", "Plan schedule has no eligible work", "owner edits conflict with expected content revision", managed.TaskRef, key, "plan-request-692-6680", fmt.Sprintf(`"schedule_id":%d`, scheduled.ID), fmt.Sprintf(`"session_id":%d`, managed.ID)} {
			if !strings.Contains(logs, want) {
				t.Fatalf("missing %q in %s", want, logs)
			}
		}
	}
	after, err := db.GetSessionTicket(ctx, managed.ID)
	linearWorkflowCheck(t, err)
	if after != binding {
		t.Fatal("scheduled read rebound history")
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	reviews := 0
	for _, comment := range remote.comments {
		if strings.Contains(linearPlanCommentBody(t, comment), "Scheduled Linear story review") {
			reviews++
			if linearWorkflowObject(t, comment["issue"])["id"] != child.QualifiedSnapshot.Ref.Identity.RemoteID {
				t.Fatal("review sent to wrong ticket")
			}
		}
	}
	wantReviews := 1
	if held {
		wantReviews = 0
	}
	if reviews != wantReviews {
		t.Fatalf("reviews=%d", reviews)
	}
}

type scheduledReadRefusal struct {
	*github.Client
	reads int
}

func (f *scheduledReadRefusal) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.reads++
	return github.Issue{}, store.ErrTicketIdentityConflict
}

func TestScheduledPreparationBackoffRetainsPendingAndReleasesCapacity(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	ctx := t.Context()
	db, d := f.d.db, f.d
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "schedules", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	linearWorkflowCheck(t, err)
	session, err := db.GetOrCreateSession(ctx, repo, 7)
	linearWorkflowCheck(t, err)
	scheduled, _, err := db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 701, OwnerHandle: "owner", Payload: "/spike retry", DueAt: f.now})
	linearWorkflowCheck(t, err)
	refusal := &scheduledReadRefusal{Client: f.client}
	d.forge = refusal
	d.pool = newPool(1)
	repos := map[int64]store.Repo{repo.ID: repo}
	at := f.now
	delay := d.recoveryInterval()
	for attempt := 1; attempt <= 12; attempt++ {
		result := TickResult{}
		if !d.offerSchedule(ctx, repos, scheduled, at, &result) || result.Dispatched != 0 {
			t.Fatal("refusal dispatched or blocked pass")
		}
		row, held := d.prepFailures.hold(session.ID, at)
		if !held || row.Attempts != attempt || row.NextAt.Sub(at) != delay || row.Reason != store.ErrTicketIdentityConflict.Error() {
			t.Fatalf("attempt %d: %+v delay=%s", attempt, row, delay)
		}
		if refusal.reads != attempt {
			t.Fatalf("reads=%d attempts=%d", refusal.reads, attempt)
		}
		stored, e := db.GetSchedule(ctx, scheduled.ID)
		linearWorkflowCheck(t, e)
		if stored.Status != store.SchedulePending || stored.RunID != 0 {
			t.Fatalf("consumed schedule: %+v", stored)
		}
		_, exists, e := db.GetRunByTrigger(ctx, session.ID, scheduled.TriggerID())
		linearWorkflowCheck(t, e)
		if exists {
			t.Fatal("consumed trigger")
		}
		// A second session can acquire both admission guards after the refusal.
		if !d.inflight.enter(session.ID+1) || !d.pool.try() {
			t.Fatal("refusal leaked capacity")
		}
		d.inflight.leave(session.ID + 1)
		d.pool.done()
		if !d.inflight.enter(session.ID) {
			t.Fatal("refusal leaked its in-flight guard")
		}
		d.inflight.leave(session.ID)
		d.offerSchedule(ctx, repos, scheduled, row.NextAt.Add(-time.Nanosecond), &result)
		before := f.logs.String()
		for i := 0; i < 3; i++ {
			d.offerSchedule(ctx, repos, scheduled, row.NextAt.Add(-time.Nanosecond), &result)
		}
		if refusal.reads != attempt || f.logs.String() != before {
			t.Fatal("held ticks repeated reads or diagnostics")
		}
		at = row.NextAt
		delay *= 2
		if delay > MaxBackoff {
			delay = MaxBackoff
		}
	}
	if delay != MaxBackoff {
		t.Fatal("did not exercise cap")
	}
	logs := f.logs.String()
	if strings.Count(logs, "this task has failed every turn") != 1 || !strings.Contains(logs, session.TaskRef) || !strings.Contains(logs, fmt.Sprintf(`"session_id":%d`, session.ID)) || !strings.Contains(logs, store.ErrTicketIdentityConflict.Error()) {
		t.Fatalf("missing or duplicate contextual escalation: %s", logs)
	}
}

func TestScheduledLinearRejectsChangedRemoteIdentity(t *testing.T) {
	for _, mismatch := range []string{"remote", "container", "team"} {
		t.Run(mismatch, func(t *testing.T) {
			f, remote, v, sub, q := linearObservationFixture(t)
			ctx := t.Context()
			handoffLinearObservation(t, f, v, sub)
			seen := map[int64]*candidate{}
			linearWorkflowCheck(t, f.d.drainTicketObservations(ctx, map[int64]*repoQueue{v.RepoID: q}, seen))
			for _, c := range seen {
				if c.discoveryReservation != nil {
					c.discoveryReservation.finish()
				}
			}
			session, err := f.d.db.GetSessionByTicket(ctx, ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"})
			linearWorkflowCheck(t, err)
			binding, err := f.d.db.GetSessionTicket(ctx, session.ID)
			linearWorkflowCheck(t, err)
			snapshot, err := f.d.db.GetExecutionSnapshot(ctx, session.ID)
			linearWorkflowCheck(t, err)
			remote.mu.Lock()
			switch mismatch {
			case "remote":
				remote.issues["parent-uuid"]["id"] = "foreign-uuid"
			case "container":
				remote.issues["parent-uuid"]["project"] = map[string]any{"id": "foreign-project"}
			case "team":
				remote.issues["parent-uuid"]["team"] = map[string]any{"id": "foreign-team", "organization": map[string]any{"id": "workspace-uuid"}}
			}
			remote.mu.Unlock()
			scheduled, _, err := f.d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: session.ID, SourceCommentID: 701, OwnerHandle: "human-uuid", Payload: planWorkPayload, DueAt: f.now})
			linearWorkflowCheck(t, err)
			f.d.pool = newPool(1)
			result := TickResult{}
			f.d.offerSchedule(ctx, map[int64]store.Repo{q.repo.ID: q.repo}, scheduled, f.now, &result)
			if result.Dispatched != 0 {
				t.Fatal("foreign ticket dispatched")
			}
			row, held := f.d.prepFailures.hold(session.ID, f.now)
			if !held || row.Attempts != 1 {
				t.Fatalf("refusal not retained: %+v logs=%s", row, f.logs.String())
			}
			after, err := f.d.db.GetSessionTicket(ctx, session.ID)
			linearWorkflowCheck(t, err)
			afterSnapshot, err := f.d.db.GetExecutionSnapshot(ctx, session.ID)
			linearWorkflowCheck(t, err)
			if after != binding || afterSnapshot != snapshot {
				t.Fatal("refusal rebound retained history")
			}
		})
	}
}
