package turn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// Gap tests for story-001 T-007: policy admission across the whole registry, and
// the unchanged new-session route selection that keeps graph Plan held back.

// AC-001, AC-033: every registered workflow is admitted, and none of them can
// complete an attempt from terminal output alone, whatever decoder it names. A new
// registered mode has to add a result fixture here or this test fails: it cannot
// join the registry without the guard being exercised for it.
func TestRegisteredWorkflowPolicies_EachRefusesTerminalOnlyCompletion(t *testing.T) {
	t.Parallel()
	seen := map[agent.Mode]bool{}
	for _, policy := range RegisteredWorkflowPolicies() {
		mode := policy.Mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			if err := policy.Admit(); err != nil {
				t.Fatalf("registered policy refused: %v", err)
			}
			if mode != agent.ModeResearch && mode != agent.ModePlan {
				t.Fatalf("registered mode %q has no result fixture in this guard; add one", mode)
			}
			f := newRetainedFixture(t, string(mode))
			_, _, err := AgentActivity{Policy: policy}.acceptRetained(t.Context(), f.service, envelope("done", f.result))
			if !errors.Is(err, errNoRetainedResult) {
				t.Fatalf("terminal-only output completed the %s attempt: %v", mode, err)
			}
			f.submit(t, f.result)
			if _, _, err = (AgentActivity{Policy: policy}).acceptRetained(t.Context(), f.service, envelope("done", f.result)); err != nil {
				t.Fatalf("the confirmed retained result was refused: %v", err)
			}
		})
		if seen[mode] {
			t.Fatalf("mode %q is registered twice", mode)
		}
		seen[mode] = true
	}
	if !seen[agent.ModeResearch] || !seen[agent.ModePlan] {
		t.Fatalf("the registry lost a shipped workflow: %v", seen)
	}
}

// AC-012: a new session's Plan route is graph and never falls back to legacy; a
// repeat is idempotent; a mode other than Plan is never admitted; and a session
// already admitted to either route keeps it.
func TestPlanAdmission_NewSessionsGetGraphAndExistingRoutesAreKept(t *testing.T) {
	t.Parallel()
	newFixtureWithoutRun := func(t *testing.T) *fixture {
		return newFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
			t.Error("admission launched an agent")
			return nil, errors.New("unexpected launch")
		}))
	}
	planRequest := func(f *fixture) Request {
		req := f.request()
		req.Mode = agent.ModePlan
		req.Command = ParseCommand("/plan shape stories")
		req.Trigger.Body = "/plan shape stories"
		return req
	}
	t.Run("new session selects graph and never falls back to legacy", func(t *testing.T) {
		t.Parallel()
		f := newFixtureWithoutRun(t)
		req := planRequest(f)
		for range 2 {
			// Without durable reporting the graph route refuses; the refusal is the
			// graph's own and the turn is not handed to legacy Plan.
			_, handled, err := admitPlanExecution(t.Context(), f.opts, req, Result{Ref: f.ref(), Session: f.session, SessionID: f.session.ID})
			if !handled || err == nil || !strings.Contains(err.Error(), "graph Plan requires durable reporting") {
				t.Fatalf("new session did not take the graph route: handled=%v err=%v", handled, err)
			}
		}
		if route, err := f.db.PlanExecutionPath(t.Context(), f.session.ID); err != nil || route != "graph" {
			t.Fatalf("route = %q %v", route, err)
		}
	})
	t.Run("other modes are never admitted to a Plan route", func(t *testing.T) {
		t.Parallel()
		f := newFixtureWithoutRun(t)
		req := f.request() // Spike
		if _, handled, err := admitPlanExecution(t.Context(), f.opts, req, Result{Ref: f.ref(), Session: f.session, SessionID: f.session.ID}); err != nil || handled {
			t.Fatalf("a non-Plan turn was handled by Plan admission: handled=%v err=%v", handled, err)
		}
		if _, err := f.db.PlanExecutionPath(t.Context(), f.session.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a non-Plan turn froze a Plan route: %v", err)
		}
	})
	t.Run("an admitted graph session keeps its route", func(t *testing.T) {
		t.Parallel()
		f := newFixtureWithoutRun(t)
		if route, err := f.db.AdmitPlanWorkflowPath(t.Context(), f.session.ID, "graph"); err != nil || route != "graph" {
			t.Fatalf("setup: %q %v", route, err)
		}
		// Without durable reporting the graph route refuses. It must not treat that as
		// a reason to run legacy: handled stays true and the cause is the graph's.
		_, handled, err := admitPlanExecution(t.Context(), f.opts, planRequest(f), Result{Ref: f.ref(), Session: f.session, SessionID: f.session.ID})
		if !handled || err == nil || !strings.Contains(err.Error(), "graph Plan requires durable reporting") {
			t.Fatalf("an admitted graph session did not stay on graph: handled=%v err=%v", handled, err)
		}
		if route, err := f.db.PlanExecutionPath(t.Context(), f.session.ID); err != nil || route != "graph" {
			t.Fatalf("route changed to %q %v", route, err)
		}
	})
	t.Run("an admitted legacy session cannot be moved by a later graph selection", func(t *testing.T) {
		t.Parallel()
		f := newFixtureWithoutRun(t)
		if route, err := f.db.AdmitPlanWorkflowPath(t.Context(), f.session.ID, "legacy"); err != nil || route != "legacy" {
			t.Fatalf("setup: %q %v", route, err)
		}
		if route, err := f.db.AdmitPlanWorkflowPath(t.Context(), f.session.ID, "graph"); err != nil || route != "legacy" {
			t.Fatalf("a later selection moved a frozen legacy route: %q %v", route, err)
		}
	})
}
