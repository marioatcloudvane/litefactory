package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// NewExecutor adapts [turn.Execute] to the [Executor] the loop consumes.
//
// turn.Execute is a plain function of its dependencies and one comment, which
// is what makes the business path testable without a clock. This is the three
// lines that bind the dependencies once, at startup, so that the loop can hand
// it one comment at a time.
func NewExecutor(opts turn.Options, factories ...*projectconnection.Factory) Executor {
	return newTurnExecutor(opts, factories)
}

// NewConfirmApplier adapts [turn.ExecuteApplyResolution] to the
// [ConfirmApplier] US-12's confirm dispatch consumes, the same three-line
// binding [NewExecutor] gives [turn.Execute]. It is a separate constructor
// from [NewExecutor] rather than a shared one returning both interfaces,
// because [Options.Turn] and [Options.Confirmer] are declared as two
// different consumer interfaces (TEST-5) — production wiring calls both
// constructors with the same turn.Options (setup.go).
func NewConfirmApplier(opts turn.Options, factories ...*projectconnection.Factory) ConfirmApplier {
	return newTurnExecutor(opts, factories)
}

type turnExecutor struct {
	opts        turn.Options
	connections *projectconnection.Factory
}

func newTurnExecutor(opts turn.Options, factories []*projectconnection.Factory) turnExecutor {
	if opts.GraphRetryDelay <= 0 {
		opts.GraphRetryDelay = 30 * time.Second
	}
	e := turnExecutor{opts: opts}
	if len(factories) > 0 {
		e.connections = factories[0]
	}
	return e
}

func (e turnExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	ctx = sessionContext(ctx, req.SessionID)
	opts := e.opts
	if e.connections != nil {
		saved, err := opts.DB.GetExecutionSnapshot(ctx, req.SessionID)
		if err != nil {
			return turn.Result{}, err
		}
		version, err := opts.DB.GetConnectionVersion(ctx, saved.TicketVersionID)
		if err != nil {
			return turn.Result{}, err
		}
		clients, err := e.connections.Clients(ctx, version, saved.ActorID)
		if err != nil {
			return turn.Result{}, err
		}
		if clients.Code != nil {
			opts.AssetFetcher = clients.Code
		} else {
			opts.AssetFetcher = nil
		}
	}
	return turn.Execute(ctx, opts, req)
}

func (e turnExecutor) ApplyConfirmedResolution(ctx context.Context, req turn.ApplyRequest) (turn.ApplyResult, error) {
	return turn.ExecuteApplyResolution(sessionContext(ctx, req.SessionID), e.opts, req)
}

// PollStoryProgress is independent of Execute and worker capacity.
func (e turnExecutor) PollStoryProgress(ctx context.Context) error {
	if e.opts.StoryProgress == nil {
		return nil
	}
	return errors.Join(e.opts.StoryProgress.RecoverSubmissions(ctx), e.opts.StoryProgress.Poll(ctx), e.opts.DB.ReconcileSubmissionEffects(ctx))
}

// RecoverWorkflowJudges settles advisory bookkeeping at quiescent startup.
func (e turnExecutor) RecoverWorkflowJudges(ctx context.Context, at time.Time) error {
	return turn.RecoverWorkflowJudges(ctx, e.opts, at)
}

// RecoverImplementTimeout delivers and settles only factory-owned timeout
// bookkeeping after a stopped worker. No provider or worktree step runs here.
func (e turnExecutor) RecoverImplementTimeout(ctx context.Context, row store.ImplementStory) error {
	ctx = sessionContext(ctx, row.WorkerSession)
	return turn.RecoverImplementTimeoutPark(ctx, e.opts, row)
}
