package daemon

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
)

// startNaturalIntake polls independently of the slower command/model worker.
func (d *Daemon) startNaturalIntake(ctx context.Context, wg *sync.WaitGroup, intake *messaging.Intake) {
	if intake == nil {
		return
	}
	service := d.chat.service
	wg.Add(1)
	go func() {
		defer wg.Done()
		delay := time.Duration(0)
		failures := 0
		for {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			err := intake.Poll(ctx, time.Now())
			delay = 5 * time.Second
			if err != nil {
				failures++
				delay = time.Duration(1<<min(failures, 6)) * time.Second
				var failure *discord.IntakeFailure
				if errors.As(err, &failure) {
					delay = max(delay, failure.RetryAfter)
				}
				d.setChatStatus("available; ordinary messages unavailable: check Message Content and channel visibility/history")
			} else {
				failures = 0
				d.setChatStatus(chatAvailable)
				service.Notify()
			}
		}
	}()
}
