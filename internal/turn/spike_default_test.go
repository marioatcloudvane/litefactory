package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func spikeWriteSetting(t *testing.T, f *fixture, yaml string) {
	t.Helper()
	dir := filepath.Join(f.repo.Dir, ".litefactory")
	mustProgress(t, os.MkdirAll(dir, 0700))
	mustProgress(t, os.WriteFile(filepath.Join(dir, "spike.yaml"), []byte(yaml), 0600))
}

// spikePersistedRoute observes the stored route without admitting one; it is
// empty when nothing was persisted.
func spikePersistedRoute(t *testing.T, f *fixture) string {
	t.Helper()
	route, err := f.db.ResearchExecutionPath(t.Context(), f.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return ""
	}
	mustProgress(t, err)
	return route
}

func spikeDoneRunner(t *testing.T) Runner {
	return lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		submitLiveResult(t, req, spikeDoneNone)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
}

func TestSpikeRunsOnTheGraphWhateverTheRetiredSettingHolds(t *testing.T) {
	for name, yaml := range map[string]string{
		"absent":         "",
		"graph":          "execution: graph\n",
		"legacy":         "execution: legacy\n",
		"unknown value":  "execution: maybe\n",
		"unknown field":  "execution: graph\nextra: 1\n",
		"duplicate key":  "execution: graph\nexecution: legacy\n",
		"malformed":      "execution: [\n",
		"binary garbage": "\x00\x01\x02",
	} {
		t.Run(name, func(t *testing.T) {
			f, forge, req := assembledSpikeFixture(t, spikeDoneRunner(t), yaml)
			out, err := Execute(t.Context(), f.opts, req)
			if err != nil || !out.Succeeded() || !out.ResearchGraph || forge.attemptCount() != 1 {
				t.Fatalf("the retired setting changed the route: %+v err=%v", out, err)
			}
			if spikeInstance(t, f).Status != store.GraphSucceeded {
				t.Fatal("graph instance did not finish")
			}
			if got := spikePersistedRoute(t, f); got != "graph" {
				t.Fatalf("persisted route %q", got)
			}
		})
	}
}

func TestSpikeSettingFileIsNeverRead(t *testing.T) {
	f, _, req := assembledSpikeFixture(t, nil, "")
	// A directory in place of the file, and an oversized file, would each fail a
	// reader; an inert file changes nothing.
	dir := filepath.Join(f.repo.Dir, ".litefactory", "spike.yaml")
	mustProgress(t, os.MkdirAll(dir, 0700))
	f.opts.spikeCapture = func(context.Context, WorkflowPolicy) (*teamsource.WorkflowDefinition, error) {
		return nil, errors.New("stop after admission")
	}
	if _, handled, err := admitSpikeExecution(t.Context(), f.opts, req, Result{Session: f.session}); !handled || err == nil || !strings.Contains(err.Error(), "stop after admission") {
		t.Fatalf("a leftover setting reached admission: handled=%v err=%v", handled, err)
	}
}

func TestSpikeRoutePersistedBeforeDispatch(t *testing.T) {
	f, _, req := assembledSpikeFixture(t, spikeDoneRunner(t), "")
	var atCapture string
	f.opts.spikeCapture = func(context.Context, WorkflowPolicy) (*teamsource.WorkflowDefinition, error) {
		atCapture = spikePersistedRoute(t, f)
		return nil, errors.New("fault before dispatch")
	}
	if _, handled, err := admitSpikeExecution(t.Context(), f.opts, req, Result{Session: f.session}); !handled || err == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if atCapture != "graph" {
		t.Fatalf("route was not persisted before dispatch: %q", atCapture)
	}
}

func TestSpikeRetiredOrCrossModeProvenanceIsRefusedBeforeAnyAgent(t *testing.T) {
	t.Run("recorded legacy route", func(t *testing.T) {
		f, forge, req := assembledSpikeFixture(t, spikeDoneRunner(t), "execution: graph\n")
		_, err := f.db.AdmitWorkflowPath(t.Context(), f.session.ID, "legacy")
		mustProgress(t, err)
		out, err := Execute(t.Context(), f.opts, req)
		if err != nil || out.Reason != spikeRetiredReason || out.Succeeded() || forge.attemptCount() != 0 {
			t.Fatalf("recorded legacy route was not refused: %+v err=%v", out, err)
		}
		if route := spikePersistedRoute(t, f); route != "legacy" {
			t.Fatalf("route rewritten to %q", route)
		}
		if _, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "spike"); err == nil {
			t.Fatal("a refused task created a graph instance")
		}
	})
	t.Run("continuation of a non-graph session", func(t *testing.T) {
		f, forge, req := assembledSpikeFixture(t, spikeDoneRunner(t), "")
		req.Continuation = &store.Continuation{}
		out, handled, err := admitSpikeExecution(t.Context(), f.opts, req, Result{Session: f.session, SessionID: f.session.ID})
		if err != nil || !handled || out.Reason != spikeRetiredReason || forge.attemptCount() != 0 {
			t.Fatalf("handled=%v err=%v out=%+v", handled, err, out)
		}
	})
}

func TestSpikeRuntimeFaultDoesNotFallBackToAnotherRoute(t *testing.T) {
	f, _, req := assembledSpikeFixture(t, nil, "")
	f.opts.spikeCapture = func(context.Context, WorkflowPolicy) (*teamsource.WorkflowDefinition, error) {
		return nil, errors.New("capture broke")
	}
	_, handled, err := admitSpikeExecution(t.Context(), f.opts, req, Result{Session: f.session})
	if !handled || err == nil || !strings.Contains(err.Error(), "capture broke") {
		t.Fatalf("fault fell back: handled=%v err=%v", handled, err)
	}
	if _, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "spike"); err == nil {
		t.Fatal("a faulted admission created an instance")
	}
}

func TestSpikeCancellationIsNotAFallback(t *testing.T) {
	f, _, req := assembledSpikeFixture(t, nil, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, handled, err := admitSpikeExecution(ctx, f.opts, req, Result{Session: f.session})
	if !handled || err == nil {
		t.Fatalf("cancelled admission: handled=%v err=%v", handled, err)
	}
	if _, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "spike"); err == nil {
		t.Fatal("a cancelled admission created an instance")
	}
}

func TestSpikeRestartAndContinuation(t *testing.T) {
	t.Run("graph session requires explicit owner continuation", func(t *testing.T) {
		f, _, req := assembledSpikeFixture(t, spikeDoneRunner(t), "")
		if _, err := Execute(t.Context(), f.opts, req); err != nil {
			t.Fatal(err)
		}
		req.Continuation = &store.Continuation{}
		_, handled, err := admitSpikeExecution(t.Context(), f.opts, req, Result{Session: f.session})
		if !handled || err == nil {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
	})
	t.Run("restart keeps the admitted graph route", func(t *testing.T) {
		f, _, req := assembledSpikeFixture(t, spikeDoneRunner(t), "")
		if _, err := Execute(t.Context(), f.opts, req); err != nil {
			t.Fatal(err)
		}
		spikeWriteSetting(t, f, "execution: legacy\n")
		if got := spikePersistedRoute(t, f); got != "graph" {
			t.Fatalf("route after restart %q", got)
		}
	})
}
