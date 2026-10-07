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
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// budgetRun runs one agent node that declares attrs, with a runner that waits for
// release or for its own context to end, and returns the graph result and error.
func budgetRun(t *testing.T, attrs map[string]string, runner func(context.Context) (*agent.RunResult, error)) (GraphNodeResult, *store.DB, int64, error) {
	t.Helper()
	f := newRetainedFixture(t, "research")
	at := time.Now().UTC()
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("e", 64), CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "work", StartedAt: at})
	mustProgress(t, err)
	profile := &graphexec.Profile{Mode: f.policy.Mode, Team: "builtin", ProfileVersion: 1, Digest: def.Digest, Nodes: []graphexec.Node{{Name: "work", Action: ResearchAgentAction, Limit: 1, Attrs: attrs}}}
	run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
	mustProgress(t, err)
	segment, err := f.db.GetSegment(t.Context(), run.SegmentID)
	mustProgress(t, err)
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	worktree := t.TempDir()
	mustProgress(t, os.MkdirAll(filepath.Join(worktree, ".aiteam/session"), 0700))
	stub := newStubRunner(func(runctx context.Context, _ agent.Request) (*agent.RunResult, error) { return runner(runctx) })
	handler := AgentActivity{Policy: f.policy, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
		request := agent.Request{Mode: string(f.policy.Mode), Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: worktree, Prompt: "Work", Permissions: agent.Permissions{Profile: "research"}}
		return ResearchInvocation{Request: request, Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: f.service}, nil
	}}
	var registry ActivityRegistry
	mustProgress(t, registry.Register(ResearchAgentAction, handler))
	out, err := ExecuteNodeAction(t.Context(), Options{DB: f.db, Runners: runnerSet(stub)}, GraphNodeRequest{Profile: profile, Activities: registry, InstanceID: instance.ID, RunID: run.ID, At: at})
	return out, f.db, instance.ID, err
}

// AC-004: an agent node with a time budget is cancelled when the budget runs out
// and the attempt is recorded as timed out.
func TestAgentBudgetCancelsTheRunAndRecordsATimeout(t *testing.T) {
	old := agentBudgetClock
	agentBudgetClock = func(time.Duration) time.Duration { return 50 * time.Millisecond }
	defer func() { agentBudgetClock = old }()
	cancelled := false
	out, db, instance, err := budgetRun(t, map[string]string{graphexec.BudgetAttribute: "15"}, func(runctx context.Context) (*agent.RunResult, error) {
		select {
		case <-runctx.Done():
			cancelled = true
			return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "cancelled"}, runctx.Err()
		case <-time.After(10 * time.Second):
			return &agent.RunResult{Outcome: agent.OutcomeSucceeded}, nil
		}
	})
	if !cancelled || !errors.Is(err, ErrAgentTimedOut) {
		t.Fatalf("cancelled=%v err=%v", cancelled, err)
	}
	if !strings.Contains(out.Activity.Diagnostic, "15-minute time budget") {
		t.Fatalf("diagnostic %q", out.Activity.Diagnostic)
	}
	attempt, err := db.LatestGraphNodeAttempt(t.Context(), instance, "work")
	if err != nil || attempt.Status != store.GraphFailed {
		t.Fatalf("attempt %+v %v", attempt, err)
	}
	timeout, err := db.GetAttemptTimeout(t.Context(), attempt.ID)
	if err != nil || timeout.Budget != 15*time.Minute {
		t.Fatalf("timeout %+v %v", timeout, err)
	}
}

// AC-004: a node that finishes inside its budget, and a node with no budget, are not timed out.
func TestAgentBudgetDoesNotTouchARunThatFinishes(t *testing.T) {
	for name, attrs := range map[string]map[string]string{"inside the budget": {graphexec.BudgetAttribute: "15"}, "no budget": nil} {
		t.Run(name, func(t *testing.T) {
			_, db, instance, err := budgetRun(t, attrs, func(context.Context) (*agent.RunResult, error) {
				return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "exit status 1"}, errors.New("process failure")
			})
			if errors.Is(err, ErrAgentTimedOut) {
				t.Fatalf("a run inside its budget was timed out: %v", err)
			}
			attempt, aerr := db.LatestGraphNodeAttempt(t.Context(), instance, "work")
			mustProgress(t, aerr)
			if _, terr := db.GetAttemptTimeout(t.Context(), attempt.ID); !errors.Is(terr, store.ErrNotFound) {
				t.Fatalf("timeout recorded: %v", terr)
			}
		})
	}
}

// AC-004: a factory shutdown is not a timeout.
func TestAgentBudgetShutdownIsNotATimeout(t *testing.T) {
	_, db, instance, err := budgetRun(t, map[string]string{graphexec.BudgetAttribute: "15"}, func(context.Context) (*agent.RunResult, error) {
		return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "stopped"}, context.Canceled
	})
	if errors.Is(err, ErrAgentTimedOut) {
		t.Fatalf("a shutdown was recorded as a timeout: %v", err)
	}
	attempt, aerr := db.LatestGraphNodeAttempt(t.Context(), instance, "work")
	mustProgress(t, aerr)
	if _, terr := db.GetAttemptTimeout(t.Context(), attempt.ID); !errors.Is(terr, store.ErrNotFound) {
		t.Fatalf("timeout recorded: %v", terr)
	}
}

// AC-004: Validate settles a failed or timed-out attempt as the declared failed
// outcome, and leaves a factory shutdown an error.
func TestValidateAfterAgentFailureSettlesFailedButNotShutdown(t *testing.T) {
	out, err := validateAfterAgentFailure(t.Context(), Options{}, ActivityRequest{}, ActivityResult{Diagnostic: "timed out"}, ErrAgentTimedOut)
	if err != nil || out.Outcome != "failed" || out.Diagnostic != "timed out" {
		t.Fatalf("%+v %v", out, err)
	}
	out, err = validateAfterAgentFailure(t.Context(), Options{}, ActivityRequest{}, ActivityResult{}, errors.New("boom"))
	if err != nil || out.Outcome != "failed" || out.Diagnostic == "" {
		t.Fatalf("%+v %v", out, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = validateAfterAgentFailure(ctx, Options{}, ActivityRequest{}, ActivityResult{}, context.Canceled); err == nil {
		t.Fatal("a shutdown was settled as a failed outcome")
	}
}
