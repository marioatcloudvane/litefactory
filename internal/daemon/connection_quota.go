package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Destination and version deliberately do not participate: projects sharing
// the configured credential reference share the same provider quota.
type quotaIdentity struct {
	provider                      ticket.Provider
	origin                        ticket.Origin
	source, service, account, env string
}

type connectionQuota struct {
	backoff        backoff
	discovery      discoveryBudget
	reportMu       sync.Mutex
	reportedReason string
	reportedUntil  time.Time
}

type connectionQuotas struct {
	mu       sync.Mutex
	now      time.Time
	states   map[quotaIdentity]*connectionQuota
	versions map[int64]store.ConnectionVersion
}

func (q *connectionQuotas) tick(now time.Time) {
	q.mu.Lock()
	q.now = now
	q.mu.Unlock()
}

func (q *connectionQuotas) forVersion(v store.ConnectionVersion) *connectionQuota {
	key := quotaIdentity{v.Provider, v.Origin, v.CredentialSource, v.CredentialService, v.CredentialAccount, v.CredentialEnv}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.states == nil {
		q.states = make(map[quotaIdentity]*connectionQuota)
		q.versions = make(map[int64]store.ConnectionVersion)
	}
	q.versions[v.ID] = v
	if q.states[key] == nil {
		q.states[key] = &connectionQuota{}
	}
	return q.states[key]
}

type quotaVersionKey struct{}
type quotaReservationKey struct{}

func quotaVersionContext(ctx context.Context, v store.ConnectionVersion) context.Context {
	return context.WithValue(ctx, quotaVersionKey{}, v)
}

func (d *Daemon) quotaVersion(ctx context.Context, role store.ConnectionRole) (store.ConnectionVersion, bool, error) {
	if d.connections == nil {
		return store.ConnectionVersion{}, false, nil
	}
	if v, ok := ctx.Value(quotaVersionKey{}).(store.ConnectionVersion); ok && role == store.TicketConnection {
		return v, true, nil
	}
	if source, ok := ctx.Value(doctorSourceContextKey{}).(store.DoctorCommandSource); ok && role == store.TicketConnection {
		v, err := d.db.GetConnectionVersion(ctx, source.ConnectionVersionID)
		return v, true, err
	}
	scope, _ := ctx.Value(connectionScopeKey{}).(connectionScope)
	if id := turn.ExecutionSessionID(ctx); id > 0 {
		scope.sessionID = id
	}
	if scope.sessionID > 0 {
		snapshot, err := d.db.GetExecutionSnapshot(ctx, scope.sessionID)
		if err != nil {
			return store.ConnectionVersion{}, true, err
		}
		id := snapshot.TicketVersionID
		if role == store.CodeConnection {
			id = snapshot.CodeVersionID
		}
		v, err := d.db.GetConnectionVersion(ctx, id)
		return v, true, err
	}
	if scope.projectID > 0 {
		v, err := d.db.CurrentConnection(ctx, scope.projectID, role)
		return v, true, err
	}
	return store.ConnectionVersion{}, false, nil
}

func (d *Daemon) quotaBudget(ctx context.Context) (*discoveryBudget, error) {
	v, selected, err := d.quotaVersion(ctx, store.TicketConnection)
	if err != nil {
		return nil, err
	}
	if !selected {
		return &d.discovery, nil
	}
	q := d.quotas.forVersion(v)
	now := d.quotaNow(ctx)
	q.discovery.tick(now)
	if _, held := q.backoff.hold(now); held {
		return nil, ErrDiscoveryDeferred
	}
	return &q.discovery, nil
}

func (d *Daemon) quotaNow(ctx context.Context) time.Time {
	if now := recoveryTime(ctx); !now.IsZero() {
		return now
	}
	d.quotas.mu.Lock()
	defer d.quotas.mu.Unlock()
	return d.quotas.now
}

// bindQuotaRequest runs at the provider boundary, so retained sessions, code
// connections and optional inspection calls cannot lose source attribution.
func (d *Daemon) bindQuotaRequest(ctx context.Context, v store.ConnectionVersion) (context.Context, func(error), error) {
	q := d.quotas.forVersion(v)
	now := d.quotaNow(ctx)
	q.discovery.tick(now)
	if _, held := q.backoff.hold(now); held {
		return ctx, nil, ErrDiscoveryDeferred
	}
	r, _ := ctx.Value(quotaReservationKey{}).(*discoveryReservation)
	if r != nil && r.fallback && r.budget == &q.discovery {
		q.discovery.mu.Lock()
		exhausted := r.requests >= r.bound || (q.discovery.limit > 0 && now.Before(q.discovery.reset) && (q.discovery.remaining <= q.discovery.floorLocked() || q.discovery.spent >= q.discovery.discoveryShareLocked()))
		q.discovery.mu.Unlock()
		if exhausted {
			return ctx, nil, ErrDiscoveryDeferred
		}
	}
	ctx = github.WithRequestObserver(ctx, func(o github.RequestObservation) {
		q.discovery.observe(o)
		if r != nil && r.budget == &q.discovery {
			r.observe(o)
		}
	})
	ctx = linear.WithRequestObserver(ctx, func(o linear.RequestObservation) {
		q.discovery.observeLinear(o)
		if r != nil && r.budget == &q.discovery {
			r.observeLinear(o)
		}
	})
	return ctx, func(err error) {
		var ticketRate *ticket.RateLimitError
		var githubRate *github.RateLimitError
		switch {
		case errors.As(err, &ticketRate):
			q.backoff.fail(d.quotaNow(ctx), ticketRate.RetryAfter)
		case errors.As(err, &githubRate):
			q.backoff.fail(d.quotaNow(ctx), githubRate.RetryAfter)
		case err == nil:
			// A concurrently successful request cannot release a standing hold.
			q.backoff.succeeded(d.quotaNow(ctx))
		}
	}, nil
}

func (d *Daemon) connectionHold(ctx context.Context, now time.Time) (time.Time, bool) {
	if d.connections == nil {
		return d.backoff.hold(now)
	}
	var until time.Time
	for _, role := range []store.ConnectionRole{store.TicketConnection, store.CodeConnection} {
		v, selected, err := d.quotaVersion(ctx, role)
		if err != nil {
			return time.Time{}, true
		}
		if selected {
			if at, held := d.quotas.forVersion(v).backoff.hold(now); held && at.After(until) {
				until = at
			}
		}
	}
	return until, !until.IsZero()
}

// Connection loading sanitizes provider errors. Consult the hold recorded at
// the request boundary as well, without mistaking a storage lookup failure for
// quota evidence.
func (d *Daemon) scopedQuotaDeferred(ctx context.Context, err error) bool {
	if d.connections == nil {
		return false
	}
	if providerRateLimited(err) || errors.Is(err, ErrDiscoveryDeferred) || errors.Is(err, github.ErrRateLimited) {
		return true
	}
	for _, role := range []store.ConnectionRole{store.TicketConnection, store.CodeConnection} {
		v, selected, lookupErr := d.quotaVersion(ctx, role)
		if lookupErr == nil && selected {
			if _, held := d.quotas.forVersion(v).backoff.hold(d.quotaNow(ctx)); held {
				return true
			}
		}
	}
	return false
}

func (d *Daemon) scopedPauseSummary(now time.Time) string {
	d.quotas.mu.Lock()
	versions := make([]store.ConnectionVersion, 0, len(d.quotas.versions))
	for _, v := range d.quotas.versions {
		versions = append(versions, v)
	}
	d.quotas.mu.Unlock()
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID < versions[j].ID })
	seen := make(map[string]bool)
	var pauses []string
	for _, v := range versions {
		q := d.quotas.forVersion(v)
		reason, until := "", time.Time{}
		if at, held := q.backoff.hold(now); held {
			reason, until = pauseReasonBackoff, at
		} else if v.Provider == ticket.Linear {
			q.discovery.tick(now)
			view := q.discovery.view()
			if view.HeldBack > 0 || !view.ProbeAt.IsZero() || !view.FallbackAt.IsZero() {
				if !view.Fresh || (view.Remaining <= view.Reserve || view.Spent >= view.Share) {
					reason, until = view.deferral()
					if reason == pauseReasonStale {
						until = view.ProbeAt.Add(time.Minute)
						if !view.FallbackAt.IsZero() {
							reason += "; bounded fallback"
							if at := view.FallbackAt.Add(time.Minute); at.After(until) {
								until = at
							}
						}
					}
				}
			}
		}
		q.reportMu.Lock()
		changed := reason != q.reportedReason || !until.Equal(q.reportedUntil)
		q.reportedReason, q.reportedUntil = reason, until
		q.reportMu.Unlock()
		if changed && d.log != nil {
			if reason == "" {
				d.log.Info("source discovery recovered", slog.Int64("project_id", v.RepoID), slog.String("provider", string(v.Provider)))
			} else {
				d.log.Warn("source discovery deferred", slog.Int64("project_id", v.RepoID), slog.String("provider", string(v.Provider)), slog.String("reason", reason), slog.Time("retry_at", until))
			}
		}
		if reason != "" {
			line := fmt.Sprintf("project %d %s %s: %s until %s", v.RepoID, v.Provider, v.Origin, reason, until.UTC().Format(time.RFC3339))
			if !seen[line] {
				seen[line] = true
				pauses = append(pauses, line)
			}
		}
	}
	sort.Strings(pauses)
	return strings.Join(pauses, "; ")
}
