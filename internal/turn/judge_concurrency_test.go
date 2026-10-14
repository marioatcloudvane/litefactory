package turn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

type heldJudgeRunner struct {
	entered  chan struct{}
	release  chan struct{}
	delegate recoveryJudgeRunner
}

func (r *heldJudgeRunner) RunJudge(ctx context.Context, in agent.JudgeRequest) (agent.JudgeRunResult, error) {
	close(r.entered)
	select {
	case <-r.release:
		return r.delegate.RunJudge(ctx, in)
	case <-ctx.Done():
		return agent.JudgeRunResult{}, ctx.Err()
	}
}

func TestJudgeConcurrentRedeliveryCannotInterruptActiveAssessment(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	runner := &heldJudgeRunner{entered: make(chan struct{}), release: make(chan struct{})}
	service.Providers[agent.Kind("codex")] = runner
	finished := make(chan error, 1)
	// One test-owned invocation; its lifetime ends when release closes or the test context ends.
	go func() { _, err := service.Execute(t.Context(), request); finished <- err }()
	select {
	case <-runner.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("waiting for admitted judge launch")
	}
	if _, err := service.Execute(t.Context(), request); !errors.Is(err, store.ErrStoryProgressBusy) {
		close(runner.release)
		t.Fatalf("duplicate command did not defer: %v", err)
	}
	close(runner.release)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("waiting for held judge completion")
	}
	q, err := service.DB.FindJudgeRequest(t.Context(), request.TicketKey, request.RemoteCommentID)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := service.DB.ListJudgeAttempts(t.Context(), q.ID)
	if err != nil || len(attempts) != 1 || attempts[0].State != "completed" {
		t.Fatalf("active attempt was interrupted by redelivery: %+v %v", attempts, err)
	}
}
