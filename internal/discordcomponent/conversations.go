package discordcomponent

import (
	"context"
	"sync"
	"time"
)

// startConversations owns two goroutines, both joined by the serving wait group
// and bounded by its context. One runs turns, one dispatches saved replies, so
// a slow provider never blocks reply retries, ordinary commands or intake.
func (c *Component) startConversations(ctx context.Context, wg *sync.WaitGroup, at time.Time) {
	if c.conversations == nil {
		return
	}
	if err := c.service.EnableConversations(ctx, *c.conversations, at); err != nil {
		c.log.WarnContext(ctx, "conversation configuration could not be fully applied; retained work remains isolated")
	}
	for _, step := range []func(context.Context, time.Time) (bool, error){c.service.ConversationStep, c.service.ConversationDeliveryStep} {
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
						c.log.WarnContext(ctx, "conversation processing deferred; durable state retained", "error", err)
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
