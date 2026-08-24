package turn

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// runAndHarvest starts claude and turns what it did into a record.
//
// The order is what makes a killed daemon survivable:
//
//  1. Run the subprocess.
//  2. Finish the runs row immediately, so the outcome is durable before
//     anything slower is attempted (S-9).
//  3. Commit and push whatever the run produced (AC-3.21, AC-3.22).
//  4. Record the commit on the row and the whole story in NNNN.meta.json.
//
// The metadata document is written from a defer, best effort, so a run that
// panics or is killed mid-harvest still leaves one behind: a document naming a
// timeout is worth far more than a missing one (S-3, AC-3.17).
func runAndHarvest(ctx context.Context, opts Options, prep *preparation, result Result, logger *slog.Logger) (Result, error) {
	// Bookkeeping outlives cancellation. The run's context is cancelled the
	// instant the daemon is asked to shut down (D-B), and the row, the metadata
	// document and the session state all still have to be written after that —
	// otherwise a SIGTERM produces exactly the silence AC-4.1 forbids.
	bookkeeping := context.WithoutCancel(ctx)

	meta := harness.NewMeta(prep.request, prep.prompt, nil)
	meta.SessionID = prep.session.ID
	meta.Branch = prep.session.Branch
	meta.SHABefore = prep.shaBefore
	meta.LogPath = prep.run.LogPath
	defer func() {
		if err := harness.WriteMeta(prep.metaPath, meta); err != nil {
			logger.Error("write the run metadata document",
				slog.String("path", prep.metaPath), slog.Any("error", err))
		}
	}()

	runResult, runErr := opts.Claude.Run(ctx, prep.request)
	if runErr != nil {
		// The run never started, so there is no RunResult to report — but the
		// row exists and owes the issue a comment all the same.
		result.Disposition = DispositionRan
		result.Outcome = harness.OutcomeFailed
		result.Reason = fmt.Sprintf("claude did not start: %v", runErr)
		result.Err = runErr
		meta.Outcome = harness.OutcomeFailed.String()
		meta.ExitCode = harness.SignalExitCode
		meta.Error = &result.Reason
		return finish(bookkeeping, opts, prep, result, nil, &meta, logger)
	}

	result.Disposition = DispositionRan
	result.Harness = runResult.Clone()
	result.Outcome = runResult.Outcome
	result.Reason = runResult.Reason
	result.LogPath = runResult.LogPath

	// Rebuilt from the result so every field the harness knows about is
	// present; the fields only a turn can know are put back afterwards.
	meta = harness.NewMeta(prep.request, prep.prompt, runResult)
	meta.SessionID = prep.session.ID
	meta.Branch = prep.session.Branch
	meta.SHABefore = prep.shaBefore

	run, err := opts.DB.FinishRun(bookkeeping, prep.run.ID, runOutcome(runResult))
	if err != nil {
		return result, fmt.Errorf("finish run %d of task %s: %w", prep.run.Ordinal, prep.ref, err)
	}
	result.Run = run

	result = save(ctx, opts, prep, result, runResult, logger)
	return finish(bookkeeping, opts, prep, result, runResult, &meta, logger)
}

// runOutcome maps a harness outcome onto the runs row (S-9, AC-3.20).
//
// The five outcomes stay five. Only success is not a failure, and the reason
// string the harness already produced is recorded verbatim rather than
// flattened into a shared "failed": a timeout, a shutdown and an exit status of
// 2 call for three different responses from the person reading the comment, and
// an abandonment is not the run's fault at all.
func runOutcome(result *harness.RunResult) store.RunOutcome {
	exitCode := int64(result.ExitCode)
	outcome := store.RunOutcome{
		Status:     store.RunFailed,
		ExitCode:   &exitCode,
		Error:      result.Reason,
		FinishedAt: result.FinishedAt,
	}
	if result.Succeeded() {
		outcome.Status = store.RunSucceeded
	}
	if result.HasResult {
		// A run that emitted no terminal event has an unknown cost, not a cost
		// of zero, so the column stays NULL (SD-3).
		cost := result.CostUSD
		outcome.CostUSD = &cost
	}
	return outcome
}

// save commits and pushes whatever the run left in the worktree.
//
// It runs for a failed run as well as a successful one. A run that timed out
// after two hours may have produced most of the work, and throwing that away
// because the process did not get to say goodbye is not a decision worth
// making. The exception is an abandoned run: the daemon has under three seconds
// to exit (D-B), a push takes up to two minutes, and the work is safe where it
// is — the next run on this task reuses the same worktree and commits it then.
func save(
	ctx context.Context,
	opts Options,
	prep *preparation,
	result Result,
	runResult *harness.RunResult,
	logger *slog.Logger,
) Result {
	if runResult.Outcome == harness.OutcomeAbandoned {
		logger.Warn("run was abandoned; leaving the work uncommitted in the worktree",
			slog.String("worktree", prep.worktree.Path()))
		return result
	}

	commit, err := prep.worktree.Commit(ctx, git.CommitOptions{
		// Untrusted: claude wrote it. internal/git strips any forged
		// Litefactory- line out of it before the real trailers are appended
		// (AC-3.21).
		Summary: runResult.ResultText,
		Task:    prep.ref.String(),
		Mode:    prep.run.Mode,
		Run:     prep.run.Ordinal,
		Author:  prep.author,
	})
	if err != nil {
		result.Err = err
		logger.Error("commit the run's changes",
			slog.String("worktree", prep.worktree.Path()), slog.Any("error", err))
		return result
	}

	result.Changes = commit.Files
	result.SHAAfter = commit.Head
	result.CommitSHA = commit.SHA
	result.Committed = commit.Committed
	// The branch moving is the test, not the commit happening: a run holding
	// the Bash tool may have committed for itself, leaving a clean worktree and
	// work that still has to reach the remote (AC-3.24).
	result.Changed = commit.Head != prep.shaBefore

	if !result.Changed {
		// AC-3.23. A normal result, not an error — and the report says so
		// explicitly rather than posting an empty success.
		logger.Info("run produced no changes; nothing to commit or push",
			slog.String("branch", prep.session.Branch))
		return result
	}

	if err := prep.worktree.Push(ctx, opts.remote()); err != nil {
		// The commit is not lost. *git.PushError names the worktree still
		// holding it, which is what the report quotes (AC-3.24).
		result.Err = err
		logger.Error("push the task branch",
			slog.String("branch", prep.session.Branch),
			slog.String("worktree", prep.worktree.Path()),
			slog.Any("error", err))
		return result
	}
	result.Pushed = true

	logger.Info("run committed and pushed",
		slog.String("branch", prep.session.Branch),
		slog.String("commit", commit.SHA),
		slog.String("sha_before", prep.shaBefore),
		slog.String("sha_after", commit.Head),
		slog.Int("files", len(commit.Files)))
	return result
}

// finish records the commit on the runs row, completes the metadata document
// and settles the session state.
//
// It runs on the abnormal paths too, which is why it takes the bookkeeping
// context: by the time it is reached the caller's context may be long
// cancelled, and none of these writes are optional.
func finish(
	ctx context.Context,
	opts Options,
	prep *preparation,
	result Result,
	runResult *harness.RunResult,
	meta *harness.Meta,
	logger *slog.Logger,
) (Result, error) {
	if runResult == nil {
		// The subprocess never started, so there is no outcome to derive: the
		// row is failed by hand with the signal exit code S-9 reserves for "no
		// status to record".
		exitCode := int64(store.SignalExitCode)
		run, err := opts.DB.FinishRun(ctx, prep.run.ID, store.RunOutcome{
			Status:   store.RunFailed,
			ExitCode: &exitCode,
			Error:    result.Reason,
		})
		if err != nil {
			return result, fmt.Errorf("finish run %d of task %s: %w", prep.run.Ordinal, prep.ref, err)
		}
		result.Run = run
	}

	if result.CommitSHA != "" {
		if err := opts.DB.SetRunCommit(ctx, prep.run.ID, result.CommitSHA); err != nil {
			return result, fmt.Errorf("record the commit of run %d: %w", prep.run.Ordinal, err)
		}
		result.Run.CommitSHA = result.CommitSHA
	}

	meta.SHAAfter = result.SHAAfter
	meta.FilesTouched = result.Files()
	if meta.FilesTouched == nil {
		meta.FilesTouched = []string{}
	}
	if result.Err != nil {
		// The run's own reason keeps its place when it has one: a claude that
		// timed out and then could not be pushed failed twice, and the report
		// needs both halves.
		reason := result.Err.Error()
		if result.Reason != "" {
			reason = result.Reason + "; " + reason
		}
		meta.Error = &reason
	}

	state := store.SessionFailed
	if result.Succeeded() {
		state = store.SessionIdle
	}
	if err := opts.DB.SetSessionState(ctx, prep.session.ID, state); err != nil {
		return result, fmt.Errorf("set the state of task %s: %w", prep.ref, err)
	}
	result.Session.State = state

	logger.Info("turn finished",
		slog.String("outcome", result.Outcome.String()),
		slog.Int("run_ordinal", prep.run.Ordinal),
		slog.Bool("changed", result.Changed),
		slog.Bool("committed", result.Committed),
		slog.Bool("pushed", result.Pushed),
		slog.String("session_state", string(state)))
	return result, nil
}
