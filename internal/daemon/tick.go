package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/schedule"
	"github.com/cloudvane/litefactory/internal/store"
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
func (d *Daemon) Tick(ctx context.Context) (TickResult, error) {
	startedAt := d.now()
	d.discovery.tick(startedAt)
	ctx = d.discoveryContext(ctx)
	counts := &discoveryCounts{}
	ctx = github.WithRequestObserver(ctx, counts.observe)
	defer d.discoverySummary(ctx, "tick", counts)
	ctx = context.WithValue(ctx, recoveryTickKey{}, startedAt)
	d.refreshRecoveries(ctx, startedAt)
	// Stopped execution debt is independent of discovery/storage availability.
	stoppedDispatched := d.retryStoppedResults(ctx, startedAt)
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

	if retryAt, held := d.backoff.hold(startedAt); held {
		d.log.DebugContext(ctx, "skipping the tick; still backing off from a rate limit",
			slog.Time("retry_at", retryAt.UTC()),
			slog.String("remaining", retryAt.Sub(startedAt).String()))
		return TickResult{StartedAt: startedAt, Skipped: true, RetryAt: retryAt}, nil
	}

	// Progress is reconciled even while the sole turn worker is occupied.
	if progress, ok := d.turn.(interface{ PollStoryProgress(context.Context) error }); ok {
		if err := progress.PollStoryProgress(ctx); err != nil {
			d.log.ErrorContext(ctx, "story progress remains pending", slog.Any("error", err))
			var rate *github.RateLimitError
			if errors.As(err, &rate) {
				return TickResult{StartedAt: startedAt}, d.observe(ctx, startedAt, err)
			}
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

	result := TickResult{StartedAt: startedAt, Repos: len(repos), Cursor: cursor, Dispatched: stoppedDispatched}

	if err := d.replayContinuationCancellations(ctx, repos); err != nil {
		return result, d.observe(ctx, startedAt, err)
	}
	if err := d.replayInterruptionReports(ctx, repos); err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, d.observe(ctx, startedAt, err)
	}

	queues, err := d.discover(ctx, repos, listingWindow(cursor), &result)
	discoveryDeferred := errors.Is(err, ErrDiscoveryDeferred)
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
	if err := d.dispatchContinuations(ctx, reposByID, startedAt, &result); err != nil {
		return result, err
	}
	if err := d.dispatchSchedules(ctx, reposByID, startedAt, &result); err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, err
	}
	if err := d.replayContinuationCancellations(ctx, repos); err != nil {
		return result, d.observe(ctx, startedAt, err)
	}
	d.dispatch(ctx, queues, startedAt, &result)
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

	// The cursor is a pure watermark now: it advances to this tick's start time
	// on every tick that completed, whatever any worker went on to do (T-PAR-13).
	// Holding it back to the oldest issue that did not finish was the only way
	// to re-find that issue while there was no local record of one, and it
	// pinned the whole fleet — one busy repository rewound discovery for every
	// other. sessions.needs_poll is that local record, and [Daemon.discover]
	// unions it with the listings, so re-discovery no longer needs the cursor's
	// help. See [store.DB.ListBacklogSessions].
	if discoveryDeferred {
		result.Duration = d.now().Sub(startedAt)
		return result, nil
	}
	persisted := store.PollCursor{TasksUpdatedSince: startedAt, LastTickAt: d.now()}
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

// listingWindow is the `since` a tick asks the forge for.
//
// The overlap is withheld from the very first tick ever, whose whole purpose is
// to plant the cursor at now so that registering a busy repository does not
// replay its history (AC-2.9). A zero LastTickAt is what says no tick has
// completed before.
func listingWindow(cursor store.PollCursor) time.Time {
	if cursor.LastTickAt.IsZero() {
		return cursor.TasksUpdatedSince
	}
	return cursor.TasksUpdatedSince.Add(-ListingOverlap)
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
	since time.Time,
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

		forgeRepo := github.Repo{Owner: repo.Owner, Name: repo.Name}
		q := &repoQueue{repo: repo, forgeRepo: forgeRepo, log: repoLog}
		queues = append(queues, q)
		byRepo[repo.ID] = q

		if err := d.discoveryProbe(ctx, forgeRepo); err != nil {
			return nil, err
		}
		issues, err := discoveryCall(ctx, d, discoveryKey(forgeRepo, "issues", 0), discoveryListBound, func(ctx context.Context) ([]github.Issue, error) { return d.forge.ListIssues(ctx, forgeRepo, since) })
		if errors.Is(err, ErrDiscoveryDeferred) {
			deferred = true
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
			deferred = true
		} else if err != nil {
			return nil, err
		}
		issues = append(issues, commented...)
		result.Issues += len(issues)

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
	}

	if err := d.managedParents(ctx, byRepo, seen); err != nil {
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
	if _, err := d.db.ManagedParentForChild(ctx, q.repo.ID, issue.Number); err == nil {
		return nil, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if issue.State != github.StateOpen {
		q.log.DebugContext(ctx, "skipping an issue that is not open; closing an issue is how "+
			"litefactory is told to stop working on it",
			slog.Int("issue", issue.Number),
			slog.String("state", issue.State))
		return nil, nil
	}

	session, err := d.db.GetOrCreateSession(ctx, q.repo, issue.Number)
	if err != nil {
		return nil, fmt.Errorf("find the task for %s#%d: %w", q.forgeRepo, issue.Number, err)
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

// candidate is one session discovery found eligible, before the dispatcher has
// decided whether to run it.
//
// It is the discovery phase's output and the dispatch phase's input, and it
// exists rather than a [task] because half of a candidate's issue may not have
// been fetched yet: a session reached only through the backlog has no
// [github.Issue] until [Daemon.materialise] reads one for it.
type candidate struct {
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
	numbers, err := discoveryCall(ctx, d, discoveryKey(repo, "commented", 0), discoveryListBound, func(ctx context.Context) ([]int, error) { return d.forge.ListCommentedIssues(ctx, repo, since) })
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
	// workerLog holds immutable admission context for worker lifetime events.
	workerLog *slog.Logger
	worker    *workerState

	scheduled          *store.Schedule
	continuation       *store.Continuation
	continuationLocked bool

	refining  bool
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
			case offerFull, offerHeld:
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
	if d.sessionRecoveryHeld(c.session.ID, tickAt) {
		return offerSkipped
	}
	if !d.inflight.enter(c.session.ID) {
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

	t, err := d.materialise(ctx, c, tickAt)
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
			return offerSkipped
		}
		if err != nil {
			d.rateLimited(ctx, c.log, tickAt, err)
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
	if retryAt, held := d.backoff.hold(tickAt); held {
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
	go d.runWorker(ctx, t, func() { d.work(ctx, t) })

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
			fetched, err = d.forge.GetIssue(ctx, c.forgeRepo, c.session.IssueNumber)
		} else {
			fetched, err = d.discoveryIssue(ctx, c.forgeRepo, c.session.IssueNumber)
		}
		if err != nil {
			return nil, fmt.Errorf("read %s#%d, which still needs a poll: %w",
				c.forgeRepo, c.session.IssueNumber, err)
		}
		issue = fetched
	}
	if issue.State == "closed" {
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
	if issue.State != github.StateOpen {
		c.log.DebugContext(ctx, "a task that still needs a poll is no longer open; nothing runs",
			slog.String("state", issue.State))
		return nil, nil
	}

	return &task{
		repo:      c.repo,
		forgeRepo: c.forgeRepo,
		issue:     issue,
		session:   c.session,
		ref:       c.ref,
		log:       c.log,
		tickAt:    tickAt,
		workerLog: c.log.With(slog.Any("dispatch_sources", slices.Clone(c.sources)), slog.Time("tick_at", tickAt)),
	}, nil
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
	counts := &discoveryCounts{}
	ctx = github.WithRequestObserver(ctx, counts.observe)
	defer d.discoverySummary(ctx, "worker", counts)
	if deferred, err := d.refineStories(ctx, t); err != nil {
		d.stopped(ctx, t, "could not refine managed stories", err)
		return
	} else if deferred {
		t.recordWorkerStatus(workerDeferred)
		return
	}
	watermark, exists, err := d.db.GetWatermark(ctx, t.session.ID)
	if err != nil {
		d.stopped(ctx, t, "could not read this task's watermark", err)
		return
	}

	var deferred bool
	if !exists {
		err = d.bootstrap(ctx, t)
	} else {
		deferred, err = d.drain(ctx, t, watermark.LastCommentID)
	}
	if err != nil {
		d.stopped(ctx, t, "this task's turn could not be completed", err)
		return
	}
	if deferred {
		t.recordWorkerStatus(workerDeferred)
	}
	if !deferred {
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
// The retry itself is deliberate and is not changed here: the comment stays
// unconsumed so the work is not lost to a blip. What is added is the
// difference between a blip and an origin that is never coming back, which
// otherwise reads identically in the log — the same error line, every tick,
// with nothing that counts them.
func (d *Daemon) prepareFailed(ctx context.Context, t *task, err error) {
	reason := err.Error()
	if sanitizer := d.diagnosticRedactor; sanitizer != nil {
		if body, redactErr := sanitizer.Apply(ctx, reason, 1024); redactErr == nil {
			reason = body.String()
		}
	}
	state, announce := d.prepFailures.record(t.session.ID, reason)
	if !announce || state.Attempts < prepareEscalationAttempts {
		return
	}
	t.log.ErrorContext(ctx, "this task has failed every turn since it last made progress; "+
		"the trigger is still unconsumed and the next tick will try again, but nothing "+
		"will stop retrying on its own",
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
	comments, err := d.discoveryComments(ctx, t.forgeRepo, t.issue.Number, 0)
	if err != nil {
		return fmt.Errorf("list the existing comments of task %s: %w", t.ref, err)
	}

	var highest int64
	for _, c := range comments {
		if c.ID > highest {
			highest = c.ID
		}
	}

	watermark, created, err := d.db.BootstrapWatermark(ctx, t.session.ID, highest)
	if err != nil {
		return fmt.Errorf("bootstrap the watermark of task %s: %w", t.ref, err)
	}

	t.log.InfoContext(ctx, "first sight of a task; its existing comments are skipped",
		slog.Int("existing_comments", len(comments)),
		slog.Int64("watermark", watermark.LastCommentID),
		slog.Bool("created", created))

	// Only now, with the watermark planted and logged. See [Daemon.runIssueBody]
	// for why that order is the whole of "exactly once".
	return d.runIssueBody(ctx, t, comments)
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
//  2. The issue is assigned to the owner, matched on the numeric user id and
//     never on a handle (D-I). This is the authorisation, and it is the whole
//     reason the old ban could be lifted: anyone can open an issue, but only
//     the owner's account can assign one to the owner. Assigning an issue to
//     yourself is the act of instruction.
//  3. Its body has something in it.
//  4. It has no comments at all. A body is a *first* instruction; an issue that
//     has already been talked on is one whose conversation is the instruction,
//     and replaying its opening post underneath that would be a second voice
//     nobody asked for (AC-2.16d).
//
// # Watermark first, then run
//
// [Daemon.bootstrap] plants the watermark before calling this, and that
// ordering is what makes "fires exactly once" a property of the control flow
// rather than of a check: the next tick finds a watermark and takes
// [Daemon.drain], so this function is unreachable a second time. A crash
// between the two leaves a `running` row that startup reconciliation reports as
// abandoned (AC-2.21) — which is the failure mode to prefer over a body that
// silently runs twice.
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
// Author is the *issue* author's handle, which is not necessarily the owner —
// the gate is assignment, not authorship. That is safe precisely because the
// prompt keeps the `opened by X` attribution: a run started from a stranger's
// body still tells the model who wrote what it was handed.
//
// # No 👀
//
// There is no comment to react to, and reacting to the issue would be an eighth
// forge endpoint for a cosmetic gain. AC-2.17 stays comment-only.
func (d *Daemon) runIssueBody(ctx context.Context, t *task, comments []github.Comment) error {
	var refusal string
	switch {
	case !t.issue.AssignedTo(d.filter.Identities().OwnerUserID):
		refusal = "it is not assigned to the owner"
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
	if !command.Spawns() {
		// /plan, /frobnicate, a bare /spike: nothing runs, and — unlike the
		// same command in a comment — nothing is posted either (AC-2.16c). A
		// reply would be litefactory answering a question nobody asked it, on
		// an issue whose author may not even have meant the first line as a
		// command.
		log.DebugContext(ctx, "the issue body names a command that spawns nothing; "+
			"nothing runs and nothing is posted",
			slog.String("command", command.Name),
			slog.String("kind", command.Kind.String()))
		return nil
	}

	log.InfoContext(ctx, "running the issue body as the task's first instruction",
		slog.String("command", command.Name),
		slog.String("kind", command.Kind.String()))
	trigger := turn.Trigger{CommentID: 0, Body: "", Author: t.issue.Author.Login}
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
	comments, err := d.discoveryComments(ctx, t.forgeRepo, t.issue.Number, watermark)
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
	log := t.log.With(
		slog.Int64("comment_id", c.ID),
		slog.Int64("author_user_id", c.Author.ID),
		slog.String("author", c.Author.Login))
	// The self-filter runs first and matches on numeric ids, never handles
	// (D-I, AC-2.14). Everything is recorded, including litefactory's own
	// comments: a dropped comment that left no trace cannot be told from one
	// that was never fetched.
	admission, err := d.filter.Admit(ctx, t.session.ID, c)
	if err != nil {
		return fmt.Errorf("classify comment %d on task %s: %w", c.ID, t.ref, err)
	}
	if !admission.Triggers() {
		// litefactory's own comment, or a third party's: recorded and tagged,
		// nothing runs, the comment is consumed (AC-2.14, AC-2.15).
		_, err := d.reporter.Consume(ctx, t.session.ID, c.ID)
		return d.leave(ctx, t, log, "could not consume a comment litefactory does not act on", err)
	}

	if pending, err := d.resumeReport(ctx, t, c.ID); pending || err != nil {
		return d.leave(ctx, t, log, "prior delivery remains pending", err)
	}
	command := turn.ParseCommand(c.Body)
	if command.Name == "/deliver" || command.Name == "/merge" {
		return d.handleDelivery(ctx, t, c, log)
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

	if !command.Spawns() {
		return d.reply(ctx, t, c, command, log)
	}
	trigger := turn.Trigger{CommentID: c.ID, Body: c.Body, Author: c.Author.Login}
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
	if err := d.forge.AddCommentReaction(ctx, t.forgeRepo, commentID, github.ReactionEyes); err != nil {
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
	if pending, err := d.resumeReport(ctx, t, trigger.CommentID); pending || err != nil {
		return d.leave(ctx, t, log, "prior delivery remains pending", err)
	}

	mode, err := d.admitMode(ctx, t, trigger, command)
	if err != nil {
		var refusal *modeRefusal
		if errors.As(err, &refusal) {
			return d.refuseMode(ctx, t, trigger, refusal.kind)
		}
		return d.leave(ctx, t, log, "mode selection was refused", err)
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
	tools, ok := d.resolveTools(mode, agent.Kind(selectedAgent))
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

	if t.worker != nil {
		t.worker.executionRequests++
	}
	turnResult, err := d.turn.Execute(ctx, turn.Request{
		Continuation: t.continuation,
		Mode:         mode,
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
	if turnResult.Outcome == agent.OutcomeAbandoned {
		t.recordWorkerStatus(workerCancelled)
	}
	if err != nil {
		return d.leave(ctx, t, log, "the turn could not be conducted", err)
	}

	if turnResult.Disposition == turn.DispositionLocked {
		t.recordWorkerStatus(workerDeferred)
		t.continuationLocked = t.continuation != nil
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
		return nil
	}

	return d.completeTurn(ctx, t, trigger, mode, turnResult, log)
}

func (d *Daemon) completeTurn(ctx context.Context, t *task, trigger turn.Trigger, mode agent.Mode, turnResult turn.Result, log *slog.Logger) error {
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
	d.rateLimited(ctx, log, t.tickAt, err)
	log.ErrorContext(ctx, what+"; the comment stays unconsumed and this task's queue stops here",
		slog.Any("error", err))
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
