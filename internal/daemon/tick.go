package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TickResult is what one tick did. It is returned so that a test can assert on
// a tick without reading log lines, and so that the loop has something to
// summarise.
type TickResult struct {
	// StartedAt is when the tick began, from the daemon's clock.
	StartedAt time.Time
	// Duration is how long it took.
	Duration time.Duration

	// Skipped reports that the tick did nothing because a rate-limit backoff
	// is still in force, and RetryAt is when ticks resume (AC-2.12).
	Skipped bool
	RetryAt time.Time

	// Repos is how many registered repositories were polled — one bounded,
	// logged query each, which is the whole per-tick API cost (AC-2.8).
	Repos int
	// Issues is how many non-pull-request issues came back across them.
	Issues int
	// Bootstrapped is how many tasks were seen for the first time and had a
	// watermark planted without any of their existing comments being processed
	// (AC-2.10). A first sight can still run the issue body, which counts in
	// Runs and not here (AC-2.16b).
	Bootstrapped int
	// Comments is how many comments above a watermark were examined.
	Comments int
	// Runs is how many turns were executed, and Replies how many run-less
	// replies were posted (AC-2.18, AC-2.20).
	Runs    int
	Replies int
	// Deferred is how many tasks stopped their comment queue early, leaving a
	// comment for a later tick. See [Daemon.drain].
	Deferred int

	// Cursor is the poll cursor as it stands after the tick. A failed tick
	// leaves it as it was found, which is what makes the next one redo the work
	// (AC-2.13).
	Cursor store.PollCursor
}

// Tick runs one poll cycle.
//
// It is exported and synchronous on purpose: the loop is a thin wrapper around
// it, and everything worth asserting about polling can be asserted by calling
// this and looking at the database, with no clock to wait for.
//
// An error means the tick did not complete. The poll cursor is not persisted,
// so the next tick asks for the same window again; every watermark already
// advanced stays advanced, because a watermark only moves after the comment it
// covers has genuinely been dealt with.
func (d *Daemon) Tick(ctx context.Context) (TickResult, error) {
	startedAt := d.now()

	if retryAt, held := d.backoff.hold(startedAt); held {
		d.log.DebugContext(ctx, "skipping the tick; still backing off from a rate limit",
			slog.Time("retry_at", retryAt.UTC()),
			slog.String("remaining", retryAt.Sub(startedAt).String()))
		return TickResult{StartedAt: startedAt, Skipped: true, RetryAt: retryAt}, nil
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

	result := TickResult{StartedAt: startedAt, Repos: len(repos), Cursor: cursor}
	// pending is the update time of the earliest issue whose comment queue did
	// not drain. The cursor is held back to it so that issue is listed again;
	// otherwise `since` would move past it and the unconsumed comment would
	// never be seen a second time.
	var pending time.Time

	if err := d.poll(ctx, repos, listingWindow(cursor), &result, &pending); err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, d.observe(startedAt, err)
	}

	next := startedAt
	if !pending.IsZero() && pending.Before(next) {
		next = pending
	}
	persisted := store.PollCursor{TasksUpdatedSince: next, LastTickAt: d.now()}
	if err := d.db.SetPollCursor(ctx, persisted); err != nil {
		result.Duration = d.now().Sub(startedAt)
		return result, err
	}
	result.Cursor = persisted
	result.Duration = result.Cursor.LastTickAt.Sub(startedAt)

	if held, wasHeld := d.backoff.clear(); wasHeld {
		d.log.InfoContext(ctx, "resumed polling after a rate-limit backoff",
			slog.String("held_for", held.String()))
	}

	d.log.InfoContext(ctx, "tick complete",
		slog.Int("repos", result.Repos),
		slog.Int("issues", result.Issues),
		slog.Int("bootstrapped_tasks", result.Bootstrapped),
		slog.Int("comments", result.Comments),
		slog.Int("runs", result.Runs),
		slog.Int("replies", result.Replies),
		slog.Int("deferred_tasks", result.Deferred),
		slog.Time("cursor", next.UTC()),
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

// poll walks every registered repository (D-A, AC-2.8).
//
// One listing per repository, so the tick's API cost is O(registered repos) and
// each call logs what it cost — that is the criterion, not "one API call".
// Issue *search* would collapse it into one request and was dropped: its query
// string caps at 256 characters, it is eventually consistent, and it runs on a
// thirtieth of the quota.
func (d *Daemon) poll(
	ctx context.Context,
	repos []store.Repo,
	since time.Time,
	result *TickResult,
	pending *time.Time,
) error {
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return err
		}
		if repo.Forge != store.ForgeGitHub {
			// M0 supports one forge. A row from a future migration is skipped
			// loudly rather than sent to the GitHub client.
			d.log.WarnContext(ctx, "skipping a repository on an unsupported forge",
				slog.String("path", repo.Path), slog.String("forge", repo.Forge))
			continue
		}

		forgeRepo := github.Repo{Owner: repo.Owner, Name: repo.Name}
		issues, err := d.forge.ListIssues(ctx, forgeRepo, since)
		if err != nil {
			return fmt.Errorf("list the issues of %s touched since %s: %w",
				forgeRepo, since.UTC().Format(time.RFC3339), err)
		}
		commented, err := d.commentedIssues(ctx, forgeRepo, since, issues)
		if err != nil {
			return err
		}
		issues = append(issues, commented...)
		result.Issues += len(issues)

		for _, issue := range issues {
			deferred, err := d.processIssue(ctx, repo, forgeRepo, issue, result)
			if err != nil {
				return err
			}
			if deferred {
				result.Deferred++
				if pending.IsZero() || issue.UpdatedAt.Before(*pending) {
					*pending = issue.UpdatedAt
				}
			}
		}
	}
	return nil
}

// task is one issue's context for the duration of a tick.
type task struct {
	repo      store.Repo
	forgeRepo github.Repo
	issue     github.Issue
	session   store.Session
	ref       naming.Ref
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
	repo github.Repo,
	since time.Time,
	listed []github.Issue,
) ([]github.Issue, error) {
	numbers, err := d.forge.ListCommentedIssues(ctx, repo, since)
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
		issue, err := d.forge.GetIssue(ctx, repo, number)
		if err != nil {
			// Every pull request is an issue, so pull request comments arrive
			// in that listing too and the payload cannot tell them apart. The
			// issue read is where they are found out, and skipping one is
			// routine rather than a failed tick.
			if errors.Is(err, github.ErrInvalidArgument) {
				d.log.DebugContext(ctx, "skipping a commented number that is not an issue",
					slog.String("repo", repo.String()), slog.Int("number", number))
				continue
			}
			return nil, fmt.Errorf("read %s#%d, which has a new comment: %w", repo, number, err)
		}
		extra = append(extra, issue)
	}
	return extra, nil
}

// processIssue finds or creates the task for one issue and drains its comment
// queue, reporting whether the queue stopped early.
//
// # A closed issue is left alone
//
// Closing an issue is how litefactory is told to stop working on it, so an
// issue that is not open is skipped here and nowhere else (AC-2.22). Here,
// because this is where the two discovery paths meet: `state=open` on the
// issue listing covers one of them, but [Daemon.commentedIssues] reaches an
// issue through GetIssue, which never sees that query — a filter on the query
// alone would leave the faster path wide open.
//
// And *before* [store.DB.GetOrCreateSession], so that a comment on an issue
// closed two months ago leaves no sessions row and no watermarks row behind.
// A skip that first created the task it was refusing to work on would put
// rows in the database for every closed issue anyone ever comments on.
func (d *Daemon) processIssue(
	ctx context.Context,
	repo store.Repo,
	forgeRepo github.Repo,
	issue github.Issue,
	result *TickResult,
) (bool, error) {
	if issue.State != github.StateOpen {
		d.log.DebugContext(ctx, "skipping an issue that is not open; closing an issue is how "+
			"litefactory is told to stop working on it",
			slog.String("repo", forgeRepo.String()),
			slog.Int("issue", issue.Number),
			slog.String("state", issue.State))
		return false, nil
	}

	session, err := d.db.GetOrCreateSession(ctx, repo, issue.Number)
	if err != nil {
		return false, fmt.Errorf("find the task for %s#%d: %w", forgeRepo, issue.Number, err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return false, fmt.Errorf("task ref %q on task %d: %w", session.TaskRef, session.ID, err)
	}
	t := &task{repo: repo, forgeRepo: forgeRepo, issue: issue, session: session, ref: ref}

	watermark, exists, err := d.db.GetWatermark(ctx, session.ID)
	if err != nil {
		return false, fmt.Errorf("read the watermark of task %s: %w", ref, err)
	}
	if !exists {
		return false, d.bootstrap(ctx, t, result)
	}
	return d.drain(ctx, t, watermark.LastCommentID, result)
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
func (d *Daemon) bootstrap(ctx context.Context, t *task, result *TickResult) error {
	comments, err := d.forge.ListCommentsAfter(ctx, t.forgeRepo, t.issue.Number, 0)
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
	result.Bootstrapped++

	d.log.InfoContext(ctx, "first sight of a task; its existing comments are skipped",
		slog.String("task", t.ref.String()),
		slog.Int64("session_id", t.session.ID),
		slog.Int("existing_comments", len(comments)),
		slog.Int64("watermark", watermark.LastCommentID),
		slog.Bool("created", created))

	// Only now, with the watermark planted and logged. See [Daemon.runIssueBody]
	// for why that order is the whole of "exactly once".
	return d.runIssueBody(ctx, t, comments, result)
}

// runIssueBody runs the issue body as the task's first instruction, when the
// issue is one that asked for it (AC-2.16b).
//
// # The gate
//
// Four conditions, all of them required:
//
//  1. The issue is open. Not checked here: [Daemon.processIssue] returns before
//     GetOrCreateSession on anything that is not open (AC-2.22), so an issue
//     that is not open can never reach a bootstrap at all. Re-checking it would
//     be a second place to keep in step with the first, and the first is the
//     one that also stops the session row being written.
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
func (d *Daemon) runIssueBody(ctx context.Context, t *task, comments []github.Comment, result *TickResult) error {
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
		d.log.DebugContext(ctx, "not running the issue body on first sight; "+refusal,
			slog.String("task", t.ref.String()),
			slog.Int64("session_id", t.session.ID),
			slog.Int("existing_comments", len(comments)))
		return nil
	}

	log := d.log.With(
		slog.String("task", t.ref.String()),
		slog.Int64("session_id", t.session.ID),
		slog.Int64("comment_id", 0),
		slog.String("trigger", "issue_body"),
		slog.Int64("author_user_id", t.issue.Author.ID),
		slog.String("author", t.issue.Author.Login))

	command := turn.ParseCommand(t.issue.Body)
	if command.Kind == turn.KindNone {
		// A body with nothing in it, which the gate above has already refused.
		// Kept because ParseCommand's contract, not this function's, is what
		// decides it — and an empty body must end here silently in either case.
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
	return d.run(ctx, t, trigger, command, log, result)
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
// not moved past ends this task's queue. Other tasks in the same tick are
// unaffected, and the tick holds its cursor back to this issue's update time so
// that the comment is listed again.
//
// It is also what makes a contended per-task lock behave: nothing was consumed,
// so the queue stops and a later tick retries, with no queue and no "busy"
// reply (AC-3.1).
func (d *Daemon) drain(ctx context.Context, t *task, watermark int64, result *TickResult) (bool, error) {
	comments, err := d.forge.ListCommentsAfter(ctx, t.forgeRepo, t.issue.Number, watermark)
	if err != nil {
		return false, fmt.Errorf("list the comments of task %s above %d: %w", t.ref, watermark, err)
	}

	for _, c := range comments {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := d.handleComment(ctx, t, c, result); err != nil {
			return false, err
		}
		result.Comments++

		consumed, err := d.consumed(ctx, t.session.ID, c.ID)
		if err != nil {
			return false, fmt.Errorf("read the watermark of task %s: %w", t.ref, err)
		}
		if !consumed {
			d.log.InfoContext(ctx, "comment was not consumed; stopping this task's queue here so a "+
				"later tick retries it. Processing the comments after it would advance the watermark past this one",
				slog.String("task", t.ref.String()),
				slog.Int64("session_id", t.session.ID),
				slog.Int64("comment_id", c.ID))
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

// handleComment classifies one comment and does whatever it asks for.
//
// The error return is reserved for failures that end the whole tick: a database
// that will not answer, and a rate limit, which there is no point carrying on
// through. Everything else is logged and left: the comment stays unconsumed,
// which [Daemon.drain] detects and turns into a retry on a later tick.
func (d *Daemon) handleComment(ctx context.Context, t *task, c github.Comment, result *TickResult) error {
	log := d.log.With(
		slog.String("task", t.ref.String()),
		slog.Int64("session_id", t.session.ID),
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
		return d.leave(ctx, log, "could not consume a comment litefactory does not act on", err)
	}

	command := turn.ParseCommand(c.Body)
	if command.Kind == turn.KindNone {
		// An empty body: nothing to act on. Recorded, ignored, consumed.
		//
		// Free text used to end here too. It no longer does — it spawns a run
		// whose mode the agent infers — so this branch is now only the empty
		// case, and every non-empty owner comment starts work.
		log.DebugContext(ctx, "comment is empty; recorded and ignored")
		_, err := d.reporter.Consume(ctx, t.session.ID, c.ID)
		return d.leave(ctx, log, "could not consume an empty comment", err)
	}

	// AC-2.17: the acknowledgement goes on before anything that could start a
	// run, and its failure is logged rather than fatal — a missing reaction is
	// a cosmetic loss, and refusing the turn over one would be a real one.
	d.acknowledge(ctx, t, c.ID)

	if !command.Spawns() {
		return d.reply(ctx, t, c, command, log, result)
	}
	trigger := turn.Trigger{CommentID: c.ID, Body: c.Body, Author: c.Author.Login}
	return d.run(ctx, t, trigger, command, log, result)
}

// acknowledge adds the 👀 reaction to a comment litefactory is about to act on
// (AC-2.17).
func (d *Daemon) acknowledge(ctx context.Context, t *task, commentID int64) {
	if err := d.forge.AddCommentReaction(ctx, t.forgeRepo, commentID, github.ReactionEyes); err != nil {
		d.log.WarnContext(ctx, "could not acknowledge the comment with a reaction; carrying on",
			slog.String("task", t.ref.String()),
			slog.Int64("comment_id", commentID),
			slog.Any("error", err))
		return
	}
	d.log.DebugContext(ctx, "acknowledged the comment",
		slog.String("task", t.ref.String()), slog.Int64("comment_id", commentID))
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
	result *TickResult,
) error {
	posted, err := d.reporter.ReportReply(ctx, turn.ReplyRequest{
		Ref:       t.ref,
		SessionID: t.session.ID,
		CommentID: c.ID,
		Command:   command,
	})
	if err != nil {
		return d.leave(ctx, log, "could not reply to "+command.Name, d.orphaned(ctx, log, posted, err))
	}
	if !posted.Skipped {
		result.Replies++
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
	result *TickResult,
) error {
	turnResult, err := d.turn.Execute(ctx, turn.Request{
		SessionID: t.session.ID,
		Issue: turn.Issue{
			Number: t.issue.Number,
			Title:  t.issue.Title,
			Body:   t.issue.Body,
			Author: t.issue.Author.Login,
		},
		Trigger: trigger,
		Command: command,
	})
	if err != nil {
		return d.leave(ctx, log, "the turn could not be conducted", err)
	}
	result.Runs++

	if turnResult.Disposition == turn.DispositionLocked {
		log.InfoContext(ctx, "task is locked by another daemon instance; leaving the comment for a later tick")
		return nil
	}

	posted, err := d.reporter.ReportTurn(ctx, turnResult)
	if err != nil {
		return d.leave(ctx, log, "the turn could not be reported", d.orphaned(ctx, log, posted, err))
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

// leave decides what a per-comment failure costs.
//
// A rate limit ends the tick, so that the backoff is applied once and the
// remaining quota is not spent discovering the same limit on every other task.
// Everything else is logged here and goes no further: the comment was not
// consumed, [Daemon.drain] will notice, and this task's queue stops with the
// comment still waiting.
func (d *Daemon) leave(ctx context.Context, log *slog.Logger, what string, err error) error {
	if err == nil {
		return nil
	}
	if errors.As(err, new(*github.RateLimitError)) {
		return err
	}
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		// Shutdown, not a failure of this comment. Ending the tick is what the
		// signal asked for.
		return err
	}
	log.ErrorContext(ctx, what+"; the comment stays unconsumed and this task's queue stops here",
		slog.Any("error", err))
	return nil
}

// orphaned names the one failure that costs something: the comment reached
// GitHub and the transaction that records it did not commit.
//
// [turn.Posted.CommentID] is set as soon as the post returns, so a non-zero id
// alongside an error is not "nothing happened" — it is a comment on the issue
// that litefactory does not know about. The comment stays unconsumed, so a
// later tick posts a second one. That is the direction the trade has to fall:
// the alternative, consuming before the post is recorded, turns a transient
// database error into permanent silence, which AC-4.1 forbids.
func (d *Daemon) orphaned(ctx context.Context, log *slog.Logger, posted turn.Posted, err error) error {
	if err == nil || posted.CommentID == 0 {
		return err
	}
	log.ErrorContext(ctx, "the comment was posted to the issue but litefactory could not record it; "+
		"a later tick will post another one",
		slog.Int64("posted_comment_id", posted.CommentID), slog.Any("error", err))
	return err
}

// observe records a rate limit and returns the error that ended the tick.
//
// The cursor was not persisted and no watermark was touched by the failure, so
// there is nothing to undo: the tick simply did not happen, and the next one
// asks for the same window (AC-2.12, AC-2.13).
func (d *Daemon) observe(now time.Time, err error) error {
	var rateLimit *github.RateLimitError
	if !errors.As(err, &rateLimit) {
		return err
	}

	failures, delay, until := d.backoff.fail(now, rateLimit.RetryAfter)
	d.log.Warn("github refused the tick with a rate limit; backing off. The poll cursor and every "+
		"watermark are unchanged, and polling resumes on its own",
		slog.Int("consecutive_refusals", failures),
		slog.String("retry_after", rateLimit.RetryAfter.String()),
		slog.String("backoff", delay.String()),
		slog.Time("resumes_at", until.UTC()),
		slog.Bool("secondary", rateLimit.Secondary),
		slog.String("resource", rateLimit.Resource),
		slog.Any("error", err))
	return err
}
