package daemon_test

import (
	"context"
	"github.com/cloudvane/litefactory/internal/testutil"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The three issues this file dispatches, in the order their sessions are
// created and therefore in the order the per-repo session rotation offers them.
const (
	firstIssue  = testIssue // dispatched, and the worker that meets the 429
	gatedIssue  = 124       // offered next, and held at the forge until it has
	thirdIssue  = 125       // never offered a slot at all
	limitDelay  = 90 * time.Second
	pastListing = 10 * time.Minute
)

// TestABackoffArmedMidTickStopsFurtherDispatch is AC-PAR-4.4's hard half: the
// hold that arms *while the dispatch loop is still running*.
//
// The tick passes the check at the top of [daemon.Daemon.Tick] because nothing
// is backing off when it begins. It dispatches one turn, and that turn's worker
// meets a 429 and arms the backoff underneath the loop that started it. From
// that instant no further run may start, for any repository — which is what the
// second check, immediately before the `go` in Daemon.offer, is there for.
//
// # The rendezvous, and why it is not a sleep
//
// Two seams make the interleaving exact rather than likely, and neither of them
// waits on a clock.
//
//  1. armingHandler watches the daemon's own log. [daemon.Daemon] arms the hold
//     and then, on the same goroutine and with nothing in between, logs
//     "backing off". Closing a channel when that line is handled is therefore a
//     signal that means "the hold now stands", taken from the one place that
//     knows.
//  2. gatedForge blocks the *tick* goroutine inside the GetIssue that
//     Daemon.materialise makes for the second candidate, until that channel
//     closes. Only a session reached through the backlog needs that read, which
//     is why the second and third issues are pushed out of the listing window
//     and flagged by hand: it puts a controllable pause in the dispatch path
//     between the first `go` and the decision about the next candidate.
//
// So by the time the dispatch loop decides about the gated candidate, the hold
// provably stands. There is no sleep, no polling loop and no assumption about
// which goroutine is faster: if the worker were somehow to arm the hold before
// the gated candidate is even reached, the check refuses it there and GetIssue
// is never called — the assertions below hold either way.
func TestABackoffArmedMidTickStopsFurtherDispatch(t *testing.T) {
	f := newFixture(t)

	armed := newArmedSignal()
	f.daemon = f.newDaemon(daemon.Options{
		// Three, so that a refused dispatch is unambiguously the backoff and
		// never the pool: every candidate in this test could have had a slot.
		MaxConcurrentRuns: 3,
		Forge:             gatedForge{labelFixtureForge: f.client, number: gatedIssue, until: armed.ch, t: t},
		Logger:            slog.New(newArmingHandler(f.logger.Handler(), armed)),
	})

	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.forge.addIssue(fakeIssue{Number: gatedIssue, Title: "Second task", User: stranger()})
	f.forge.addIssue(fakeIssue{Number: thirdIssue, Title: "Third task", User: stranger()})
	// Creates the three sessions, in issue order, and bootstraps each watermark.
	f.settle()

	// Push the two issues with nothing new on them out of the listing window, so
	// that they can only be reached through the backlog — which is what makes
	// Daemon.materialise read them, and gatedForge able to hold one.
	//
	// It takes an advance *and* a tick, because the window is anchored to the
	// stored poll cursor and not to the clock: a tick asks for everything
	// updated since the previous tick began, less [daemon.ListingOverlap].
	// Advancing alone moves only the next tick's start; the tick after it is
	// the first whose window has moved with it.
	f.clock.advance(pastListing)
	f.settle()

	// The first issue's comment lands after all that, so it is the one thing
	// the listings still return.
	f.forge.addComment(firstIssue, 71, owner(), "/spike the one that meets the limit")
	f.needsPoll(gatedIssue)
	f.needsPoll(thirdIssue)

	// The turn that meets the forge over quota. It is the executor rather than
	// the HTTP fake because what has to be exact here is *which* turn refuses:
	// this is the only run the tick gets to start.
	f.turns.set(func(context.Context, turn.Request) (turn.Result, error) {
		return turn.Result{}, &github.RateLimitError{
			APIError: github.APIError{
				Method:     http.MethodGet,
				URL:        "https://api.github.com/repos/acme/acme-api/issues/123/comments",
				StatusCode: http.StatusForbidden,
				Message:    "API rate limit exceeded",
			},
			RetryAfter: limitDelay,
			Limit:      5000,
			Resource:   "core",
		}
	})

	result := f.settle()

	if result.Dispatched != 1 {
		t.Fatalf("dispatched %d turns, want 1: no run may start once the backoff is armed\nlogs:\n%s",
			result.Dispatched, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{71}) {
		t.Errorf("turns ran for %v, want [71]", got)
	}

	// The two that were not run are accounted for the way a pool-full tick
	// accounts for them, and by the same path: counted as deferred, and left
	// carrying needs_poll so the next tick re-finds them. A worker had it run
	// for either of them would have found nothing above its watermark and
	// cleared the flag, so a set flag is also the assertion that no `go`
	// happened.
	if result.Deferred != 2 {
		t.Errorf("deferred %d sessions, want 2 — the gated one and the one behind it\nlogs:\n%s",
			result.Deferred, f.logs)
	}
	for _, issue := range []int{gatedIssue, thirdIssue} {
		if !f.sessionOf(issue).NeedsPoll {
			t.Errorf("needs_poll is clear on issue %d; a session the backoff stopped being "+
				"dispatched must be re-found by the next tick", issue)
		}
	}

	// A tick stopped mid-dispatch is not a skipped tick: it polled, discovered
	// and started a run. Skipped is reserved for a tick that did nothing at all.
	if result.Skipped {
		t.Error("a tick that dispatched a turn reported itself as skipped")
	}
	if !result.RetryAt.IsZero() {
		t.Errorf("RetryAt is %s on a tick that was not skipped", result.RetryAt)
	}
	if !strings.Contains(f.logs.String(), "armed the backoff while this tick was still dispatching") {
		t.Errorf("the mid-tick hold was not logged\nlogs:\n%s", f.logs)
	}

	// And the hold really is standing afterwards, so the *next* tick is the
	// ordinary skipped one — the two checks are one backoff, not two.
	skipped := f.settle()
	if !skipped.Skipped {
		t.Errorf("the tick after a mid-tick rate limit was not skipped\nlogs:\n%s", f.logs)
	}
}

// needsPoll flags a session the way a deferred turn does, so that discovery
// reaches it through the backlog rather than through a listing.
func (f *fixture) needsPoll(issue int) {
	f.t.Helper()

	if err := f.db.SetSessionNeedsPoll(f.t.Context(), f.sessionOf(issue).ID); err != nil {
		f.t.Fatalf("flag issue %d as needing a poll: %v", issue, err)
	}
}

// armedSignal is a channel closed exactly once, however many handlers hold it.
//
// slog.Handler.WithAttrs returns a new handler, and the daemon derives a child
// logger per repository and per task — so the handler that finally sees the
// backoff line is not the one this test constructed. The close has to be
// idempotent across all of them, which is what a shared once buys.
type armedSignal struct {
	once sync.Once
	ch   chan struct{}
}

func newArmedSignal() *armedSignal { return &armedSignal{ch: make(chan struct{})} }

func (s *armedSignal) fire() { s.once.Do(func() { close(s.ch) }) }

// armingHandler closes a signal when the daemon logs that it has armed the
// rate-limit backoff.
//
// [daemon.Daemon] calls backoff.fail and then logs, on one goroutine with
// nothing in between, so this line is a report of a hold that already stands.
// That is the whole rendezvous: a test waiting on the signal is waiting for a
// state the daemon has already reached, not guessing at when it might.
//
// WithAttrs and WithGroup are implemented rather than promoted because the
// daemon logs this through a child logger — every derivation has to keep the
// wrapper, or the one line this exists to see would go straight to the
// underlying handler.
type armingHandler struct {
	slog.Handler
	armed *armedSignal
}

// armedLine is the substring identifying the log line the daemon writes
// immediately after arming the hold. It is deliberately the distinctive middle
// of that message rather than the whole of it.
const armedLine = "backing off"

func newArmingHandler(inner slog.Handler, armed *armedSignal) *armingHandler {
	return &armingHandler{Handler: inner, armed: armed}
}

func (h *armingHandler) Handle(ctx context.Context, record slog.Record) error {
	if strings.Contains(record.Message, armedLine) {
		h.armed.fire()
	}
	return h.Handler.Handle(ctx, record)
}

func (h *armingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &armingHandler{Handler: h.Handler.WithAttrs(attrs), armed: h.armed}
}

func (h *armingHandler) WithGroup(name string) slog.Handler {
	return &armingHandler{Handler: h.Handler.WithGroup(name), armed: h.armed}
}

// gatedForge is the real client with one issue read held open until a signal
// closes.
//
// It is the pause the test needs inside the tick goroutine, and it is placed on
// GetIssue because that is the one forge call [daemon.Daemon] makes *between*
// dispatching one candidate and deciding about the next: the read that fills in
// a session discovery reached only through the backlog.
//
// The context is honoured rather than ignored, so a gate that is never opened
// unblocks when the test's own context is cancelled instead of parking a
// goroutine for the life of the run. The timeout is a failure guard only: the
// channel still establishes the interleaving, and a missing signal fails with
// the rendezvous named rather than hanging the entire package.
type gatedForge struct {
	labelFixtureForge
	number int
	until  <-chan struct{}
	t      *testing.T
}

func (g gatedForge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	if number == g.number {
		wait, cancel := context.WithTimeout(ctx, testutil.Wait)
		defer cancel()
		select {
		case <-g.until:
		case <-wait.Done():
			g.t.Errorf("waiting for backoff-armed signal before reading issue %d: %v", number, wait.Err())
			return github.Issue{}, wait.Err()
		}
	}
	return g.labelFixtureForge.GetIssue(ctx, repo, number)
}
