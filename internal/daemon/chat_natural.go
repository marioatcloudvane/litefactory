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

// safetyPoll reads ordinary messages while the Gateway is down. A healthy
// Gateway pushes each owner message, so nothing is polled then.
const safetyPoll = 5 * time.Minute

// naturalIntake reads ordinary messages when the Gateway reports one, and
// catches up from the stored cursor at start, after every (re)connect and,
// while the Gateway is down, every safetyPoll.
type naturalIntake struct {
	intake   *messaging.Intake
	run      func(context.Context, discord.GatewayOptions) error
	notify   func()
	status   func(string)
	reaction func(discord.OwnerReaction)
	reads    *discordReadLog
	log      *slog.Logger
	safety   time.Duration
	// backoff bounds Gateway reconnects; zero keeps the adapter's defaults.
	backoff [2]time.Duration

	mu     sync.Mutex
	dirty  map[string]bool
	state  discord.GatewayState
	logged string
	wake   chan struct{}
}

// startNaturalIntake runs the Gateway and the reads it triggers, both
// joined by wg when ctx ends.
func (d *Daemon) startNaturalIntake(ctx context.Context, wg *sync.WaitGroup, intake *messaging.Intake) {
	if intake == nil {
		return
	}
	n := &naturalIntake{intake: intake, run: d.chat.adapter.RunGateway, notify: d.chat.service.Notify, status: d.setChatStatus, reads: d.chat.reads, log: d.log, safety: safetyPoll}
	n.reaction = func(r discord.OwnerReaction) { d.ownerReaction(ctx, r) }
	n.start(ctx, wg)
}

func (n *naturalIntake) start(ctx context.Context, wg *sync.WaitGroup) {
	n.dirty = make(map[string]bool)
	n.wake = make(chan struct{}, 1)
	n.state = discord.GatewayState{Phase: discord.GatewayConnecting}
	n.markAll()
	// A permanent failure is logged and shown by gatewayState; reads go on
	// every safety period.
	wg.Go(func() {
		_ = n.run(ctx, discord.GatewayOptions{Owner: n.intake.Owner, Channels: n.intake.Channels, OnMessage: n.mark, OnReaction: n.reaction, OnReady: n.markAll, OnState: n.gatewayState, MinBackoff: n.backoff[0], MaxBackoff: n.backoff[1]})
	})
	wg.Go(func() { n.serve(ctx) })
}

func (n *naturalIntake) mark(channel string) {
	n.mu.Lock()
	n.dirty[channel] = true
	n.mu.Unlock()
	n.poke()
}

func (n *naturalIntake) markAll() {
	n.mu.Lock()
	for _, channel := range n.intake.Channels {
		n.dirty[channel] = true
	}
	n.mu.Unlock()
	n.poke()
}

func (n *naturalIntake) poke() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

// gatewayState records the connection, logs each change of phase or cause
// once and wakes the reader so it starts or stops the safety poll.
func (n *naturalIntake) gatewayState(s discord.GatewayState) {
	n.mu.Lock()
	n.state = s
	key := string(s.Phase) + "|" + s.Reason
	quiet := s.Phase == discord.GatewayConnecting || s.Phase == discord.GatewayResuming || key == n.logged
	if !quiet {
		n.logged = key
	}
	n.mu.Unlock()
	switch {
	case quiet:
	case s.Phase == discord.GatewayConnected:
		n.log.Info("Discord Gateway connected; ordinary messages arrive as they are sent")
	case s.Phase == discord.GatewayFailed:
		n.log.Error("Discord Gateway failed permanently", slog.String("reason", s.Reason))
	default:
		n.log.Warn("Discord Gateway disconnected; reconnecting", slog.String("reason", s.Reason), slog.Duration("retry", s.Retry))
	}
	n.poke()
}

// serve reads every marked channel, retries failed reads with backoff and,
// only while the Gateway is unhealthy, marks all channels every safety period.
func (n *naturalIntake) serve(ctx context.Context) {
	failures := 0
	var retryAt, lastRead time.Time
	readFailure := ""
	for {
		now := time.Now()
		n.mu.Lock()
		state := n.state
		if !state.Healthy() && now.Sub(lastRead) >= n.safety {
			for _, channel := range n.intake.Channels {
				n.dirty[channel] = true
			}
		}
		var channels []string
		if !now.Before(retryAt) {
			for _, channel := range n.intake.Channels {
				if n.dirty[channel] {
					channels = append(channels, channel)
					delete(n.dirty, channel)
				}
			}
		}
		n.mu.Unlock()
		if len(channels) > 0 {
			var errs []error
			for _, channel := range channels {
				if err := n.intake.PollChannel(ctx, channel, time.Now()); err != nil {
					errs = append(errs, err)
					n.mark(channel)
				}
			}
			if ctx.Err() != nil {
				return
			}
			lastRead = time.Now()
			err := errors.Join(errs...)
			n.reads.report(ctx, "ordinary message intake", err)
			if err != nil {
				failures++
				delay := time.Duration(1<<min(failures, 6)) * time.Second
				var failure *discord.IntakeFailure
				if errors.As(err, &failure) {
					delay = max(delay, failure.RetryAfter)
				}
				retryAt = lastRead.Add(delay)
				readFailure = intakeStatus(err, delay)
			} else {
				failures = 0
				retryAt = time.Time{}
				readFailure = ""
				n.notify()
			}
		}
		if readFailure != "" {
			n.status(readFailure)
		} else {
			n.status(gatewayStatus(state, n.safety))
		}
		wait := time.Until(retryAt)
		if retryAt.IsZero() {
			wait = -1
			if !state.Healthy() {
				wait = time.Until(lastRead.Add(n.safety))
			}
		}
		var timer *time.Timer
		var fired <-chan time.Time
		if wait >= 0 {
			timer = time.NewTimer(wait)
			fired = timer.C
		}
		select {
		case <-ctx.Done():
		case <-n.wake:
		case <-fired:
		}
		if timer != nil {
			timer.Stop()
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// gatewayStatus tells the operator how ordinary messages arrive.
func gatewayStatus(s discord.GatewayState, safety time.Duration) string {
	switch s.Phase {
	case discord.GatewayConnected:
		return chatAvailable + "; Discord Gateway connected"
	case discord.GatewayConnecting, discord.GatewayResuming:
		return chatAvailable + "; Discord Gateway " + string(s.Phase)
	case discord.GatewayFailed:
		return fmt.Sprintf("%s; Discord Gateway permanently failed: %s; ordinary messages read every %s", chatAvailable, s.Reason, safety)
	}
	return fmt.Sprintf("%s; Discord Gateway reconnecting in %s (%s); ordinary messages read every %s meanwhile", chatAvailable, s.Retry.Round(time.Second), s.Reason, safety)
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
