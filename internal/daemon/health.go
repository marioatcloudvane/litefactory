package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// The mode words `daemon status` prints per repository.
const (
	// ModeWebhook: a valid delivery arrived since the daemon started.
	ModeWebhook = "webhook"
	// ModeWebhookIdle: a hook is registered and the listener is on, but nothing
	// has arrived yet.
	ModeWebhookIdle = "webhook, no delivery yet"
	// ModePollingOnly: the listener is off, or no hook is registered and nothing
	// has arrived.
	ModePollingOnly = "polling only"
)

// unannouncedMessage is the warning logged when a listing finds a change that no
// webhook announced.
const unannouncedMessage = "poll found a change no webhook announced; check the webhook path"

// webhookMode names how a repository is currently kept up to date.
func webhookMode(listening, delivered, hookRegistered bool) string {
	switch {
	case !listening:
		return ModePollingOnly
	case delivered:
		return ModeWebhook
	case hookRegistered:
		return ModeWebhookIdle
	default:
		return ModePollingOnly
	}
}

// healthPublisher writes the display-only health snapshot `daemon status`
// reads. It is driven from the tick goroutine only, never from the HTTP
// handler, and nothing in the daemon reads the snapshot back.
//
// The first publish after construction writes unconditionally, which resets the
// previous run's rows: a delivery counts "since start". After that a write
// happens at once when a fact other than the last heartbeat's start changed (or
// the previous write failed), and otherwise at most once per
// [healthRefreshInterval], which keeps the last heartbeat current.
type healthPublisher struct {
	mu        sync.Mutex
	startedAt time.Time
	started   bool
	signature string
	written   bool
	writtenAt time.Time
}

// healthRefreshInterval is the longest a snapshot whose facts have not changed
// goes without being rewritten, and the shortest between two such rewrites.
const healthRefreshInterval = time.Minute

// publishHealth collects the schedule, secret cache, recorded hooks and
// listener state and, when anything changed since the last successful write,
// replaces the stored snapshot. A failure is logged and retried on the next
// heartbeat; it never fails the tick.
func (d *Daemon) publishHealth(ctx context.Context, now time.Time) {
	listener, rows, err := d.collectHealth(ctx)
	if err != nil {
		d.log.WarnContext(ctx, "health snapshot not published; it is retried on the next heartbeat", slog.Any("error", err))
		return
	}

	p := &d.health
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started {
		p.started, p.startedAt = true, now
	}
	listener.DaemonStartedAt = p.startedAt
	listener.LastHeartbeatStartedAt = now
	pause := d.pause.snapshot()
	listener.DiscoveryPaused, listener.PauseReason = pause.Paused, pause.Reason
	listener.PausedSince, listener.ExpectedResumeAt = pause.Since, pause.ResumeAt
	if summary := d.scopedPauseSummary(now); summary != "" {
		listener.PauseReason = summary
	}
	listener.RunsInFlight = d.inflight.size()
	sig := healthSignature(listener, rows)
	if p.written && sig == p.signature && now.Sub(p.writtenAt) < healthRefreshInterval {
		return
	}
	listener.WrittenAt = now
	for i := range rows {
		rows[i].WrittenAt = now
	}
	if err := d.db.ReplaceHealthSnapshot(ctx, listener, rows); err != nil {
		d.log.WarnContext(ctx, "health snapshot not published; it is retried on the next heartbeat", slog.Any("error", err))
		return
	}
	p.written, p.signature, p.writtenAt = true, sig, now
}

func (d *Daemon) collectHealth(ctx context.Context) (store.ListenerStatus, []store.RepoDiscoveryStatus, error) {
	repos, err := d.db.ListRepos(ctx)
	if err != nil {
		return store.ListenerStatus{}, nil, fmt.Errorf("list repositories: %w", err)
	}
	var listener store.ListenerStatus
	if d.webhook != nil {
		listener.Listening = true
		listener.ListenAddress = d.webhook.ln.Addr().String()
	}
	if status := d.chatStatus.Load(); status != nil {
		listener.ChatStatus = *status
	}
	rows := make([]store.RepoDiscoveryStatus, 0, len(repos))
	for _, repo := range repos {
		if repo.Forge != store.ForgeGitHub {
			continue
		}
		registered, err := d.hookRegistered(ctx, repo.ID)
		if err != nil {
			return store.ListenerStatus{}, nil, err
		}
		snap, _ := d.schedule.Snapshot(repo.ID)
		row := store.RepoDiscoveryStatus{
			RepoID:             repo.ID,
			LastDeliveryAt:     snap.LastWebhook,
			LastPollFinishedAt: snap.LastListing,
			NextPollDueAt:      snap.NextDue,
			PollOwed:           snap.Owed,
			WebhookMode:        webhookMode(listener.Listening, !snap.LastWebhook.IsZero(), registered),
		}
		if d.webhook != nil {
			row.SecretPresent = d.webhook.hasProjectSecret(repo.ID, webhookKey(repo.Owner, repo.Name))
		}
		rows = append(rows, row)
	}
	return listener, rows, nil
}

// hookRegistered reports whether a hook is recorded for the repository.
func (d *Daemon) hookRegistered(ctx context.Context, repoID int64) (bool, error) {
	_, err := d.db.GetRepoWebhook(ctx, repoID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, store.ErrNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("read the recorded webhook of repository %d: %w", repoID, err)
	}
}

func healthSignature(l store.ListenerStatus, rows []store.RepoDiscoveryStatus) string {
	var b strings.Builder
	at := func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	fmt.Fprintf(&b, "%q %t %s %t %q %s %s %d %q", l.ListenAddress, l.Listening, at(l.DaemonStartedAt),
		l.DiscoveryPaused, l.PauseReason, at(l.PausedSince), at(l.ExpectedResumeAt), l.RunsInFlight, l.ChatStatus)
	for _, r := range rows {
		fmt.Fprintf(&b, "|%d %s %s %s %t %t %s", r.RepoID, at(r.LastDeliveryAt), at(r.LastPollFinishedAt),
			at(r.NextPollDueAt), r.PollOwed, r.SecretPresent, r.WebhookMode)
	}
	return b.String()
}

// warnUnannounced logs, once per repository per listing, that the listing found
// an issue changed after the repository's previous listing started although no
// webhook hint named it since. Issues in the overlap window were updated before
// that listing started and are never counted. It speaks only for repositories
// that are in webhook mode, and only when this daemon has finished an earlier
// listing of the repository, because the announced set starts empty with the
// process.
func (d *Daemon) warnUnannounced(ctx context.Context, q *repoQueue, issues []github.Issue, previous time.Time) {
	if previous.IsZero() || len(issues) == 0 || d.webhook == nil {
		return
	}
	snap, ok := d.schedule.Snapshot(q.repo.ID)
	if !ok || snap.LastListing.IsZero() {
		return
	}
	announced, complete := d.schedule.Announced(q.repo.ID)
	if !complete {
		return
	}
	var unannounced []int
	for _, issue := range issues {
		if !issue.UpdatedAt.After(previous) {
			continue
		}
		if _, hinted := announced[issue.Number]; hinted {
			continue
		}
		unannounced = append(unannounced, issue.Number)
	}
	if len(unannounced) == 0 {
		return
	}
	registered, err := d.hookRegistered(ctx, q.repo.ID)
	if err != nil {
		q.log.DebugContext(ctx, "not checking for unannounced changes", slog.Any("error", err))
		return
	}
	if webhookMode(true, !snap.LastWebhook.IsZero(), registered) == ModePollingOnly {
		return
	}
	slices.Sort(unannounced)
	attrs := []any{slog.Int("issue", unannounced[0])}
	if len(unannounced) > 1 {
		attrs = append(attrs, slog.Int("others", len(unannounced)-1))
	}
	q.log.WarnContext(ctx, unannouncedMessage, attrs...)
}
