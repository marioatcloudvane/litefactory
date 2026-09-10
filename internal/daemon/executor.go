package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/turn"
)

// NewExecutor adapts [turn.Execute] to the [Executor] the loop consumes.
//
// turn.Execute is a plain function of its dependencies and one comment, which
// is what makes the business path testable without a clock. This is the three
// lines that bind the dependencies once, at startup, so that the loop can hand
// it one comment at a time.
func NewExecutor(opts turn.Options) Executor { return turnExecutor{opts: opts} }

// NewConfirmApplier adapts [turn.ExecuteApplyResolution] to the
// [ConfirmApplier] US-12's confirm dispatch consumes, the same three-line
// binding [NewExecutor] gives [turn.Execute]. It is a separate constructor
// from [NewExecutor] rather than a shared one returning both interfaces,
// because [Options.Turn] and [Options.Confirmer] are declared as two
// different consumer interfaces (TEST-5) — production wiring calls both
// constructors with the same turn.Options (setup.go).
func NewConfirmApplier(opts turn.Options) ConfirmApplier { return turnExecutor{opts: opts} }

type turnExecutor struct{ opts turn.Options }

func (e turnExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	return turn.Execute(ctx, e.opts, req)
}

func (e turnExecutor) ApplyConfirmedResolution(ctx context.Context, req turn.ApplyRequest) (turn.ApplyResult, error) {
	return turn.ExecuteApplyResolution(ctx, e.opts, req)
}

// PollStoryProgress is independent of Execute and worker capacity.
func (e turnExecutor) PollStoryProgress(ctx context.Context) error {
	if e.opts.StoryProgress == nil {
		return nil
	}
	return errors.Join(e.opts.StoryProgress.RecoverSubmissions(ctx), e.opts.StoryProgress.Poll(ctx), e.opts.DB.ReconcileSubmissionEffects(ctx))
}
