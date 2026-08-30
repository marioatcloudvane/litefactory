package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// preparation is everything the run needs, assembled and recorded before the
// subprocess starts. It is built by [prepare] and consumed by [runAndHarvest].
type preparation struct {
	repo    store.Repo
	session store.Session
	ref     naming.Ref

	author   git.Author
	worktree *git.Worktree

	// shaBefore is where the branch stood before claude ran. It is read here,
	// not after the run, because that is the only moment it means anything
	// (AC-3.24).
	shaBefore string

	segment  store.Segment
	run      store.Run
	request  harness.Request
	prompt   string
	metaPath string

	// promptInput is what the prompt was built from, kept so that the resume
	// fallback in [runAndHarvest] can rebuild it cold without reassembling any
	// of it here. Flipping Resume is the whole difference between the two.
	promptInput harness.PromptInput
}

// prepare takes a locked task from "there is a comment" to "claude can start".
//
// The order is load-bearing and each step is an acceptance criterion:
//
//  1. Open the registered clone and settle who commits.
//  2. Fetch --prune and record last_fetch_at (AC-3.5). A failed fetch aborts
//     before anything on disk has been created.
//  3. Freeze the feature slug on the first run, reuse it afterwards (AC-3.6),
//     and refuse the turn if the frozen value fails its post-condition
//     (AC-3.7).
//  4. Create or reuse the branch and the worktree (AC-3.8, AC-3.9).
//  5. Read sha_before and record the claude session id on the segment.
//  6. Allocate the runs row — and with it the ordinal and the log path —
//     before the subprocess starts, so a crashed run still owns its number
//     (S-1).
//  7. Decide whether this run continues the previous run's conversation, read
//     the thread as litefactory has recorded it, and assemble the prompt that
//     decision calls for.
//
// Everything that can go wrong because of the state of the world is returned as
// a *failure, which [Execute] turns into a reportable result rather than an
// error. A database that will not answer is an ordinary error.
func prepare(
	ctx context.Context,
	opts Options,
	req Request,
	session store.Session,
	ref naming.Ref,
	logger *slog.Logger,
) (*preparation, error) {
	repo, err := opts.DB.GetRepo(ctx, session.RepoID)
	if err != nil {
		return nil, fmt.Errorf("read the repository of task %s: %w", ref, err)
	}

	clone, err := opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return nil, failuref(err, "the registered clone at %s could not be opened", repo.Path)
	}
	author, err := resolveAuthor(ctx, opts, clone)
	if err != nil {
		return nil, err
	}

	if err = fetch(ctx, opts, clone, repo, logger); err != nil {
		return nil, err
	}

	session, branch, err := freezeSlug(ctx, opts, req, session, repo)
	if err != nil {
		return nil, err
	}

	worktree, err := ensureWorktree(ctx, opts, clone, session, repo, branch, logger)
	if err != nil {
		return nil, err
	}
	if session.WorktreePath != worktree.Path() {
		if err = opts.DB.SetSessionWorktree(ctx, session.ID, worktree.Path()); err != nil {
			return nil, fmt.Errorf("record the worktree of task %s: %w", ref, err)
		}
		session.WorktreePath = worktree.Path()
	}
	if err = opts.DB.SetSessionState(ctx, session.ID, store.SessionRunning); err != nil {
		return nil, fmt.Errorf("mark task %s running: %w", ref, err)
	}

	shaBefore, err := worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return nil, failuref(err, "the head of %s could not be read", branch)
	}

	// The one place in the tree that derives a claude session id. Every later
	// consumer — the harness, the run log, the metadata document — reads it back
	// off the segment below, because GetOrCreateSegment only uses this value
	// when it creates the row and returns an existing row as stored. Its doc
	// comment says the stored value wins if the two ever disagree; a resumed run
	// is what makes that real, since the stored id is the only one that names a
	// conversation claude can still find.
	claudeSessionID, err := naming.ClaudeSessionID(ref, store.FirstSegmentOrdinal)
	if err != nil {
		return nil, fmt.Errorf("derive the claude session id for task %s: %w", ref, err)
	}
	segment, err := opts.DB.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, claudeSessionID)
	if err != nil {
		return nil, fmt.Errorf("record the conversation segment of task %s: %w", ref, err)
	}

	// Read before the row for this run is inserted, so that the run being
	// prepared cannot be found as its own predecessor. The terminal-status
	// filter would exclude it anyway — it is inserted as running — and both are
	// kept: one of them is an ordering that a later edit could quietly reverse.
	previous, resume, err := previousRun(ctx, opts, session, segment)
	if err != nil {
		return nil, err
	}

	run, err := allocateRun(ctx, opts, req, session, segment, ref)
	if err != nil {
		return nil, err
	}
	metaPath, err := harness.MetaPath(opts.Paths.Home(), ref, run.Ordinal)
	if err != nil {
		return nil, fmt.Errorf("derive the metadata path of run %d: %w", run.Ordinal, err)
	}

	history, previousTriggerBody, err := commentHistory(ctx, opts, session, req, previous.TriggerCommentID)
	if err != nil {
		return nil, err
	}

	// A trigger id of 0 is the issue body rather than a comment (see
	// [Trigger.CommentID]): the run was started by assigning the issue, so the
	// body is the instruction and must not also be framed as third-party
	// material to evaluate (D-N, AC-2.25). The condition is decided here, once;
	// harness does the rest of it.
	bodyIsInstruction := req.Trigger.CommentID == 0

	// Permission to ask is the conjunction, assembled here and handed to the
	// harness already decided (AC-A1.6): the tag off this run's own trigger, and
	// a mode that may ask at all. [ParseCommand] found the tag on whichever text
	// started the run, so the comment path and the issue-body path are covered by
	// the same call.
	mayAsk := req.Command.Tagged && modeMayAsk(req.mode())

	promptInput := harness.PromptInput{
		Ref:               ref,
		FeatureSlug:       session.FeatureSlug,
		Branch:            branch,
		Mode:              req.mode(),
		RunOrdinal:        run.Ordinal,
		Instruction:       req.Command.Text,
		IssueTitle:        req.Issue.Title,
		IssueBody:         req.Issue.Body,
		IssueAuthor:       req.Issue.Author,
		CommentBody:       req.Trigger.Body,
		CommentAuthor:     req.Trigger.Author,
		BodyIsInstruction: bodyIsInstruction,
		Resume:            resume,
		MayAsk:            mayAsk,
		PreviousMayAsk:    previousMayAsk(previous, resume, previousTriggerBody, req.Issue.Body),
		PreviousMode:      previous.Mode,
		History:           history,
	}
	prompt, err := harness.BuildPrompt(promptInput)
	if err != nil {
		return nil, fmt.Errorf("assemble the prompt for run %d of task %s: %w", run.Ordinal, ref, err)
	}

	path := harness.PathCold
	if resume {
		path = harness.PathResume
	}

	logger.Info("run prepared",
		slog.Int("run_ordinal", run.Ordinal),
		slog.Int64("run_id", run.ID),
		slog.String("branch", branch),
		slog.String("worktree", worktree.Path()),
		slog.String("sha_before", shaBefore),
		slog.String("path", path.String()),
		slog.Int("previous_run_ordinal", previous.Ordinal),
		slog.Int("history_comments", len(history)),
		slog.Int("prompt_bytes", len(prompt)))

	return &preparation{
		repo:        repo,
		session:     session,
		ref:         ref,
		author:      author,
		worktree:    worktree,
		shaBefore:   shaBefore,
		segment:     segment,
		run:         run,
		prompt:      prompt,
		metaPath:    metaPath,
		promptInput: promptInput,
		request: harness.Request{
			Ref:             ref,
			RunOrdinal:      run.Ordinal,
			SegmentOrdinal:  segment.Ordinal,
			ClaudeSessionID: segment.ClaudeSessionID,
			Mode:            req.mode(),
			Path:            path,
			WorktreePath:    worktree.Path(),
			Prompt:          prompt,
			// Passed straight through: the request's lists are already the
			// caller's copies of operator-authored configuration, and the
			// harness re-checks them against its ceiling before it builds argv
			// (AC-2.6). Empty means this run takes the harness's own surface,
			// which is every run that carries no tool profile.
			Tools:        req.Tools,
			AllowedTools: req.AllowedTools,
			ToolProfile:  req.ToolProfile,
		},
	}, nil
}

// previousRun finds the last run of this segment that claude was actually
// invoked for, and with it the answer to "is there a conversation to continue?".
//
// The evidence is the runs table and nothing else. A row on this segment in a
// terminal status means a subprocess was started with the segment's claude
// session id, and claude keeps a transcript for every session id it is given —
// so a conversation should exist and this run can resume it. There is no column
// for this and there does not need to be one: runs.segment_id and runs.status
// already say it, and a column would be a second copy of the same fact to keep
// in step.
//
// A row still marked running is not evidence. It is either this daemon's own
// run, which cannot be its own predecessor, or a run a killed daemon left
// behind — and that one becomes failed, and therefore terminal, the moment
// reconciliation sees it (D-B).
//
// Note what this is not: proof. The rows are here and the transcript is on
// whichever machine wrote it, under whichever working directory it ran in. A
// machine move or a changed worktree_root leaves the rows pointing at a
// conversation that no longer exists, which is what the cold rerun in
// [runAndHarvest] exists to survive.
func previousRun(
	ctx context.Context,
	opts Options,
	session store.Session,
	segment store.Segment,
) (store.Run, bool, error) {
	runs, err := opts.DB.ListRuns(ctx, session.ID)
	if err != nil {
		return store.Run{}, false, fmt.Errorf("list the runs of task %s: %w", session.TaskRef, err)
	}

	var previous store.Run
	var found bool
	for _, run := range runs {
		// ListRuns is in ordinal order, oldest first, so the last match wins
		// and previous is the most recent run of this conversation.
		if run.SegmentID == segment.ID && terminalRunStatus(run.Status) {
			previous, found = run, true
		}
	}
	return previous, found, nil
}

// commentHistory reads the comments litefactory has recorded on this task and
// turns them into the prompt's history entries, oldest first.
//
// Assembly, not markup. The <note> frame, the size caps and the neutralisation
// are harness's single responsibility; a block pre-rendered here would drift
// from the framing the other notes use, and the drift would be invisible until
// something read the wrong half of a prompt as litefactory's own words. All
// this does is map the store's classification onto the label the prompt shows
// and drop the comment that triggered this run — that one already appears
// verbatim in its own note, and a thread where the live request also appears as
// history is a thread where it is ambiguous which one is live.
//
// It is read on every path even though only a cold prompt renders it. The
// resume fallback in [runAndHarvest] rebuilds this same input with Resume
// flipped to false, and that rerun is a cold start that needs the history like
// any other (AC-SR-2.8); reading it here is what makes the flip the whole of
// the difference between the two prompts.
//
// It is best effort over what the comments table holds. A newly watched issue
// is bootstrapped by recording the highest comment id on it and discarding the
// bodies, so anything said before litefactory first saw the issue is not stored
// and cannot be rendered. The block says so in its own Source line rather than
// implying it is the whole thread.
//
// It also returns the body of the comment previousTriggerID names, or "" if the
// list does not hold it. That is a second job for one loop, and it is here
// rather than in a query of its own for two reasons: this is already the only
// place the whole comment list is read, and [harness.HistoryEntry] carries no
// forge comment id, so a caller holding the history has nothing left to match
// on. The body is what [previousMayAsk] re-parses the ask tag out of, which is
// how the previous run's permission is recovered with no new column, no
// migration and no second query (AC-A1.13).
//
// There is no watermark hazard in reading it back: store.ListComments selects
// on the session alone, in forge id order, with no watermark predicate, and
// nothing in internal deletes comment rows. A comment that triggered a run is
// therefore still there on every later run of the same task.
func commentHistory(
	ctx context.Context,
	opts Options,
	session store.Session,
	req Request,
	previousTriggerID int64,
) ([]harness.HistoryEntry, string, error) {
	comments, err := opts.DB.ListComments(ctx, session.ID)
	if err != nil {
		return nil, "", fmt.Errorf("list the comments of task %s: %w", session.TaskRef, err)
	}

	// ListComments is in forge id order, which is the order they were written.
	var previousTriggerBody string
	history := make([]harness.HistoryEntry, 0, len(comments))
	for _, comment := range comments {
		// Matched before the trigger is skipped, not after. The two ids are
		// different on every path that reaches here — a comment that has already
		// produced a run cannot produce a second one — but a match that depended
		// on that would be a match with a reason to stop working.
		if previousTriggerID != 0 && comment.ForgeCommentID == previousTriggerID {
			previousTriggerBody = comment.Body
		}
		if comment.ForgeCommentID == req.Trigger.CommentID {
			continue
		}
		history = append(history, harness.HistoryEntry{
			Role:   historyRole(comment.Classification),
			Author: comment.AuthorHandle,
			Body:   comment.Body,
			At:     comment.CreatedAt,
		})
	}
	return history, previousTriggerBody, nil
}

// previousMayAsk re-derives whether the run before this one was allowed to ask.
//
// It is a re-parse, not a lookup. Permission is not stored anywhere — there is
// no column for it and this story adds none (AC-A1.13) — but everything it was
// computed from still is: the previous run's row names the comment that
// triggered it and the mode it ran in, and the comment's body is in the list
// [commentHistory] has just read.
//
// It is the same conjunction this run's own permission is, evaluated with the
// *previous* run's mode. Using this run's mode would be the subtle bug: a spike
// carrying the tag was never allowed to ask, and reading it as though it had
// been would make the next run announce that permission has been withdrawn from
// a conversation that never had it.
//
// Two cases where the comment is not where it usually is:
//
//   - the previous run was triggered by the issue body (AC-2.16b), so its
//     trigger id is 0 and there is no comment row to find. The body is the text
//     that was parsed then and it is already in memory, so it is parsed again
//     here (T-A1.7);
//   - there was no previous run at all. The answer is false and is never read:
//     [writeResumeHeader] is the only consumer and a cold run does not reach it.
//     Returning early on resume keeps that honest rather than accidental — it
//     stops a zero store.Run, whose trigger id is also 0, from being read as a
//     body-triggered predecessor and having the issue body parsed for it.
func previousMayAsk(previous store.Run, resume bool, previousTriggerBody, issueBody string) bool {
	if !resume {
		return false
	}
	body := previousTriggerBody
	if previous.TriggerCommentID == 0 {
		body = issueBody
	}
	return HasAskTag(body) && modeMayAsk(previous.Mode)
}

// historyRole maps the store's classification onto the label the prompt renders
// it under.
//
// The relabelling is the point of the block, not a naming detail. "instruction"
// is what a comment was when it arrived; replaying a thread full of comments
// still called instructions is how a `/spike …` from last week gets carried out
// a second time. An unrecognised classification is labelled as a third party's,
// which is the reading that claims least.
func historyRole(classification store.Classification) harness.HistoryRole {
	switch classification {
	case store.ClassificationInstruction:
		return harness.RoleOldOwner
	case store.ClassificationThirdParty:
		return harness.RoleOldThirdParty
	case store.ClassificationSelf:
		return harness.RoleLitefactory
	default:
		return harness.RoleOldThirdParty
	}
}

// terminalRunStatus reports whether a run has stopped, whatever it stopped as.
// Failed counts as much as succeeded: claude was invoked either way, and a
// conversation that went badly is still a conversation the next run should be
// continuing rather than repeating.
func terminalRunStatus(status store.RunStatus) bool {
	switch status {
	case store.RunSucceeded, store.RunFailed, store.RunReported:
		return true
	case store.RunRunning:
		return false
	default:
		return false
	}
}

// resolveAuthor settles who the run's commit is authored as.
//
// The configured identity wins; otherwise the clone answers, through the
// `git config --get` that S-6 already allows. Falling back to the clone is the
// right default rather than a convenience: it is the same name and address the
// user's own commits carry, which is what makes CODEOWNERS, branch protection
// and blame behave the same for a litefactory commit as for a hand-made one.
func resolveAuthor(ctx context.Context, opts Options, clone *git.Clone) (git.Author, error) {
	author := opts.Author
	for _, field := range []struct {
		key   string
		value *string
	}{
		{"user.name", &author.Name},
		{"user.email", &author.Email},
	} {
		if *field.value != "" {
			continue
		}
		value, err := clone.ConfigGet(ctx, field.key)
		if err != nil {
			return git.Author{}, failuref(err,
				"no commit identity: %s is not set in %s and none is configured", field.key, clone.Path())
		}
		*field.value = value
	}
	return author, nil
}

// fetch refreshes the registered clone's remote-tracking refs (AC-3.5).
//
// It runs first, before the branch or the worktree exists, so a fetch failure
// leaves nothing behind to clean up. The timestamp is recorded only on success:
// last_fetch_at answers "how fresh are these refs", and a failed fetch made
// them no fresher.
func fetch(ctx context.Context, opts Options, clone *git.Clone, repo store.Repo, logger *slog.Logger) error {
	if err := clone.Fetch(ctx, opts.remote()); err != nil {
		return failuref(err, "git fetch --prune %s failed in %s", opts.remote(), repo.Path)
	}
	if err := opts.DB.TouchRepoFetch(ctx, repo.ID, time.Now().UTC()); err != nil {
		return fmt.Errorf("record the fetch of %s: %w", repo.Path, err)
	}
	logger.Debug("fetched the registered clone",
		slog.String("clone", repo.Path),
		slog.String("remote", opts.remote()))
	return nil
}

// freezeSlug derives the feature slug on the first run and reuses it on every
// run after that (AC-3.6, D-4), returning the session as stored and the branch
// to work on.
//
// The derivation is a pure function of the issue number and title, so the slug
// survives a total loss of the database: the same title produces the same slug
// and therefore the same branch. Freezing it is what stops a *retitled* issue
// from silently starting a second branch halfway through a task.
//
// The post-condition is checked on both paths (AC-3.7). On the derivation path
// it is naming's own assertion; on the reuse path it is a check on a value that
// has been through the database, where a hand-edited row or a botched migration
// could have left something git would not accept as a branch component. Either
// way the turn refuses rather than building a branch name out of it.
func freezeSlug(
	ctx context.Context,
	opts Options,
	req Request,
	session store.Session,
	repo store.Repo,
) (store.Session, string, error) {
	slug := session.FeatureSlug
	if slug == "" {
		derived, err := naming.Slug(req.Issue.Title)
		if err != nil {
			return session, "", failuref(err, "the feature slug for issue %d could not be derived", req.Issue.Number)
		}
		slug = derived
	}
	if err := naming.ValidateSlug(slug); err != nil {
		return session, "", failuref(err, "the feature slug %q is not usable", slug)
	}
	branch, err := naming.Branch(repo.FeaturePrefix, session.IssueNumber, slug)
	if err != nil {
		return session, "", failuref(err, "the branch for issue %d could not be derived", session.IssueNumber)
	}

	// The write is conditional inside the store: it fills the columns in only
	// while they are empty, and hands back whichever slug won. Using the
	// returned row rather than the value computed above is what makes a race
	// with another instance converge instead of forking the branch.
	frozen, err := opts.DB.FreezeSessionSlug(ctx, session.ID, slug, branch)
	if err != nil {
		return session, "", fmt.Errorf("freeze the slug of task %s: %w", session.TaskRef, err)
	}
	if frozen.FeatureSlug != slug {
		if err := naming.ValidateSlug(frozen.FeatureSlug); err != nil {
			return frozen, "", failuref(err, "the frozen feature slug %q is not usable", frozen.FeatureSlug)
		}
	}
	if frozen.Branch == "" {
		return frozen, "", failuref(naming.ErrInvalidSlug, "task %s has a frozen slug but no branch", frozen.TaskRef)
	}
	return frozen, frozen.Branch, nil
}

// ensureWorktree creates or reuses the task's branch and worktree (AC-3.8,
// AC-3.9).
//
// All of the reuse logic, and the refusal to touch an occupied path, live in
// internal/git. What is here is the decision to treat every one of those
// outcomes as a reportable failure rather than an error: a directory in the way
// is something the user has to move, and they cannot move it if the only place
// it is mentioned is daemon.log.
func ensureWorktree(
	ctx context.Context,
	opts Options,
	clone *git.Clone,
	session store.Session,
	repo store.Repo,
	branch string,
	logger *slog.Logger,
) (*git.Worktree, error) {
	dir, err := naming.WorktreePath(repo.WorktreeRoot, session.IssueNumber)
	if err != nil {
		return nil, failuref(err, "the worktree path for issue %d could not be derived", session.IssueNumber)
	}

	ensured, err := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{
		Dir:        dir,
		Branch:     branch,
		BaseBranch: repo.BaseBranch,
		Remote:     opts.remote(),
	})
	if err != nil {
		var conflict *git.WorktreeConflictError
		if errors.As(err, &conflict) {
			return nil, failuref(err, "the worktree at %s is not available", conflict.Path)
		}
		return nil, failuref(err, "the worktree for %s could not be prepared at %s", branch, dir)
	}

	logger.Info("worktree ready",
		slog.String("worktree", ensured.Worktree.Path()),
		slog.String("branch", branch),
		slog.Bool("branch_created", ensured.BranchCreated),
		slog.String("branch_start", ensured.BranchStart),
		slog.Bool("worktree_created", ensured.WorktreeCreated))
	return ensured.Worktree, nil
}

// allocateRun inserts the runs row before the subprocess starts (S-1).
//
// The insert is what assigns the per-task ordinal and therefore the NNNN of
// both artefact files. Doing it first means a run that dies without a word
// still owns its number, so the next run cannot be handed the same one and
// overwrite its log.
func allocateRun(
	ctx context.Context,
	opts Options,
	req Request,
	session store.Session,
	segment store.Segment,
	ref naming.Ref,
) (store.Run, error) {
	// The store calls this inside the allocating transaction and has nowhere to
	// put an error, so the failure is carried out here instead. It cannot
	// happen for a ref that has already parsed, which is exactly why it must
	// not be swallowed if it ever does.
	var pathErr error
	run, err := opts.DB.AllocateRun(ctx, store.AllocateRunParams{
		SessionID:        session.ID,
		SegmentID:        segment.ID,
		Mode:             req.mode(),
		TriggerCommentID: req.Trigger.CommentID,
		LogPath: func(ordinal int) string {
			path, err := naming.RunLogPath(opts.Paths.Home(), ref, ordinal)
			if err != nil {
				pathErr = err
				return ""
			}
			return path
		},
	})
	if pathErr != nil {
		return store.Run{}, fmt.Errorf("derive the log path for a run of task %s: %w", ref, pathErr)
	}
	if err != nil {
		return store.Run{}, fmt.Errorf("allocate a run for comment %d on task %s: %w",
			req.Trigger.CommentID, ref, err)
	}
	return run, nil
}
