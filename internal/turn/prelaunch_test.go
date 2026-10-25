package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestUnreadyScopeContinuesAssessmentAndReusesRacedAllocation(t *testing.T) {
	for _, allocated := range []bool{false, true} {
		t.Run(map[bool]string{false: "early", true: "allocated"}[allocated], func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				if req.Permissions.Profile != "validate" || !strings.Contains(req.Prompt, "limited to preparation and reading") {
					t.Fatal("unready managed request gained implementation authority")
				}
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			req := f.request()
			req.Command = ParseCommand("/implement finish")
			req.Mode = "implement"
			mustProgress(t, f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: f.session.TaskRef, Generation: 1, State: "planning"}))
			if allocated {
				mustProgress(t, f.db.SetManagedFeatureReady(t.Context(), f.session.TaskRef, 1, true))
				// Required approval reads remain failures and preserve the same allocation.
				for range 2 {
					if _, err := Execute(t.Context(), f.opts, req); err == nil {
						t.Fatal("missing forge unexpectedly prepared approval")
					}
					runs := f.runs()
					if len(runs) != 1 || runs[0].Status != store.RunPrelaunch || runs[0].Ordinal != 1 {
						t.Fatalf("retry allocation=%+v", runs)
					}
				}
				mustProgress(t, f.db.SetManagedFeatureReady(t.Context(), f.session.TaskRef, 1, false))
			}
			result, err := Execute(t.Context(), f.opts, req)
			if err != nil || runner.callCount() != 1 || result.Harness == nil {
				t.Fatalf("original request did not reach assessment: %+v %v", result, err)
			}
			if result.Committed || result.Pushed || watermark(t, f) != 0 {
				t.Fatal("assessment saved or consumed the request before delivery")
			}
			runs := f.runs()
			if len(runs) != 1 || runs[0].Status != store.RunSucceeded || runs[0].Ordinal != 1 {
				t.Fatalf("assessment did not use the request's allocation: %+v", runs)
			}
		})
	}
}

func TestStoppedOutcomeSurvivesTotalStorageOutage(t *testing.T) {
	for _, token := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal", true: "resume-token"}[token], func(t *testing.T) {
			var f *fixture
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				result := succeeded(req, f.paths.Home())
				if token {
					result.ResumeToken = "retained-context"
				}
				if err := f.db.Close(); err != nil {
					t.Fatal(err)
				}
				return result, nil
			})
			f = newFixture(t, runner)
			result, err := Execute(t.Context(), f.opts, f.request())
			if err == nil || result.Repair == nil {
				t.Fatalf("missing stopped debt: result=%+v err=%v", result, err)
			}
			failed, err := result.Repair(t.Context())
			if err == nil || failed.Repair == nil {
				t.Fatal("outage retry lost stopped debt")
			}
			reopened, err := store.Open(t.Context(), f.paths.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			// No concurrent user remains: restore this fixture's sole handle so captured
			// repair callbacks encounter repaired storage, with unchanged durable bytes.
			*f.db = *reopened
			if runner.callCount() != 1 {
				t.Fatal("repair relaunched")
			}
			session := f.reloadSession()
			if session.LockOwner != testInstance {
				t.Fatalf("repair lost ownership: %+v", session)
			}
			if f.runs()[0].Reportable() {
				t.Fatal("outage run became reportable")
			}
			result, err = failed.Repair(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if result.Repair != nil || !result.Run.Reportable() {
				t.Fatalf("repaired outcome=%+v", result)
			}
			if runner.callCount() != 1 || watermark(t, f) != 0 {
				t.Fatal("repair relaunched or consumed")
			}
			if session = f.reloadSession(); session.LockOwner != "" {
				t.Fatalf("repair did not release lock: %+v", session)
			}
			replay, err := Execute(t.Context(), f.opts, f.request())
			if err != nil {
				t.Fatal(err)
			}
			if replay.Disposition != DispositionAlreadyRan || runner.callCount() != 1 {
				t.Fatal("terminal outcome replayed execution")
			}
		})
	}
}

func TestPrelaunchAndRefusalAreNotConversationPredecessors(t *testing.T) {
	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("not launched")
	}))
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "prelaunch-session", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := allocateRun(t.Context(), f.opts, f.request(), f.session, segment, f.ref())
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := previousRun(t.Context(), f.opts, f.session, segment); err != nil || found {
		t.Fatalf("prelaunch predecessor: %v %v", found, err)
	}
	if _, err := f.db.FinishRun(t.Context(), pre.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "refused", Error: "never launched"}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := previousRun(t.Context(), f.opts, f.session, segment); err != nil || found {
		t.Fatalf("refusal predecessor: %v %v", found, err)
	}
}

func TestNonterminalReportNeverRendersOrPosts(t *testing.T) {
	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("unexpected")
	}))
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "nonterminal", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := allocateRun(t.Context(), f.opts, f.request(), f.session, segment, f.ref())
	if err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge(t)
	renderErr := errors.New("renderer must not be called")
	reporter := newReporter(t, f, forge, stubRenderer{err: renderErr})
	for _, started := range []bool{false, true} {
		if started {
			if _, err := f.db.StartRun(t.Context(), run.ID); err != nil {
				t.Fatal(err)
			}
		}
		_, err := reporter.ReportRun(t.Context(), RunReport{SessionID: f.session.ID, CommentID: testComment, RunID: run.ID, Run: report.Run{Ref: f.ref(), Ordinal: run.Ordinal}})
		if err == nil || errors.Is(err, renderErr) {
			t.Fatalf("nonterminal guard: %v", err)
		}
	}
	if len(forge.all()) != 0 || watermark(t, f) != 0 {
		t.Fatal("nonterminal report posted or consumed")
	}
}

func TestRegistrationFailureRetainsPrelaunchAllocation(t *testing.T) {
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	setupProgressScope(t, f)
	dir := seedTaskWorktree(t, f)
	obstruction := filepath.Join(dir, agent.StoryEventsDirectory)
	if err := os.WriteFile(obstruction, []byte("blocking file"), 0600); err != nil {
		t.Fatal(err)
	}
	req := f.request()
	req.Mode = "implement"
	req.Command = ParseCommand("/implement work")
	_, err := Execute(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), "story event transport") {
		t.Fatalf("registration error=%v", err)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Status != store.RunPrelaunch || runner.callCount() != 0 {
		t.Fatalf("registration failure: %+v launches=%d", runs, runner.callCount())
	}
	if watermark(t, f) != 0 {
		t.Fatal("registration failure consumed source")
	}
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), f.opts, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.ID != runs[0].ID || result.Run.Ordinal != runs[0].Ordinal || runner.callCount() != 1 {
		t.Fatalf("recovery allocated/relaunched: %+v calls=%d", result.Run, runner.callCount())
	}
}

func TestGraphPreparationRetryReusesRunBeforeStart(t *testing.T) {
	f := newFixture(t, newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Fatal("preparation launched agent")
		return nil, nil
	}))
	f.opts.WorkflowSource = nil // This test covers allocation reuse before graph admission.
	graph := f.graphRequest(graphTestDOT, nil)
	d, err := f.db.ParkGraphInstance(t.Context(), graph.InstanceID, "work", graph.At, graph.At)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.db.ReserveGraphDispatch(t.Context(), d.ID, graph.At); err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	req := f.request()
	req.Trigger.CommentID = d.TriggerID()
	first, err := prepare(t.Context(), f.opts, req, f.session, f.ref(), f.opts.Logger)
	if err != nil {
		t.Fatal(err)
	}
	if first.run.Status != store.RunPrelaunch {
		t.Fatalf("preparation started run: %+v", first.run)
	}
	if ok, err := f.db.RequeueGraphDispatch(t.Context(), d.ID, graph.NextDueAt, "retry preparation"); err != nil || !ok {
		t.Fatalf("requeue: %v %v", ok, err)
	}
	reserved, ok, err := f.db.ReserveGraphDispatch(t.Context(), d.ID, graph.NextDueAt)
	if err != nil || !ok || reserved.Attempts != 2 || reserved.AllocatedRunID != 0 {
		t.Fatalf("retry reservation: %+v %v %v", reserved, ok, err)
	}
	second, err := prepare(t.Context(), f.opts, req, f.reloadSession(), f.ref(), f.opts.Logger)
	if err != nil {
		t.Fatal(err)
	}
	if second.run.ID != first.run.ID || second.run.Ordinal != first.run.Ordinal || len(f.runs()) != 1 {
		t.Fatalf("retry changed run identity: first=%+v second=%+v", first.run, second.run)
	}
	started, err := f.db.StartRun(t.Context(), second.run.ID)
	if err != nil || started.Status != store.RunRunning {
		t.Fatalf("start: %+v %v", started, err)
	}
	if ok, err := f.db.RequeueGraphDispatch(t.Context(), d.ID, graph.NextDueAt, "already started"); err != nil || ok {
		t.Fatalf("started dispatch requeued: %v %v", ok, err)
	}
}
