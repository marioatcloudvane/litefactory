package daemon_test

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func progressMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func reopenPlanScheduler(t *testing.T, x *lifecycleBoundaryFixture, realRecovery bool) {
	t.Helper()
	progressMust(t, x.db.Close())
	db, err := store.Open(t.Context(), x.paths.DBPath())
	progressMust(t, err)
	t.Cleanup(func() { progressMust(t, db.Close()) })
	x.db = db
	x.filter, err = turn.NewFilter(db, turn.Identities{OwnerUserID: ownerID, BotUserID: botID}, x.logger)
	progressMust(t, err)
	redactor := redact.New(redact.Options{})
	renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: x.logger})
	progressMust(t, err)
	x.reporter, err = turn.NewReporter(turn.ReporterOptions{DB: db, Forge: x.client, Renderer: renderer, Logger: x.logger, BotUserID: botID, StoryLockDir: x.paths.StoryProgressLocksDir()})
	progressMust(t, err)
	forge := lifecycleBoundaryForge{Client: x.client, parents: x.parents}
	options := daemon.Options{Forge: forge}
	if realRecovery {
		progressMust(t, os.MkdirAll(x.paths.StoryProgressLocksDir(), 0700))
		options.Turn = daemon.NewExecutor(turn.Options{DB: db, MCP: &turn.MCPRuntime{Paths: x.paths, Redactor: redactor}})
	}
	x.daemon = x.newDaemon(options)
}

func settleHistoricalPlanCreates(t *testing.T, x *lifecycleBoundaryFixture) {
	t.Helper()
	for _, key := range []string{"affected", "healthy"} {
		state, revision, err := x.db.GetPlanWorkflowState(t.Context(), x.session.TaskRef)
		progressMust(t, err)
		progressMust(t, x.db.SettlePlanStoryDebt(t.Context(), x.session.TaskRef, "create-"+key, state, revision, true))
	}
}

func seedRetirementRecovery(t *testing.T, x *lifecycleBoundaryFixture, kind string) {
	t.Helper()
	state, revision, err := x.db.GetPlanWorkflowState(t.Context(), x.session.TaskRef)
	progressMust(t, err)
	progressMust(t, state.OpenPlanWait("affected", "retire-wait", "retire", 2000))
	_, err = x.db.SavePlanWorkflowState(t.Context(), x.session.TaskRef, revision, state)
	progressMust(t, err)
	binding, err := x.db.GetPlanStoryDraft(t.Context(), x.session.TaskRef, "affected")
	progressMust(t, err)
	request := graphexec.PlanStoryRequest{RequestID: "retire-affected", Operation: "retire", Stage: "story", Candidate: "retirement-candidate", SharedRevision: "shared", ExpectedRevision: binding.Revision, Draft: state.Stories[0].Draft}
	raw, err := json.Marshal(request)
	progressMust(t, err)
	progressMust(t, x.db.PutPlanStoryCandidate(t.Context(), store.PlanStoryCandidate{Parent: x.session.TaskRef, RequestID: request.RequestID, Key: "affected", Candidate: request.Candidate, RequestJSON: string(raw)}))
	a := turn.PlanStoryAuthority{Parent: x.session.TaskRef, Repo: github.Repo{Owner: testOwnerName, Name: testRepoName}, ParentNumber: testIssue, ParentID: 1230, SelectedKey: "affected", Candidate: request.Candidate, Stage: "story", OwnerID: ownerID, ActorID: ownerID, RetirementCandidate: "candidate-affected", RetirementRevision: binding.Revision, ReviewRevision: state.Stories[0].Wait.Revision, Confirmation: &planbrief.PlanOwnerReply{ID: 99, OwnerID: ownerID, ActorID: ownerID, IssueID: 2000, Key: "affected", Stage: "story", Candidate: "candidate-affected", WaitID: "retire-wait", Body: "/retire-plan affected candidate-affected"}}
	raw, err = json.Marshal(a)
	progressMust(t, err)
	progressMust(t, x.db.RetainPlanStoryAuthority(t.Context(), x.session.TaskRef, request.RequestID, string(raw)))
	effect := store.PlanStoryEffect{Parent: x.session.TaskRef, RequestID: request.RequestID, Phase: "retire", Outcome: "uncertain", IssueID: 2000, IssueNumber: 200, Revision: binding.Revision}
	progressMust(t, x.db.AppendPlanStoryEffect(t.Context(), effect))
	if kind != "ambiguous-open" {
		child := x.children[0]
		child.State = "closed"
		child.StateReason = "not_planned"
		x.forge.addIssue(child)
	}
	if kind == "local-gap" {
		binding.Retired = true
		binding.Candidate = request.Candidate
		effect.Outcome = "applied"
		progressMust(t, x.db.ApplyPlanStoryDraft(t.Context(), binding, binding.Revision, effect))
	}
}

func TestPlanProgressSchedulerNoCommentRecoveryAfterRestart(t *testing.T) {
	for _, kind := range []string{"remote-gap", "local-gap", "ambiguous-open", "manual-close"} {
		t.Run(kind, func(t *testing.T) {
			x := newLifecycleBoundaryFixture(t)
			settleHistoricalPlanCreates(t, x)
			if kind == "manual-close" {
				child := x.children[0]
				child.State = "closed"
				child.StateReason = "completed"
				x.forge.addIssue(child)
			} else {
				seedRetirementRecovery(t, x, kind)
			}
			// No new owner input is provided. The restarted real executor must recover
			// through ordinary Tick admission, before any shaping command is selected.
			reopenPlanScheduler(t, x, true)
			x.settle()
			state := x.state()
			switch kind {
			case "ambiguous-open":
				if state.Stories[0].EffectHold == "" || state.Stories[0].Retired {
					t.Fatalf("ambiguous effect not held: %+v logs=%s", state, x.logs)
				}
			case "manual-close":
				if state.Stories[0].Lifecycle.State != "closed" || state.Stories[0].Retired {
					t.Fatalf("manual close not adopted: %+v logs=%s", state, x.logs)
				}
			default:
				if !state.Stories[0].Retired || !slices.Contains(state.ConsumedReplies, int64(99)) {
					t.Fatalf("retirement not recovered by Tick: %+v logs=%s", state, x.logs)
				}
			}
			if !slices.Contains(state.EligiblePlanStories(), "healthy") || x.watermark() != 0 {
				t.Fatalf("healthy progress/input changed: %+v watermark=%d", state, x.watermark())
			}
			x.assertNoChildMutation()
			x.settle()
			x.assertNoChildMutation()
			// Replace only costly agent execution. The normal Tick input scheduler and
			// retained lifecycle/recovery state remain, so healthy replies are observable.
			x.restart()
			x.assertHealthyReply(70)
			x.forge.addComment(testIssue, 80, owner(), "discuss the remaining independent plan")
			x.settle()
			if !slices.ContainsFunc(x.turns.calls(), func(req turn.Request) bool { return req.Trigger.CommentID == 80 && req.Trigger.PlanReply == nil }) {
				t.Fatalf("parent discussion starved: %+v logs=%s", x.turns.calls(), x.logs)
			}
		})
	}
}

func TestPlanProgressSchedulerStaleReplyUncertainThenHealthy(t *testing.T) {
	x := newLifecycleBoundaryFixture(t)
	child := x.children[0]
	child.State = "closed"
	child.StateReason = "completed"
	x.forge.addIssue(child)
	// Deliver the lifecycle notice before injecting loss into the stale reply's
	// explanation, so the fault is bound to precisely one original input.
	x.settle()
	x.forge.onPostComment = func(w http.ResponseWriter, _ int) bool {
		http.Error(w, `{"message":"lost reply response"}`, http.StatusInternalServerError)
		return true
	}
	x.forge.addComment(200, 60, owner(), "/approve-plan affected story candidate-affected")
	x.settle()
	cursor, err := x.db.StoryDiscoveryCursor(t.Context(), x.session.ID, 200)
	progressMust(t, err)
	if cursor >= 60 {
		t.Fatalf("unconfirmed explanation consumed child reply %d", cursor)
	}
	explanation, err := x.db.PlanReplyExplanation(t.Context(), x.session.TaskRef, 60)
	progressMust(t, err)
	delivery, err := x.db.GetReportDelivery(t.Context(), explanation.DeliveryKey)
	progressMust(t, err)
	if delivery.Completed || !delivery.PossiblySent {
		t.Fatalf("missing delivery debt %+v", delivery)
	}
	x.forge.onPostComment = nil
	// While delivery remains unknown (no trustworthy receipt), an independent
	// healthy input must still be routed on an eligible pass after backoff.
	reopenPlanScheduler(t, x, false)
	x.clock.advance(time.Minute)
	x.assertHealthyReply(70)
	cursor, err = x.db.StoryDiscoveryCursor(t.Context(), x.session.ID, 200)
	progressMust(t, err)
	if cursor >= 60 {
		t.Fatal("healthy reply consumed held child's cursor")
	}
	x.forge.addCommentAt(testIssue, 900060, bot(), delivery.Body, x.clock.Now())
	x.settle()
	cursor, err = x.db.StoryDiscoveryCursor(t.Context(), x.session.ID, 200)
	progressMust(t, err)
	if cursor != 60 {
		t.Fatalf("confirmed explanation failed to consume cursor %d logs=%s", cursor, x.logs)
	}
	delivery, err = x.db.GetReportDelivery(t.Context(), explanation.DeliveryKey)
	progressMust(t, err)
	if !delivery.Completed || delivery.CommentID != 900060 {
		t.Fatalf("original delivery not recovered %+v", delivery)
	}
	for _, call := range x.turns.calls() {
		if call.Trigger.PlanReply != nil && call.Trigger.PlanReply.Key == "affected" {
			t.Fatal("stale approval reached shaping executor")
		}
	}
	before, _, _ := x.forge.snapshot()
	x.settle()
	after, _, _ := x.forge.snapshot()
	if len(before) != len(after) {
		t.Fatal("unchanged poll repeated a notice")
	}
}

func TestPlanProgressSchedulerSharedFailureBackoff(t *testing.T) {
	x := newLifecycleBoundaryFixture(t)
	settleHistoricalPlanCreates(t, x)
	seedRetirementRecovery(t, x, "ambiguous-open")
	reopenPlanScheduler(t, x, true)
	x.forge.onListComments = func(w http.ResponseWriter, _ int) bool {
		w.Header().Set("Retry-After", "120")
		http.Error(w, `{"message":"rate limited"}`, http.StatusTooManyRequests)
		return true
	}
	x.settle()
	if !strings.Contains(x.logs.String(), "429") {
		t.Fatalf("shared failure was concealed logs=%s", x.logs)
	}
	state := x.state()
	if state.PlanReady(planbrief.ContentScope{}) {
		t.Fatal("failed shared service permitted readiness")
	}
	// The scheduler's retained retry state must suppress immediate repeated HTTP.
	_, before, _, _ := x.forge.counts()
	x.settle()
	_, after, _, _ := x.forge.counts()
	if after != before {
		t.Fatalf("shared-failure backoff not honored %d -> %d logs=%s", before, after, x.logs)
	}
	x.forge.onListComments = nil
	x.clock.advance(2 * time.Minute)
	x.restart()
	x.assertHealthyReply(80)
}
