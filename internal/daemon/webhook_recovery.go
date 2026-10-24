package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const hookRecoveryInterval = time.Minute
const hookRecoveryCapacity = 1000
const hookRecoveryAttempts = 3
const hookRecoveryWork = 20

// Tick owns this bounded, volatile state. Losing any accounting discards POST
// eligibility; neither a restart nor capacity pressure creates a new allowance.
type hookRecoveryState struct {
	fallback                bool
	selector                string
	revision, version, hook int64
	next                    time.Time
	cutoff                  time.Time
	pollingOnly             bool
	cursor                  string
	pages                   int
	history                 map[int64]github.HookDelivery
	groups                  [][]github.HookDelivery
	index                   int
	intents                 map[string]*hookIntent
}

type hookIntent struct {
	pendingID int64
	count     int
	pending   bool
	refused   bool
	remote    int
	last      time.Time
}

func newHookRecovery(sub store.TicketSubscription, hook int64) *hookRecoveryState {
	return &hookRecoveryState{selector: sub.ID, revision: sub.Revision, version: sub.ConnectionVersionID, hook: hook,
		history: make(map[int64]github.HookDelivery), intents: make(map[string]*hookIntent)}
}

func (s *hookRecoveryState) resetPage() {
	s.cursor = ""
	s.pages = 0
	s.history = make(map[int64]github.HookDelivery)
	s.groups = nil
	s.index = 0
}

func (s *hookRecoveryState) addPage(page github.HookDeliveryPage) bool {
	// Date has one-second resolution. Use its upper bound, not local wall time
	// or first-seen time, to prove an original arose after tracking began.
	if s.cutoff.IsZero() {
		if page.ServerTime.IsZero() {
			s.pollingOnly = true
		} else {
			s.cutoff = page.ServerTime.Add(time.Second)
		}
	}
	s.pages++
	for _, item := range page.Deliveries {
		if len(s.history) >= hookRecoveryCapacity {
			s.pollingOnly = true
			s.resetPage()
			return false
		}
		s.history[item.ID] = item
	}
	if page.Next != "" {
		if page.Next == s.cursor || s.pages >= 10 {
			s.pollingOnly = true
			s.resetPage()
			return false
		}
		s.cursor = page.Next
		return false
	}
	grouped := make(map[string][]github.HookDelivery)
	for _, item := range s.history {
		if item.GUID != "" {
			grouped[item.GUID] = append(grouped[item.GUID], item)
		}
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	s.groups = make([][]github.HookDelivery, 0, len(keys))
	for _, key := range keys {
		s.groups = append(s.groups, grouped[key])
	}
	return true
}

// hookRecoveryUnfinished reports a recovery pass that stopped part-way through
// the delivery history (another page or more groups to decide). Such a pass
// continues on the following ticks; a new pass starts only on a listing tick.
func (d *Daemon) hookRecoveryUnfinished(repo int64) bool {
	state := d.hookRecovery[repo]
	return state != nil && (state.cursor != "" || state.groups != nil)
}

// recoverHookDeliveries replays failed webhook deliveries. It runs on the
// repository's listing tick (ordinary) and on the ticks that finish a pass the
// listing tick started; it is not a per-minute poll.
func (d *Daemon) recoverHookDeliveries(ctx context.Context, q *repoQueue, now time.Time, ordinary bool) error {
	version, err := d.db.CurrentConnection(ctx, q.repo.ID, store.TicketConnection)
	if err != nil {
		return err
	}
	if !version.Enabled || version.Provider != ticket.GitHub {
		delete(d.hookRecovery, q.repo.ID)
		return nil
	}
	ctx = quotaVersionContext(ctx, version)
	subs, err := d.db.ListTicketSubscriptions(ctx, q.repo.ID)
	if err != nil {
		return err
	}
	var sub store.TicketSubscription
	for _, candidate := range subs {
		if candidate.DesiredEnabled && candidate.ConnectionVersionID == version.ID && candidate.Provider == version.Provider && candidate.Origin == version.Origin && candidate.Container == version.Destination {
			if sub.ID != "" {
				sub = store.TicketSubscription{}
				break
			}
			sub = candidate
		}
	}
	hook, _ := strconv.ParseInt(sub.RemoteHookID, 10, 64)
	if d.hookRecovery == nil {
		d.hookRecovery = make(map[int64]*hookRecoveryState)
	}
	state := d.hookRecovery[q.repo.ID]
	if state == nil || state.selector != sub.ID || state.revision != sub.Revision || state.version != version.ID || state.hook != hook {
		state = newHookRecovery(sub, hook)
		state.version = version.ID
		d.hookRecovery[q.repo.ID] = state
		if hook <= 0 || sub.ID == "" {
			q.log.InfoContext(ctx, "webhook recovery off: no recorded hook; webhooks and ordinary polling cover intake")
		}
	}
	if now.Before(state.next) {
		return nil
	}
	// The ordinary listing is the fallback. On a listing tick it runs right
	// after this; on a tick that only continues an unfinished recovery the
	// repository is marked owed, so the next tick lists it.
	fallback := func(reason string) error {
		q.log.InfoContext(ctx, "webhook recovery polling fallback", slog.Int64("hook", hook), slog.String("reason", reason))
		if !ordinary && d.schedule != nil {
			d.schedule.ListingFailed(q.repo.ID)
		}
		return nil
	}
	// Without a recorded hook there are no deliveries to replay. Polling the
	// source every minute instead costs API budget the webhook already saves;
	// ordinary polling stays the backstop for a lost delivery.
	if hook <= 0 || sub.ID == "" {
		return nil
	}
	clients, err := d.connections.Clients(ctx, version, 0)
	if err != nil {
		return err
	}
	// Verify the current source's immutable repository identity before hook I/O.
	// A settled scope observation spends no discovery budget step.
	identity, hit, err := cachedContainerIdentity(ctx, version.ID, 0)
	if !hit {
		identity, err = discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "hook-container", 0), 1, func(ctx context.Context) (ticket.Identity, error) {
			return tickContainerIdentity(ctx, clients.Tickets, version.ID, 0, version.Destination)
		})
	}
	if err != nil {
		return err
	}
	if identity.Provider != version.Provider || identity.Origin != version.Origin || identity.RemoteID != sub.ContainerID || (version.RemoteID != "" && identity.RemoteID != version.RemoteID) {
		delete(d.hookRecovery, q.repo.ID)
		return store.ErrTicketIdentityConflict
	}
	if state.groups == nil {
		page, e := discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "hook-history", 0), 1, func(ctx context.Context) (github.HookDeliveryPage, error) {
			return clients.Code.HookDeliveriesPage(ctx, q.forgeRepo, hook, state.cursor)
		})
		if e != nil {
			if errors.Is(e, ErrDiscoveryDeferred) || providerRateLimited(e) || ctx.Err() != nil {
				return e
			}
			state.resetPage()
			if err := fallback("delivery history unavailable"); err != nil {
				return err
			}
			state.next = now.Add(hookRecoveryInterval)
			return nil
		}
		if !state.addPage(page) {
			return fallback("delivery history incomplete")
		}
	}
	end := min(state.index+hookRecoveryWork, len(state.groups))
	scanFallback := state.fallback
	for state.index < end {
		if err := ctx.Err(); err != nil {
			return err
		}
		group := state.groups[state.index]
		item, retry, reason := state.decision(group, now)
		if item.GUID != "" && strconv.FormatInt(item.RepositoryID, 10) != sub.ContainerID {
			return store.ErrTicketIdentityConflict
		}
		if item.GUID != "" && !d.hookDeliveryAccepted(sub, item.GUID) {
			log := q.log.With(slog.Int64("hook", hook), slog.String("guid", item.GUID))
			if reason == "confirmed" {
				log.InfoContext(ctx, "webhook recovery confirmed recovered")
			} else {
				if retry {
					// Recheck revision immediately before the effect. Admission deferral
					// records no intent; once admitted, record before entering the transport.
					if !d.observationSubscriptionCurrent(ctx, sub.ID, sub.Revision) {
						delete(d.hookRecovery, q.repo.ID)
						return nil
					}
					_, e := discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "hook-redeliver", 0), 1, func(ctx context.Context) (struct{}, error) {
						intent := state.intents[item.GUID]
						if intent == nil {
							intent = &hookIntent{}
							state.intents[item.GUID] = intent
						}
						intent.count++
						intent.pending = true
						intent.last = now
						receipt, err := clients.Code.RedeliverHookDelivery(ctx, q.forgeRepo, hook, item.ID)
						if receipt.GUID == item.GUID && receipt.Redelivery && receipt.ID > item.ID {
							intent.pendingID = receipt.ID
						}
						if errors.Is(err, github.ErrForbidden) || errors.Is(err, github.ErrUnauthorized) || errors.Is(err, github.ErrNotFound) {
							intent.refused = true
						}
						return struct{}{}, err
					})
					if errors.Is(e, ErrDiscoveryDeferred) {
						return e
					}
					if e == nil {
						log.InfoContext(ctx, "webhook recovery requested", slog.Int("attempt", state.intents[item.GUID].count))
					} else {
						log.InfoContext(ctx, "webhook recovery deferred", slog.String("reason", "request outcome requires reconciliation"))
						if ctx.Err() != nil || providerRateLimited(e) {
							return e
						}
					}
				} else {
					log.InfoContext(ctx, "webhook recovery polling fallback", slog.String("reason", reason))
				}
				// Even an acknowledged request can remain undelivered indefinitely.
				// Polling beside it preserves latency without blindly repeating a POST.
				known, e := d.pollHookDelivery(ctx, q, version, sub, item, hook, now)
				if errors.Is(e, ErrDiscoveryDeferred) || providerRateLimited(e) || ctx.Err() != nil {
					return e
				}
				if e != nil || !known {
					scanFallback = true
					state.fallback = true
				}
			}
		}
		state.index++
	}
	if scanFallback {
		if err := fallback("delivery details unavailable"); err != nil {
			return err
		}
	}
	state.fallback = false
	if state.index == len(state.groups) {
		state.resetPage()
		state.next = now.Add(hookRecoveryInterval)
	}
	return nil
}

func (s *hookRecoveryState) decision(group []github.HookDelivery, now time.Time) (github.HookDelivery, bool, string) {
	var original, latest, succeeded github.HookDelivery
	remote, originals := 0, 0
	ambiguous, unfinished, failed := false, false, false
	for _, item := range group {
		if item.ID > latest.ID {
			latest = item
		}
		if item.StatusCode >= 200 && item.StatusCode < 300 {
			succeeded = item
		} else {
			failed = true
		}
		if item.StatusCode <= 0 {
			unfinished = true
		}
		switch {
		case item.Redelivery:
			remote++
		case item.OriginalKnown:
			originals++
			original = item
		default:
			ambiguous = true
		}
	}
	if latest.Event != "issues" && latest.Event != "issue_comment" {
		return github.HookDelivery{}, false, "unsupported"
	}
	if succeeded.ID != 0 {
		if failed || s.intents[latest.GUID] != nil {
			return succeeded, false, "confirmed"
		}
		return github.HookDelivery{}, false, "confirmed"
	}
	if ambiguous || originals > 1 {
		return latest, false, "ambiguous original"
	}
	if unfinished || latest.StatusCode < 300 {
		return latest, false, "unfinished attempt"
	}
	if s.pollingOnly || original.ID == 0 || !original.DeliveredAt.After(s.cutoff) || s.cutoff.IsZero() {
		return latest, false, "untracked original"
	}
	intent := s.intents[latest.GUID]
	if intent == nil {
		if len(s.intents) >= hookRecoveryCapacity {
			s.pollingOnly = true
			s.intents = make(map[string]*hookIntent)
			return latest, false, "tracking capacity exhausted"
		}
		intent = &hookIntent{remote: remote, count: remote}
		if remote > 0 {
			intent.last = now
		}
		s.intents[latest.GUID] = intent
	}
	if intent.refused {
		return latest, false, "redelivery refused"
	}
	if remote > intent.remote && !intent.pending {
		intent.last = now
	}
	if intent.pending {
		reconciled := false
		for _, attempt := range group {
			if intent.pendingID != 0 && attempt.ID == intent.pendingID && attempt.Redelivery {
				reconciled = true
			}
		}
		if !reconciled {
			if remote+1 >= hookRecoveryAttempts {
				return latest, false, "redelivery exhausted"
			}
			return latest, false, "unreconciled request"
		}
		intent.pending = false
	}
	// Remote attempts and reconciled local intents describe the same attempts.
	intent.count = max(intent.count, remote)
	intent.remote = remote
	if intent.count >= hookRecoveryAttempts {
		return latest, false, "redelivery exhausted"
	}
	if now.Sub(intent.last) < hookRecoveryInterval {
		return latest, false, "retry interval"
	}
	return latest, true, "eligible"
}

func (d *Daemon) hookDeliveryAccepted(sub store.TicketSubscription, guid string) bool {
	if d.webhook == nil {
		return false
	}
	d.webhook.mu.RLock()
	handler := d.webhook.handler
	d.webhook.mu.RUnlock()
	return handler != nil && handler.AcceptedDelivery(string(sub.Provider), string(sub.Origin), sub.ID, guid)
}

func (d *Daemon) pollHookDelivery(ctx context.Context, q *repoQueue, version store.ConnectionVersion, sub store.TicketSubscription, item github.HookDelivery, hook int64, now time.Time) (bool, error) {
	clients, err := d.connections.Clients(ctx, version, 0)
	if err != nil {
		return false, err
	}
	type detail struct {
		delivery github.HookDelivery
		url      string
		number   int
	}
	value, err := discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "hook-detail", 0), 1, func(ctx context.Context) (detail, error) {
		delivery, url, number, e := clients.Code.HookDeliveryIssue(ctx, q.forgeRepo, hook, item.ID)
		return detail{delivery, url, number}, e
	})
	if err != nil {
		return false, err
	}
	expected := fmt.Sprintf("%s/repos/%s/issues/%d", version.Origin, version.Destination, value.number)
	if value.delivery.ID != item.ID || value.delivery.GUID != item.GUID || strconv.FormatInt(value.delivery.RepositoryID, 10) != sub.ContainerID || value.url != expected {
		return false, store.ErrTicketIdentityConflict
	}
	snapshot, err := discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "hook-ticket", value.number), 1, func(ctx context.Context) (ticket.Snapshot, error) {
		return clients.Tickets.Resolve(ctx, ticket.Locator{Provider: version.Provider, Origin: version.Origin, Container: version.Destination, Key: strconv.Itoa(value.number)})
	})
	if err != nil {
		return false, err
	}
	ref := snapshot.Ref
	if ref.Identity.Provider != version.Provider || ref.Identity.Origin != version.Origin || ref.Locator.Container != version.Destination || ref.Locator.Key != strconv.Itoa(value.number) {
		return false, store.ErrTicketIdentityConflict
	}
	enrollment := version.CreatedAt
	if version.Provenance == "legacy" {
		enrollment = q.repo.CreatedAt
	}
	position, err := d.db.InitTicketRecoveryPosition(ctx, version, enrollment)
	if err != nil {
		return false, err
	}
	if _, err = d.db.PinTicketRecoveryContainer(ctx, version.ID, position.Revision, sub.ContainerID); err != nil {
		return false, err
	}
	err = d.db.HandoffSubscriptionObservation(ctx, sub.ID, sub.Revision, store.PendingTicketObservation{
		ConnectionVersionID: version.ID, RepoID: q.repo.ID, Provider: version.Provider, Origin: version.Origin, Container: version.Destination, ContainerID: sub.ContainerID,
		TicketID: ref.Identity.RemoteID, TicketKey: ref.Locator.Key, ObservedAt: now,
	})
	return err == nil, err
}
