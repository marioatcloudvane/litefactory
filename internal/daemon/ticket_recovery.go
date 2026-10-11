package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const recoveryPageSize = 50
const reconciliationInterval = 24 * time.Hour

type ticketRecoveryDiscovery interface {
	ChangesPage(context.Context, string, time.Time, ticket.DiscoveryCursor, int) (ticket.ChangePage, error)
	ReplyChangesPage(context.Context, string, time.Time, ticket.DiscoveryCursor, int) (ticket.ChangePage, error)
	ReconcilePage(context.Context, string, ticket.DiscoveryCursor, int) (ticket.ChangePage, error)
}

func recoveryCursor(raw string) (ticket.DiscoveryCursor, error) {
	if raw == "" {
		return ticket.DiscoveryCursor{}, nil
	}
	var cursor ticket.DiscoveryCursor
	err := json.Unmarshal([]byte(raw), &cursor)
	return cursor, err
}
func encodedRecoveryCursor(cursor ticket.DiscoveryCursor) (string, error) {
	if cursor == (ticket.DiscoveryCursor{}) {
		return "", nil
	}
	raw, err := json.Marshal(cursor)
	return string(raw), err
}

// recoverSource performs at most one page of each stream per scheduling pass.
// The page's signals and continuation commit together; a finished ticket stream
// never moves coverage while the reply stream is unfinished.
func (d *Daemon) recoverSource(ctx context.Context, q *repoQueue, now time.Time) (bool, error) {
	version, err := d.db.CurrentConnection(ctx, q.repo.ID, store.TicketConnection)
	if err != nil {
		return false, err
	}
	clients, err := d.connections.Clients(ctx, version, 0)
	if err != nil {
		return false, err
	}
	access, err := discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "recovery-container", 0), 1, func(ctx context.Context) (github.RepositoryAccess, error) {
		return clients.Code.InspectRepository(ctx, q.forgeRepo)
	})
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(access.FullName, version.Destination) || (version.RemoteID != "" && version.RemoteID != strconv.FormatInt(access.ID, 10)) {
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
	position, err = d.db.PinTicketRecoveryContainer(ctx, version.ID, position.Revision, strconv.FormatInt(access.ID, 10))
	if err != nil {
		return false, err
	}
	adapter := ticketRecoveryDiscovery(clients.Tickets)
	if position.ScanThrough.IsZero() || (position.TicketsComplete && position.RepliesComplete) {
		position.ScanThrough = now
		position.TicketsComplete, position.RepliesComplete = false, false
		position.TicketCursor, position.ReplyCursor = "", ""
		position, err = d.db.CommitTicketRecoveryPage(ctx, position, position.Revision, nil)
		if err != nil {
			return false, err
		}
	}
	since := position.CompletedThrough.Add(-ListingOverlap)
	if since.Before(position.EnrollmentAt) {
		since = position.EnrollmentAt
	}
	for _, stream := range []string{"tickets", "replies"} {
		if stream == "tickets" && position.TicketsComplete || stream == "replies" && position.RepliesComplete {
			continue
		}
		raw := position.TicketCursor
		if stream == "replies" {
			raw = position.ReplyCursor
		}
		cursor, err := recoveryCursor(raw)
		if err != nil {
			return false, d.restartRecovery(ctx, position)
		}
		page, err := discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "recovery-"+stream, 0), 1, func(ctx context.Context) (ticket.ChangePage, error) {
			if stream == "tickets" {
				return adapter.ChangesPage(ctx, version.Destination, since, cursor, recoveryPageSize)
			}
			return adapter.ReplyChangesPage(ctx, version.Destination, since, cursor, recoveryPageSize)
		})
		if err != nil {
			if errors.Is(err, github.ErrInvalidArgument) {
				return false, d.restartRecovery(ctx, position)
			}
			return false, err
		}
		observations, err := d.recoveryObservations(ctx, version, position, page, now, clients.Tickets)
		if err != nil {
			return false, err
		}
		next, err := encodedRecoveryCursor(page.Next)
		if err != nil {
			return false, err
		}
		if !page.Complete && next == "" {
			return false, fmt.Errorf("daemon: incomplete recovery page has no continuation")
		}
		if stream == "tickets" {
			position.TicketCursor, position.TicketsComplete = next, page.Complete
		} else {
			position.ReplyCursor, position.RepliesComplete = next, page.Complete
		}
		if position.TicketsComplete && position.RepliesComplete {
			position.CompletedThrough = position.ScanThrough
		}
		position, err = d.db.CommitTicketRecoveryPage(ctx, position, position.Revision, observations)
		if err != nil {
			return false, err
		}
	}
	if position.ReconcileCompletedAt.IsZero() || now.Sub(position.ReconcileCompletedAt) >= reconciliationInterval || position.ReconcileCursor != "" {
		cursor, err := recoveryCursor(position.ReconcileCursor)
		if err != nil {
			position.ReconcileCursor = ""
			_, err = d.db.CommitTicketRecoveryPage(ctx, position, position.Revision, nil)
			return false, err
		}
		page, err := discoveryCall(ctx, d, discoveryKey(q.forgeRepo, "reconciliation", 0), 1, func(ctx context.Context) (ticket.ChangePage, error) {
			return adapter.ReconcilePage(ctx, version.Destination, cursor, recoveryPageSize)
		})
		if err != nil {
			if errors.Is(err, github.ErrInvalidArgument) {
				position.ReconcileCursor = ""
				_, err = d.db.CommitTicketRecoveryPage(ctx, position, position.Revision, nil)
				return false, err
			}
			return false, err
		}
		observations, err := d.recoveryObservations(ctx, version, position, page, now, clients.Tickets)
		if err != nil {
			return false, err
		}
		position.ReconcileCursor, err = encodedRecoveryCursor(page.Next)
		if err != nil {
			return false, err
		}
		if page.Complete {
			position.ReconcileCompletedAt = now
		}
		if _, err = d.db.CommitTicketRecoveryPage(ctx, position, position.Revision, observations); err != nil {
			return false, err
		}
	}
	return position.TicketsComplete && position.RepliesComplete, nil
}

func (d *Daemon) restartRecovery(ctx context.Context, p store.TicketRecoveryPosition) error {
	p.TicketCursor, p.ReplyCursor = "", ""
	p.TicketsComplete, p.RepliesComplete = false, false
	_, err := d.db.CommitTicketRecoveryPage(ctx, p, p.Revision, nil)
	return err
}

func (d *Daemon) recoveryObservations(ctx context.Context, v store.ConnectionVersion, p store.TicketRecoveryPosition, page ticket.ChangePage, now time.Time, adapter *github.Tickets) ([]store.PendingTicketObservation, error) {
	snapshots := append([]ticket.Snapshot(nil), page.Tickets...)
	for _, locator := range page.Locators {
		if locator.Provider != v.Provider || locator.Origin != v.Origin || locator.Container != v.Destination {
			return nil, store.ErrTicketIdentityConflict
		}
		snapshot, err := discoveryCall(ctx, d, discoveryKey(githubRepo(v.Destination), "recovery-resolve", mustTicketNumber(locator.Key)), 1, func(ctx context.Context) (ticket.Snapshot, error) { return adapter.Resolve(ctx, locator) })
		if errors.Is(err, github.ErrNotFound) || errors.Is(err, github.ErrInvalidArgument) {
			continue
		}
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	out := make([]store.PendingTicketObservation, 0, len(snapshots))
	for _, snapshot := range snapshots {
		ref := snapshot.Ref
		if ref.Validate() != nil || ref.Identity.Provider != v.Provider || ref.Identity.Origin != v.Origin || ref.Locator.Container != v.Destination {
			return nil, store.ErrTicketIdentityConflict
		}
		// Reconciliation also visits old tickets because enrolled replies may be new.
		out = append(out, store.PendingTicketObservation{ConnectionVersionID: v.ID, RepoID: v.RepoID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: p.ContainerID, TicketID: ref.Identity.RemoteID, TicketKey: ref.Locator.Key, ObservedAt: now})
	}
	return out, nil
}
func githubRepo(container string) github.Repo {
	owner, name, _ := strings.Cut(container, "/")
	return github.Repo{Owner: owner, Name: name}
}
func mustTicketNumber(key string) int { n, _ := strconv.Atoi(key); return n }

// drainTicketObservations converts durable qualified signals into the existing
// session backlog and child discovery records. Clearing follows that handoff;
// generation checks keep observations received during the read pending.
func (d *Daemon) drainTicketObservations(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	if d.connections == nil {
		return nil
	}
	pending, err := d.db.ListPendingTicketObservations(ctx, 0)
	if err != nil {
		return err
	}
	verifiedContainers := make(map[int64]ticket.Identity)
	failedSources := make(map[int64]bool)
	pending = d.rotateTicketObservations(pending)
	for _, observation := range pending {
		if failedSources[observation.ConnectionVersionID] {
			continue
		}
		q := byRepo[observation.RepoID]
		if q == nil {
			continue
		}
		version, err := d.db.GetConnectionVersion(ctx, observation.ConnectionVersionID)
		if err != nil {
			return err
		}
		parent, err := d.observationParent(ctx, q, observation)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failedSources[observation.ConnectionVersionID] = true
			q.log.WarnContext(ctx, "child ticket observation remains pending", slog.Any("error", err))
			continue
		}
		if parent != nil {
			d.queueObservation(q, seen, parent, observation)
			continue
		}
		identity := ticket.Identity{Provider: observation.Provider, Origin: observation.Origin, RemoteID: observation.TicketID}
		session, sessionErr := d.db.GetSessionByTicket(ctx, identity)
		if sessionErr == nil && session.RepoID != observation.RepoID {
			// A source may be configured by multiple projects; its existing
			// binding remains owned by the project that admitted the session.
			continue
		}
		if observation.SubscriptionID != "" && errors.Is(sessionErr, store.ErrNotFound) {
			if !d.observationSubscriptionCurrent(ctx, observation.SubscriptionID, observation.SubscriptionRevision) {
				continue
			}
			if observation.RelatedSubscriptionID != "" && !d.observationSubscriptionCurrent(ctx, observation.RelatedSubscriptionID, observation.RelatedSubscriptionRevision) {
				continue
			}
		}
		current, err := d.db.CurrentConnection(ctx, observation.RepoID, store.TicketConnection)
		if err != nil {
			return err
		}
		readCtx := projectContext(ctx, observation.RepoID)
		targetQueue := q
		var actorID int64
		switch {
		case sessionErr == nil:
			snapshot, err := d.db.GetExecutionSnapshot(ctx, session.ID)
			if err != nil {
				return err
			}
			if snapshot.TicketVersionID != version.ID {
				original, e := d.db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
				if e != nil {
					return e
				}
				if original.Provider != observation.Provider || original.Origin != observation.Origin || original.Destination != observation.Container || snapshot.TicketRemoteID != observation.ContainerID {
					continue
				}
				version = original
			}
			actorID = snapshot.ActorID
			readCtx = sessionContext(ctx, session.ID)
			copyQueue := *q
			copyQueue.forgeRepo = githubRepo(version.Destination)
			targetQueue = &copyQueue
		case !errors.Is(sessionErr, store.ErrNotFound):
			return sessionErr
		case !current.Enabled || current.ID != version.ID:
			continue
		}
		container, verified := verifiedContainers[version.ID]
		if !verified {
			clients, e := d.connections.Clients(readCtx, version, actorID)
			if e != nil {
				failedSources[observation.ConnectionVersionID] = true
				q.log.WarnContext(ctx, "pending source credentials unavailable")
				continue
			}
			container, e = discoveryCall(readCtx, d, discoveryKey(targetQueue.forgeRepo, "pending-container", 0), 1, func(ctx context.Context) (ticket.Identity, error) {
				return clients.Tickets.ContainerIdentity(ctx, version.Destination)
			})
			if e != nil {
				failedSources[observation.ConnectionVersionID] = true
				q.log.WarnContext(ctx, "pending source container verification deferred", slog.Any("error", e))
				continue
			}
			verifiedContainers[version.ID] = container
		}
		if container.Provider != observation.Provider || container.Origin != observation.Origin || container.RemoteID != observation.ContainerID {
			continue
		}
		issue, err := discoveryCall(readCtx, d, discoveryKey(targetQueue.forgeRepo, "pending-ticket", mustTicketNumber(observation.TicketKey)), 1, func(ctx context.Context) (github.Issue, error) {
			return d.readIssue(ctx, targetQueue.forgeRepo, mustTicketNumber(observation.TicketKey))
		})
		if errors.Is(err, ErrDiscoveryDeferred) {
			continue
		}
		if errors.Is(err, github.ErrNotFound) {
			continue
		}
		if err != nil {
			q.log.WarnContext(ctx, "pending ticket reread deferred", slog.Any("error", err))
			continue
		}
		if strconv.FormatInt(issue.ID, 10) != observation.TicketID {
			continue
		}
		var c *candidate
		if sessionErr == nil {
			ref, err := naming.ParseRef(session.TaskRef)
			if err != nil {
				return err
			}
			c = targetQueue.candidate(session, ref)
			c.issue = issue
			c.resolved = true
		} else {
			c, err = d.sight(readCtx, targetQueue, issue)
			if err != nil {
				if errors.Is(err, store.ErrBindingConflict) || errors.Is(err, store.ErrConnectionConflict) || errors.Is(err, store.ErrTicketIdentityConflict) || errors.Is(err, store.ErrConnectionDisabled) {
					q.log.WarnContext(ctx, "ticket observation cannot be admitted through its saved source", slog.Any("error", err))
					continue
				}
				return err
			}
		}

		if c != nil {
			if err := d.db.SetSessionNeedsPoll(ctx, c.session.ID); err != nil {
				return err
			}
			d.queueObservation(q, seen, c, observation)
		} else if issue.State == "closed" {
			if err := d.db.ClearPendingTicketObservation(ctx, observation.ConnectionVersionID, observation.TicketID, observation.Generation); err != nil {
				return err
			}
		}

	}
	return nil
}

// Retained sessions always reread through their execution snapshots. Current
// connection disable or replacement blocks new admissions, not saved work.
func (d *Daemon) recoverRetainedSessions(ctx context.Context, q *repoQueue) error {
	current, err := d.db.CurrentConnection(ctx, q.repo.ID, store.TicketConnection)
	if err != nil {
		return err
	}
	sessions, err := d.db.ListRepoSessions(ctx, q.repo.ID)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		snapshot, err := d.db.GetExecutionSnapshot(ctx, session.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if current.Enabled && snapshot.TicketVersionID == current.ID {
			continue
		}
		if err := d.db.SetSessionNeedsPoll(ctx, session.ID); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daemon) queueObservation(q *repoQueue, seen map[int64]*candidate, c *candidate, observation store.PendingTicketObservation) {
	source := "ticket_recovery"
	if observation.SubscriptionID != "" {
		source = "webhook"
	}
	if previous := seen[c.session.ID]; previous != nil {
		previous.addSource(source)
		previous.observations = append(previous.observations, observation)
	} else {
		c.addSource(source)
		c.observations = append(c.observations, observation)
		seen[c.session.ID] = c
		q.candidates = append(q.candidates, c)
	}
}

func (d *Daemon) observationParent(ctx context.Context, q *repoQueue, observation store.PendingTicketObservation) (*candidate, error) {
	number := mustTicketNumber(observation.TicketKey)
	parent, err := d.db.ManagedParentForChild(ctx, q.repo.ID, number)
	if errors.Is(err, store.ErrNotFound) {
		parent, err = d.db.GraphPlanParentForChild(ctx, q.repo.ID, number)
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session, err := d.db.GetSessionByTaskRef(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot, err := d.db.GetExecutionSnapshot(ctx, session.ID)
	if err != nil {
		return nil, err
	}

	version, err := d.db.GetConnectionVersion(ctx, snapshot.TicketVersionID)
	if err != nil {
		return nil, err
	}
	if version.Provider != observation.Provider || version.Origin != observation.Origin || version.Destination != observation.Container || snapshot.TicketRemoteID != observation.ContainerID {
		return nil, nil
	}
	clients, err := d.connections.Clients(ctx, version, snapshot.ActorID)
	if err != nil {
		return nil, err
	}
	verified, err := discoveryCall(sessionContext(ctx, session.ID), d, discoveryKey(githubRepo(version.Destination), "recovery-child", number), 1, func(ctx context.Context) (ticket.Snapshot, error) {
		return clients.Tickets.Resolve(ctx, locatorAt(version.Origin, githubRepo(version.Destination), number))
	})
	if err != nil {
		return nil, err
	}
	if verified.Ref.Identity.RemoteID != observation.TicketID {
		return nil, store.ErrTicketIdentityConflict
	}
	if err := d.db.MarkStoryDiscoveryPending(ctx, session.ID, number); err != nil {
		return nil, err
	}
	if err := d.db.SetSessionNeedsPoll(ctx, session.ID); err != nil {
		return nil, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return nil, err
	}
	c := q.candidate(session, ref)
	c.forgeRepo = githubRepo(version.Destination)
	return c, nil
}

func (d *Daemon) clearTicketObservations(ctx context.Context, t *task) {
	for _, observation := range t.observations {
		if err := d.db.ClearPendingTicketObservation(ctx, observation.ConnectionVersionID, observation.TicketID, observation.Generation); err != nil {
			t.recordWorkerError(err)
			t.log.WarnContext(ctx, "ticket observation remains pending after drain", slog.Any("error", err))
		}
	}
}

// Each source gets a bounded round-robin pass. Failed or stale signals cannot
// permanently hide later tickets or another project's recovery observations.
// These offsets are scheduling hints only; durable generations own consumption.
func (d *Daemon) rotateTicketObservations(pending []store.PendingTicketObservation) []store.PendingTicketObservation {
	if d.pendingObservationCursor == nil {
		d.pendingObservationCursor = make(map[int64]int)
	}
	sources := make(map[int64][]store.PendingTicketObservation)
	var versions []int64
	for _, observation := range pending {
		if _, exists := sources[observation.ConnectionVersionID]; !exists {
			versions = append(versions, observation.ConnectionVersionID)
		}
		sources[observation.ConnectionVersionID] = append(sources[observation.ConnectionVersionID], observation)
	}
	slices.Sort(versions)
	var out []store.PendingTicketObservation
	for _, version := range versions {
		observations := sources[version]
		offset := d.pendingObservationCursor[version] % len(observations)
		count := min(recoveryPageSize, len(observations))
		for index := range count {
			out = append(out, observations[(offset+index)%len(observations)])
		}
		d.pendingObservationCursor[version] = (offset + count) % len(observations)
	}
	return out
}

// Saved sessions keep their original binding; new webhook admission must still
// match both the authenticating subscription and any related destination.
func (d *Daemon) observationSubscriptionCurrent(ctx context.Context, id string, revision int64) bool {
	sub, err := d.db.GetTicketSubscription(ctx, id)
	if err != nil || !sub.DesiredEnabled || sub.Revision != revision {
		return false
	}
	current, err := d.db.CurrentConnection(ctx, sub.RepoID, store.TicketConnection)
	return err == nil && current.Enabled && current.ID == sub.ConnectionVersionID && current.Provider == sub.Provider && current.Origin == sub.Origin && current.Destination == sub.Container
}
