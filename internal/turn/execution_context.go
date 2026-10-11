package turn

import "context"

type executionSessionKey struct{}

// ExecutionContext carries the retained session through publication recovery
// and managed child operations without passing mutable project configuration.
func ExecutionContext(ctx context.Context, sessionID int64) context.Context {
	return context.WithValue(ctx, executionSessionKey{}, sessionID)
}

// ExecutionSessionID returns the retained session selected by the effect owner.
func ExecutionSessionID(ctx context.Context) int64 {
	sessionID, _ := ctx.Value(executionSessionKey{}).(int64)
	return sessionID
}
