package alerts

import (
	"context"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// Readiness records complete coverage independently of whether incidents exist.
// A failed source read invalidates the clean streak without resolving anything.
func (e *Engine) Readiness(ctx context.Context, rule string, at time.Time, ready bool, reason string) error {
	return e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		o := l.Observers[rule]
		o.LastAttempt = at
		o.Ready = ready
		o.Reason = reason
		if ready {
			o.LastSuccess = at
		} else {
			for _, s := range l.Subjects {
				if s.Rule == rule {
					s.CleanCount = 0
				}
			}
		}
		l.Observers[rule] = o
		return nil
	})
}
