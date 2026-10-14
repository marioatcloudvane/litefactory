package daemon

import (
	"context"
	"sync"
	"time"
)

// startConversations owns two goroutines, both joined by the serving wait group
// and bounded by its context. One runs turns, one dispatches saved replies, so
// a slow provider never blocks reply retries, ordinary commands or intake.
func (d *Daemon) startConversations(ctx context.Context, wg *sync.WaitGroup, at time.Time) {
	if d.chat.conversations == nil {
		return
	}
	if err := d.chat.service.EnableConversations(ctx, *d.chat.conversations, at); err != nil {
		d.log.WarnContext(ctx, "conversation configuration could not be fully applied; retained work remains isolated")
	}
	for _, step := range []func(context.Context, time.Time) (bool, error){d.chat.service.ConversationStep, d.chat.service.ConversationDeliveryStep} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				for range 32 {
					more, err := step(ctx, time.Now())
					if err != nil {
						d.log.WarnContext(ctx, "conversation processing deferred; durable state retained")
						break
					}
					if !more {
						break
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
		}()
	}
}
