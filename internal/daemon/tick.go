package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/schedule"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TickResult is what one tick did.
//
// It is returned so that a test can assert on a tick without reading log lines,
// and so that the loop has something to summarise.
//
// # Every field is written by the Tick goroutine and by nothing else (RD-5)
//
// A tick no longer performs the turns it finds; it discovers them and hands
// them to workers that outlive it. Counting what those workers went on to do —
// comments examined, replies posted, tasks bootstrapped — would mean either a
// mutex around this struct or a race. Neither is worth having: a mutexed
// counter read at the end of a tick is a snapshot of an unfinished computation,
// whose value depends on how the scheduler happened to interleave, and a test
// asserting on it would be asserting on timing.
//
// So the counters that described the *work* are gone, and what remains
// describes the *decision*, which is entirely the Tick goroutine's own: how
// many repositories were polled, how many issues came back, how many turns were
// dispatched, how many were left for the next tick. What the work did is
// asserted where it actually happens — on the forge, in the store, and in the
// executor the fixture substitutes.
type TickResult struct {
	// StartedAt is when the tick began, from the daemon's clock.
	StartedAt time.Time
	// Duration is how long it took. It is the duration of discovery and
	// dispatch, not of the turns dispatched — those are still running when it
	// is measured.
	Duration time.Duration

	// Skipped reports that the tick did nothing because a rate-limit backoff
	// was already in force when it began, and RetryAt is when ticks resume
	// (AC-2.12).
	//
	// It says nothing about a hold that armed *while* the tick was running —
	// a worker's 429 can do that, and [Daemon.offer] stops dispatching when it
	// does (AC-PAR-4.4). Such a tick polled, discovered and may have started
	// runs, so calling it skipped would be false; what it did not dispatch is
	// counted under Deferred instead.
	Skipped bool
	RetryAt time.Time

	// Repos is how many registered repositories were polled — one bounded,
	// logged listing each.
	//
	// That listing stopped being the whole per-tick API cost some time ago:
	// [Daemon.commentedIssues] adds a second one per repository, plus an issue
	// read for each number only it returns, and [Daemon.backlog] adds a third
	// source that costs no quota to discover. Backlog issue reads occur after
	// admission guards accept a candidate; released slots can admit further
	// reads, so worker concurrency is not a request budget. See "AC-2.8 — a tick's
	// API cost is no longer one listing per repository" in
	// .aiteam/AC_AMENDMENTS.md.
	Repos int
	// Issues is how many non-pull-request issues came back across them. It
	// counts what the forge listings returned, so a session reached only
	// through the local backlog is not one of them.
	Issues int
	// Hinted is how many issues a webhook named that this tick read fresh and
	// handed to admission as a candidate source.
	Hinted int
	// Dispatched is how many turns this tick handed to a worker.
	//
	// It is the old Runs counter under the name it always deserved: Runs was
	// incremented as soon as [turn.Execute] returned and before its disposition
	// was looked at, so it never meant "ran" — it meant "handed over". Now the
	// handing over is a `go`, and the count is taken by the goroutine doing it.
	Dispatched int
	// InFlight is how many turns this daemon had running when the tick finished
	// dispatching — the ones it just started plus any still going from an
	// earlier tick. It is a reading taken on the Tick goroutine, so it is a
	// number for the log rather than a promise about the next instant.
	InFlight int
	// Deferred is how many eligible sessions this tick took out of the eligible
	// set and did not run — because the pool had no free slot, or because a
	// rate-limit backoff armed under the dispatch loop. They are not queued:
	// each keeps its backlog flag and is reconsidered from scratch, in rotation
	// order, by the next tick.
	Deferred int

	// Scheduled is the number of outstanding pending schedule rows after this
	// tick's dispatch decision. It is distinct from Deferred: deferred work is
	// ordinary session work that missed current capacity, while scheduled work
	// is durable future work that has not reached a terminal state.
	Scheduled int

	// Cursor is the poll cursor as it stands after the tick. A failed tick
	// leaves it as it was found, which is what makes the next one redo the work
	// (AC-2.13).
	Cursor store.PollCursor
}

// Tick runs one poll cycle: discover what has work, then dispatch as much of it
// as there are free slots for.
//
// It is exported and synchronous on purpose — the loop is a thin wrapper around
// it — but "synchronous" now covers the decision only. A tick returns as soon
// as it has dispatched, without waiting for any turn it started, which is the
// whole point of the split: a thirty-minute run on issue #7 no longer stops
// issue #8 being noticed thirty seconds later (AC-PAR-1.1).
//
// What a test asserts on therefore divides in two. What the tick decided is in
// the returned [TickResult] the instant this returns. What the turns did is
// visible once they have finished, which is what [Daemon.Quiesce] is for.
//
// An error means the tick did not complete: discovery failed, or shutdown
// arrived. The poll cursor is not persisted, so the next tick asks for the same
// window again; every watermark already advanced stays advanced, because a
// watermark only moves after the comment it covers has genuinely been dealt
// with.
func (d *Daemon) Tick(ctx context.Context) (result TickResult, err error) {
	hb := &heartbeat{source: wakeSourceFrom(ctx), counts: &discoveryCounts{}}
	startedAt := d.now()
	d.quotas.tick(startedAt)
	hb.startedAt = startedAt
	// Last, whatever path the tick leaves by: the heartbeat line and the health
	// snapshot are reporting only and never change the tick's result.
	defer func() {
		d.finishHeartbeat(ctx, hb, result, err)
		d.publishHealth(ctx, startedAt)
	}()
	d.discovery.tick(startedAt)
	ctx = d.discoveryContext(ctx)
	counts := hb.counts
	ctx = github.WithRequestObserver(ctx, counts.observe)
	defer d.discoverySummary(ctx, "tick", counts)
	ctx = context.WithValue(ctx, recoveryTickKey{}, startedAt)
	ctx, checks := withContainerChecks(ctx)
	defer checks.close()
	d.doctorTick(ctx, startedAt)
	d.refreshRecoveries(ctx, startedAt)
	// Stopped execution debt is independent of discovery/storage availability.
	stoppedDispatched := d.retryStoppedResults(ctx, startedAt)
	if err := d.reconcileGraphDispatches(ctx, startedAt); err != nil {
		return TickResult{StartedAt: startedAt}, err
	}
	if !d.autoResumeOnSessionLimit {
		pending, err := d.db.ListPendingContinuations(ctx, 0)
		if err != nil {
			return TickResult{StartedAt: startedAt}, err
		}
		for _, c := range pending {
			if err := d.db.CancelContinuation(ctx, c.ID, "automatic recovery disabled; resume manually", startedAt); err != nil {
				return TickResult{StartedAt: startedAt}, err
			}
		}
	}

	// Local capability expiry continues even while remote publication backs off.
	if e, ok := d.turn.(turnExecutor); ok && e.opts.MCP != nil {
		if err := e.opts.MCP.CleanActivities(ctx, startedAt); err != nil {
			d.log.ErrorContext(ctx, "activity cleanup remains pending", slog.Any("error", err))
		}
	}

	d.refreshWebhookSecrets(ctx, startedAt)

	if retryAt, held := d.connectionHold(ctx, startedAt); held {
		d.log.DebugContext(ctx, "skipping the tick; still backing off from a rate limit",
			slog.Time("retry_at", retryAt.UTC()),
			slog.String("remaining", retryAt.Sub(startedAt).String()))
		d.owePendingListings(ctx, startedAt)
		return TickResult{StartedAt: startedAt, Skipped: true, RetryAt: retryAt}, nil
	}

	if err := d.pollPublications(ctx, startedAt); err != nil {
		d.log.ErrorContext(ctx, "publication observation remains pending", slog.Any("error", err))
		if providerRateLimited(err) {
			if d.connections == nil {
				return TickResult{StartedAt: startedAt}, d.observe(ctx, startedAt, err)
			}
			d.rateLimited(ctx, d.log, startedAt, err)
		}
	}
	// Progress is reconciled even while the sole turn worker is occupied.
	if progress, ok := d.turn.(interface{ PollStoryProgress(context.Context) error }); ok {
		if err := progress.PollStoryProgress(ctx); err != nil {
			d.log.ErrorContext(ctx, "story progress remains pending", slog.Any("error", err))
			if providerRateLimited(err) {
				if d.connections == nil {
					return TickResult{StartedAt: startedAt}, d.observe(ctx, startedAt, err)
				}
				d.rateLimited(ctx, d.log, startedAt, err)
			}
		}
	}

	// Code-host controls have their own sources and must be polled even when
	// work-management replies come from Linear.
	if err := d.PollCodeDeliveryControls(ctx); err != nil {
		d.log.ErrorContext(ctx, "code delivery controls remain pending", slog.Any("error", err))
		if providerRateLimited(err) {
			if d.connections == nil {
				return TickResult{StartedAt: startedAt}, d.observe(ctx, startedAt, err)
			}
			d.rateLimited(ctx, d.log, startedAt, err)
		}
	}

	// Proposals are advice, not work. Expiring them on the tick clock keeps a
	// stale approval from becoming actionable after a daemon restart without
	// adding a timer or a background worker.
	if _, err := d.db.ExpireScheduleProposals(ctx, startedAt.Add(-proposalLifetime), startedAt); err != nil {
		return TickResult{StartedAt: startedAt}, fmt.Errorf("expire schedule proposals: %w", err)
	}

	// AC-2.9: the very first tick ever plants the cursor at now rather than at
	// the beginning of history, so registering a busy repository does not
	// replay years of issues. Doing it in the store, conditionally, keeps the
	// window in which a restart could push the cursor forward closed.
	cursor, err := d.db.InitPollCursor(ctx, startedAt)
	if err != nil {
		return TickResult{StartedAt: startedAt}, err
	}
	repos, err := d.db.ListRepos(ctx)
	if err != nil {
		return TickResult{StartedAt: startedAt, Cursor: cursor}, err
	}

	result = TickResult{StartedAt: startedAt, Repos: len(repos), Cursor: cursor, Dispatched: stoppedDispatched}

	if err := d.replayContinuationCancellations(ctx, repos); err != nil {
		return result, d.observe(ctx, startedAt, err)
	}
	if err := d.replayInterruptionReports(ctx, repos); err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, d.observe(ctx, startedAt, err)
	}
	if d.connections != nil {
		for _, project := range repos {
			if err := d.db.RecoverConsumedProjectAuthorityClaims(ctx, project.ID); err != nil {
				return result, d.observe(ctx, startedAt, err)
			}
		}
	}

	d.researchReminders(ctx, startedAt)

	// The heartbeat runs every local source each time; only the forge listing is
	// scheduled. A tick that does not list still dispatches and reconciles.
	if d.webhook != nil && d.webhook.hints.takeOverflow() {
		// Hints were dropped; only a full listing can recover what they named.
		d.schedule.ForceDue()
	}
	for _, id := range d.schedule.Known() {
		if !slices.ContainsFunc(repos, func(r store.Repo) bool { return r.ID == id }) {
			d.schedule.Forget(id)
		}
	}
	queues, err := d.discover(ctx, repos, startedAt, &result)
	defer finishCandidateReservations(queues)
	discoveryDeferred := errors.Is(err, ErrDiscoveryDeferred)
	hb.deferred = discoveryDeferred
	for _, q := range queues {
		if q.listing && q.finished {
			hb.polled++
		}
	}
	if err != nil && !discoveryDeferred {
		result.Duration = d.now().Sub(startedAt)
		return result, d.observe(ctx, startedAt, err)
	}

	// The context handed to every worker is this one — the daemon's, which
	// lives as long as the process does. It is deliberately not wrapped in a
	// WithTimeout or a WithCancel first: a tick-scoped context given to a
	// goroutine that outlives the tick would cancel every run at the instant
	// discovery finished. The bound on a worker is not this context but the
	// deadline on every call it makes (git, claude, the forge), and shutdown
	// reaches it because cancelling *this* context cancels all of them at once.
	reposByID := make(map[int64]store.Repo, len(repos))
	for _, repo := range repos {
		reposByID[repo.ID] = repo
	}
	// Below the free-disk threshold no run starts this tick, exactly as with a
	// full pool: owed work stays pending and starts on a later tick.
	lowDisk := d.lowDisk(ctx)
	if !lowDisk {
		if err := d.dispatchContinuations(ctx, reposByID, startedAt, &result); err != nil {
			return result, err
		}
		if err := d.dispatchSchedules(ctx, reposByID, startedAt, &result); err != nil {
			result.Duration = d.now().Sub(startedAt)
			return result, err
		}
	}
	if err := d.replayContinuationCancellations(ctx, repos); err != nil {
		return result, d.observe(ctx, startedAt, err)
	}
	if lowDisk {
		d.deferRest(ctx, queues, &result)
	} else {
		d.dispatch(ctx, queues, startedAt, &result)
		// Owed graph work goes last. An owner is waiting on a comment; nobody is
		// waiting on a stage the factory owes itself, and a session claimed by
		// discovery above keeps its dispatch pending for the next tick rather than
		// competing for the same worker.
		if err := d.dispatchGraphSteps(ctx, reposByID, startedAt, &result); err != nil {
			return result, err
		}
	}
	result.InFlight = d.inflight.size()
	result.Scheduled, err = d.db.CountPendingSchedules(ctx)
	if err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, fmt.Errorf("count outstanding scheduled work: %w", err)
	}

	// Retirement runs after normal admission and never blocks a full scan here.
	if retirementErr := d.RunRetirementBatch(ctx, startedAt); retirementErr != nil {
		d.log.ErrorContext(ctx, "retirement batch incomplete", slog.Bool("completed", false))
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
	}

	// The archive pass follows retirement and is bounded the same way: one owner
	// per tick, after normal admission, held back by the same discovery backoff.
	if archiveErr := d.RunArchiveBatch(ctx, startedAt); archiveErr != nil {
		d.log.ErrorContext(ctx, "archive batch incomplete", slog.Bool("completed", false))
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
	}

	// Local housekeeping runs last and never fails a tick.
	d.RunHousekeeping(ctx, startedAt)

	// The cursor is a pure watermark now: it advances to this tick's start time
	// on every tick that completed, whatever any worker went on to do (T-PAR-13).
	// Holding it back to the oldest issue that did not finish was the only way
	// to re-find that issue while there was no local record of one, and it
	// pinned the whole fleet — one busy repository rewound discovery for every
	// other. sessions.needs_poll is that local record, and [Daemon.discover]
	// unions it with the listings, so re-discovery no longer needs the cursor's
	// help. See [store.DB.ListBacklogSessions].
	// Each repository's window moves on its own, only when that repository's
	// listing finished: a deferred neighbour does not hold it back and a
	// webhook or heartbeat never moves it.
	if discoveryDeferred {
		finishedAt := d.now()
		result.Duration = finishedAt.Sub(startedAt)
		if err := d.finishListings(ctx, queues, startedAt, finishedAt); err != nil {
			return result, err
		}
		return result, nil
	}
	// The global row keeps only the completion time `daemon status` reports;
	// its tasks_updated_since column is no longer read.
	persisted := store.PollCursor{TasksUpdatedSince: cursor.TasksUpdatedSince, LastTickAt: d.now()}
	if err := d.finishListings(ctx, queues, startedAt, persisted.LastTickAt); err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, err
	}
	if err := d.db.SetPollCursor(ctx, persisted); err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, err
	}
	result.Cursor = persisted
	result.Duration = result.Cursor.LastTickAt.Sub(startedAt)

	d.log.InfoContext(ctx, "tick complete",
		slog.Int("repos", result.Repos),
		slog.Int("issues", result.Issues),
		slog.Int("dispatched", result.Dispatched),
		slog.Int("in_flight", result.InFlight),
		slog.Int("workers_dispatched", result.Dispatched),
		slog.Int("workers_in_flight", result.InFlight),
		slog.Int("forge_issues", result.Issues),
		slog.Int("deferred_sessions", result.Deferred),
		slog.Int("scheduled", result.Scheduled),
		slog.Time("cursor", startedAt.UTC()),
		slog.String("duration", result.Duration.String()))
	return result, nil
}

// ListingOverlap is how far back before the poll cursor each tick asks.
//
// The forge's listings are eventually consistent, and the cursor is monotonic:
// it advances to the tick's start time whether or not the index had caught up.
// With no overlap those two facts combine into permanent loss. Measured on
// issue #3 of this repository on 2026-08-25:
//
//	11:40:37Z  owner comments
//	11:40:56Z  tick since=11:40:26Z -> issues=0   the index has not caught up
//	11:41:26Z  tick since=11:40:56Z -> issues=0   the cursor is already past it
//	           … and it never comes back. Sixty ticks, thirty minutes, nothing.
//
// The comment surfaced seven hours later, when the issue was closed by hand and
// its update time moved for a second time.
//
// Five minutes is far beyond any lag observed and costs nothing to re-ask for:
// the per-task watermark is a monotonic MAX over comment ids, so a comment
// listed twice is compared and dropped. Re-listing is cheap; losing a comment
// is not.
const ListingOverlap = 5 * time.Minute

// repoWindow is the `since` one repository's listing asks the forge for: its
// own cursor less [ListingOverlap]. A repository with no cursor row has never
// had a listing finish, so its history starts at its registration; the overlap
// is withheld there because nothing before registration is wanted.
//
// previous is when the previous listing of the repository started (its cursor);
// zero when there is none.
func (d *Daemon) repoWindow(ctx context.Context, repo store.Repo) (since, previous time.Time, err error) {
	at, ok, err := d.db.GetRepoPollCursor(ctx, repo.ID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !ok {
		return repo.CreatedAt, time.Time{}, nil
	}
	return at.Add(-ListingOverlap), at, nil
}

// owePendingListings marks every due repository as owed a listing. A tick that
// is skipped (rate-limit backoff) never reaches discover; the flag records the
// outstanding listing until it finishes.
func (d *Daemon) owePendingListings(ctx context.Context, now time.Time) {
	repos, err := d.db.ListRepos(ctx)
	if err != nil {
		d.log.WarnContext(ctx, "could not list repositories to mark their listings owed", slog.Any("error", err))
		return
	}
	for _, repo := range repos {
		if d.schedule.Due(repo.ID, now) {
			d.schedule.ListingFailed(repo.ID)
		}
	}
}

// finishListings records every repository whose listing completed this tick:
// the cursor first, then the schedule, so a failed write leaves the repository
// owed and listing from where it was.
func (d *Daemon) finishListings(ctx context.Context, queues []*repoQueue, startedAt, finishedAt time.Time) error {
	for _, q := range queues {
		if !q.finished {
			continue
		}
		if d.connections == nil {
			if err := d.db.SetRepoPollCursor(ctx, q.repo.ID, startedAt); err != nil {
				return err
			}
		}
		d.schedule.ListingFinished(q.repo.ID, finishedAt)
	}
	return nil
}

// discover walks every registered repository and every session the store says
// still owes the daemon a look, and returns what is eligible to run, grouped by
// repository (D-A, AC-2.8).
//
// It is the first of the two phases a tick is made of, and it runs to
// completion before anything is dispatched. Dispatching as it discovered — what
// this function used to do, inside the `for _, repo := range repos` below — is
// unconditionally starving with a daemon-wide cap: repository 1 fills every
// slot before repository 2's listing has even been issued. Separating the walk
// from the decision is what makes the round-robin in [Daemon.dispatch] possible;
// reordering the loop could not have.
//
// One listing per repository, so the tick's API cost is O(registered repos) and
// each call logs what it cost — that is the criterion, not "one API call".
// Issue *search* would collapse it into one request and was dropped: its query
// string caps at 256 characters, it is eventually consistent, and it runs on a
// thirtieth of the quota.
//
// An error here ends the tick. Everything this function does is on the Tick
// goroutine, before any task has been touched: the cursor is not persisted and
// no watermark has moved, so there is nothing to undo.
func (d *Daemon) discover(
	ctx context.Context,
	repos []store.Repo,
	now time.Time,
	result *TickResult,
) ([]*repoQueue, error) {
	// cleared makes the resume happen at most once per tick, on the first
	// listing that answers. Everything in this loop runs on the Tick goroutine,
	// which is what makes an unguarded bool the right thing here — and which is
	// also why [backoff.clear] is called from here and nowhere below.
	cleared := false
	deferred := false

	queues := make([]*repoQueue, 0, len(repos))
	byRepo := make(map[int64]*repoQueue, len(repos))
	seen := make(map[int64]*candidate)
	transferred := false
	defer func() {
		if !transferred {
			finishCandidateReservations(queues)
		}
	}()

	// Which repositories list this tick is decided before any of them is marked
	// owed. An attempt is owed until it finishes, so a tick that fails midway
	// leaves the repository due whatever webhooks arrive meanwhile.
	due := make(map[int64]bool, len(repos))
	for _, repo := range repos {
		if repo.Forge == store.ForgeGitHub && d.schedule.Due(repo.ID, now) {
			due[repo.ID] = true
			d.schedule.ListingFailed(repo.ID)
		}
	}

	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// One child logger per repository, derived once here and handed down, so
		// that every line below this point says which repository it came from
		// without each call site restating it (AC-PAR-7.1). It is derived above
		// the forge check rather than below it because owner/name identifies a
		// repository on any forge, and the line that refuses one wants it too.
		repoLog := d.log.With(slog.String("repo", repo.Owner+"/"+repo.Name))

		if repo.Forge != store.ForgeGitHub {
			// M0 supports one forge. A row from a future migration is skipped
			// loudly rather than sent to the GitHub client.
			repoLog.WarnContext(ctx, "skipping a repository on an unsupported forge",
				slog.String("path", repo.Path), slog.String("forge", repo.Forge))
			continue
		}

		ctx := ctx
		forgeRepo := github.Repo{Owner: repo.Owner, Name: repo.Name}
		connectionEnabled := true
		if d.connections != nil {
			ctx = projectContext(ctx, repo.ID)
			v, e := d.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
			if e != nil {
				repoLog.WarnContext(ctx, "project ticket connection unavailable", slog.Any("error", e))
				continue
			}
			owner, name, _ := strings.Cut(v.Destination, "/")
			forgeRepo = github.Repo{Owner: owner, Name: name}
			connectionEnabled = v.Enabled
			if state, e := d.db.GetProjectImportState(ctx, repo.ID); e == nil && state.Status == "conflict" {
				connectionEnabled = false
			}
		}
		q := &repoQueue{repo: repo, forgeRepo: forgeRepo, log: repoLog}
		queues = append(queues, q)
		byRepo[repo.ID] = q

		if d.connections != nil && d.schedule.Due(repo.ID, now) {
			if err := d.recoverRetainedSessions(ctx, q); err != nil {
				return nil, err
			}
		}
		if !due[repo.ID] || !connectionEnabled {
			if due[repo.ID] && !connectionEnabled {
				q.finished = true
			}
			continue
		}
		if d.connections != nil {
			q.listing = true
			if err := d.discoveryProbe(ctx, forgeRepo); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if errors.Is(err, ErrDiscoveryDeferred) {
					deferred = true
				}
				if !d.scopedQuotaDeferred(ctx, err) {
					repoLog.WarnContext(ctx, "ticket source quota or access probe deferred", slog.Any("error", err))
				}
				if providerRateLimited(err) {
					d.rateLimited(ctx, repoLog, now, err)
					continue
				}
				continue
			}
			finished, err := d.recoverSource(ctx, q, now)
			if errors.Is(err, ErrDiscoveryDeferred) {
				deferred = true
				continue
			}
			if err != nil {
				repoLog.WarnContext(ctx, "ticket recovery remains pending", slog.Any("error", err))
				if providerRateLimited(err) {
					d.rateLimited(ctx, repoLog, now, err)
					continue
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}
			q.finished = finished
			continue
		}
		q.listing = true
		since, previous, err := d.repoWindow(ctx, repo)
		if err != nil {
			return nil, err
		}
		repoDeferred := false
		if err := d.discoveryProbe(ctx, forgeRepo); err != nil {
			return nil, err
		}
		issues, err := discoveryCall(ctx, d, discoveryKey(forgeRepo, "issues", 0), discoveryListBound, func(ctx context.Context) ([]github.Issue, error) { return d.discoverIssues(ctx, forgeRepo, since) })
		if errors.Is(err, ErrDiscoveryDeferred) {
			deferred, repoDeferred = true, true
		} else if err != nil {
			return nil, fmt.Errorf("list the issues of %s touched since %s: %w",
				forgeRepo, since.UTC().Format(time.RFC3339), err)
		}
		if err == nil && !cleared {
			// Evidence that the forge is answering again, which is the only thing
			// that ends a run of refusals. See [backoff.clear] for why this is
			// here rather than at the end of the tick.
			cleared = true
			if held, wasHeld := d.backoff.clear(); wasHeld {
				repoLog.InfoContext(ctx, "resumed polling after a rate-limit backoff",
					slog.String("held_for", held.String()))
			}
		}
		commented, err := d.commentedIssues(ctx, repoLog, q, since, issues)
		if errors.Is(err, ErrDiscoveryDeferred) {
			deferred, repoDeferred = true, true
		} else if err != nil {
			return nil, err
		}
		issues = append(issues, commented...)
		result.Issues += len(issues)
		if !repoDeferred {
			d.warnUnannounced(ctx, q, issues, previous)
		}

		for _, issue := range issues {
			c, err := d.sight(ctx, q, issue)
			if err != nil {
				return nil, err
			}
			if c == nil {
				continue
			}
			if existing := seen[c.session.ID]; existing != nil {
				existing.addSource("forge_discovery")
				continue
			}
			c.addSource("forge_discovery")
			seen[c.session.ID] = c
			q.candidates = append(q.candidates, c)
		}
		q.finished = !repoDeferred
	}

	if err := d.webhookHints(ctx, byRepo, seen, result); err != nil {
		return nil, err
	}
	if err := d.drainTicketObservations(ctx, byRepo, seen); err != nil {
		return nil, err
	}
	if err := d.graphPlanParents(ctx, byRepo, seen); err != nil {
		return nil, err
	}
	if err := d.graphPlanStories(ctx, byRepo, seen); err != nil {
		return nil, err
	}
	if err := d.graphImplementStories(ctx, byRepo, seen, now); err != nil {
		return nil, err
	}
	if err := d.graphValidateStarts(ctx, byRepo, seen); err != nil {
		return nil, err
	}
	if err := d.managedParents(ctx, byRepo, seen); err != nil {
		return nil, err
	}
	if err := d.graphOwnerWaits(ctx, byRepo, seen); err != nil {
		return nil, err
	}
	if err := d.backlog(ctx, byRepo, seen); err != nil {
		return nil, err
	}

	// Ascending session id within a repository, because that is the order the
	// per-repo session cursor rotates through and it has to be the same order on
	// every tick. It cannot come from the listings: the forge returns issues by
	// update time, and the backlog union appends to the end.
	for _, q := range queues {
		slices.SortFunc(q.candidates, func(a, b *candidate) int {
			return cmp.Compare(a.session.ID, b.session.ID)
		})
	}
	transferred = true
	if deferred {
		return queues, ErrDiscoveryDeferred
	}
	return queues, nil
}

// backlog adds sessions the store says are still owed a look, or records that
// source on candidates already discovered elsewhere (RD-2).
//
// This is the local half of discovery and it costs no API quota at all: one
// indexed query over the sessions carrying needs_poll. It is what replaces
// holding the poll cursor back — the cursor is now monotonic, so an issue busy
// for longer than [ListingOverlap] is no longer re-listed by timestamp, and
// this is how it is found again (AC-PAR-5.2).
//
// A backlogged session arrives here without its [github.Issue], and stays that
// way: the fetch happens in [Daemon.materialise], only for a session the
// dispatcher has actually taken a slot for. This avoids reads for candidates
// already in flight or refused capacity. Skips and errors release that slot,
// so concurrency does not bound the number of issue reads in a tick.
//
// A session whose repository is not registered any more, or is on a forge this
// build does not support, is left alone: there is nothing that could be done
// with it, and its flag is harmless where it stands.
func (d *Daemon) backlog(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	sessions, err := d.db.ListBacklogSessions(ctx)
	if err != nil {
		return fmt.Errorf("list the sessions that still need a poll: %w", err)
	}
	for _, session := range sessions {
		if existing := seen[session.ID]; existing != nil {
			existing.addSource("backlog")
			continue
		}
		q := byRepo[session.RepoID]
		if q == nil {
			d.log.DebugContext(ctx, "a session still needs a poll but its repository is no longer "+
				"pollable; leaving its backlog flag where it stands",
				slog.Int64("session_id", session.ID),
				slog.Int64("repo_id", session.RepoID))
			continue
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return fmt.Errorf("task ref %q on task %d: %w", session.TaskRef, session.ID, err)
		}
		c := q.candidate(session, ref)
		c.addSource("backlog")
		seen[session.ID] = c
		q.candidates = append(q.candidates, c)
	}
	return nil
}

// sight turns one issue from a listing into a candidate, or into nothing.
//
// # A closed issue is left alone
//
// Closing an issue is how litefactory is told to stop working on it, so an
// issue that is not open is skipped here (AC-2.22). Here, because this is where
// the two discovery paths meet: `state=open` on the issue listing covers one of
// them, but [Daemon.commentedIssues] reaches an issue through GetIssue, which
// never sees that query — a filter on the query alone would leave the faster
// path wide open.
//
// And *before* [store.DB.GetOrCreateSession], so that a comment on an issue
// closed two months ago leaves no sessions row and no watermarks row behind.
// A skip that first created the task it was refusing to work on would put rows
// in the database for every closed issue anyone ever comments on. The backlog
// path re-checks the same thing in [Daemon.materialise], where the session
// already exists and the only question left is whether to run.
func (d *Daemon) sight(ctx context.Context, q *repoQueue, issue github.Issue) (*candidate, error) {
	if issue.QualifiedSnapshot != nil && issue.QualifiedSnapshot.Ref.Identity.Provider == ticket.Linear && d.connections != nil {
		ref := issue.QualifiedSnapshot.Ref
		version, err := d.db.CurrentConnection(ctx, q.repo.ID, store.TicketConnection)
		if err != nil {
			return nil, err
		}
		parent, err := d.observationParent(ctx, q, store.PendingTicketObservation{RepoID: q.repo.ID, ConnectionVersionID: version.ID, Provider: ref.Identity.Provider, Origin: ref.Identity.Origin, Container: ref.Locator.Container, ContainerID: version.Linear.ProjectID, TicketID: ref.Identity.RemoteID, TicketKey: ref.Locator.Key})
		if err != nil || parent != nil {
			return parent, err
		}
	}
	if parent, err := d.db.ManagedParentForChild(ctx, q.repo.ID, issue.Number); err == nil {
		same, e := d.sameTicketSource(ctx, q.repo.ID, parent)
		if e != nil {
			return nil, e
		}
		if same {
			return nil, nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err := d.markPlanChild(ctx, q, issue.Number); err != nil {
		return nil, err
	}
	if issue.State != github.StateOpen {
		q.log.DebugContext(ctx, "skipping an issue that is not open; closing an issue is how "+
			"litefactory is told to stop working on it",
			slog.Int("issue", issue.Number),
			slog.String("state", issue.State))
		return nil, nil
	}

	var session store.Session
	var err error
	if d.connections == nil {
		session, err = d.db.GetOrCreateSession(ctx, q.repo, issue.Number)
	} else {
		ctx = projectContext(ctx, q.repo.ID)
		v, e := d.db.CurrentConnection(ctx, q.repo.ID, store.TicketConnection)
		if e != nil {
			return nil, e
		}
		// The issue was observed before this current-version read. An edit
		// between discovery and admission must not qualify old input with the
		// replacement origin, even if both sources reuse the same numeric ID.
		observedPath, sameOrigin := strings.CutPrefix(issue.APIURL, string(v.Origin)+"/repos/")
		observedParts := strings.SplitN(observedPath, "/", 3)
		owner, name, _ := strings.Cut(v.Destination, "/")
		if v.Provider == ticket.GitHub && (q.forgeRepo.String() != v.Destination || !sameOrigin || len(observedParts) != 3 ||
			!strings.EqualFold(observedParts[0], owner) || !strings.EqualFold(observedParts[1], name) ||
			observedParts[2] != "issues/"+strconv.Itoa(issue.Number)) {
			return nil, store.ErrConnectionConflict
		}
		ref := ticket.Ref{Identity: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: strconv.FormatInt(issue.ID, 10)}, Locator: locatorAt(v.Origin, q.forgeRepo, issue.Number)}
		if v.Provider == ticket.Linear {
			if issue.QualifiedSnapshot == nil {
				return nil, store.ErrTicketIdentityConflict
			}
			ref = issue.QualifiedSnapshot.Ref
			if ref.Identity.Provider != v.Provider || ref.Identity.Origin != v.Origin || ref.Locator.Container != v.Destination {
				return nil, store.ErrConnectionConflict
			}
		}
		binding, e := d.db.GetTicketBinding(ctx, ref.Identity)
		if errors.Is(e, store.ErrNotFound) {
			_, e = d.db.AddTicketBinding(ctx, store.TicketBinding{RepoID: q.repo.ID, ConnectionVersionID: v.ID, Ref: ref, Enabled: true, Provenance: "discovery"})
		} else if e == nil && (binding.RepoID != q.repo.ID || !binding.Enabled) {
			e = store.ErrBindingConflict
		}
		if e != nil {
			return nil, e
		}
		session, err = AdmitProjectTicket(ctx, d.db, d.connections, q.repo, ref)
	}

	if err != nil {
		return nil, fmt.Errorf("find the task for %s#%d: %w", q.forgeRepo, issue.Number, err)
	}
	if err := d.bindObservedIssue(ctx, session.ID, q.forgeRepo, issue); err != nil {
		return nil, err
	}
	// Parsed on the Tick goroutine, like everything else that names a task: the
	// ref is what a task's logger is decorated with, and a worker that had to
	// derive its own identity before it could report a failure would have
	// nothing to report it under.
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return nil, fmt.Errorf("task ref %q on task %d: %w", session.TaskRef, session.ID, err)
	}
	// The issue is open, and the listing that produced it is fresh. That is the
	// reopen path for [DB.SetManagedParentClosed]: reopening moves the issue's
	// timestamp, so the listing returns it and it arrives here. One indexed
	// write on a primary key, and a no-op for everything that is not a managed
	// parent, which is why it is unconditional rather than preceded by a read.
	if err := d.db.SetManagedParentClosed(ctx, ref.String(), false); err != nil {
		return nil, fmt.Errorf("restore the managed registry entry for reopened %s#%d: %w",
			q.forgeRepo, issue.Number, err)
	}

	c := q.candidate(session, ref)
	c.issue = issue
	c.resolved = true
	return c, nil
}

// markPlanChild records that a graph Plan draft child was named by a listing or
// a hint, so its parent's next worker sweeps it. [Daemon.refinePlanChildren]
// reads only children owed a sweep. Not a graph Plan draft child is a no-op.
func (d *Daemon) markPlanChild(ctx context.Context, q *repoQueue, number int) error {
	parent, err := d.db.GraphPlanParentForChild(ctx, q.repo.ID, number)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	same, e := d.sameTicketSource(ctx, q.repo.ID, parent)
	if e != nil {
		return e
	}
	if !same {
		return nil
	}
	session, err := d.db.GetSessionByTaskRef(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return d.db.MarkStoryDiscoveryPending(ctx, session.ID, number)
}

// candidate is one session discovery found eligible, before the dispatcher has
// decided whether to run it.
//
// It is the discovery phase's output and the dispatch phase's input, and it
// exists rather than a [task] because half of a candidate's issue may not have
// been fetched yet: a session reached only through the backlog has no
// [github.Issue] until [Daemon.materialise] reads one for it.
type candidate struct {
	// Owned by discovery until offer transfers it to the worker.
	discoveryReservation *discoveryReservation
	observations         []store.PendingTicketObservation
	// sources are ordered discovery evidence, not proof of actionable work.
	sources   []string
	repo      store.Repo
	forgeRepo github.Repo
	session   store.Session
	ref       naming.Ref

	// log is the task's own child logger, carrying the canonical ref and the
	// session id (AC-PAR-7.1). It is derived here rather than in the worker so
	// that every line about this task — including the ones saying it was skipped
	// or deferred and so never became a task at all — is attributable.
	log *slog.Logger

	// issue is the forge's view of the issue, and resolved says whether it has
	// been read. Discovery through a listing already has it in hand; discovery
	// through the backlog does not, and [Daemon.materialise] is what fills it in.
	issue    github.Issue
	resolved bool
}

// addSource keeps the first-seen source order without changing candidate order.
func (c *candidate) addSource(source string) {
	if !slices.Contains(c.sources, source) {
		c.sources = append(c.sources, source)
	}
}

// repoQueue is one repository's eligible sessions plus the rotation state used
// to walk them.
//
// Candidates are held ascending by session id, and next is where the per-repo
// session cursor says this tick should start reading. taken counts how many
// have been handed out, so the walk wraps once and stops rather than looping.
type repoQueue struct {
	repo      store.Repo
	forgeRepo github.Repo
	log       *slog.Logger

	candidates []*candidate
	next       int
	taken      int

	// listing says this tick attempted the repository's forge listing and
	// finished says it completed without being deferred; only then do the
	// repository's cursor and schedule move.
	listing  bool
	finished bool
}

// candidate builds a candidate belonging to this repository, with no issue read
// for it yet.
func (q *repoQueue) candidate(session store.Session, ref naming.Ref) *candidate {
	return &candidate{
		repo:      q.repo,
		forgeRepo: q.forgeRepo,
		session:   session,
		ref:       ref,
		// The per-task child, derived from the repository's own so that a line
		// carries both. The ref cannot be named any earlier than this.
		log: q.log.With(
			slog.String("task", ref.String()),
			slog.Int64("session_id", session.ID)),
	}
}

// pop hands out the next candidate in rotation order, or nil once this
// repository has offered all of them.
func (q *repoQueue) pop() *candidate {
	if q.taken >= len(q.candidates) {
		return nil
	}
	c := q.candidates[(q.next+q.taken)%len(q.candidates)]
	q.taken++
	return c
}

// rest is every candidate pop has not handed out yet, which is what the
// dispatcher has to account for when it stops early.
func (q *repoQueue) rest() []*candidate {
	if q.taken >= len(q.candidates) {
		return nil
	}
	out := make([]*candidate, 0, len(q.candidates)-q.taken)
	for i := q.taken; i < len(q.candidates); i++ {
		out = append(out, q.candidates[(q.next+i)%len(q.candidates)])
	}
	return out
}

// commentedIssues finds the issues that received a comment since the cursor and
// that the issue listing did not already return.
//
// Both listings are eventually consistent, and neither is a substitute for the
// other. An issue whose body or state changed comes back from the issue
// listing; an issue that only received a comment reaches the daemon fastest
// through the comment listing, which is indexed on the comments themselves
// rather than on a timestamp copied onto the issue. Asking both is one extra
// request per repository per tick, and the two indexes are unlikely to lag
// together.
//
// The overlap in [listingWindow] is what makes either of them safe. This is the
// second listing, not the safety net.
//
// One issue read per issue that is *only* reachable this way — which is to say,
// one read per issue that has genuine new work on it.
func (d *Daemon) commentedIssues(
	ctx context.Context,
	log *slog.Logger,
	q *repoQueue,
	since time.Time,
	listed []github.Issue,
) ([]github.Issue, error) {
	repo := q.forgeRepo
	numbers, err := discoveryCall(ctx, d, discoveryKey(repo, "commented", 0), discoveryListBound, func(ctx context.Context) ([]int, error) { return d.discoverCommented(ctx, repo, since) })
	if err != nil {
		return nil, fmt.Errorf("list the comments of %s written since %s: %w",
			repo, since.UTC().Format(time.RFC3339), err)
	}
	if len(numbers) == 0 {
		return nil, nil
	}

	already := make(map[int]bool, len(listed))
	for i := range listed {
		already[listed[i].Number] = true
	}

	extra := make([]github.Issue, 0, len(numbers))
	for _, number := range numbers {
		if already[number] {
			continue
		}
		// A registered story child is never a candidate of its own — [Daemon.sight]
		// drops it — so reading its issue here bought nothing. What this listing
		// does know is the one thing per-story discovery could not find out
		// without three requests: that something was said on it. Recording that
		// is the whole of the child's discovery cost, and it replaces the walk
		// over every child of every dispatched parent on every tick.
		marked, err := d.markCommentedChild(ctx, log, q, number)
		if err != nil {
			return nil, err
		}
		if marked {
			continue
		}
		issue, err := d.discoveryIssue(ctx, repo, number)
		if err != nil {
			// Every pull request is an issue, so pull request comments arrive
			// in that listing too and the payload cannot tell them apart. The
			// issue read is where they are found out, and skipping one is
			// routine rather than a failed tick.
			if errors.Is(err, github.ErrInvalidArgument) {
				log.DebugContext(ctx, "skipping a commented number that is not an issue",
					slog.Int("number", number))
				continue
			}
			return nil, fmt.Errorf("read %s#%d, which has a new comment: %w", repo, number, err)
		}
		extra = append(extra, issue)
	}
	return extra, nil
}

// markCommentedChild records a comment on a registered story child against its
// parent's session, and reports whether the number was one.
//
// Both lookups are local. A child whose parent has no session is left to the
// ordinary path rather than silently dropped: the registry is the only thing
// claiming a relationship here, and a claim with nothing behind it must not be
// able to make an issue invisible to discovery.
func (d *Daemon) markCommentedChild(ctx context.Context, log *slog.Logger, q *repoQueue, number int) (bool, error) {
	parent, err := d.db.ManagedParentForChild(ctx, q.repo.ID, number)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	same, sourceErr := d.sameTicketSource(ctx, q.repo.ID, parent)
	if sourceErr != nil {
		return false, sourceErr
	}
	if !same {
		return false, nil
	}

	session, err := d.db.GetSessionByTaskRef(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := d.db.MarkStoryDiscoveryPending(ctx, session.ID, number); err != nil {
		return false, err
	}
	log.DebugContext(ctx, "a registered story child was commented on; its parent owes it a sweep",
		slog.Int("child", number), slog.String("parent", parent))
	return true, nil
}

// task is one issue's context for the duration of a turn.
//
// It is built on the Tick goroutine, by [Daemon.materialise], and then handed to
// exactly one worker. Admission context stays immutable; subsequent state is
// owned by that worker, including sequential refinement copies. Nothing is
// shared with another worker, so no lock is needed.
type task struct {
	discoveryReservation   *discoveryReservation
	prepareFailureRecorded bool
	observations           []store.PendingTicketObservation
	enrollmentAt           time.Time
	// workerLog holds immutable admission context for worker lifetime events.
	workerLog *slog.Logger
	worker    *workerState

	scheduled          *store.Schedule
	continuation       *store.Continuation
	continuationLocked bool
	bugDispatchLocked  bool

	// commentUnderWay is the owner comment [Daemon.handleComment] is dealing
	// with, so that [Daemon.leave] can say why it was deferred. Zero otherwise.
	commentUnderWay int64

	refining bool
	// implementing marks a run the factory started for an Implement story worker.
	// The story is a graph Plan story issue too, so without it the issue would
	// run Plan, whatever its labels say.
	implementing bool
	// validating marks a graph step of a Validate instance. The issue still
	// carries its Implement label, so without it the step would run Implement.
	validating bool
	// hinted is set when a webhook named this task's issue. Its comment
	// listing then takes the operational lane, so an owner's comment does not
	// wait behind background discovery.
	hinted    bool
	repo      store.Repo
	forgeRepo github.Repo
	issue     github.Issue
	session   store.Session
	ref       naming.Ref

	// log is the task's own child logger, carrying the canonical ref and the
	// session id so that every line emitted for this task says which task it
	// belongs to (AC-PAR-7.1). It is the candidate's, carried through. Anything
	// below [Daemon.dispatch] logs through this or through a child of it, never
	// through the daemon's own logger — with two tasks in flight at once, an
	// undecorated line is attributable only by guesswork.
	log *slog.Logger

	// tickAt is the instant the tick that dispatched this task began, copied in
	// rather than read when it is wanted. [Daemon.now] is the clock seam the
	// tests substitute, and the test clock advances a whole second on every
	// reading — so only [Daemon.Tick] may turn it, and anything a worker needs
	// an instant for takes this one. The backoff is what needs it: see
	// [Daemon.rateLimited].
	tickAt time.Time
}

func finishCandidateReservations(queues []*repoQueue) {
	for _, q := range queues {
		for _, c := range q.candidates {
			if c.discoveryReservation != nil {
				c.discoveryReservation.finish()
				c.discoveryReservation = nil
			}
		}
	}
}

// dispatch is the second of the two phases a tick is made of: it decides which
// of the eligible sessions get one of the free slots, and starts a worker for
// each of them.
//
// # The policy is stated in the package documentation
//
// The two-level round-robin — what it guarantees and, just as importantly, the
// four things it does not promise — is written once, in doc.go under "The
// dispatch policy" (AC-PAR-2.1). It is the statement of the policy; this
// comment is the mechanism that implements it, and deliberately does not repeat
// it, because two copies of a fairness argument are two things to keep in step.
//
// The mechanism is three parts: repositories are walked from the cyclic
// successor of the one this daemon last dispatched from, one candidate each per
// pass; within a repository, sessions are walked from just after the session id
// that repository last dispatched, ascending and wrapping; and the walk stops
// when the candidates run out, when the pool refuses a slot, or when a
// rate-limit backoff arms under it (AC-PAR-4.4).
//
// # The cursors need no synchronisation
//
// lastRepoID and repoSessionCursor live on [Daemon], which invites the
// assumption that they are shared state. They are not: they are read and written
// only from here, [Daemon.Tick] is never called concurrently with itself, and a
// worker cannot reach them. There is deliberately no mutex, because adding one
// would say the opposite.
func (d *Daemon) dispatch(ctx context.Context, queues []*repoQueue, tickAt time.Time, result *TickResult) {
	defer finishCandidateReservations(queues)
	if len(queues) == 0 {
		return
	}

	// Ascending repository id, which is the order the repo rotation below turns
	// in. It is imposed here rather than taken from [store.DB.ListRepos], whose
	// own ORDER BY is owner/name/path and is nobody's idea of a rotation: with
	// the sort, reordering that listing — or renaming a repository — provably
	// changes nothing about what eventually runs (AC-PAR-2.3), and the resume
	// below becomes a genuine cyclic successor rather than a scan of whatever
	// order the rows arrived in.
	slices.SortFunc(queues, func(a, b *repoQueue) int {
		return cmp.Compare(a.repo.ID, b.repo.ID)
	})

	// Where the per-repo session cursor says each repository should start
	// reading. A repository that has never dispatched, or whose last dispatched
	// session is gone, starts at its lowest session id.
	for _, q := range queues {
		last, ok := d.repoSessionCursor[q.repo.ID]
		if !ok {
			continue
		}
		for i, c := range q.candidates {
			if c.session.ID > last {
				q.next = i
				break
			}
		}
	}

	// The cyclic successor of the repository this daemon last dispatched from:
	// the first id greater than it, wrapping to the lowest when there is none.
	//
	// Resolved by id rather than held as an index, because the eligible set is
	// rebuilt every tick and an index into last tick's slice would name a
	// different repository in this one. And resolved as a *successor* rather
	// than as "the one after the match" (CR-A-003): the repository last
	// dispatched from need not be in this tick's eligible set at all — it may
	// have gone quiet, or been deregistered — and matching on it would then find
	// nothing and fall back to index 0, re-serving the lowest id more often than
	// the rotation intends. Asking for the successor needs no match, so churn
	// costs the rotation nothing.
	start := 0
	for i, q := range queues {
		if q.repo.ID > d.lastRepoID {
			start = i
			break
		}
	}

	for progress := true; progress; {
		progress = false
		for i := range queues {
			q := queues[(start+i)%len(queues)]
			c := q.pop()
			if c == nil {
				continue
			}
			progress = true

			switch d.offer(ctx, c, tickAt, result) {
			case offerDispatched:
				d.lastRepoID = q.repo.ID
				d.repoSessionCursor[q.repo.ID] = c.session.ID
			case offerHeld:
				if d.connections != nil {
					continue
				}
				d.deferRest(ctx, queues, result)
				return
			case offerFull:
				// No queue, by design: everything still standing keeps its
				// backlog flag and is reconsidered from scratch next tick,
				// against fresh fairness and backoff state.
				//
				// The two endings share this one line because they owe the
				// candidates still standing exactly the same thing, and
				// [Daemon.deferRest] is the only path that pays it. A tick
				// stopped by the backoff is deliberately *not* reported as a
				// skipped tick: TickResult.Skipped means a tick that did
				// nothing at all, and this one polled, discovered and may
				// already have started runs. What it left behind is counted
				// under Deferred, which is precisely what happened to those
				// sessions — eligible, not run, still carrying needs_poll.
				d.deferRest(ctx, queues, result)
				return
			case offerSkipped:
			}
		}
	}
}

// offerResult is what came of trying to dispatch one candidate.
type offerResult int

const (
	// offerDispatched: a worker is running this candidate's turn.
	offerDispatched offerResult = iota
	// offerSkipped: this candidate is not runnable right now and no slot was
	// consumed, so the dispatch pass carries on with the next one.
	offerSkipped
	// offerFull: the pool refused a slot, which ends the dispatch pass.
	offerFull
	// offerHeld: a rate-limit backoff armed while this tick was still
	// dispatching, so no further run may start (AC-PAR-4.4). It ends the
	// dispatch pass exactly as offerFull does; it is a separate value because
	// the reason is different and the log line has to say which it was.
	offerHeld
)

// offer tries to turn one candidate into a running turn.
//
// # The order of the two guards is deliberate
//
// [inflight.enter] is checked before [pool.try], and the order is not
// interchangeable. Both can refuse, and each refusal has to undo whatever the
// other already did: a slot taken and then refused entry leaks that slot for the
// life of the process, and a session entered and then refused a slot is wedged
// until something leaves it. Entering first makes the common refusal — a full
// pool — cost one [inflight.leave] on a set this goroutine owns, which cannot go
// wrong. The other order would put the more dangerous undo on the more frequent
// path.
//
// The issue read for a backlogged session comes last, after the slot is held,
// avoiding a read for a candidate that finds the pool full or is already in
// flight. A skipped candidate or materialisation error releases the slot
// synchronously, and a worker can finish during dispatch, so the worker limit
// is not a per-tick issue-read budget.
//
// A third guard sits below both of them and below the issue read, immediately
// above the `go`: the rate-limit backoff (AC-PAR-4.4). It is last rather than
// first on purpose — see the comment at the check itself.
func (d *Daemon) offer(ctx context.Context, c *candidate, tickAt time.Time, result *TickResult) offerResult {
	defer func() {
		if c.discoveryReservation != nil {
			c.discoveryReservation.finish()
			c.discoveryReservation = nil
		}
	}()
	ctx = sessionContext(ctx, c.session.ID)
	if d.observeCandidateHold(ctx, c, tickAt) {
		return offerSkipped
	}
	if !d.enterDoctorGuarded(ctx, c.session.ID) {
		// AC-PAR-5.3: a session this daemon is already running a turn for is
		// skipped, and skipping it posts nothing and moves no watermark. Its
		// backlog flag is standing — the tick that dispatched it set it, and
		// only that worker's clean drain may clear it — so nothing is owed here.
		c.log.DebugContext(ctx, "skipping a task that already has a turn in flight; "+
			"its own worker still owes it the drain")
		return offerSkipped
	}

	// Before anything that could fail, and before the worker exists (RD-2). Every
	// session this tick took out of the eligible set is marked, not only the ones
	// that get a slot: a session deferred for want of a slot has to be found
	// again next tick, and with the poll cursor monotonic this flag is the only
	// thing that will find it.
	d.markBacklog(ctx, c.log, c.session.ID)

	if !d.pool.try() {
		d.inflight.leave(c.session.ID)
		result.Deferred++
		c.log.InfoContext(ctx, "no free slot for this task; it keeps its backlog flag and the "+
			"next tick reconsiders it against fresh state")
		return offerFull
	}

	readCtx := ctx
	if c.discoveryReservation == nil && d.connections != nil {
		v, selected, err := d.quotaVersion(ctx, store.TicketConnection)
		if err == nil && selected && v.Provider == ticket.Linear {
			readCtx, _, err = d.linearFallback(ctx, v, true)
			if r, ok := readCtx.Value(quotaReservationKey{}).(*discoveryReservation); ok && r.fallback {
				c.discoveryReservation = r
			}
		}
		if err != nil {
			d.pool.done()
			d.inflight.leave(c.session.ID)
			result.Deferred++
			return offerSkipped
		}
	}
	if c.discoveryReservation != nil {
		readCtx = context.WithValue(ctx, quotaReservationKey{}, c.discoveryReservation)
	}
	t, err := d.materialise(readCtx, c, tickAt)
	if t == nil || err != nil {
		// The one release that is not a worker's defer. It is exactly the undo
		// of the [pool.try] above — the slot goes back and the join's count comes
		// down — and it is here rather than inside a goroutine because there is
		// no turn to run: the issue is not open, the forge would not say, or
		// retiring its backlog flag failed.
		d.pool.done()
		d.inflight.leave(c.session.ID)
		if errors.Is(err, ErrDiscoveryDeferred) {
			result.Deferred++
			d.logDiscoveryDeferral(ctx, c)
			return offerSkipped
		}
		if err != nil {
			d.rateLimited(ctx, c.log, tickAt, err)
			d.prepareFailed(ctx, &task{session: c.session, log: c.log, tickAt: tickAt}, err)
			c.log.ErrorContext(ctx, "could not materialise a task that still needs a poll; "+
				"it keeps its backlog flag and the next tick tries again",
				slog.Any("error", err))
		}
		return offerSkipped
	}

	// AC-PAR-4.4, the second of the backoff's two checks — the first being at
	// the top of [Daemon.Tick]. It is here because a hold can arm *during* a
	// tick: a worker that met a 429 arms it through [Daemon.rateLimited] while
	// this dispatch loop is still running, and a tick that had only checked at
	// the top would carry on starting runs into a forge that is refusing them.
	//
	// # It is the last admission check before the `go`
	//
	// The materialise above may read an issue from the forge, which is the
	// widest window in the whole dispatch path for a hold to arm inside. A check
	// placed at the top of this function would leave exactly that window open,
	// and the invariant would then be "no `go` unless the backoff was clear a
	// network round trip ago". Here it is the final check above the dispatch
	// point, so the invariant is the plain one: no `go` while a hold stands.
	//
	// It costs at most one GetIssue per tick, because the first held check ends
	// the dispatch pass outright.
	//
	// # The instant is the tick's own, and that is sufficient
	//
	// tickAt, never d.now() — nothing below [Daemon.Tick] may read the clock
	// (AC-PAR-7.2, and TestClockIsReadOnlyFromTick fails the build if it does).
	// That is not a compromise here, because every arming this check exists to
	// catch is made with the *same* instant: a worker arms with [task.tickAt],
	// and the materialise branch above arms with this very tickAt. The hold that
	// results stands until tickAt+delay with delay at least a second, so
	// hold(tickAt) sees it. A hold left over from an earlier tick was armed at an
	// earlier instant still, so tickAt only ever understates how much of it has
	// elapsed — this check errs towards refusing to dispatch, which is the safe
	// direction for a forge that is refusing calls.
	//
	// # Nothing here touches a worker (AC-PAR-4.3)
	//
	// The backoff holds an instant and nothing else: no CancelFunc, no context,
	// no handle on anything already running. The turns in flight keep running and
	// their claude processes are left alone. This gates the dispatch of new runs,
	// and that is the whole of what a rate limit costs.
	if retryAt, held := d.connectionHold(ctx, tickAt); held {
		// The same undo as the materialise branch above, for the same reason:
		// [pool.try] said yes and no worker will run, so the slot goes back
		// here, on this goroutine, before anything else can be refused one.
		d.pool.done()
		d.inflight.leave(c.session.ID)
		result.Deferred++
		c.log.InfoContext(ctx, "a rate limit armed the backoff while this tick was still "+
			"dispatching; no further run starts. This task keeps its backlog flag and the next "+
			"tick reconsiders it; the turns already in flight are untouched",
			slog.Time("retry_at", retryAt.UTC()))
		return offerHeld
	}

	t.workerLog.DebugContext(ctx, "worker dispatched")

	// The dispatch point uses the daemon's long-lived context. runWorker owns
	// the shared pool and in-flight lifetime through terminal diagnostics.
	t.discoveryReservation = c.discoveryReservation
	c.discoveryReservation = nil
	go d.runTicketPollingWorker(ctx, t, func(ctx context.Context) { d.work(ctx, t) })

	result.Dispatched++
	return offerDispatched
}

// deferRest accounts for every candidate still standing when the pool ran out
// of slots (CR-A-001).
//
// Discovery and dispatch used to be the same event, so marking a session at the
// moment it was found was the same as marking it at the moment it was run. They
// are two events now, and this is the half that would otherwise be lost: a
// session that was eligible, was never offered a slot, and whose issue may not
// be touched again before the poll cursor has moved past it.
func (d *Daemon) deferRest(ctx context.Context, queues []*repoQueue, result *TickResult) {
	for _, q := range queues {
		for _, c := range q.rest() {
			d.markBacklog(ctx, c.log, c.session.ID)
			result.Deferred++
		}
	}
}

// materialise turns a candidate into the task a worker is handed, reading the
// issue behind it if discovery did not already have one.
//
// A nil task with a nil error means there is nothing to run: the issue has been
// observed as non-open. Confirmed closure durably retires ordinary backlog
// without consuming comments or reports (US-1 AC-1, AC-5). An unknown state
// retains the flag for a later observation; only explicit closure is terminal.
// Reopening is found through normal discovery using the retained session.
//
// It runs on the Tick goroutine, so [naming.ParseRef] and the whole of task
// construction stay where a test can count them, and no worker has to work out
// its own identity before it can log.
func (d *Daemon) materialise(ctx context.Context, c *candidate, tickAt time.Time) (*task, error) {
	if d.connections != nil {
		ctx = sessionContext(ctx, c.session.ID)
		snapshot, e := d.db.GetExecutionSnapshot(ctx, c.session.ID)
		if e != nil {
			return nil, e
		}
		c.repo = snapshot.CodeTarget
		binding, e := d.db.GetSessionTicket(ctx, c.session.ID)
		if e != nil {
			return nil, e
		}
		owner, name, _ := strings.Cut(binding.Locator.Container, "/")
		c.forgeRepo = github.Repo{Owner: owner, Name: name}
	}

	issue := c.issue
	if !c.resolved {
		// Lazily, and only now that a slot is held: see [Daemon.offer] for why
		// this is the last thing that happens rather than the first.
		pending, err := d.db.ListPendingStoryRefinements(ctx, c.session.ID)
		if err != nil {
			return nil, err
		}
		var fetched github.Issue
		if len(pending) > 0 {
			fetched, err = d.readIssue(ctx, c.forgeRepo, c.session.IssueNumber)
		} else {
			fetched, err = d.discoveryIssue(ctx, c.forgeRepo, c.session.IssueNumber)
		}
		if err != nil {
			return nil, fmt.Errorf("read %s#%d, which still needs a poll: %w",
				c.forgeRepo, c.session.IssueNumber, err)
		}
		issue = fetched
	}
	if err := d.bindObservedIssue(ctx, c.session.ID, c.forgeRepo, issue); err != nil {
		return nil, err
	}
	if issue.State == "closed" {
		cleared, err := d.cancelClosedIssueGraphs(ctx, c.session.ID, tickAt)
		if err != nil {
			return nil, fmt.Errorf("clear graph execution before retiring closed %s#%d backlog: %w", c.forgeRepo, c.session.IssueNumber, err)
		}
		if !cleared {
			return nil, nil
		}
		if err := d.db.ClearSessionNeedsPoll(ctx, c.session.ID); err != nil {
			return nil, fmt.Errorf("retire polling backlog for closed %s#%d: %w",
				c.forgeRepo, c.session.IssueNumber, err)
		}
		// Clearing the backlog flag retires the listing-driven axis only. The
		// managed registry re-queues this parent on the next tick whatever the
		// flag says, so the read that just established the closure is also
		// where it has to be recorded, or it is paid for again every minute
		// for the life of the registry row. Not a managed parent is a no-op.
		if err := d.db.SetManagedParentClosed(ctx, c.ref.String(), true); err != nil {
			return nil, fmt.Errorf("retire the managed registry entry for closed %s#%d: %w",
				c.forgeRepo, c.session.IssueNumber, err)
		}
	}
	if issue.State == "closed" {
		for _, observation := range c.observations {
			if err := d.db.ClearPendingTicketObservation(ctx, observation.ConnectionVersionID, observation.TicketID, observation.Generation); err != nil {
				return nil, err
			}
		}
	}
	if issue.State != github.StateOpen {
		c.log.DebugContext(ctx, "a task that still needs a poll is no longer open; nothing runs",
			slog.String("state", issue.State))
		return nil, nil
	}

	return &task{
		observations: slices.Clone(c.observations),
		repo:         c.repo,
		forgeRepo:    c.forgeRepo,
		issue:        issue,
		session:      c.session,
		ref:          c.ref,
		log:          c.log,
		tickAt:       tickAt,
		hinted:       slices.Contains(c.sources, "webhook"),
		workerLog:    c.log.With(slog.Any("dispatch_sources", slices.Clone(c.sources)), slog.Time("tick_at", tickAt)),
	}, nil
}

// taskComments lists a task's comments after a watermark. A webhook-hinted task
// reads through the operational lane ([doctorCall]); every other task reads
// through background discovery.
func (d *Daemon) taskComments(ctx context.Context, t *task, after int64) ([]github.Comment, error) {
	if t.hinted {
		return doctorCall(ctx, d, func(ctx context.Context) ([]github.Comment, error) {
			return d.readComments(ctx, t.forgeRepo, t.issue.Number, after)
		})
	}
	if t.discoveryReservation != nil {
		ctx = context.WithValue(ctx, quotaReservationKey{}, t.discoveryReservation)
	}
	return d.discoveryComments(ctx, t.forgeRepo, t.issue.Number, after)
}

// work is everything one dispatched turn does, and it is the whole of what runs
// on a worker goroutine.
//
// It returns nothing. There is nobody left to return to — the tick that
// dispatched it has long since gone — so every outcome is a log line and a row
// in the database, which is where the assertions were always going to have to
// be. What used to escalate out of here and end the tick now ends this task's
// turn and no other: with two turns in flight, one repository's failed comment
// listing is not a reason to stop the other.
//
// Three bounded ways out, which is what makes the pool's join terminate: the
// comment list is exhausted, a comment was left unconsumed, or the context is
// done.
func (d *Daemon) work(ctx context.Context, t *task) {
	if d.connections != nil {
		ctx = sessionContext(ctx, t.session.ID)
	}
	counts := &discoveryCounts{}
	ctx = github.WithRequestObserver(ctx, counts.observe)
	defer d.discoverySummary(ctx, "worker", counts)
	if err := d.recoverPlanStories(ctx, t); err != nil {
		d.stopped(ctx, t, "could not reconcile Plan stories", err)
		return
	}
	handledPlan, err := d.refinePlanChildren(ctx, t)
	if err != nil {
		d.stopped(ctx, t, "could not route Plan child discussion", err)
		return
	}
	if err = d.reportPlanProgress(ctx, t); err != nil {
		d.stopped(ctx, t, "could not report Plan progress", err)
		return
	}
	if handledPlan {
		d.clearTicketObservations(ctx, t)
		return
	}

	if deferred, err := d.refineStories(ctx, t); err != nil {
		d.stopped(ctx, t, "could not refine managed stories", err)
		return
	} else if deferred {
		t.recordWorkerStatus(workerDeferred)
		return
	}
	// A presentation owed from an earlier turn goes before any comment: the
	// run's trigger is already consumed, so only its recovery debt brings the
	// session back to it. A session with debt still owed is held by the offer
	// gate until due, so this normally runs exactly when an attempt is eligible.
	if err := d.resumeCreationPresentations(ctx, t); err != nil {
		d.stopped(ctx, t, "could not present a retained creation proposal", err)
		return
	}
	watermark, exists, err := d.db.GetWatermark(ctx, t.session.ID)
	if err != nil {
		d.stopped(ctx, t, "could not read this task's watermark", err)
		return
	}

	var deferred bool
	bodyPending := false
	if exists && d.connections != nil {
		bodyPending, err = d.db.TicketBodyPending(ctx, t.session.ID)
		if err != nil {
			d.stopped(ctx, t, "could not read first ticket instruction recovery", err)
			return
		}
	}
	if !exists || bodyPending {
		err = d.bootstrap(ctx, t)
		if err == nil && d.connections != nil {
			initialized, _, readErr := d.db.GetWatermark(ctx, t.session.ID)
			if readErr != nil {
				err = readErr
			} else {
				deferred, err = d.drain(ctx, t, initialized.LastCommentID)
			}
		}
	} else {
		deferred, err = d.drain(ctx, t, watermark.LastCommentID)
	}
	if err != nil {
		d.stopped(ctx, t, "this task's turn could not be completed", err)
		return
	}
	if !deferred && len(d.creationDebt(t.session.ID)) != 0 {
		// A presentation failed during this drain. Its trigger is consumed, so
		// nothing but the backlog flag brings the next tick back to it.
		deferred = true
	}
	if deferred {
		t.recordWorkerStatus(workerDeferred)
	}
	if !deferred {
		// Comments come first: an owner's reply may change the work this task owes.
		if err := d.startPlanWork(ctx, t); err != nil {
			d.stopped(ctx, t, "could not run the Plan work of this task", err)
			return
		}
		if err := d.startImplementWork(ctx, t); err != nil {
			d.stopped(ctx, t, "could not run the Implement work of this task", err)
			return
		}
		if err := d.startValidateAuto(ctx, t); err != nil {
			d.stopped(ctx, t, "could not start Validate for this task", err)
			return
		}
		// The clean-drain clear site, deliberately a statement on the success path
		// rather than a defer: a defer would fire on the error returns above too,
		// which is exactly the stale-false the protocol forbids.
		d.clearBacklog(ctx, t)
		// Consecutive-failure counting ends where the backlog flag does: this
		// task has just made progress, so whatever it was failing at is no
		// longer failing.
		d.prepFailures.clear(t.session.ID)
	}
}

// stopped is how a worker ends badly.
//
// It arms the backoff if what refused the turn was the forge over quota —
// [Daemon.leave] does the same for a refusal met per comment, and this covers
// the listings a worker makes for itself, which used to unwind into
// [Daemon.observe] and no longer can. Shutdown is discriminated from a failure,
// because a worker abandoned by a signal has not gone wrong.
//
// Either way the session keeps its backlog flag, which is what brings the next
// tick back to it.
func (d *Daemon) stopped(ctx context.Context, t *task, what string, err error) {
	if isRecoveryPending(err) || errors.Is(err, ErrDiscoveryDeferred) {
		t.recordWorkerStatus(workerDeferred)
		return
	}
	t.recordWorkerError(err)
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		t.log.DebugContext(ctx, "shutdown ended this task's turn; it still needs a poll, so "+
			"the next start comes back to it")
		return
	}
	d.rateLimited(ctx, t.log, t.tickAt, err)
	t.log.ErrorContext(ctx, what+"; it still needs a poll, so a later tick comes back to it",
		slog.Any("error", err))
	d.prepareFailed(ctx, t, err)
}

// prepareFailed counts a turn that ended without consuming its trigger, and
// says so once when a task stops looking like it is between attempts and
// starts looking stuck.
//
// A worker can expose the same error through leave and stopped. Count that
// attempt once; report recovery owns its own independent deadline.
func (d *Daemon) prepareFailed(ctx context.Context, t *task, err error) {
	if t.prepareFailureRecorded || isRecoveryPending(err) || errors.Is(err, ErrDiscoveryDeferred) || ctx.Err() != nil {
		return
	}
	t.prepareFailureRecorded = true
	reason := err.Error()
	if sanitizer := d.diagnosticRedactor; sanitizer != nil {
		if body, redactErr := sanitizer.Apply(ctx, reason, 1024); redactErr == nil {
			reason = body.String()
		}
	}
	state, announce := d.prepFailures.record(t.session.ID, reason, t.tickAt, d.recoveryInterval())
	if !announce || state.Attempts < prepareEscalationAttempts {
		return
	}
	t.log.ErrorContext(ctx, "this task has failed every turn since it last made progress; "+
		"the trigger is still unconsumed and retries use capped exponential backoff",
		slog.Int("consecutive_failures", state.Attempts),
		slog.Bool("escalated", state.Escalated),
		slog.String("reason", reason))
}

// bootstrap plants the watermark of a task seen for the first time at the
// highest comment id that already exists, and processes none of them (AC-2.10).
//
// The alternative — treating every existing comment as new — turns registering
// a repository with a long-running issue into a stampede of runs, one per
// historical `/spike` anyone ever wrote.
//
// The issue body is the one thing a first sight may act on, and only when the
// issue asked for it: see [Daemon.runIssueBody] for the gate (AC-2.16a–d) and
// `.aiteam/AC_AMENDMENTS.md` for why the old blanket ban on issue bodies
// (AC-2.16) was reversed. Existing comments are still never replayed.
func (d *Daemon) bootstrap(ctx context.Context, t *task) error {
	comments, err := d.taskComments(ctx, t, 0)
	if err != nil {
		return fmt.Errorf("list the existing comments of task %s: %w", t.ref, err)
	}

	var enrollment time.Time
	if d.connections != nil {
		snapshot, err := d.db.GetExecutionSnapshot(ctx, t.session.ID)
		if err != nil {
			return err
		}
		position, err := d.db.GetTicketRecoveryPosition(ctx, snapshot.TicketVersionID)
		if err != nil {
			return err
		}
		enrollment = position.EnrollmentAt
		t.enrollmentAt = enrollment
	}
	var highest int64
	for _, c := range comments {
		if !enrollment.IsZero() && c.CreatedAt.IsZero() {
			return fmt.Errorf("daemon: enrolled reply has no creation time")
		}
		if !enrollment.IsZero() && !c.CreatedAt.Before(enrollment) {
			break
		}
		if c.ID > highest {
			highest = c.ID
		}
	}

	bootstrapWatermark := d.db.BootstrapWatermark
	if d.connections != nil {
		bootstrapWatermark = d.db.BootstrapTicketWatermark
	}
	watermark, created, err := bootstrapWatermark(ctx, t.session.ID, highest)
	if err != nil {
		return fmt.Errorf("bootstrap the watermark of task %s: %w", t.ref, err)
	}

	if created {
		t.log.InfoContext(ctx, "first sight of a task; its existing comments are skipped",
			slog.Int("existing_comments", len(comments)),
			slog.Int64("watermark", watermark.LastCommentID),
			slog.Bool("created", created))
	}

	// Reconcile retained body delivery before checking current eligibility:
	// its own visible reply must not turn an uncertain post into a body skip.
	pending, err := d.resumeReport(ctx, t, 0)
	if err != nil {
		return err
	}
	if !pending {
		if err := d.runIssueBody(ctx, t, comments); err != nil {
			return err
		}
	}
	if d.connections != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if t.worker != nil && (t.worker.status == workerDeferred || t.worker.status == workerFailed) {
			return &recoveryPending{}
		}
		if err := d.db.CompleteTicketBody(ctx, t.session.ID); err != nil {
			return err
		}
		d.prepFailures.clear(t.session.ID)
	}
	return nil
}

// runIssueBody runs the issue body as the task's first instruction, when the
// issue is one that asked for it (AC-2.16b).
//
// # The gate
//
// Four conditions, all of them required:
//
//  1. The issue is open. Not checked here: [Daemon.sight] returns before
//     GetOrCreateSession on anything that is not open (AC-2.22), and
//     [Daemon.materialise] re-checks it for a session reached through the
//     backlog, so an issue that is not open can never reach a bootstrap at all.
//     Re-checking it a third time would be another place to keep in step with
//     those two, and the first of them is the one that also stops the session
//     row being written.
//  2. Its author is the verified ticket approver, matched on immutable ID.
//     Assignment grants no authority. Provider creation time must satisfy the
//     appointment boundary, and the retained body must satisfy source enrollment.
//  3. Its body has something in it.
//  4. It has no comments at all. A body is a *first* instruction; an issue that
//     has already been talked on is one whose conversation is the instruction,
//     and replaying its opening post underneath that would be a second voice
//     nobody asked for (AC-2.16d).
//
// # Watermark first, then run
//
// Qualified bootstrap plants a durable body-pending flag with the watermark.
// An interruption before run allocation resumes here; existing trigger ownership
// handles an allocated run. Successful processing or an intentional refusal
// clears the flag. Already initialized legacy sessions retain their history.
//
// # The trigger
//
// CommentID is 0: not a comment, and deliberately not the issue's own forge id
// either — reporting a run advances the watermark to the trigger id, and any
// positive value would push it past every comment the issue will ever receive
// (AC-2.23). See [turn.Trigger.CommentID].
//
// Body is left empty. There is no comment, and the prompt drops the comment
// note on this path, so a copy of the issue body there would be dead weight;
// the body reaches the model as the instruction itself and as the issue note.
// Nothing validates Trigger.Body.
//
// Author is the verified project approver who authored this body. Assignment
// grants no authority; the provider creation time must satisfy activation.
//
// # No 👀
//
// There is no comment to react to, and reacting to the issue would be an eighth
// forge endpoint for a cosmetic gain. AC-2.17 stays comment-only.
func (d *Daemon) runIssueBody(ctx context.Context, t *task, comments []github.Comment) error {
	if d.connections != nil {
		appointment, err := d.db.CurrentApprover(ctx, t.session.RepoID)
		if err != nil {
			return err
		}
		if appointment.RequireFresh && !t.issue.CreatedAt.After(appointment.ActivatedAt) {
			return nil
		}
	}
	var refusal string
	switch {
	case !t.enrollmentAt.IsZero() && t.issue.UpdatedAt.Before(t.enrollmentAt):
		refusal = "its retained body predates ticket source enrollment"
	case t.issue.Author.ID != d.ticketIdentities(ctx).OwnerUserID || t.issue.Author.IsBot || turn.HasMarker(t.issue.Body):
		refusal = "its author is not the verified ticket approver"
	case strings.TrimSpace(t.issue.Body) == "":
		refusal = "its body is empty"
	case len(comments) != 0:
		refusal = "it already has comments, which are the conversation to follow instead"
	}
	if refusal != "" {
		t.log.DebugContext(ctx, "not running the issue body on first sight; "+refusal,
			slog.Int("existing_comments", len(comments)))
		return nil
	}

	log := t.log.With(
		slog.Int64("comment_id", 0),
		slog.String("trigger", "issue_body"),
		slog.Int64("author_user_id", t.issue.Author.ID),
		slog.String("author", t.issue.Author.Login))

	command := turn.ParseCommand(t.issue.Body)
	if command.Kind == turn.KindNone {
		// A body with nothing to act on. The gate above has already refused an
		// empty one, so what reaches here is a body whose whole content was the
		// `lf: ask` tag: it is non-empty text that asks for nothing, because the
		// tag grants permission and gives no instruction (AC-A1.9). It ends here
		// silently, exactly as an empty body would — there is no comment to
		// answer on the body path, and an issue is not a question.
		log.DebugContext(ctx, "the issue body has nothing to act on; nothing runs")
		return nil
	}
	if !command.Spawns() || command.Kind == turn.KindPlanControl || command.Kind == turn.KindValidateControl {
		// /plan, /frobnicate, a bare /spike: nothing runs, and — unlike the
		// same command in a comment — nothing is posted either (AC-2.16c). A
		// reply would be litefactory answering a question nobody asked it, on
		// an issue whose author may not even have meant the first line as a
		// command. A Plan control answers a proposal the factory posted, so an
		// issue body cannot carry one.
		log.DebugContext(ctx, "the issue body names a command that spawns nothing; "+
			"nothing runs and nothing is posted",
			slog.String("command", command.Name),
			slog.String("kind", command.Kind.String()))
		return nil
	}

	log.InfoContext(ctx, "running the issue body as the task's first instruction",
		slog.String("command", command.Name),
		slog.String("kind", command.Kind.String()))
	trigger := turn.Trigger{CommentID: 0, Body: "", Author: t.issue.Author.Login, AuthorID: t.issue.Author.ID, CreatedAt: t.issue.CreatedAt}
	if d.connections != nil {
		mapping, err := d.db.GetSessionTicket(ctx, t.session.ID)
		if err != nil {
			return err
		}
		trigger.Actor = ticket.Identity{Provider: mapping.Locator.Provider, Origin: mapping.Locator.Origin, RemoteID: strconv.FormatInt(t.issue.Author.ID, 10)}
		if t.issue.QualifiedSnapshot != nil {
			trigger.Actor = t.issue.QualifiedSnapshot.Author.Identity
		}
		trigger.Source = ticket.Identity{Provider: mapping.Locator.Provider, Origin: mapping.Locator.Origin, RemoteID: mapping.RemoteID}
	}
	return d.run(ctx, t, trigger, command, log)
}

// drain processes the comments above the watermark, in ascending id order
// (AC-2.11), and reports whether it stopped before the end.
//
// # The queue stops at the first comment that was not consumed
//
// This is the rule the whole package is built around. store.AdvanceWatermark
// takes a MAX and never rewinds, so consuming comment 12 after failing to
// report comment 11 moves the watermark past 11 for ever: 11's report is never
// retried, and nothing anywhere says so — the watermark looks healthy and the
// missing comment is simply never noticed.
//
// So after every comment the watermark itself is read back. Not a return value,
// not an inference from an error: the store, which is the only thing that knows
// whether a later tick will see this comment again. The first comment it has
// not moved past ends this task's queue. Other tasks are unaffected — they are
// other goroutines now — and the session keeps its sessions.needs_poll flag,
// which is what brings the next tick's discovery back to it. The poll cursor
// does not: it advances past this issue whatever happened here (T-PAR-13).
//
// It is also what makes a contended per-task lock behave: nothing was consumed,
// so the queue stops and a later tick retries, with no queue and no "busy"
// reply (AC-3.1).
func (d *Daemon) drain(ctx context.Context, t *task, watermark int64) (bool, error) {
	// Recovery must finish even when the watermark leaves no new comments.
	if err := d.retireModeSelections(ctx, t.session.ID); err != nil {
		return false, fmt.Errorf("retire completed mode selections: %w", err)
	}
	comments, err := d.taskComments(ctx, t, watermark)
	if err != nil {
		return false, fmt.Errorf("list the comments of task %s above %d: %w", t.ref, watermark, err)
	}

	for _, c := range comments {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := d.handleComment(ctx, t, c); err != nil {
			return false, err
		}

		consumed, err := d.consumed(ctx, t.session.ID, c.ID)
		if err != nil {
			return false, fmt.Errorf("read the watermark of task %s: %w", t.ref, err)
		}
		if !consumed {
			t.log.DebugContext(ctx, "comment was not consumed; stopping this task's queue here so a "+
				"later tick retries it. Processing the comments after it would advance the watermark past this one",
				slog.Int64("comment_id", c.ID))
			return true, nil
		}
		d.prepFailures.clear(t.session.ID)
		// Refresh after each actionable instruction so later owner label edits
		// are observed before admitting another trigger. Persistent selection
		// does not make this tick's issue snapshot authoritative forever.
		if len(d.classifiers) > 0 && turn.ParseCommand(c.Body).Spawns() {
			return true, nil
		}
	}
	return false, nil
}

// consumed asks the store whether the watermark has moved past a comment.
//
// It is the ground truth for "will a later tick see this again", and it is
// deliberately a database read rather than a deduction from what the reporting
// path returned. The expensive case is precise: a post that reached GitHub but
// whose recording transaction did not commit returns a comment id *and* an
// error, and only the watermark knows which of those to believe.
func (d *Daemon) consumed(ctx context.Context, sessionID, commentID int64) (bool, error) {
	watermark, exists, err := d.db.GetWatermark(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return exists && watermark.LastCommentID >= commentID, nil
}

// markBacklog records that a session still owes the daemon a look, whatever the
// forge's listings can still see (RD-2).
//
//	Protocol — the tick sets needs_poll = 1 for every session it takes out
//	of the eligible set, before the worker starts. A fully drained, error-free
//	drain/bootstrap clears it, as does an explicit closed observation during
//	materialisation (US-1 AC-7). Other exits retain retry intent. Stale-true
//	is recoverable after a clean drain or confirmed closure; stale-false can
//	lose work. That asymmetry follows AdvanceWatermark's never-rewind rule.
//
// It takes a session id and the logger the caller has already decorated,
// because it is called before there is a task: a candidate the dispatcher
// deferred for want of a slot never becomes one, and it is exactly as owed a
// second look as one that ran (CR-A-001).
//
// It is written unconditionally, never skipped on the grounds that the session
// row read this tick already said needs_poll. Between that read and this write a
// worker from an earlier tick can have finished and cleared the flag, and
// declining to set it then is the stale-false the protocol forbids. One
// repeated UPDATE preserves retry intent and refreshes updated_at even if
// the flag was already true.
//
// A store failure is logged and goes no further. The flag is a re-discovery
// hint rather than the work, so failing the turn over one would trade
// everything the tick has already done for a hint. It is logged at error level
// all the same, because a lost *set* is the dangerous direction: now that the
// cursor is monotonic, this line is the only thing that will explain a session
// that went quiet. Shutdown is not that failure — the tick is abandoned, its
// cursor is never persisted, and the next start lists the session again — so it
// is discriminated here the way [Daemon.leave] discriminates it.
func (d *Daemon) markBacklog(ctx context.Context, log *slog.Logger, sessionID int64) {
	err := d.db.SetSessionNeedsPoll(ctx, sessionID)
	if err == nil {
		return
	}
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		log.DebugContext(ctx, "shutdown interrupted marking this task as still needing a poll; "+
			"the tick is abandoned and its cursor is not persisted, so the next start lists it again")
		return
	}
	log.ErrorContext(ctx, "could not mark this task as still needing a poll; the turn carries on, "+
		"but a later tick may have nothing local telling it to come back",
		slog.Any("error", err))
}

// clearBacklog takes the flag back off a session whose work reached the end
// without an error. Terminal closure clears separately in [Daemon.materialise],
// where persistence errors must propagate through admission error handling.
//
// The asymmetry with [Daemon.markBacklog] runs all the way down to the log
// level: losing a clear leaves a stale-true flag, recoverable when a later
// tick can complete a clean drain or confirm closure. That retries observation
// rather than losing a comment, so a failed clean-drain clear is a warning.
// It needs no shutdown branch — every path that reaches here has already
// returned the context's error rather than calling this.
//
// A bootstrap counts as drained when it returns no error, including one whose
// issue-body run was swallowed by [Daemon.leave]. That is not a stale-false:
// [Daemon.bootstrap] plants the watermark before the body runs, so a body that
// failed is not re-runnable by any mechanism this flag could reach, and leaving
// it standing would buy the next tick a listing that finds nothing.
func (d *Daemon) clearBacklog(ctx context.Context, t *task) {
	d.clearTicketObservations(ctx, t)
	if err := d.db.ClearSessionNeedsPoll(ctx, t.session.ID); err != nil {
		t.recordWorkerError(err)
		t.log.WarnContext(ctx, "could not clear this task's backlog flag after a clean drain; "+
			"a later tick re-lists it, finds nothing above its watermark and clears it then",
			slog.Any("error", err))
	}
}

// handleComment classifies one comment and does whatever it asks for.
//
// The error return is reserved for failures that end the whole tick: a database
// that will not answer, and shutdown. Everything else is logged and left — the
// comment stays unconsumed, which [Daemon.drain] detects and turns into a retry
// on a later tick — including a rate limit, which arms the backoff where it was
// met and then leaves by that same door. See [Daemon.leave].
func (d *Daemon) handleComment(ctx context.Context, t *task, c github.Comment) error {
	ctx = sessionContext(ctx, t.session.ID)
	qualifiedReply, err := d.qualifyReply(ctx, t.session.ID, c)
	if err != nil {
		return err
	}
	if held, e := d.doctorHeldSession(ctx, t.session.ID); e != nil {
		return e
	} else if held {
		return errors.New("issue held for doctor repair")
	}
	if routed, err := d.admitDoctor(ctx, t.repo, t.issue, c); routed {
		if err != nil {
			return err
		}
		_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
		return err
	}
	log := t.log.With(
		slog.Int64("comment_id", c.ID),
		slog.Int64("author_user_id", c.Author.ID),
		slog.String("author", c.Author.Login))
	commandForCode := turn.ParseCommand(c.Body)
	if isCodeAuthorityControl(commandForCode.Name) {
		if err := d.authorizeCodeControl(ctx, t.session.ID, c); err != nil {
			if recordErr := d.recordCodeObservation(ctx, t.session.ID, c, qualifiedReply, false); recordErr != nil {
				return recordErr
			}
			_, consumeErr := d.reporter.Consume(ctx, t.session.ID, c.ID)
			return consumeErr
		}
		if err := d.recordCodeObservation(ctx, t.session.ID, c, qualifiedReply, true); err != nil {
			return err
		}
		if d.connections != nil {
			claim, err := d.claimCodeControl(ctx, t.session.ID, c)
			if err != nil {
				return err
			}
			defer func() {
				if err := d.db.CompleteProjectAuthority(context.WithoutCancel(ctx), claim.ID); err != nil {
					log.ErrorContext(ctx, "release code authority claim", "error", err)
				}
			}()
		}
		if commandForCode.Name == "/deliver" || commandForCode.Name == "/merge" {
			return d.handleDelivery(ctx, t, c, log)
		}
		trigger, err := d.projectTrigger(ctx, t.session.ID, c)
		if err != nil {
			return err
		}
		return d.run(ctx, t, trigger, commandForCode, log)
	}
	// The self-filter runs first and matches on numeric ids, never handles
	// (D-I, AC-2.14). Everything is recorded, including litefactory's own
	// comments: a dropped comment that left no trace cannot be told from one
	// that was never fetched.
	admission, err := d.admitReply(ctx, t.session.ID, c, qualifiedReply)
	if err != nil {
		return fmt.Errorf("classify comment %d on task %s: %w", c.ID, t.ref, err)
	}

	if !admission.Triggers() {
		// litefactory's own comment, or a third party's: recorded and tagged,
		// nothing runs, the comment is consumed (AC-2.14, AC-2.15).
		_, err := d.reporter.Consume(ctx, t.session.ID, c.ID)
		return d.leave(ctx, t, log, "could not consume a comment litefactory does not act on", err)
	}

	t.commentUnderWay = c.ID
	defer func() { t.commentUnderWay = 0 }()

	// A graph Plan story issue that is a task of its own has its owner comments
	// answered here, where they were written.
	if story, owned, err := d.graphPlanStoryForTask(ctx, t); err != nil {
		return d.leave(ctx, t, log, "could not tell whether this is a graph Plan story issue", err)
	} else if owned {
		return d.handlePlanStoryComment(ctx, t, c, story, log)
	}
	// Any other graph Plan story issue belongs to its parent, whose worker reads
	// the issue's comments as Plan input. Acting here as well would answer the
	// same reply twice, or start a second run beside the Plan revision.
	if parent, err := d.graphPlanParentForTask(ctx, t); !errors.Is(err, store.ErrNotFound) {
		if err != nil {
			return d.leave(ctx, t, log, "could not read retained Plan story membership", err)
		}
		// Retired stories still belong to their parent. Its discovery path
		// explains the refusal; this task must not consume ahead of that reply.
		explanation, err := d.db.PlanReplyExplanation(ctx, parent, c.ID)
		if errors.Is(err, store.ErrNotFound) {
			return &recoveryPending{errors.New("retained Plan story awaits its parent's reply")}
		}
		if err != nil {
			return err
		}
		delivery, err := d.db.GetReportDelivery(ctx, explanation.DeliveryKey)
		if err != nil {
			return err
		}
		if !delivery.Completed {
			return &recoveryPending{errors.New("retained Plan story reply awaits confirmed delivery")}
		}
		_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
		return d.leave(ctx, t, log, "could not consume the answered Plan story comment", err)
	}
	if pending, err := d.resumeReport(ctx, t, c.ID); pending || err != nil {
		return d.leave(ctx, t, log, "prior delivery remains pending", err)
	}
	if recovered, err := d.resumeCreationSuccess(ctx, t, c); recovered || err != nil {
		return d.leave(ctx, t, log, "successful creation intake recovery pending", err)
	}
	command := turn.ParseCommand(c.Body)
	if d.bug != nil && isBugControlCommand(command.Name) {
		admission, err := d.db.GetTaskWorkflowAdmission(ctx, t.session.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return d.leave(ctx, t, log, "could not read Bug control admission", err)
		}
		if err == nil && admission.Workflow == string(agent.WorkflowBug) {
			// The runtime authenticates and parses the exact original body against
			// its published wait. This only routes an existing task's control.
			trigger, triggerErr := d.projectTrigger(ctx, t.session.ID, c)
			if triggerErr != nil {
				return triggerErr
			}
			return d.run(ctx, t, trigger, turn.Command{Kind: turn.KindFreeText, Text: c.Body}, log)
		}
	}
	if command.Name == "/deliver" || command.Name == "/merge" {
		return d.handleDelivery(ctx, t, c, log)
	}
	if command.Name == "/mcp-create" {
		return d.handleMCPCreation(ctx, t, c, log)
	}
	if command.Name == "/followup" {
		return d.handleFollowup(ctx, t, c, log)
	}
	if command.Name == "/waive" {
		return d.handleOwnerException(ctx, t, c, log)
	}
	if command.Kind == turn.KindNone {
		// Nothing to act on. Recorded, ignored, consumed.
		//
		// Free text used to end here too. It no longer does — it spawns a run
		// whose mode the agent infers. What reaches this branch is a body that
		// is empty, or one whose only content was the `lf: ask` tag: the tag
		// grants permission and asks for nothing, so a comment carrying only the
		// tag has given no instruction to carry out (AC-A1.9). Consuming it is
		// what stops the next tick reading it again.
		log.DebugContext(ctx, "comment asks for nothing; recorded and ignored")
		d.commentFate(ctx, t, c.ID, fateIgnored, "ignored, it asks for nothing")
		_, err := d.reporter.Consume(ctx, t.session.ID, c.ID)
		return d.leave(ctx, t, log, "could not consume an empty comment", err)
	}

	// AC-2.17: the acknowledgement goes on before anything that could start a
	// run, and its failure is logged rather than fatal — a missing reaction is
	// a cosmetic loss, and refusing the turn over one would be a real one.
	d.acknowledge(ctx, t, c.ID)

	if command.Kind == turn.KindSchedule {
		return d.handleSchedule(ctx, t, c, command, log)
	}

	if command.Kind == turn.KindConfirm {
		return d.handleConfirm(ctx, t, c, command, log)
	}

	if command.Kind == turn.KindPlanControl {
		return d.handlePlanControl(ctx, t, c, command, log)
	}

	if command.Kind == turn.KindValidateControl {
		return d.handleValidateControl(ctx, t, c, command, log)
	}

	if !command.Spawns() {
		d.commentFate(ctx, t, c.ID, fateIgnored, "ignored, it is not a command that starts a run")
		return d.reply(ctx, t, c, command, log)
	}
	trigger, triggerErr := d.projectTrigger(ctx, t.session.ID, c)
	if triggerErr != nil {
		return triggerErr
	}
	return d.run(ctx, t, trigger, command, log)
}

// handlePlanControl delivers a graph Plan control on the parent issue, such as
// `/approve-plan set <revision>`, to a Plan turn. The graph Plan runtime applies
// or refuses it from the exact comment body. A session not admitted to graph
// Plan has no such controls, so the comment is answered as an unknown command,
// as it was before graph Plan existed.
func (d *Daemon) handlePlanControl(
	ctx context.Context,
	t *task,
	c github.Comment,
	command turn.Command,
	log *slog.Logger,
) error {
	route, err := d.db.PlanExecutionPath(ctx, t.session.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return d.leave(ctx, t, log, "could not read the Plan route for "+command.Name, err)
	}
	if route != "graph" {
		return d.reply(ctx, t, c, turn.Command{Kind: turn.KindUnknown, Name: command.Name, Tagged: command.Tagged}, log)
	}
	trigger, triggerErr := d.projectTrigger(ctx, t.session.ID, c)
	if triggerErr != nil {
		return triggerErr
	}
	return d.run(ctx, t, trigger, command, log)
}

// handleValidateControl delivers a Validate owner command (a manual check, a
// finding choice, push, merge or close) to the session's Validate graph. The
// resolve step authenticates and parses the exact comment body against the
// published owner wait and the current report revision. A session with no
// Validate graph has no such controls, so the comment is answered as an unknown
// command, as it was before.
func (d *Daemon) handleValidateControl(
	ctx context.Context,
	t *task,
	c github.Comment,
	command turn.Command,
	log *slog.Logger,
) error {
	graph, err := d.db.HasWorkflowGraphSession(ctx, t.session.ID, string(agent.ModeValidate))
	if err != nil {
		return d.leave(ctx, t, log, "could not read the Validate route for "+command.Name, err)
	}
	if !graph {
		return d.reply(ctx, t, c, turn.Command{Kind: turn.KindUnknown, Name: command.Name, Tagged: command.Tagged}, log)
	}
	trigger, triggerErr := d.projectTrigger(ctx, t.session.ID, c)
	if triggerErr != nil {
		return triggerErr
	}
	return d.run(ctx, t, trigger, command, log)
}

// handleSchedule accepts direct factory scheduling controls. A schedule is
// saved before its acknowledgement is posted, but its source comment is not
// consumed until that post succeeds; replay therefore reuses the stored row
// instead of creating a second unit of work.
func (d *Daemon) handleSchedule(
	ctx context.Context,
	t *task,
	c github.Comment,
	command turn.Command,
	log *slog.Logger,
) error {
	request, err := schedule.Parse(command.Text, t.tickAt, time.Local)
	if err != nil {
		return d.replySchedule(ctx, t, c, report.ReplyScheduleInvalid, report.ScheduleReply{}, log)
	}
	if request.Kind != schedule.KindDirect {
		return d.handleProposalControl(ctx, t, c, request, log)
	}

	stored, _, err := d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{
		SessionID:       t.session.ID,
		SourceCommentID: c.ID,
		OwnerHandle:     c.Author.Login,
		Payload:         request.Payload,
		DueAt:           request.DueAt,
		Delay:           request.Delay,
		PrerequisiteIDs: request.After,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not save the schedule", err)
	}

	outstanding, err := d.db.CountPendingSchedules(ctx)
	if err != nil {
		return d.leave(ctx, t, log, "could not count scheduled work", err)
	}

	reply := report.ScheduleReply{
		ID:          stored.ID,
		DueAt:       stored.DueAt,
		Delay:       stored.Delay,
		After:       append([]int64(nil), request.After...),
		Outstanding: outstanding,
	}
	return d.replySchedule(ctx, t, c, report.ReplyScheduleConfirmed, reply, log)
}

// replySchedule posts and only then consumes a factory scheduling response.
// It uses the ordinary Reporter path so scheduling obeys the same post-before-
// consume guarantee as every other reply.
func (d *Daemon) replySchedule(
	ctx context.Context,
	t *task,
	c github.Comment,
	kind report.ReplyKind,
	scheduled report.ScheduleReply,
	log *slog.Logger,
) error {
	posted, err := d.reportReply(ctx, turn.ReplyRequest{
		Ref:          t.ref,
		SessionID:    t.session.ID,
		CommentID:    c.ID,
		ScheduleKind: kind,
		Schedule:     scheduled,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not reply to /schedule", d.orphaned(ctx, log, posted, err))
	}
	if !posted.Skipped {
		log.InfoContext(ctx, "handled a factory scheduling control",
			slog.String("reply", kind.String()),
			slog.Int64("schedule_id", scheduled.ID),
			slog.Int("scheduled", scheduled.Outstanding),
			slog.Int64("reply_comment_id", posted.CommentID))
	}
	return nil
}

// acknowledge adds the 👀 reaction to a comment litefactory is about to act on
// (AC-2.17).
func (d *Daemon) acknowledge(ctx context.Context, t *task, commentID int64) {
	if err := d.reactToComment(ctx, t.forgeRepo, t.issue.Number, commentID, github.ReactionEyes); err != nil {
		t.log.WarnContext(ctx, "could not acknowledge the comment with a reaction; carrying on",
			slog.Int64("comment_id", commentID),
			slog.Any("error", err))
		return
	}
	t.log.DebugContext(ctx, "acknowledged the comment", slog.Int64("comment_id", commentID))
}

// reply answers a command litefactory will not act on: one from a later
// milestone, one that is not in the vocabulary at all, or a bare /spike with no
// text (AC-2.18, AC-2.20). Nothing is spawned and the watermark advances.
func (d *Daemon) reply(
	ctx context.Context,
	t *task,
	c github.Comment,
	command turn.Command,
	log *slog.Logger,
) error {
	posted, err := d.reportReply(ctx, turn.ReplyRequest{
		Ref:       t.ref,
		SessionID: t.session.ID,
		CommentID: c.ID,
		Command:   command,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not reply to "+command.Name, d.orphaned(ctx, log, posted, err))
	}
	if !posted.Skipped {
		log.InfoContext(ctx, "replied to a command litefactory does not act on",
			slog.String("command", command.Name),
			slog.String("kind", command.Kind.String()),
			slog.Int64("reply_comment_id", posted.CommentID))
	}
	return nil
}

// run executes one instruction and reports what it did.
//
// It takes a [turn.Trigger] rather than the comment it usually came from,
// because it no longer always comes from one: [Daemon.runIssueBody] calls this
// with the issue body's trigger, whose comment id is 0. Everything the turn
// needs about what started it is in that struct, and everything the *log* needs
// is in the logger the caller has already decorated — nothing is read back off
// a comment here.
//
// The turn itself is [turn.Execute]: lock, prepare, run claude, harvest. None
// of that is this package's business — what is, is that a turn which found the
// task locked consumes nothing, and so stops this task's queue rather than
// drawing a "busy" reply nobody asked for (AC-3.1).
func (d *Daemon) run(
	ctx context.Context,
	t *task,
	trigger turn.Trigger,
	command turn.Command,
	log *slog.Logger,
) error {
	ctx = sessionContext(ctx, t.session.ID)
	if d.bug != nil {
		if err := d.bug.CheckExistingAdmission(ctx, t.session.ID); err != nil {
			return d.leave(ctx, t, log, "unsupported pinned Bug work preserved", err)
		}
	}

	// A graph Plan story issue only ever runs Plan, whatever its labels say, and
	// so does Plan work the factory started itself.
	workerRun := t.scheduled != nil && t.scheduled.Payload == implementWorkPayload
	if !workerRun && store.IsGraphTrigger(trigger.CommentID) {
		// A repair of a story worker is owed as a graph dispatch, not a schedule.
		if _, err := d.db.ImplementStoryByWorkerSession(ctx, t.session.ID); err == nil {
			workerRun = true
		} else if !errors.Is(err, store.ErrNotFound) {
			return d.leave(ctx, t, log, "could not tell whether this is an Implement story worker", err)
		}
	}
	if workerRun {
		t.implementing = true
	} else if t.scheduled != nil && t.scheduled.Payload == planWorkPayload {
		t.refining = true
	} else if _, owned, err := d.graphPlanStoryForTask(ctx, t); err != nil {
		return d.leave(ctx, t, log, "could not tell whether this is a graph Plan story issue", err)
	} else if owned {
		t.refining = true
	}

	if pending, err := d.resumeReport(ctx, t, trigger.CommentID); pending || err != nil {
		return d.leave(ctx, t, log, "prior delivery remains pending", err)
	}

	workflow, mode, err := d.admitWorkflow(ctx, t, trigger, command)
	if err != nil {
		var refusal *modeRefusal
		if errors.As(err, &refusal) {
			return d.refuseMode(ctx, t, trigger, refusal.kind, refusal.mode)
		}
		return d.leave(ctx, t, log, "mode selection was refused", err)
	}
	if workflow == agent.WorkflowBug && d.bug == nil {
		return d.refuseMode(ctx, t, trigger, report.ReplyBugUnavailable, "")
	}
	// A story issue without its own agent label takes its parent's.
	if err := d.inheritParentAgent(ctx, t); err != nil {
		return d.leave(ctx, t, log, "could not give the story its parent's agent", err)
	}
	// Resolve the agent-selection label before checking for an obsolete
	// permission label and before anything is locked, fetched, or written.
	agentChoice := d.resolveAgent(t.issue.Labels)
	if agentChoice.refused() {
		return d.refuseAgent(ctx, t, trigger, agentChoice, log)
	}
	if agentChoice.Agent != "" {
		log = log.With(slog.String("agent_label", agentChoice.Agent))
	}
	selectedAgent := agentChoice.Agent
	if selectedAgent == "" {
		selectedAgent = string(d.defaultAgent)
	}

	// Permission labels are obsolete and always refuse. Detection folds case,
	// and no label value crosses this boundary into logs or replies.
	if legacyToolsLabel(t.issue.Labels) {
		return d.refuseTools(ctx, t, trigger,
			toolDecision{Refusal: report.ReplyLegacyToolLabel}, log)
	}

	// Select permissions only after both the concrete mode and effective agent
	// are known. A missing entry is an invariant violation, never a reason to
	// fall back to a potentially broader agent-global surface.
	tools, ok := d.resolveWorkflowTools(workflow, mode, agent.Kind(selectedAgent))
	if workflow == agent.WorkflowBug && d.bug != nil {
		// This carries identity only. The runtime resolves the particular
		// analyst/fixer grant when dispatching that role, including resumes.
		tools, ok = toolDecision{Profile: "bug"}, true
	}
	if !ok {
		return d.leave(ctx, t, log, "could not select permissions for the resolved mode and agent",
			fmt.Errorf("no permission profile for mode %q and agent %q", mode, selectedAgent))
	}
	// The ingestion permission is resolved with the rest of the mode profile.
	attachmentsAllowed := tools.Attachments
	log = log.With(slog.String("tool_profile", tools.Profile))
	log.InfoContext(ctx, "selected the mode permission profile for this run",
		slog.Any("tools", tools.Tools),
		slog.Any("allowed_tools", tools.AllowedTools),
		slog.String("codex_sandbox", tools.Sandbox),
		slog.Any("tool_ceiling", d.toolCeiling))

	// "Sync:" is a per-issue escape hatch, not a gate on this turn: a
	// malformed, miscased, or duplicated label leaves syncOverride nil so
	// the configured automatic-sync default applies, same as silence — it
	// never blocks the run the way a refused agent or mode label does.
	var syncOverride *bool
	if syncChoice := resolveSync(t.issue.Labels); syncChoice.Resolved {
		value := syncChoice.Value
		syncOverride = &value
	}

	if handled, err := d.recoverGraphReply(ctx, t, trigger, command); handled || err != nil {
		return d.leave(ctx, t, log, "stopped graph reply recovery deferred", err)
	}

	// Claim after mode, agent and permission admission can no longer consume
	// this input as a runless refusal; preparation failure retains the claim.
	// A /validate start goes to Validate admission only: the owner wait can
	// never apply it, and a refused start must leave the wait open.
	validateStart := mode == agent.ModeValidate && command.Name == "/validate"
	if (mode == agent.ModeResearch || mode == agent.ModeSpike || mode == agent.ModeImplement || mode == agent.ModeValidate || workflow == agent.WorkflowBug) && t.continuation == nil && !validateStart {
		if err := d.claimGraphReply(ctx, t, trigger, string(mode)); err != nil {
			// A comment written before the open question was published can
			// never answer it. Refuse it once, visibly; the reply path consumes
			// it only after confirmed delivery, so later comments proceed.
			if errors.Is(err, store.ErrGraphReplyPredatesQuestion) {
				log.InfoContext(ctx, "Research owner comment predates the open question; refusing it with a reply",
					slog.Any("error", err))
				return d.refuseMode(ctx, t, trigger, report.ReplyGraphReplyBetweenQuestions, "")
			}
			// A plain Research or Implement comment refused because the wait holds
			// another reply is answered and consumed. Explicit commands keep retry.
			if errors.Is(err, store.ErrGraphOwnerWait) && (mode == agent.ModeResearch || (mode == agent.ModeImplement && command.Name == "")) &&
				workflow != agent.WorkflowBug && command.Kind == turn.KindFreeText {
				log.InfoContext(ctx, "Owner comment refused while the wait holds another reply; refusing it with a reply",
					slog.Any("error", err))
				return d.refuseMode(ctx, t, trigger, report.ReplyGraphOwnerWaitRefused, "")
			}
			return d.leave(ctx, t, log, "Research owner reply admission failed", err)
		}
	}
	if t.worker != nil {
		t.worker.executionRequests++
	}
	turnResult, err := d.turn.Execute(ctx, turn.Request{
		Continuation: t.continuation,
		Mode:         mode,
		Workflow:     workflow,
		SessionID:    t.session.ID,
		Issue: turn.Issue{
			Number: t.issue.Number,
			Title:  t.issue.Title,
			Body:   t.issue.Body,
			Author: t.issue.Author.Login,
			// Copied rather than shared: the tick's issue snapshot outlives
			// this call, and a turn that appended to a slice it did not own
			// would reach back into it.
			Labels: append([]string(nil), t.issue.Labels...),
		},
		Trigger: trigger,
		Command: command,
		// Copies of the mode-selected operator configuration. No forge label
		// contributes permission data to this request.
		Tools:              append([]string(nil), tools.Tools...),
		AllowedTools:       append([]string(nil), tools.AllowedTools...),
		ToolProfile:        tools.Profile,
		Agent:              agent.Kind(selectedAgent),
		CodexSandbox:       tools.Sandbox,
		AttachmentsAllowed: attachmentsAllowed,
		SyncOverride:       syncOverride,
	})
	if turnResult.Repair != nil {
		d.retainStoppedResult(t, turnResult.Repair, trigger, mode)
		source := store.ReportRecovery{Key: "stopped:" + recoveryKey(t.session.ID, trigger.CommentID), SessionID: t.session.ID, TriggerID: trigger.CommentID, Task: t.ref.String(), RunID: turnResult.Run.ID, State: "stopped"}
		if err != nil {
			d.recoveryFailure(ctx, source, t.tickAt, err)
		}
		return d.leave(ctx, t, log, "stopped outcome requires repair", &recoveryPending{err})
	}
	if _, stopped := turnResult.Run.OwnerStop(); stopped {
		t.recordWorkerStatus(workerCancelled)
		if trigger.CommentID == 0 {
			return nil
		}
		_, consumeErr := d.reporter.Consume(context.WithoutCancel(ctx), t.session.ID, trigger.CommentID)
		return consumeErr
	}
	if turnResult.Outcome == agent.OutcomeAbandoned {
		t.recordWorkerStatus(workerCancelled)
	}
	if err != nil {
		var refusal *workflowRefusal
		if errors.As(err, &refusal) {
			return d.refuseMode(ctx, t, trigger, refusal.kind, mode)
		}
		if mode == agent.ModeImplement && workflow != agent.WorkflowBug && errors.Is(err, turn.ErrImplementReplyNotApplied) {
			return d.refuseMode(ctx, t, trigger, report.ReplyImplementCommentNotApplied, "")
		}
		// A Bug owner comment written before the open review or question can
		// never answer it; refuse it once and consume it, as for Research.
		if workflow == agent.WorkflowBug && errors.Is(err, store.ErrGraphReplyPredatesQuestion) {
			log.InfoContext(ctx, "Bug owner comment predates the open review or question; refusing it with a reply",
				slog.Any("error", err))
			return d.refuseMode(ctx, t, trigger, report.ReplyGraphReplyBetweenQuestions, "")
		}
		if workflow == agent.WorkflowBug && errors.Is(err, turn.ErrBugReplyNotApplied) {
			log.InfoContext(ctx, "Bug owner comment could not be applied at the current step; refusing it with a reply",
				slog.Any("error", err))
			return d.refuseMode(ctx, t, trigger, report.ReplyBugCommentNotApplied, "")
		}
		if workflow == agent.WorkflowBug && errors.Is(err, turn.ErrBugCommentRefused) {
			log.InfoContext(ctx, "Bug owner comment refused with a reply and consumed", slog.Any("error", err))
			return nil
		}
		left := d.leave(ctx, t, log, "the turn could not be conducted", err)
		if trigger.PlanReply != nil && left == nil {
			// A child reply's cursor advances whenever run returns nil, and leave
			// swallows the error. The reply is handled only together with the
			// failure comment, so it must stay pending until a replay of the
			// failed run has published one.
			return &recoveryPending{err}
		}
		return left
	}

	if turnResult.Disposition == turn.DispositionLocked {
		d.commentFate(ctx, t, trigger.CommentID, fateQueued, "queued behind the run already in progress on this issue")
		t.recordWorkerStatus(workerDeferred)
		t.continuationLocked = t.continuation != nil
		t.bugDispatchLocked = workflow == agent.WorkflowBug
		// RD-4: a lock lost to a second process is promoted from a drop to a
		// marker-set. The dispatch above has already set the flag and the drain's
		// deferral keeps it, but this is the one disposition whose entire meaning
		// is "this work did not happen and the session must be reconsidered", and
		// saying so here rather than inferring it from the drain's bookkeeping is
		// what keeps it true when the drain is no longer what notices. One
		// idempotent UPDATE, on a path only a contended task ever takes.
		d.markBacklog(ctx, log, t.session.ID)
		// Another *process*, and no longer merely another instance. The in-flight
		// set is checked and inserted on the Tick goroutine and Tick is never
		// concurrent with itself, so two workers of this daemon can never both
		// reach AcquireSessionLock for one session: within this process the store
		// lock has become defence in depth. A second daemon against the same
		// $LF_HOME is the only thing left that can produce this. See [inflight].
		log.InfoContext(ctx, "task is locked by another daemon process; leaving the comment for a later tick")
		if trigger.PlanReply != nil {
			return &recoveryPending{errors.New("plan child reply waits for the current session lock owner")}
		}
		return nil
	}

	if turnResult.Run.ID != 0 {
		d.commentFate(ctx, t, trigger.CommentID, fateDispatched, fmt.Sprintf("dispatched as run %d", turnResult.Run.ID))
	}
	if workflow == agent.WorkflowBug {
		// Bug owns durable notices, review publication and input consumption.
		// Generic mode reporting would duplicate them and could consume a reply
		// before its publication or domain operation has been confirmed.
		return nil
	}
	return d.completeTurn(ctx, t, trigger, mode, turnResult, log)
}

func (d *Daemon) completeTurn(ctx context.Context, t *task, trigger turn.Trigger, mode agent.Mode, turnResult turn.Result, log *slog.Logger) error {
	if mode == agent.ModePlan && turnResult.ResearchGraph && trigger.CommentID > 0 && !store.IsPrivateTrigger(trigger.CommentID) && turn.ParseCommand(trigger.Body).Kind == turn.KindFreeText {
		if err := d.requirePlanDiscussionReported(ctx, turnResult); err != nil {
			return &recoveryPending{err}
		}
	}
	if trigger.PlanReply != nil {
		// A child comment belongs to its own discovery cursor, except on a story
		// that is a task of its own, whose watermark this reply advances. The Plan
		// policy journals consumed reply IDs before effects; refinePlanChildren
		// advances a parent-routed child's cursor after this return. Parent input
		// consumption would skip unrelated parent comments whose globally
		// allocated IDs are lower.
		reply := trigger.PlanReply
		if mode != agent.ModePlan || reply.ID != trigger.CommentID || reply.ActorID != trigger.AuthorID || reply.OwnerID != d.ticketIdentities(ctx).OwnerUserID {
			return errors.New("plan child completion correlation mismatch")
		}
		route, err := d.db.PlanExecutionPath(ctx, t.session.ID)
		if err != nil {
			return err
		}
		if route != "graph" {
			return errors.New("plan child completion lacks graph admission")
		}
		if err := d.requirePlanFailureReported(ctx, turnResult); err != nil {
			return &recoveryPending{err}
		}
		// Handled, or answered with a published failure the owner can reply to:
		// either way this reply is done and the story's next reply is a new one.
		if _, task, err := d.graphPlanStoryForTask(ctx, t); err != nil {
			return err
		} else if task {
			if _, err := d.reporter.Consume(ctx, t.session.ID, trigger.CommentID); err != nil {
				return d.leave(ctx, t, log, "could not consume a Plan story reply", err)
			}
		}
		return nil
	}
	if turnResult.ResearchGraph {
		if mode == agent.ModePlan && turnResult.Run.ID == 0 {
			attrs := []any{slog.String("task", t.ref.String()), slog.Int64("session_id", t.session.ID),
				slog.Int64("trigger_id", trigger.CommentID), slog.String("reason", turnResult.Reason)}
			if t.scheduled != nil {
				attrs = append(attrs, slog.Int64("schedule_id", t.scheduled.ID))
			}
			log.InfoContext(ctx, "Plan work returned without a run", attrs...)
		}
		if err := d.requirePlanFailureReported(ctx, turnResult); err != nil {
			return d.leave(ctx, t, log, "the failed Plan run has no published failure comment", err)
		}
		// Explicit graph comment activities own publication. Consuming an input
		// here acknowledges handling only; it never rewrites the run outcome.
		if _, err := d.reporter.Consume(ctx, t.session.ID, trigger.CommentID); err != nil {
			return d.leave(ctx, t, log, "could not consume Research graph input", err)
		}
		// After the consume, never before: the presentation must not be the first
		// thing to advance the watermark. Its failure leaves the consumed input
		// and the run outcome exactly as they are.
		return d.leave(ctx, t, log, "the creation proposal could not be presented",
			d.presentCreationProposal(ctx, t, turnResult.Run.ID, turnResult.Run.TriggerCommentID))
	}
	posted, err := d.reportTurn(ctx, turnResult)
	if err != nil {
		return d.leave(ctx, t, log, "the turn could not be reported", d.orphaned(ctx, log, posted, err))
	}
	if mode.Valid() && len(d.classifiers) > 0 && t.continuation == nil {
		if err := d.db.MarkModeCleanupPending(ctx, t.session.ID, trigger.CommentID); err != nil {
			return err
		}
		if err := d.db.DeleteModeSelection(ctx, t.session.ID, trigger.CommentID); err != nil {
			return err
		}
	}
	if t.continuation != nil && turnResult.Run.ID == 0 {
		return fmt.Errorf("automatic recovery could not prepare; manual recovery required: %s", turnResult.Disposition)
	}
	log.InfoContext(ctx, "turn complete",
		slog.String("disposition", turnResult.Disposition.String()),
		slog.String("outcome", turnResult.Outcome.String()),
		slog.Bool("succeeded", turnResult.Succeeded()),
		slog.String("branch", turnResult.Branch),
		slog.String("commit", turnResult.CommitSHA),
		slog.Bool("pushed", turnResult.Pushed),
		slog.Int64("report_comment_id", posted.CommentID),
		slog.Bool("reported", !posted.Skipped))
	// The run's own result is reported and its trigger consumed, so a
	// presentation failure below changes neither. This call site is shared by
	// [Daemon.repairStoppedResult], which is how a repaired stopped result
	// inherits the hook.
	return d.leave(ctx, t, log, "the creation proposal could not be presented",
		d.presentCreationProposal(ctx, t, turnResult.Run.ID, turnResult.Run.TriggerCommentID))
}

// requirePlanFailureReported keeps the owner's triggering comment unhandled after
// a failed graph Plan run until that run's failure comment is published. The
// comment and its handling are one act: a failed run whose failure never reached
// the issue would otherwise spend the owner's input and leave nothing visible.
// A successful run, and any run of another mode, owes nothing here.
func (d *Daemon) requirePlanFailureReported(ctx context.Context, result turn.Result) error {
	if !result.ResearchGraph || result.Run.ID == 0 || result.Run.Mode != string(agent.ModePlan) || result.Run.Succeeded() {
		return nil
	}
	reported, err := d.db.GraphFailureReported(ctx, result.Run.ID, turn.PlanFailureNode)
	if err != nil {
		return fmt.Errorf("read the failure comment of failed Plan run %d: %w", result.Run.ID, err)
	}
	if !reported {
		attempts, err := d.db.ListGraphNodeAttemptsByRun(ctx, result.Run.ID)
		if err != nil {
			return err
		}
		if len(attempts) > 0 {
			// A failed publication may have recovered its original answer or
			// review without creating a second failure notice.
			reported, err = d.db.GraphFailureReported(ctx, result.Run.ID, attempts[len(attempts)-1].Node)
			if err != nil {
				return err
			}
		}
		if !reported {
			return fmt.Errorf("failed Plan run %d has no published failure comment; the owner's comment stays unhandled", result.Run.ID)
		}
	}
	return nil
}

// refuseTools answers any obsolete lf:tools= label. The label's value is not
// carried into this function, so neither logs nor replies can echo it.
//
// Nothing runs. That is the point: a label is a permission request, and every
// way of failing to understand one has the same safe answer, which is to
// decline and say so (C9). Falling through to the default surface would answer
// "which tools may this have" with "the ones it would have had anyway", which is
// the failure this whole feature exists to prevent.
//
// The refusal is a result and not an error, exactly like every other turn that
// went badly: it owes the thread a comment, and an error return is the one shape
// that cannot carry one.
//
// # The issue-body path posts nothing
//
// A run started from the issue body has no comment to reply to — its trigger id
// is 0, and [turn.ReplyRequest] requires a real one, because a reply is
// something posted *in answer to* a comment and consumed by advancing the
// watermark past it. So on that path the refusal refuses the run, says so in the
// log, and posts nothing.
//
// That is consistent with what the body path already does with a command it will
// not act on (AC-2.16c): a body that spawns nothing posts nothing. It is
// fail-closed, because the run does not happen either way. And it is not a
// permanent silence: the next comment on the issue arrives through
// [Daemon.handleComment], which has a comment id, and refuses loudly there. If
// that trade is ever revisited, this is the only branch to change.
func (d *Daemon) refuseTools(
	ctx context.Context,
	t *task,
	trigger turn.Trigger,
	decision toolDecision,
	log *slog.Logger,
) error {
	log = log.With(slog.String("tools_refusal", decision.Refusal.String()))

	if trigger.CommentID == 0 {
		log.InfoContext(ctx, "refusing to run the issue body: it carries an obsolete lf:tools= label. "+
			"Nothing was run and no comment was posted, "+
			"because there is no comment to answer; the next comment on this issue is refused "+
			"on the thread")
		return nil
	}

	posted, err := d.reportReply(ctx, turn.ReplyRequest{
		Ref:       t.ref,
		SessionID: t.session.ID,
		CommentID: trigger.CommentID,
		Refusal:   decision.Refusal,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not refuse the tool profile this issue asks for",
			d.orphaned(ctx, log, posted, err))
	}
	if !posted.Skipped {
		log.InfoContext(ctx, "refused a run: the issue carries an obsolete lf:tools= label",
			slog.Int64("reply_comment_id", posted.CommentID))
	}
	return nil
}

// refuseAgent answers a Coding Agent label that could not be resolved into a
// coding-agent tool: unknown, malformed, ambiguous, or named on a daemon
// with no implementation of it running at all.
//
// It is checked before [Daemon.refuseTools] for the same tick: an agent
// refusal is reported without inspecting or echoing any obsolete permission
// label payload. Everything else about its shape mirrors [Daemon.refuseTools],
// including the issue-body path posting nothing.
func (d *Daemon) refuseAgent(
	ctx context.Context,
	t *task,
	trigger turn.Trigger,
	decision agentDecision,
	log *slog.Logger,
) error {
	log = log.With(slog.String("agent_refusal", decision.Refusal.String()))
	if decision.MiscasedPrefix {
		// See the identical line in [Daemon.refuseTools]: detection folds
		// case and acceptance does not (D-6), and the label text itself is
		// not logged, for the same reason it is not echoed onto the thread
		// (S-9).
		log = log.With(slog.Bool("miscased_agent_label_prefix", true))
	}

	if trigger.CommentID == 0 {
		log.InfoContext(ctx, "refusing to run the issue body: its Coding Agent label does not "+
			"resolve to a coding-agent tool litefactory has running. Nothing was run and no "+
			"comment was posted, because there is no comment to answer; the next comment on this "+
			"issue is refused on the thread")
		return nil
	}

	posted, err := d.reportReply(ctx, turn.ReplyRequest{
		Ref:       t.ref,
		SessionID: t.session.ID,
		CommentID: trigger.CommentID,
		Refusal:   decision.Refusal,
		// Empty for every refusal but the unknown one, which is the only
		// refusal that may name what it was asked for. internal/report
		// re-checks it before rendering (S-8).
		Agent: decision.Agent,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not refuse the coding-agent tool this issue asks for",
			d.orphaned(ctx, log, posted, err))
	}
	if !posted.Skipped {
		log.InfoContext(ctx, "refused a run: the Coding Agent label on this issue does not "+
			"resolve to a coding-agent tool litefactory has running",
			slog.Int64("reply_comment_id", posted.CommentID))
	}
	return nil
}

// leave decides what a per-comment failure costs.
//
// Shutdown is the only failure below the listings that still ends the tick.
// Everything else is logged here and goes no further: the comment was not
// consumed, [Daemon.drain] will notice, and this task's queue stops with the
// comment still waiting.
//
// A rate limit used to be the exception, returned so that it escalated five
// frames — handleComment, drain, processIssue, poll — into [Daemon.Tick]'s
// error branch, where the backoff was armed. There is no such chain left:
// those five frames are a worker's now, and a worker returns to nobody. It now arms the backoff here,
// through [Daemon.rateLimited], and then takes the ordinary path. That is a
// simplification rather than a concession: a return-value chain cannot cross a
// goroutine boundary, and nothing was gained by unwinding, because the hold is
// what stops the next tick either way. Two lines come out of it — the warning
// naming the hold, and the line below saying what this comment lost.
func (d *Daemon) leave(ctx context.Context, t *task, log *slog.Logger, what string, err error) error {
	if isRecoveryPending(err) {
		d.commentFate(ctx, t, t.commentUnderWay, fateDeferred, "deferred, "+what+"; it is retried on a later heartbeat")
		t.recordWorkerStatus(workerDeferred)
		return nil
	}
	if err == nil {
		return nil
	}
	t.recordWorkerError(err)
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		// Shutdown, not a failure of this comment. Ending the tick is what the
		// signal asked for.
		return err
	}
	d.commentFate(ctx, t, t.commentUnderWay, fateDeferred, "deferred, "+what+"; it is retried on a later heartbeat")
	d.rateLimited(ctx, log, t.tickAt, err)
	d.prepareFailed(ctx, t, err)
	log.ErrorContext(ctx, what+"; the comment stays unconsumed and this task's queue stops here",
		slog.Any("error", err))
	if store.IsGraphTrigger(t.commentUnderWay) || t.scheduled != nil {
		// Dispatches and schedules need the actual cause for accounting and
		// bounded retry; no owner-comment drain retains their errors.
		return err
	}
	return nil
}

// orphaned preserves the returned receipt; reportAttempt owns bounded diagnostics.
func (d *Daemon) orphaned(_ context.Context, _ *slog.Logger, _ turn.Posted, err error) error {
	return err
}

// observe records a rate limit that ended a listing and returns the error, so
// that the tick ends with it.
//
// The listing path is the one place a rate limit still unwinds, and it can
// afford to: it runs on the Tick goroutine, before any task has been touched.
// The cursor was not persisted and no watermark was moved, so there is nothing
// to undo — the tick simply did not happen, and the next one asks for the same
// window (AC-2.12, AC-2.13). A refusal met further down goes through
// [Daemon.leave], which arms the same backoff without unwinding anything.
func (d *Daemon) observe(ctx context.Context, now time.Time, err error) error {
	d.rateLimited(ctx, d.log, now, err)
	return err
}

// rateLimited arms the backoff when err is the forge refusing a call over
// quota, and says so in the log. Anything else is not its business and is left
// entirely to the caller.
//
// It takes its logger and its instant instead of reading either off the daemon,
// because it is called from wherever the refusal was met rather than from
// [Daemon.Tick]. The logger is the caller's own, already decorated with the task
// and comment the refusal belongs to. The instant is the tick's, carried down on
// [task.tickAt], because the test clock advances on every reading and only Tick
// may turn it.
//
// It is safe to call from any goroutine: [backoff] is mutex-guarded, and
// [backoff.fail] is idempotent inside a standing hold, so a tick whose tasks are
// all refused at once arms one hold between them rather than doubling it per
// refusal (AC-PAR-4.2).
func (d *Daemon) rateLimited(ctx context.Context, log *slog.Logger, now time.Time, err error) {
	if d.connections != nil {
		return
	} // The version-bound transport already recorded this refusal.
	var ticketRate *ticket.RateLimitError
	if errors.As(err, &ticketRate) {
		failures, delay, until := d.backoff.fail(now, ticketRate.RetryAfter)
		log.WarnContext(ctx, "ticket provider quota exhausted; backing off with recovery positions unchanged",
			slog.Int("consecutive_refusals", failures),
			slog.String("retry_after", ticketRate.RetryAfter.String()),
			slog.String("backoff", delay.String()),
			slog.Time("resumes_at", until.UTC()))
		return
	}
	var rateLimit *github.RateLimitError
	if !errors.As(err, &rateLimit) {
		return
	}

	failures, delay, until := d.backoff.fail(now, rateLimit.RetryAfter)
	log.WarnContext(ctx, "github refused a call with a rate limit; backing off. The poll cursor and "+
		"every watermark are unchanged, and polling resumes on its own",
		slog.Int("consecutive_refusals", failures),
		slog.String("retry_after", rateLimit.RetryAfter.String()),
		slog.String("backoff", delay.String()),
		slog.Time("resumes_at", until.UTC()),
		slog.Bool("secondary", rateLimit.Secondary),
		slog.String("resource", rateLimit.Resource),
		slog.Any("error", err))
}

// A discussion is consumed only after its own graph publication is confirmed.
func (d *Daemon) requirePlanDiscussionReported(ctx context.Context, result turn.Result) error {
	if result.Run.ID > 0 {
		for _, node := range []string{"failed", "set_reply", "set_review", "set_ask", "story_reply", "story_review", "story_ask", "technical_reply", "technical_review", "technical_ask", "acceptance_reply", "acceptance_review", "acceptance_ask"} {
			reported, err := d.db.GraphFailureReported(ctx, result.Run.ID, node)
			if err != nil {
				return err
			}
			if reported {
				return nil
			}
		}
	}
	return errors.New("plan discussion has no confirmed answer, review or refusal; the comment stays pending")
}

func providerRateLimited(err error) bool {
	var codeRate *github.RateLimitError
	var ticketRate *ticket.RateLimitError
	return errors.As(err, &codeRate) || errors.As(err, &ticketRate)
}

// lowDisk reports whether free space on the daemon volume is below the
// configured threshold and logs the shortage. An unreadable volume does not
// hold work: the guard prevents a full disk, it does not gate on probes.
func (d *Daemon) lowDisk(ctx context.Context) bool {
	if d.minFreeDiskBytes == 0 || d.diskPath == "" {
		return false
	}
	read := d.freeDiskBytes
	if read == nil {
		read = bugchecks.FreeDiskBytes
	}
	free, err := read(d.diskPath)
	if err != nil {
		d.log.WarnContext(ctx, "free disk space unreadable; dispatch continues", slog.String("path", d.diskPath), slog.Any("error", err))
		return false
	}
	if free >= d.minFreeDiskBytes {
		return false
	}
	d.log.WarnContext(ctx, "free disk space below threshold; no run starts until space is freed",
		slog.String("path", d.diskPath),
		slog.Uint64("free_bytes", free),
		slog.Uint64("required_bytes", d.minFreeDiskBytes))
	return true
}

// candidateHold contains only the metadata defining an observed admission hold.
// It never participates in eligibility or retry bookkeeping.
type candidateHold struct {
	kind, key, state, reason string
	nextAt                   time.Time
}

func (d *Daemon) observeCandidateHold(ctx context.Context, c *candidate, at time.Time) bool {
	var hold candidateHold
	if row, held := d.sessionRecoveryHold(c.session.ID, at); held {
		hold = candidateHold{kind: "report-recovery", key: row.Key, state: row.State, nextAt: row.NextAt}
	} else if row, held := d.prepFailures.hold(c.session.ID, at); held {
		hold = candidateHold{kind: "preparation", reason: row.Reason, nextAt: row.NextAt}
	} else {
		delete(d.holdDiagnostics, c.session.ID)
		return false
	}
	previous, seen := d.holdDiagnostics[c.session.ID]
	if !seen || previous.kind != hold.kind || previous.key != hold.key || previous.state != hold.state || previous.reason != hold.reason || !previous.nextAt.Equal(hold.nextAt) {
		if d.holdDiagnostics == nil {
			d.holdDiagnostics = make(map[int64]candidateHold)
		}
		d.holdDiagnostics[c.session.ID] = hold
		d.log.InfoContext(ctx, "session admission held", slog.Int64("session_id", c.session.ID), slog.String("task", c.ref.String()), slog.String("hold_kind", hold.kind), slog.String("recovery", hold.key), slog.String("state", hold.state), slog.String("reason", hold.reason), slog.Time("next_eligible", hold.nextAt))
	}
	return true
}
