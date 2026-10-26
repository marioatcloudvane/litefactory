package turn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func workerTimingFixture(t *testing.T, prepare func(context.Context, Options, ActivityRequest) (ResearchInvocation, error)) (AgentActivity, Options, ActivityRequest) {
	t.Helper()
	f := newRetainedFixture(t, "research")
	at := time.Now().UTC()
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("f", 64), CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "work", StartedAt: at})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "work", RunID: f.opts.Attempt.RunID, Limit: 1, StartedAt: at})
	mustProgress(t, err)
	return AgentActivity{Policy: f.policy, Prepare: prepare}, Options{DB: f.db, WorkerTimeoutSeconds: 61, WorkerTimeoutSource: "settings.worker_timeout_seconds"}, ActivityRequest{Mode: f.policy.Mode, Node: "work", InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: f.opts.Attempt.RunID}
}

func TestWorkerDeadlineSetupExpiryWithoutProcessResult(t *testing.T) {
	old := agentBudgetClock
	agentBudgetClock = func(time.Duration) time.Duration { return -time.Millisecond }
	defer func() { agentBudgetClock = old }()
	handler, opts, request := workerTimingFixture(t, func(ctx context.Context, _ Options, _ ActivityRequest) (ResearchInvocation, error) {
		return ResearchInvocation{}, ctx.Err()
	})
	out, err := handler.execute(t.Context(), opts, request)
	if !errors.Is(err, ErrAgentTimedOut) || out.ProcessResult != nil || !strings.Contains(out.Diagnostic, "Process evidence unavailable") {
		t.Fatalf("result=%+v error=%v", out, err)
	}
	receipt, err := opts.DB.GetAttemptTimeout(t.Context(), request.NodeAttemptID)
	if err != nil || receipt.Budget != 61*time.Second {
		t.Fatalf("timeout=%+v error=%v", receipt, err)
	}
}

func TestWorkerDeadlineParentCancellationIsNotTimeout(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler, opts, request := workerTimingFixture(t, func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
		cancel()
		return ResearchInvocation{}, context.Canceled
	})
	_, err := handler.execute(parent, opts, request)
	if errors.Is(err, ErrAgentTimedOut) || !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if _, err = opts.DB.GetAttemptTimeout(t.Context(), request.NodeAttemptID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unexpected timeout receipt: %v", err)
	}
}

func TestWorkerDeadlineRetainsPersistedTimingOnReplay(t *testing.T) {
	handler, opts, request := workerTimingFixture(t, func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
		return ResearchInvocation{}, errors.New("setup unavailable")
	})
	started := time.Now().UTC().Add(-time.Hour)
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	_, err := opts.DB.EnsureAttemptTiming(t.Context(), store.AttemptTiming{NodeAttemptID: request.NodeAttemptID, StartedAt: started, Deadline: deadline, Allowance: 2 * time.Hour, Source: "node"})
	mustProgress(t, err)
	request.Attrs = map[string]string{graphexec.BudgetAttribute: "1"}
	_, err = handler.execute(t.Context(), opts, request)
	if err == nil {
		t.Fatal("setup error absent")
	}
	timing, err := opts.DB.GetAttemptTiming(t.Context(), request.NodeAttemptID, 0)
	mustProgress(t, err)
	if !timing.Deadline.Equal(deadline) || timing.Allowance != 2*time.Hour || timing.Source != "node" {
		t.Fatalf("replay reset timing: %+v", timing)
	}
}

func TestWorkerDeadlineNodeOverrideSurvivesShortProviderFallback(t *testing.T) {
	_, db, instance, err := budgetRun(t, map[string]string{graphexec.BudgetAttribute: "120"}, func(ctx context.Context) (*agent.RunResult, error) {
		provider, cancel := agent.InvocationContext(ctx, time.Nanosecond)
		defer cancel()
		deadline, _ := provider.Deadline()
		if time.Until(deadline) < 119*time.Minute {
			t.Errorf("provider shortened node deadline: %s", deadline)
		}
		return nil, errors.New("no process evidence")
	})
	if errors.Is(err, ErrAgentTimedOut) {
		t.Fatalf("fallback timed out the attempt: %v", err)
	}
	attempt, err := db.LatestGraphNodeAttempt(t.Context(), instance, "work")
	mustProgress(t, err)
	timing, err := db.GetAttemptTiming(t.Context(), attempt.ID, 0)
	mustProgress(t, err)
	if timing.Allowance != 2*time.Hour || timing.Source != "node" {
		t.Fatalf("timing=%+v", timing)
	}
}

func TestWorkerDeadlineCleanupHangAndFailure(t *testing.T) {
	worker, cancel := agent.WithWorkerDeadline(t.Context(), time.Now().Add(-time.Minute))
	defer cancel()
	release := make(chan struct{})
	done := make(chan struct{})
	err := closeInvocation(worker, func() error { defer close(done); <-release; return nil })
	close(release)
	<-done
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hanging cleanup error=%v", err)
	}
	failure := errors.New("helper failed")
	if err = closeInvocation(t.Context(), func() error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("cleanup failure=%v", err)
	}
}

func TestWorkerDeadlineParentExpiryIsTimeout(t *testing.T) {
	handler, opts, request := workerTimingFixture(t, func(ctx context.Context, _ Options, _ ActivityRequest) (ResearchInvocation, error) {
		<-ctx.Done()
		return ResearchInvocation{}, ctx.Err()
	})
	parent, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := handler.execute(parent, opts, request)
	if !errors.Is(err, ErrAgentTimedOut) {
		t.Fatalf("parent deadline error=%v", err)
	}
	timing, err := opts.DB.GetAttemptTiming(t.Context(), request.NodeAttemptID, 0)
	mustProgress(t, err)
	deadline, _ := parent.Deadline()
	if !timing.Deadline.Equal(deadline.Truncate(time.Millisecond)) || timing.Source != "parent" {
		t.Fatalf("parent timing=%+v", timing)
	}
	if _, err = opts.DB.GetAttemptTimeout(t.Context(), request.NodeAttemptID); err != nil {
		t.Fatalf("missing timeout receipt: %v", err)
	}
}
