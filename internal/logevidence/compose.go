package logevidence

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/submission"
)

// Handle serves the same authorized, budgeted catalog inside a composed broker.
// Callers must retain this service and its budget for the whole invocation.
func (s *Service) Handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	return s.handle(ctx, q)
}

// Boundary exposes the immutable discovery boundary without spending a data call.
func (s *Service) Boundary() (time.Time, int64, string) {
	return s.cutoff, s.highWater, s.scopeDigest
}
