package daemon

import (
	"context"

	"github.com/cloudvane/litefactory/internal/turn"
)

// NewExecutor adapts [turn.Execute] to the [Executor] the loop consumes.
//
// turn.Execute is a plain function of its dependencies and one comment, which
// is what makes the business path testable without a clock. This is the three
// lines that bind the dependencies once, at startup, so that the loop can hand
// it one comment at a time.
func NewExecutor(opts turn.Options) Executor { return turnExecutor{opts: opts} }

type turnExecutor struct{ opts turn.Options }

func (e turnExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	return turn.Execute(ctx, e.opts, req)
}
