package turn

import (
	"context"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// Repairing a later execution must not rewrite the failure used as its evidence.
func TestValidateSuiteSafetyRepairPreservesFailedExecution(t *testing.T) {
	f := newSuiteFixture(t)
	f.exits["full-two"] = 1
	first, out, err := f.exec(101)
	if err != nil || first.Outcome != "failed" {
		t.Fatalf("initial failure: %s %v", first.Outcome, err)
	}
	before, err := f.db.GetSuiteRun(t.Context(), out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	commandsBefore, err := f.db.SuiteCommands(t.Context(), out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	f.exits["full-two"] = 0
	repaired, _, err := f.exec(102)
	if err != nil || repaired.Outcome != "passed" {
		t.Fatalf("repair: %s %v", repaired.Outcome, err)
	}
	after, err := f.db.GetSuiteRun(t.Context(), out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	commandsAfter, err := f.db.SuiteCommands(t.Context(), out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(commandsBefore, commandsAfter) {
		t.Fatal("repair rewrote the failed execution")
	}
	replay, _, err := f.exec(101)
	if err != nil || replay.Outcome != "failed" || string(replay.Output) != string(first.Output) {
		t.Fatalf("failure replay after repair: %s %v", replay.Outcome, err)
	}
}

type safetySuiteRepo struct {
	scratch *safetySuiteScratch
}

func (r safetySuiteRepo) Begin(context.Context, string) (ValidateSuiteScratch, error) {
	return r.scratch, nil
}

type safetySuiteScratch struct {
	ValidateSuiteScratch
	observedContexts []error
	closeContext     error
	closed           bool
}

func (s *safetySuiteScratch) Observe(ctx context.Context) (string, error) {
	s.observedContexts = append(s.observedContexts, ctx.Err())
	return s.ValidateSuiteScratch.Observe(ctx)
}
func (s *safetySuiteScratch) Close(ctx context.Context) error {
	s.closed, s.closeContext = true, ctx.Err()
	return s.ValidateSuiteScratch.Close(ctx)
}

func TestValidateSuiteSafetyCancellationRetainsCleanupAndReleasesLock(t *testing.T) {
	f := newSuiteFixture(t)
	scratch := &safetySuiteScratch{ValidateSuiteScratch: &suiteScratchFake{r: f.repo}}
	f.b.Repo = safetySuiteRepo{scratch: scratch}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	inner := f.b.Run
	f.b.Run = func(ctx context.Context, path string, command config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		result := inner(ctx, path, command, started)
		cancel()
		result.Outcome, result.Reason = store.SuiteCancelled, "cancelled"
		return result
	}
	_, err := (ValidateSuiteActivity{f.b}).Execute(ctx, Options{}, ActivityRequest{Mode: agent.ModeValidate, InstanceID: 103, Node: "suite"})
	if err == nil {
		t.Fatal("cancelled execution must return an error")
	}
	if !scratch.closed || scratch.closeContext != nil || len(scratch.observedContexts) != 2 || scratch.observedContexts[1] != nil {
		t.Fatalf("cleanup lost live context: %+v", scratch)
	}
	run, err := f.db.LatestSuiteRun(t.Context(), 103, suiteCommit)
	if err != nil || run.State != store.SuiteCancelled || run.TreeAfter != "tree-a" {
		t.Fatalf("retained cancellation: %+v %v", run, err)
	}
	commands, err := f.db.SuiteCommands(t.Context(), run.ID)
	if err != nil || len(commands) != 1 || commands[0].State != store.SuiteCancelled {
		t.Fatalf("retained commands: %+v %v", commands, err)
	}
	unlock, err := f.db.LockSuite(t.Context(), f.lockDir)
	if err != nil {
		t.Fatalf("cancellation left suite lock held: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}
