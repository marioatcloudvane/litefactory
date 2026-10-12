package messaging

import (
	"context"
	"sync"
	"time"
)

// typingInterval refreshes the transport's typing indicator; Discord's lasts
// about 10 s per call. Tests shorten it.
var typingInterval = 8 * time.Second

// typingTimeout bounds one typing call, and so how long stop can wait for it.
const typingTimeout = 5 * time.Second

// typer is an optional sender capability; failures are the sender's to ignore.
type typer interface {
	Typing(ctx context.Context, channel string)
}

// typing shows a best-effort typing indicator in channel until stop is called.
// stop ends the refresh loop and waits for an in-flight call to finish rather
// than cancelling it, so every typing call reaches the transport before the
// reply and none follows it.
func (s *Service) typing(ctx context.Context, channel string) (stop func()) {
	t, ok := s.sender.(typer)
	if !ok {
		return func() {}
	}
	done, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(typingInterval)
		defer ticker.Stop()
		for {
			call, end := context.WithTimeout(ctx, typingTimeout)
			t.Typing(call, channel)
			end()
			select {
			case <-done.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}
