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
//  7. Assemble the prompt.
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

	claudeSessionID, err := naming.ClaudeSessionID(ref, store.FirstSegmentOrdinal)
	if err != nil {
		return nil, fmt.Errorf("derive the claude session id for task %s: %w", ref, err)
	}
	segment, err := opts.DB.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, claudeSessionID)
	if err != nil {
		return nil, fmt.Errorf("record the conversation segment of task %s: %w", ref, err)
	}

	run, err := allocateRun(ctx, opts, req, session, segment, ref)
	if err != nil {
		return nil, err
	}
	metaPath, err := harness.MetaPath(opts.Paths.Home(), ref, run.Ordinal)
	if err != nil {
		return nil, fmt.Errorf("derive the metadata path of run %d: %w", run.Ordinal, err)
	}

	prompt, err := harness.BuildPrompt(harness.PromptInput{
		Ref:           ref,
		FeatureSlug:   session.FeatureSlug,
		Branch:        branch,
		Mode:          req.mode(),
		RunOrdinal:    run.Ordinal,
		Instruction:   req.Command.Text,
		IssueTitle:    req.Issue.Title,
		IssueBody:     req.Issue.Body,
		IssueAuthor:   req.Issue.Author,
		CommentBody:   req.Trigger.Body,
		CommentAuthor: req.Trigger.Author,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble the prompt for run %d of task %s: %w", run.Ordinal, ref, err)
	}

	logger.Info("run prepared",
		slog.Int("run_ordinal", run.Ordinal),
		slog.Int64("run_id", run.ID),
		slog.String("branch", branch),
		slog.String("worktree", worktree.Path()),
		slog.String("sha_before", shaBefore),
		slog.Int("prompt_bytes", len(prompt)))

	return &preparation{
		repo:      repo,
		session:   session,
		ref:       ref,
		author:    author,
		worktree:  worktree,
		shaBefore: shaBefore,
		segment:   segment,
		run:       run,
		prompt:    prompt,
		metaPath:  metaPath,
		request: harness.Request{
			Ref:            ref,
			RunOrdinal:     run.Ordinal,
			SegmentOrdinal: segment.Ordinal,
			Mode:           req.mode(),
			WorktreePath:   worktree.Path(),
			Prompt:         prompt,
		},
	}, nil
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
