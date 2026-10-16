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
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// ErrDiscoveryDeferred is local scheduling pressure, not a remote refusal.
var ErrDiscoveryDeferred = errors.New("background discovery deferred")

const (
	discoveryBurst      = 400
	discoveryListBound  = 100
	discoveryQueueBound = 4096
	// A Linear daemon history also selects its container twice, resolves the
	// issue, and may initialize a cold publisher client.
	linearHistoryBound = discoveryListBound + 4
	// Admission and materialisation each need at most five prerequisite reads.
	linearPendingBound = 5 + 5 + 2*linearHistoryBound
	// Background discovery may spend everything above the execution reserve
	// (see discoveryReserveDivisor); there is no separate spending cap.
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
	fallbackAt                                time.Time
	fallbackRecovery, fallbackPending         bool
	fallbackWorker                            bool
	// linear marks a budget fed by Linear observations. Linear's allowance
	// refills continuously while its reset header points about an hour ahead,
	// so a stale depleted record understates real capacity (#912).
	linear    bool
	deferrals int
	lists     chan struct{}
}

type discoveryReservation struct {
	budget                   *discoveryBudget
	bound, requests, charged int
	reset                    time.Time
	probe                    bool
	fallback                 bool
	pending, finished        bool
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
	b.observeRequestLocked(o.CoreRateValid, o.Limit, o.Remaining, o.Reset, o.StatusCode != http.StatusNotModified || !o.Authenticated)
}

func (b *discoveryBudget) observeLinear(o linear.RequestObservation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.linear = true
	b.observeRequestLocked(o.RequestRateValid, o.Limit, o.Remaining, o.Reset, true)
	if b.freshLocked() {
		// Fallback lanes no longer run with fresh evidence. Their rotation
		// entries must not block ordinary discovery behind an inactive lane.
		live := b.waiters[:0]
		for _, w := range b.waiters {
			if !strings.HasPrefix(w.key, "fallback-") {
				live = append(live, w)
			}
		}
		b.waiters = live
	}
}

func (b *discoveryBudget) observeRequestLocked(valid bool, limit, remaining int, reset time.Time, charged bool) {
	if b.limit > 0 && charged {
		b.remaining = max(0, b.remaining-1)
	}
	if !valid || !reset.After(b.now) || limit <= 0 || remaining < 0 || remaining > limit {
		return
	}
	switch {
	case b.reset.IsZero() || reset.After(b.reset):
		b.limit, b.remaining, b.reset, b.spent = limit, remaining, reset, 0
	case reset.Equal(b.reset):
		b.limit = min(b.limit, limit)
		b.remaining = min(b.remaining, remaining)
	default:
		return
	}
	b.observedAt = b.now
}

// listAdmission is what a history list must find above the reserve to start:
// its first page plus the container/issue/publisher prerequisites. The list's
// full bound (discoveryListBound/linearHistoryBound) still caps what it may
// spend, but requiring that whole worst case up front kept every Linear
// comment read waiting at ~350 of 2500 while a real read costs 1-2 requests;
// a rare long history may dip into the reserve instead.
const listAdmission = 5

func admissionCost(cost int) int {
	if cost >= discoveryListBound {
		return listAdmission
	}
	return cost
}

// affords reports, without reserving or queueing, whether an operation of cost
// requests fits the known window's reserve and discovery share and the token
// bucket. It mirrors acquire's ordinary (non-probe) admission.
func (b *discoveryBudget) affords(cost int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit > 0 && b.now.Before(b.reset) {
		need := admissionCost(cost)
		if b.remaining-b.reserved-need < b.floorLocked() || b.spent+b.reserved+need > b.discoveryShareLocked() {
			return false
		}
	}
	return b.tokens >= float64(cost)
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
		// An observed depleted allowance is not permission to probe before reset,
		// except for Linear: its allowance refills continuously, and only a fresh
		// response can show that capacity has returned (#912).
		if !b.linear && b.limit > 0 && b.now.Before(b.reset) && (b.remaining <= b.floorLocked() || b.spent >= b.discoveryShareLocked()) {
			return deny()
		}
	} else if b.limit > 0 && b.now.Before(b.reset) {
		// Within a known window the reserve and the discovery share apply,
		// fresh or not. Without usable quota data (none observed yet, or the
		// window has passed) only the token bucket bounds discovery: requiring
		// fresh evidence here starved every read whose budget never receives
		// observations (#793). Real refusals still arm the backoff.
		if len(b.waiters) > 0 && b.waiters[0].key != key && !discoveryPrerequisite(key, b.waiters[0].key) {
			// Materializing a deferred parent's task can require an unrelated
			// scalar identity read. Permit it only while preserving the head
			// operation's entire traffic and primary reservation.
			head := b.waiters[0].cost
			need := admissionCost(head)
			if cost != 1 || b.tokens-float64(cost) < float64(head) || b.remaining-b.reserved-cost-need < b.floorLocked() || b.spent+b.reserved+cost+need > b.discoveryShareLocked() {
				return deny()
			}
		}
		need := admissionCost(cost)
		if b.remaining-b.reserved-need < b.floorLocked() || b.spent+b.reserved+need > b.discoveryShareLocked() {
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

// A fallback pass reserves the complete operation, including prerequisites and
// reply locator reads. The existing waiter queue rotates sources in each lane.
func (b *discoveryBudget) acquireFallback(key string, cost int, pending bool) (*discoveryReservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.freshLocked() {
		return nil, nil
	}
	present := false
	first := ""
	lane := "fallback-recovery/"
	if pending {
		lane = "fallback-pending/"
	}
	key = lane + key
	for _, w := range b.waiters {
		if strings.HasPrefix(w.key, lane) && first == "" {
			first = w.key
		}
		if w.key == key {
			present = true
		}
	}
	if !present && len(b.waiters) < discoveryQueueBound {
		b.waiters = append(b.waiters, discoveryWaiter{key, cost, b.now})
	}
	if need := admissionCost(cost); b.limit > 0 && b.now.Before(b.reset) && (b.remaining-b.reserved-need < b.floorLocked() || b.spent+b.reserved+need > b.discoveryShareLocked()) {
		return nil, ErrDiscoveryDeferred
	}
	if b.tokens < float64(cost) || b.reserved+cost > discoveryBurst || (pending && b.fallbackWorker) || (first != "" && first != key) {
		return nil, ErrDiscoveryDeferred
	}
	if b.fallbackAt.IsZero() || !b.now.Before(b.fallbackAt.Add(time.Minute)) {
		b.fallbackAt = b.now
		b.fallbackRecovery, b.fallbackPending = false, false
	}
	if (pending && b.fallbackPending) || (!pending && b.fallbackRecovery) {
		return nil, ErrDiscoveryDeferred
	}
	if pending {
		b.fallbackPending = true
		b.fallbackWorker = true
	} else {
		b.fallbackRecovery = true
	}
	for i, w := range b.waiters {
		if w.key == key {
			b.waiters = append(b.waiters[:i], b.waiters[i+1:]...)
			break
		}
	}
	b.waiters = append(b.waiters, discoveryWaiter{key, cost, b.now})
	b.tokens -= float64(cost)
	b.reserved += cost
	return &discoveryReservation{budget: b, bound: cost, reset: b.reset, fallback: true, pending: pending}, nil
}

func (d *Daemon) linearFallback(ctx context.Context, v store.ConnectionVersion, pending bool) (context.Context, func(), error) {
	if v.Provider != ticket.Linear {
		return ctx, func() {}, nil
	}
	if err := d.discoveryProbe(ctx, githubRepo(v.Destination)); err != nil {
		return ctx, nil, err
	}
	b, err := d.quotaBudget(ctx)
	if err != nil {
		return ctx, nil, err
	}
	// Recovery: cold publisher, container, three pages, up to one page of
	// reply locators. Admission: cold publisher, container, reread, and the
	// admission's own ticket/container checks, then bootstrap and drain history.
	// Code reads keep their own scope.
	cost := 5 + recoveryPageSize
	if pending {
		cost = linearPendingBound
	}
	r, err := b.acquireFallback(fmt.Sprint(v.ID), cost, pending)
	if err != nil {
		return ctx, nil, err
	}
	if r == nil {
		return ctx, func() {}, nil
	}
	return context.WithValue(ctx, quotaReservationKey{}, r), r.finish, nil
}

// discoveryReserveDivisor sets how much of the window stays reserved for
// execution and reporting: background discovery may not take remaining below
// limit/discoveryReserveDivisor. Tying the reserve to the discovery share
// (half the window) starved every owner comment once runs had used half of
// the hourly allowance, until the reset (#793).
const discoveryReserveDivisor = 10

func (b *discoveryBudget) floorLocked() int { return b.limit / discoveryReserveDivisor }

// discoveryShareLocked is how many requests of the current window background
// discovery may spend, the complement of [discoveryBudget.floorLocked].
func (b *discoveryBudget) discoveryShareLocked() int {
	// Discovery may spend everything above the execution reserve. A separate
	// half-window cap starved first sight of new issues and owner comments
	// once silent retry loops had spent it, although real quota remained.
	return b.limit - b.floorLocked()
}

func (r *discoveryReservation) observe(o github.RequestObservation) {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	r.requests++
	if o.StatusCode != http.StatusNotModified || !o.Authenticated {
		r.charged++
	}
}

func (r *discoveryReservation) observeLinear(_ linear.RequestObservation) {
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	r.requests++
	r.charged++
}
func (r *discoveryReservation) finish() {
	b := r.budget
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.finished {
		return
	}
	r.finished = true
	if r.pending {
		b.fallbackWorker = false
	}
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
	OK        int `json:"ok_responses"`
	Unchanged int `json:"not_modified_responses"`
	Other     int `json:"error_responses"`
	Pages     int `json:"pages_fetched"`
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
	if d.connections != nil {
		d.connections.SetRequestContext(d.bindQuotaRequest)
	}
	ctx = github.WithRequestObserver(ctx, d.digest.observe)
	if d.connections == nil {
		return github.WithRequestObserver(ctx, d.discovery.observe)
	}
	return ctx
}

// total is how many requests the counts have seen, whatever their outcome.
func (s *discoveryCounts) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.endpoints {
		n += c.OK + c.Unchanged + c.Other
	}
	return n
}

// discoverySummary logs the per-endpoint request accounting at DEBUG. It is for
// diagnosing the request budget, not for an operator reading what the daemon
// did: the heartbeat line and the digest say that.
func (d *Daemon) discoverySummary(ctx context.Context, scope string, s *discoveryCounts) {
	if !d.log.Enabled(ctx, slog.LevelDebug) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := d.discovery.view()
	d.log.DebugContext(ctx, "request accounting for this "+scope,
		slog.String("scope", scope),
		slog.Any("endpoints", s.endpoints),
		slog.Int("requests_held_back_locally", v.HeldBack),
		slog.Int("quota_remaining_requests", v.Remaining),
		slog.Int("quota_reserve_requests", v.Reserve),
		slog.Int("background_spent_requests", v.Spent),
		slog.Int("requests_in_progress", v.InProgress),
		slog.Float64("burst_allowance_requests", v.Burst),
		slog.Bool("quota_data_fresh", v.Fresh))
}

// discoveryView is a read-only copy of what the budget knows, taken under its
// lock. Nothing decided from it feeds back into admission.
type discoveryView struct {
	Fresh               bool
	Limit               int
	Remaining           int
	Reserve             int
	Share               int
	Spent               int
	InProgress          int
	HeldBack            int
	Burst               float64
	Reset               time.Time
	ProbeAt, FallbackAt time.Time
	now                 time.Time
}

func (b *discoveryBudget) view() discoveryView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return discoveryView{
		Fresh: b.freshLocked(), Limit: b.limit, Remaining: b.remaining, Reserve: b.floorLocked(),
		Share: b.discoveryShareLocked(), Spent: b.spent, InProgress: b.reserved, HeldBack: b.deferrals,
		Burst: b.tokens, Reset: b.reset, now: b.now, ProbeAt: b.probeAt, FallbackAt: b.fallbackAt,
	}
}

// Why discovery is deferred, in the words the log uses.
const (
	pauseReasonStale   = "stale rate-limit data"
	pauseReasonSpent   = "spent allowance until reset"
	pauseReasonBackoff = "rate-limit backoff"
)

// deferral says why background discovery is being held back and when that is
// expected to end; a zero time means "when fresh rate-limit data arrives".
// A spent allowance is reported before staleness because a probe is refused
// until the reset in that state too.
func (v discoveryView) deferral() (reason string, resumeAt time.Time) {
	if v.Limit > 0 && v.now.Before(v.Reset) && (v.Remaining <= v.Reserve || v.Spent >= v.Share) {
		return pauseReasonSpent, v.Reset
	}
	return pauseReasonStale, time.Time{}
}

func discoveryCall[T any](ctx context.Context, d *Daemon, key string, cost int, call func(context.Context) (T, error)) (T, error) {
	if r, ok := ctx.Value(quotaReservationKey{}).(*discoveryReservation); ok && r.fallback {
		return call(ctx)
	}
	if d.connections != nil {
		v, selected, err := d.quotaVersion(ctx, store.TicketConnection)
		if err != nil {
			var zero T
			return zero, err
		}
		if selected {
			if err := d.discoveryProbe(ctx, githubRepo(v.Destination)); err != nil {
				var zero T
				return zero, err
			}
		}
	}
	budget, err := d.quotaBudget(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	if cost == discoveryListBound || cost == linearHistoryBound {
		// A list reserves the entire supported history before its first page.
		// Serialize these reservations until unused pages are returned, rather
		// than deferring another worker solely because a short fetch is in
		// flight. This waits only for existing HTTP work, never for a quota
		// refill; execution and reporting do not enter this discovery gate.
		budget.mu.Lock()
		if budget.lists == nil {
			budget.lists = make(chan struct{}, 1)
		}
		lists := budget.lists
		budget.mu.Unlock()
		select {
		case lists <- struct{}{}:
			defer func() { <-lists }()
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
	r, err := budget.acquire(key, cost, false)
	if err != nil {
		var zero T
		return zero, err
	}
	defer r.finish()
	if d.connections != nil {
		return call(context.WithValue(ctx, quotaReservationKey{}, r))
	}
	return call(github.WithRequestObserver(ctx, r.observe))
}
func discoveryKey(repo github.Repo, operation string, number int) string {
	return fmt.Sprintf("%s/%s/%d", repo, operation, number)
}

// drainAffordable says whether a backlog drain can pay for its issue read and
// the comment history read that follows it. A backlog session whose history
// read the budget would defer must not pay for the issue read either: each
// tick's cheap issue reads then hold the allowance just above the reserve, the
// history read never fits and every session is re-read forever, which drained
// the Linear allowance to its floor for hours (2026-10-14).
func (d *Daemon) drainAffordable(ctx context.Context) error {
	if r, ok := ctx.Value(quotaReservationKey{}).(*discoveryReservation); ok && r.fallback {
		return nil
	}
	cost := discoveryListBound
	if d.connections != nil {
		v, selected, err := d.quotaVersion(ctx, store.TicketConnection)
		if err != nil {
			return err
		}
		if selected && v.Provider == ticket.Linear {
			cost = linearHistoryBound
		}
	}
	budget, err := d.quotaBudget(ctx)
	if err != nil {
		return err
	}
	if !budget.affords(1 + cost) {
		return ErrDiscoveryDeferred
	}
	return nil
}

func (d *Daemon) discoveryIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	return discoveryCall(ctx, d, discoveryKey(repo, "issue", number), 1, func(ctx context.Context) (github.Issue, error) { return d.readIssue(ctx, repo, number) })
}
func (d *Daemon) discoveryComments(ctx context.Context, repo github.Repo, number int, after int64) ([]github.Comment, error) {
	cost := discoveryListBound
	if d.connections != nil {
		v, selected, err := d.quotaVersion(ctx, store.TicketConnection)
		if err != nil {
			return nil, err
		}
		if selected && v.Provider == ticket.Linear {
			cost = linearHistoryBound
		}
	}
	return discoveryCall(ctx, d, discoveryKey(repo, "comments", number), cost, func(ctx context.Context) ([]github.Comment, error) {
		return d.readComments(ctx, repo, number, after)
	})
}
func (d *Daemon) discoveryProbe(ctx context.Context, repo github.Repo) error {
	budget, err := d.quotaBudget(ctx)
	if err != nil {
		return err
	}
	r, err := budget.acquire("probe", 1, true)
	if err != nil {
		return nil
	}
	defer r.finish()
	if d.connections != nil {
		ctx = context.WithValue(ctx, quotaReservationKey{}, r)
	} else {
		ctx = github.WithRequestObserver(ctx, r.observe)
	}
	if d.connections != nil {
		version, selected, e := d.quotaVersion(ctx, store.TicketConnection)
		if e != nil {
			return e
		}
		if !selected {
			return ErrDiscoveryDeferred
		}
		clients, e := d.connections.Clients(ctx, version, 0)
		if e != nil {
			return e
		}
		if version.Provider == ticket.Linear {
			// A cold client already performed the single publisher request.
			budget.mu.Lock()
			requested := r.requests > 0
			budget.mu.Unlock()
			if requested {
				return nil
			}
			_, e = clients.Linear.Publisher(ctx)
			return e
		}
		_, e = clients.Code.GetIssue(github.WithoutConditionalCache(ctx), githubRepo(version.Destination), 1)
		if errors.Is(e, github.ErrNotFound) || errors.Is(e, github.ErrInvalidArgument) {
			return nil
		}
		return e
	}
	ingress, _, selectionErr := d.connectionIngress(ctx, repo, 0)
	if selectionErr != nil {
		return selectionErr
	}
	if ingress != nil && ingressProvider(ingress) == ticket.Linear {
		adapter, ok := ingress.(interface {
			ContainerIdentity(context.Context, string) (ticket.Identity, error)
		})
		if !ok {
			return fmt.Errorf("ticket container verification required")
		}
		_, e := adapter.ContainerIdentity(ctx, repo.String())
		return e
	}
	_, err = d.readIssue(github.WithoutConditionalCache(ctx), repo, 1)
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

// logDiscoveryDeferral says why a session's issue read was deferred, with the
// budget it was charged to, so a starved fleet is visible at INFO (#793).
func (d *Daemon) logDiscoveryDeferral(ctx context.Context, c *candidate) {
	ctx = sessionContext(ctx, c.session.ID)
	v, selected, verr := d.quotaVersion(ctx, store.TicketConnection)
	attrs := []any{slog.Int64("session_id", c.session.ID), slog.Bool("connection_selected", selected)}
	if verr != nil {
		attrs = append(attrs, slog.Any("connection_error", verr))
	}
	if selected {
		attrs = append(attrs, slog.Int64("connection_version_id", v.ID), slog.String("provider", string(v.Provider)))
		q := d.quotas.forVersion(v)
		if until, held := q.backoff.hold(d.quotaNow(ctx)); held {
			attrs = append(attrs, slog.Time("backoff_until", until))
		}
	}
	b, berr := d.quotaBudget(ctx)
	if berr != nil {
		attrs = append(attrs, slog.Any("budget_error", berr))
	} else {
		view := b.view()
		reason, _ := view.deferral()
		attrs = append(attrs, slog.String("reason", reason), slog.Bool("fresh", view.Fresh), slog.Int("limit", view.Limit),
			slog.Int("remaining", view.Remaining), slog.Int("reserve", view.Reserve), slog.Int("spent", view.Spent),
			slog.Int("share", view.Share), slog.Int("in_progress", view.InProgress), slog.Float64("tokens", view.Burst))
	}
	c.log.InfoContext(ctx, "issue read deferred by the discovery budget", attrs...)
}
