package alerts

import (
	"context"

	"github.com/cloudvane/litefactory/internal/store"
)

// SyncConfiguration publishes nonsecret effective switches for local status.
func (e *Engine) SyncConfiguration(ctx context.Context) error {
	return e.DB.UpdateAlerts(ctx, func(l *store.AlertLedger) error {
		rules := make(map[string]bool, len(e.Settings.Rules))
		for k, v := range e.Settings.Rules {
			rules[k] = v
		}
		l.Configuration = store.AlertConfiguration{Configured: true, Enabled: e.Settings.Enabled, Rules: rules, QuietEnabled: e.Settings.QuietHours.Enabled, QuietStart: e.Settings.QuietHours.Start, QuietEnd: e.Settings.QuietHours.End}
		return nil
	})
}
