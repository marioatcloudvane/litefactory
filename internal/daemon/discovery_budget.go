package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

// ErrDiscoveryDeferred is local scheduling pressure, not a remote refusal.
var ErrDiscoveryDeferred = errors.New("background discovery deferred")

const (
	discoveryBurst      = 400
	discoveryListBound  = 100
	discoveryQueueBound = 4096
	// discoveryQuotaDivisor sets background discovery's share of the forge
	// rate limit: it may spend limit/discoveryQuotaDivisor requests per reset
	// window, and may never take remaining below the rest, which stays
	// reserved for execution and reporting.
	//
	// The floor and the spend cap are two statements of the same share and
	// must move together. At the original 5 they left discovery 1000 of 5000
	// requests an hour, which a one-minute tick over eighteen managed
	// features exhausts well inside the window — and a listing costs
	// discoveryListBound, so discovery stops a further 100 short of the floor
	// rather than at it.
	discoveryQuotaDivisor = 2
)

type discoveryWaiter struct {
	key  string
	cost int
	seen time.Time
}
type discoveryBudget struct {
	mu                                        sync.Mutex
	now, refillAt, observedAt, reset, probeAt time.Time
	tokens                                    float64
	initialized                               bool
	limit, remaining, spent, reserved         int
	waiters                                   []discoveryWaiter
	deferrals                                 int
	lists                                     chan struct{}
}

type discoveryReservation struct {
	budget                   *discoveryBudget
	bound, requests, charged int
	reset                    time.Time
	probe                    bool
}

func (b *discoveryBudget) tick(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.initialized {
		b.tokens = discoveryBurst
		b.initialized = true
		b.refillAt = now
	}
	if now.After(b.refillAt) {
		b.tokens = min(discoveryBurst, b.tokens+now.Sub(b.refillAt).Seconds()*2)
		b.refillAt = now
	}
	if now.After(b.now) {
		b.now = now
	}
	// A disappeared operation cannot hold the queue indefinitely. Returning
	// operations renew their place; storage and user content never enter keys.
	live := b.waiters[:0]
	for _, w := range b.waiters {
		if b.now.Sub(w.seen) <= 2*time.Minute {
			live = append(live, w)
		}
	}
	b.waiters = live
}

func (b *discoveryBudget) freshLocked() bool {
	return b.limit > 0 && b.now.Before(b.reset) && b.now.Sub(b.observedAt) < 2*time.Minute
}

func (b *discoveryBudget) observe(o github.RequestObservation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observeLocked(o)
}
func (b *discoveryBudget) observeLocked(o github.RequestObservation) {
	if b.limit > 0 && (o.StatusCode != http.StatusNotModified || !o.Authenticated) {
		b.remaining = max(0, b.remaining-1)
	}
	if !o.CoreRateValid || !o.Reset.After(b.now) || o.Limit <= 0 || o.Remaining < 0 || o.Remaining > o.Limit {
		return
	}
	switch {
	case b.reset.IsZero() || o.Reset.After(b.reset):
		b.limit, b.remaining, b.reset, b.spent = o.Limit, o.Remaining, o.Reset, 0
	case o.Reset.Equal(b.reset):
		b.limit = min(b.limit, o.Limit)
		b.remaining = min(b.remaining, o.Remaining)
	default:
		return
	}
	b.observedAt = b.now
}

func (b *discoveryBudget) acquire(key string, cost int, probe bool) (*discoveryReservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	deny := func() (*discoveryReservation, error) {
		b.deferrals++
		if !probe {
			for i := range b.waiters {
				if b.waiters[i].key == key {
					b.waiters[i].seen = b.now
					return nil, ErrDiscoveryDeferred
				}
			}
			if len(b.waiters) < discoveryQueueBound {
				b.waiters = append(b.waiters, discoveryWaiter{key, cost, b.now})
			}
		}
		return nil, ErrDiscoveryDeferred
	}
	if probe {
		if b.freshLocked() || cost != 1 || (!b.probeAt.IsZero() && b.now.Sub(b.probeAt) < time.Minute) {
			return deny()
		}
		// An observed depleted allowance is not permission to probe before reset.
		if b.limit > 0 && b.now.Before(b.reset) && (b.remaining <= b.floorLocked() || b.spent >= b.discoveryShareLocked()) {
			return deny()
		}
	} else {
		if !b.freshLocked() {
			return deny()
		}
		if len(b.waiters) > 0 && b.waiters[0].key != key && !discoveryPrerequisite(key, b.waiters[0].key) {
			// Materializing a deferred parent's task can require an unrelated
			// scalar identity read. Permit it only while preserving the head
			// operation's entire traffic and primary reservation.
			head := b.waiters[0].cost
			if cost != 1 || b.tokens-float64(cost) < float64(head) || b.remaining-b.reserved-cost-head < b.floorLocked() || b.spent+b.reserved+cost+head > b.discoveryShareLocked() {
				return deny()
			}
		}
		if b.remaining-b.reserved-cost < b.floorLocked() || b.spent+b.reserved+cost > b.discoveryShareLocked() {
			return deny()
		}
	}
	if b.tokens < float64(cost) {
		return deny()
	}
	b.tokens -= float64(cost)
	b.reserved += cost
	if probe {
		b.probeAt = b.now
	}
	for i := range b.waiters {
		if b.waiters[i].key == key {
			b.waiters = append(b.waiters[:i], b.waiters[i+1:]...)
			break
		}
	}
	return &discoveryReservation{budget: b, bound: cost, reset: b.reset, probe: probe}, nil
}
func (b *discoveryBudget) floorLocked() int { return b.limit - b.discoveryShareLocked() }

// discoveryShareLocked is how many requests of the current window background
// discovery may spend, the complement of [discoveryBudget.floorLocked].
func (b *discoveryBudget) discoveryShareLocked() int { return b.limit / discoveryQuotaDivisor }

func (r *discoveryReservation) observe(o github.RequestObservation) {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	r.requests++
	if o.StatusCode != http.StatusNotModified || !o.Authenticated {
		r.charged++
	}
}
func (r *discoveryReservation) finish() {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved -= r.bound
	b.tokens = min(discoveryBurst, b.tokens+float64(max(0, r.bound-r.requests)))
	// Requests outstanding across a reset still consume the new window unless
	// their fresh response establishes otherwise. Never refund observed quota.
	b.spent += r.charged
	if r.requests > r.bound {
		b.tokens -= float64(r.requests - r.bound)
	}
}

type discoveryEndpointCounts struct {
	OK        int `json:"status_200"`
	Unchanged int `json:"status_304"`
	Other     int `json:"other_error"`
	Pages     int `json:"pages"`
}
type discoveryCounts struct {
	mu        sync.Mutex
	endpoints map[string]discoveryEndpointCounts
}

func (s *discoveryCounts) observe(o github.RequestObservation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.endpoints == nil {
		s.endpoints = make(map[string]discoveryEndpointCounts)
	}
	c := s.endpoints[o.Endpoint]
	switch o.StatusCode {
	case http.StatusOK:
		c.OK++
	case http.StatusNotModified:
		c.Unchanged++
	default:
		c.Other++
	}
	if o.Page {
		c.Pages++
	}
	s.endpoints[o.Endpoint] = c
}
func (d *Daemon) discoveryContext(ctx context.Context) context.Context {
	return github.WithRequestObserver(ctx, d.discovery.observe)
}
func (d *Daemon) discoverySummary(ctx context.Context, scope string, s *discoveryCounts) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.discovery.mu.Lock()
	defer d.discovery.mu.Unlock()
	d.log.InfoContext(ctx, "discovery request summary", slog.String("scope", scope), slog.Any("endpoints", s.endpoints), slog.Int("local_deferrals", d.discovery.deferrals), slog.Int("quota_remaining", d.discovery.remaining), slog.Int("quota_reserve", d.discovery.floorLocked()), slog.Int("background_spent", d.discovery.spent), slog.Int("outstanding_reserved", d.discovery.reserved), slog.Float64("traffic_available", d.discovery.tokens), slog.Bool("quota_fresh", d.discovery.freshLocked()))
}

func discoveryCall[T any](ctx context.Context, d *Daemon, key string, cost int, call func(context.Context) (T, error)) (T, error) {
	if cost == discoveryListBound {
		// A list reserves the entire supported history before its first page.
		// Serialize these reservations until unused pages are returned, rather
		// than deferring another worker solely because a short fetch is in
		// flight. This waits only for existing HTTP work, never for a quota
		// refill; execution and reporting do not enter this discovery gate.
		d.discovery.mu.Lock()
		if d.discovery.lists == nil {
			d.discovery.lists = make(chan struct{}, 1)
		}
		lists := d.discovery.lists
		d.discovery.mu.Unlock()
		select {
		case lists <- struct{}{}:
			defer func() { <-lists }()
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
	r, err := d.discovery.acquire(key, cost, false)
	if err != nil {
		var zero T
		return zero, err
	}
	defer r.finish()
	return call(github.WithRequestObserver(ctx, r.observe))
}
func discoveryKey(repo github.Repo, operation string, number int) string {
	return fmt.Sprintf("%s/%s/%d", repo, operation, number)
}
func (d *Daemon) discoveryIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	return discoveryCall(ctx, d, discoveryKey(repo, "issue", number), 1, func(ctx context.Context) (github.Issue, error) { return d.forge.GetIssue(ctx, repo, number) })
}
func (d *Daemon) discoveryComments(ctx context.Context, repo github.Repo, number int, after int64) ([]github.Comment, error) {
	return discoveryCall(ctx, d, discoveryKey(repo, "comments", number), discoveryListBound, func(ctx context.Context) ([]github.Comment, error) {
		return d.forge.ListCommentsAfter(ctx, repo, number, after)
	})
}
func (d *Daemon) discoveryProbe(ctx context.Context, repo github.Repo) error {
	r, err := d.discovery.acquire("probe", 1, true)
	if err != nil {
		return nil
	}
	defer r.finish()
	_, err = d.forge.GetIssue(github.WithRequestObserver(github.WithoutConditionalCache(ctx), r.observe), repo, 1)
	if errors.Is(err, github.ErrNotFound) || errors.Is(err, github.ErrInvalidArgument) {
		return nil
	}
	return err
}

// Scalar revalidation is a prerequisite of a queued history fetch. Repository
// comment listings are prerequisites when materializing a newly seen number.
func discoveryPrerequisite(key, waiting string) bool {
	k, w := strings.Split(key, "/"), strings.Split(waiting, "/")
	if len(k) != 4 || len(w) != 4 || k[0] != w[0] || k[1] != w[1] {
		return false
	}
	if k[3] == w[3] && k[3] != "0" && (k[2] == "issue" || k[2] == "parent") {
		return true
	}
	return (w[2] == "issue") && (k[2] == "issues" || k[2] == "commented")
}
