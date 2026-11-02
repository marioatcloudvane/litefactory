package daemon

import (
	"context"
	"sync"
	"time"
)

func (d *Daemon) observeMainCI(ctx context.Context, at time.Time) error {
	if d.ciMonitor == nil {
		return nil
	}
	return d.ciMonitor.Poll(ctx, at)
}

// serveMainCI owns one cancellable, joined observer goroutine. It never
// reserves work capacity, and slow Actions reads cannot hold alert delivery.
func (d *Daemon) serveMainCI(ctx context.Context, wg *sync.WaitGroup) {
	if d.ciMonitor == nil {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		initialized := false
		for {
			var err error
			if !initialized {
				err = d.ciMonitor.Reset(ctx, d.alertClock())
				initialized = err == nil
			}
			if err == nil {
				// Poll bounds the network scan itself. Keep the lifecycle context alive
				// so a scan deadline can still be journaled as unavailable monitoring.
				err = d.observeMainCI(ctx, d.alertClock())
			}
			if err != nil {
				d.log.WarnContext(ctx, "CI monitoring unavailable")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
