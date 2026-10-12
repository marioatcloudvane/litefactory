package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

// What woke a heartbeat, in the words the heartbeat line uses.
const (
	wakeTimer   = "timer"
	wakeWebhook = "webhook"
)

type wakeSourceKey struct{}

// withWakeSource records on ctx what woke the heartbeat that is about to run.
// A [Daemon.Tick] called without it is a timer heartbeat.
func withWakeSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, wakeSourceKey{}, source)
}

func wakeSourceFrom(ctx context.Context) string {
	if s, ok := ctx.Value(wakeSourceKey{}).(string); ok && s != "" {
		return s
	}
	return wakeTimer
}

// heartbeat is what one [Daemon.Tick] learns about itself for the single line
// that describes it. It lives on the Tick goroutine and is never shared.
type heartbeat struct {
	source    string
	startedAt time.Time
	counts    *discoveryCounts
	// deferred: discovery was held back locally by the request budget.
	deferred bool
	// polled: how many per-repository listings finished.
	polled int
}

// heartbeatOutcome is how the heartbeat ended, for the line and the digest.
type heartbeatOutcome int

const (
	beatIdle heartbeatOutcome = iota
	beatDone
	beatHeldBack
	beatFailed
	beatStopped
)

// finishHeartbeat runs once after every heartbeat, whichever way it left: it
// settles the pause state, feeds the digest and writes the one DEBUG sentence.
func (d *Daemon) finishHeartbeat(ctx context.Context, hb *heartbeat, res TickResult, err error) {
	retryAt, backedOff := d.backoff.hold(hb.startedAt)
	outcome := beatDone
	switch {
	case err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)):
		outcome = beatStopped
	case err != nil && isRecoveryPending(err):
		outcome = beatHeldBack
	case err != nil:
		outcome = beatFailed
	case res.Skipped:
		outcome = beatHeldBack
	case hb.deferred:
		outcome = beatHeldBack
	case res.Dispatched == 0 && res.InFlight == 0:
		outcome = beatIdle
	}

	switch {
	case backedOff:
		d.pause.enter(ctx, d.log, hb.startedAt, pauseReasonBackoff, retryAt)
	case hb.deferred && d.connections == nil:
		reason, at := d.discovery.view().deferral()
		d.pause.enter(ctx, d.log, hb.startedAt, reason, at)
	case hb.polled > 0 && err == nil:
		d.pause.leave(ctx, d.log, hb.startedAt)
	}

	asked := hb.counts.total()
	d.digest.record(hb, res, outcome, asked)
	d.logHeartbeat(ctx, hb, res, err, outcome, retryAt, asked)
	d.digest.flush(ctx, d.log, hb.startedAt, d.discovery.view(), d.pause.snapshot(), d.inflight.size())
}

func (d *Daemon) logHeartbeat(ctx context.Context, hb *heartbeat, res TickResult, err error, outcome heartbeatOutcome, retryAt time.Time, asked int) {
	if !d.log.Enabled(ctx, slog.LevelDebug) {
		return
	}
	wake := "timer"
	if hb.source == wakeWebhook {
		wake = "a webhook delivery"
	}
	asking := "GitHub was not asked"
	switch {
	case asked == 1:
		asking = "GitHub was asked once"
	case asked > 1:
		asking = fmt.Sprintf("GitHub was asked %d times", asked)
	}
	var what string
	switch outcome {
	case beatFailed:
		what = fmt.Sprintf("failed (%v)", err)
	case beatStopped:
		what = "stopped because the daemon is shutting down"
	case beatHeldBack:
		switch {
		case res.Skipped && !retryAt.IsZero():
			what = "held back by a rate-limit backoff until " + retryAt.UTC().Format(time.RFC3339)
		case hb.deferred:
			reason, _ := d.discovery.view().deferral()
			what = "held back, discovery deferred: " + reason
		default:
			what = "held back, a recovery is still pending"
		}
	case beatIdle:
		what = "idle, nothing to dispatch"
	default:
		what = fmt.Sprintf("completed, dispatched %d, %d running", res.Dispatched, res.InFlight)
	}
	d.log.DebugContext(ctx, fmt.Sprintf("heartbeat woken by %s: %s; %s", wake, what, asking),
		slog.String("wake_source", hb.source),
		slog.Bool("github_asked", asked > 0),
		slog.Int("github_requests", asked))
}

// pauseState is discovery's in-memory pause state: the daemon's own record of
// whether it is reading comments. Transitions log exactly once; the health
// publisher reads it back for `daemon status`.
type pauseState struct {
	mu       sync.Mutex
	paused   bool
	since    time.Time
	reason   string
	resumeAt time.Time
}

// pauseSnapshot is a copy of [pauseState] for readers.
type pauseSnapshot struct {
	Paused   bool
	Since    time.Time
	Reason   string
	ResumeAt time.Time
}

func (p *pauseState) snapshot() pauseSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return pauseSnapshot{Paused: p.paused, Since: p.since, Reason: p.reason, ResumeAt: p.resumeAt}
}

// enter records the pause. Only the transition into it logs; a pause that is
// already standing just keeps its latest reason and expected end.
func (p *pauseState) enter(ctx context.Context, log *slog.Logger, now time.Time, reason string, resumeAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paused {
		p.reason, p.resumeAt = reason, resumeAt
		return
	}
	p.paused, p.since, p.reason, p.resumeAt = true, now, reason, resumeAt
	expected := "when fresh rate-limit data arrives"
	if !resumeAt.IsZero() {
		expected = resumeAt.UTC().Format(time.RFC3339)
	}
	log.WarnContext(ctx, fmt.Sprintf("discovery paused: %s; owner comments are not being read; expected to resume %s", reason, expected),
		slog.String("reason", reason),
		slog.String("expected_resume", expected))
}

// leave records the end of a pause, once.
func (p *pauseState) leave(ctx context.Context, log *slog.Logger, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.paused {
		return
	}
	held := now.Sub(p.since).Round(time.Second)
	p.paused, p.reason, p.resumeAt, p.since = false, "", time.Time{}, time.Time{}
	log.InfoContext(ctx, fmt.Sprintf("discovery resumed after %s", held),
		slog.Int64("paused_for_seconds", int64(held.Seconds())))
}

// digestWindow is how often the digest is logged.
const digestWindow = 10 * time.Minute

// digest accumulates, in memory only, what the daemon did over the current
// window and logs it as one INFO line when the window ends. A restart starts
// a new window; nothing is persisted.
type digest struct {
	mu          sync.Mutex
	started     bool
	windowStart time.Time

	heartbeats, completed, webhookWakes, heldBack, failed int
	polls, newComments, dispatched                        int

	requests, ok, notModified, errs int
}

// observe counts every forge request the daemon makes, for the request summary.
func (g *digest) observe(o github.RequestObservation) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests++
	switch o.StatusCode {
	case http.StatusOK:
		g.ok++
	case http.StatusNotModified:
		g.notModified++
	default:
		g.errs++
	}
}

func (g *digest) newComment() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.newComments++
}

func (g *digest) record(hb *heartbeat, res TickResult, outcome heartbeatOutcome, _ int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.started {
		g.started, g.windowStart = true, hb.startedAt
	}
	g.heartbeats++
	if hb.source == wakeWebhook {
		g.webhookWakes++
	}
	g.polls += hb.polled
	switch outcome {
	case beatFailed:
		g.failed++
	case beatHeldBack:
		g.heldBack++
	case beatStopped:
	default:
		g.completed++
		g.dispatched += res.Dispatched
	}
}

// flush logs and resets the window once it is [digestWindow] old. It runs on
// the heartbeat's own clock, so there is no timer or goroutine to stop.
func (g *digest) flush(ctx context.Context, log *slog.Logger, now time.Time, v discoveryView, pause pauseSnapshot, inFlight int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.started || now.Sub(g.windowStart) < digestWindow {
		return
	}
	status, reason := "healthy", ""
	switch {
	case pause.Paused:
		status, reason = "unhealthy", "discovery is paused: "+pause.Reason
	case g.failed > 0:
		status, reason = "unhealthy", fmt.Sprintf("%d heartbeat(s) failed", g.failed)
	case g.completed == 0:
		status, reason = "unhealthy", "no heartbeat completed in this window"
	}
	resets := "unknown"
	if !v.Reset.IsZero() {
		resets = v.Reset.UTC().Format(time.RFC3339)
	}
	msg := "daemon digest, last 10 minutes: " + status
	if reason != "" {
		msg += " (" + reason + ")"
	}
	log.InfoContext(ctx, msg,
		slog.String("status", status),
		slog.String("reason", reason),
		slog.Int("heartbeats", g.heartbeats),
		slog.Int("heartbeats_completed", g.completed),
		slog.Int("heartbeats_webhook_wakes", g.webhookWakes),
		slog.Int("heartbeats_held_back", g.heldBack),
		slog.Int("heartbeats_failed", g.failed),
		slog.Int("github_polls", g.polls),
		slog.Int("new_comments", g.newComments),
		slog.Int("runs_dispatched", g.dispatched),
		slog.Int("runs_in_flight", inFlight),
		slog.Int("requests", g.requests),
		slog.Int("requests_ok", g.ok),
		slog.Int("requests_not_modified", g.notModified),
		slog.Int("requests_errors", g.errs),
		slog.Int("quota_left", v.Remaining),
		slog.Int("quota_limit", v.Limit),
		slog.String("quota_resets_at", resets))
	g.windowStart = now
	g.heartbeats, g.completed, g.webhookWakes, g.heldBack, g.failed = 0, 0, 0, 0, 0
	g.polls, g.newComments, g.dispatched = 0, 0, 0
	g.requests, g.ok, g.notModified, g.errs = 0, 0, 0, 0
}

// commentFates remembers the last fate logged for each owner comment so a
// comment that is seen again on a later heartbeat is not announced again. It
// is bounded: the oldest entries are forgotten first, and a comment that old
// has long since been consumed.
type commentFates struct {
	mu    sync.Mutex
	last  map[commentKey]string
	order []commentKey
}

type commentKey struct{ session, comment int64 }

const commentFateMemory = 8192

// Fates of an owner comment.
const (
	fateDispatched = "dispatched"
	fateQueued     = "queued"
	fateIgnored    = "ignored"
	fateDeferred   = "deferred"
)

// note reports whether fate is news for the comment, and remembers it. A
// deferral is news only for a comment with no fate yet: a comment that was
// dispatched or queued is not "deferred" by a later hiccup.
func (f *commentFates) note(session, comment int64, fate string) (announce, first bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := commentKey{session, comment}
	last, seen := f.last[key]
	if seen && (last == fate || fate == fateDeferred) {
		return false, false
	}
	if f.last == nil {
		f.last = make(map[commentKey]string)
	}
	if !seen {
		f.order = append(f.order, key)
		if len(f.order) > commentFateMemory {
			delete(f.last, f.order[0])
			f.order = f.order[1:]
		}
	}
	f.last[key] = fate
	return true, !seen
}

// commentFate logs, once per change, what became of an owner comment: the
// issue, the comment and the fate. It never logs for the issue body (id 0).
func (d *Daemon) commentFate(ctx context.Context, t *task, commentID int64, fate, detail string) {
	if commentID == 0 {
		return
	}
	announce, first := d.fates.note(t.session.ID, commentID, fate)
	if !announce {
		return
	}
	if first {
		d.digest.newComment()
	}
	t.log.InfoContext(ctx, fmt.Sprintf("owner comment %d on %s#%d: %s", commentID, t.forgeRepo, t.issue.Number, detail),
		slog.Int("issue", t.issue.Number),
		slog.Int64("comment_id", commentID),
		slog.String("fate", fate))
}
