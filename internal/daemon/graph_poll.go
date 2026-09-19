package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// pollDueGraphInstances is deliberately unwired. Selection does not reserve or
// dispatch work; a later admission path must own those decisions separately.
func pollDueGraphInstances(ctx context.Context, db *store.DB, now time.Time, limit int) ([]store.GraphInstance, error) {
	if db == nil {
		return nil, fmt.Errorf("daemon: poll due graph instances: missing store")
	}
	instances, err := db.ListDueGraphInstances(ctx, now, limit)
	if err != nil {
		return nil, fmt.Errorf("daemon: poll due graph instances: %w", err)
	}
	return instances, nil
}
