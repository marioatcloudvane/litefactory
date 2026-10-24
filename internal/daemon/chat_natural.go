package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
			if ctx.Err() != nil {
				return
			}
			d.chat.reads.report(ctx, "ordinary message intake", err)
			delay = 5 * time.Second
			if err != nil {
				failures++
				delay = time.Duration(1<<min(failures, 6)) * time.Second
				var failure *discord.IntakeFailure
				if errors.As(err, &failure) {
					delay = max(delay, failure.RetryAfter)
				}
				d.setChatStatus(intakeStatus(err, delay))
			} else {
				failures = 0
				d.setChatStatus(chatAvailable)
				service.Notify()
			}
		}
	}()
}

// intakeStatus tells the operator why ordinary messages are not read: lost
// access needs their action, a rate limit or a failed read clears by itself.
func intakeStatus(err error, retry time.Duration) string {
	var failure *discord.IntakeFailure
	if errors.As(err, &failure) && failure.Capability {
		return "available; ordinary messages unavailable (" + failure.Kind() + "): check Message Content and channel visibility/history"
	}
	kind := "read failed"
	if errors.As(err, &failure) {
		kind = failure.Kind()
	}
	return fmt.Sprintf("available; ordinary messages delayed: Discord %s, retrying in %s", kind, retry.Round(time.Second))
}

// discordReadLog logs Discord read failures once per change of state, not on
// every poll: the kind, Retry-After and HTTP status, never the token or any
// message content.
type discordReadLog struct {
	mu   sync.Mutex
	log  *slog.Logger
	last map[string]string
}

func (l *discordReadLog) report(ctx context.Context, source string, err error) {
	if l == nil || l.log == nil {
		return
	}
	kind := ""
	var attrs []any
	var failure *discord.IntakeFailure
	switch {
	case err == nil:
	case errors.As(err, &failure):
		kind = failure.Kind()
		attrs = append(attrs, slog.Int("status", failure.Status))
		if failure.RetryAfter > 0 {
			attrs = append(attrs, slog.Duration("retry_after", failure.RetryAfter))
		}
	default:
		// Intake's own failures carry fixed descriptions, never message text.
		kind = "other"
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	l.mu.Lock()
	previous := l.last[source]
	if l.last == nil {
		l.last = map[string]string{}
	}
	l.last[source] = kind
	l.mu.Unlock()
	if kind == previous {
		return
	}
	if kind == "" {
		l.log.InfoContext(ctx, "Discord reads recovered", slog.String("source", source), slog.String("previous", previous))
		return
	}
	l.log.WarnContext(ctx, "Discord reads failing", append([]any{slog.String("source", source), slog.String("kind", kind)}, attrs...)...)
}
