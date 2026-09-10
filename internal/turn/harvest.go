package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// runAndHarvest starts claude and turns what it did into a record.
//
// The order is what makes a killed daemon survivable:
//
//  1. Run the subprocess — rerunning it once, cold, if claude refuses to
//     resume the conversation the store said it had.
//  2. Finish the runs row immediately, so the outcome is durable before
//     anything slower is attempted (S-9).
//  3. Commit and push whatever the run produced (AC-3.21, AC-3.22).
//  4. File this task's plan into GitHub sub-issues, if there is one waiting —
//     see [fileStoryIssues]. This runs strictly after step 3's push succeeds,
//     never before, and never changes what step 3 already decided.
//  5. Record the commit on the row and the whole story in NNNN.meta.json.
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

	meta := turnMeta(prep, nil)
	defer func() {
		if err := harness.WriteMeta(prep.metaPath, meta); err != nil {
			logger.Error("write the run metadata document",
				slog.String("path", prep.metaPath), slog.Any("error", err))
		}
	}()

	if opts.StoryProgress != nil && prep.promptInput.ApprovedStories != nil {
		if err := opts.StoryProgress.Register(ctx, prep.promptInput.ApprovedStories, prep.worktree.Path()); err != nil {
			return result, err
		}
	}
	prep.validateScope = func(check context.Context) (string, error) { return validateApprovedRelationships(check, opts, prep) }
	prep.meterDB = opts.DB
	prep.planSubmission = func(c context.Context) (*PlanSubmissionCallbacks, error) {
		return prepareTypedPlanSubmission(c, opts, prep)
	}
	prep.submissionOptions = &SubmissionSessionOptions{DB: opts.DB, Progress: opts.StoryProgress, Paths: opts.Paths, RunID: prep.run.ID, Worktree: prep.worktree.Path(), Bundle: prep.promptInput.ApprovedStories, Validation: prep.promptInput.Validation}
	runResult, runErr := runClaude(ctx, prep, logger)
	runResult = harvestValidation(bookkeeping, opts, prep, runResult)
	if opts.StoryProgress != nil && prep.promptInput.ApprovedStories != nil {
		if err := opts.StoryProgress.Finish(bookkeeping, prep.run.ID, "Parent attempt finished; this story has no accepted scoped verification and remains unfinished."); err != nil {
			logger.Error("story progress remains pending", slog.Any("error", err))
		}
	}
	if runResult != nil && runResult.Succeeded() && prep.promptInput.ApprovedStories != nil && prep.promptInput.ApprovedStories.WorkflowVersion == store.WorkflowSeparated {
		pending, err := opts.DB.PendingEngineering(bookkeeping, prep.ref.String(), prep.promptInput.ApprovedStories.Generation)
		if err != nil || len(pending) > 0 {
			runResult = runResult.Clone()
			runResult.Outcome = agent.OutcomeFailed
			runResult.Reason = "Engineering evidence remains incomplete: " + strings.Join(pending, ", ")
			if err != nil {
				runResult.Reason = "Cannot confirm engineering evidence: " + err.Error()
			}
			runResult.ResultText += "\n\n" + runResult.Reason
		}
	}
	if runResult != nil && prep.promptInput.ApprovedStories != nil {
		drift, checkErr := prep.validateScope(bookkeeping)
		if checkErr != nil {
			drift += "\nApproved story relationship could not be confirmed after execution: " + checkErr.Error()
		}
		if drift == "" {
			drift = prep.scopeDrift
		}
		if drift != "" {
			runResult = runResult.Clone()
			runResult.ResultText += "\n\nFactory scope observation:\n" + drift
		}
	}
	// Rebuilt from the preparation as it finally stood, because the fallback may
	// have replaced the prompt and the path under it: what this document records
	// has to be what was actually sent (AC-3.11), not what was first assembled.
	meta = turnMeta(prep, runResult)
	if runErr != nil {
		// The run never started, so there is no RunResult to report — but the
		// row exists and owes the issue a comment all the same.
		result.Disposition = DispositionRan
		result.Outcome = harness.OutcomeFailed
		result.Reason = fmt.Sprintf("coding agent did not start: %v", runErr)
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
	if runResult.Succeeded() && runResult.ResumeToken != "" && prep.segment.ResumeToken == "" {
		if err := opts.DB.SetSegmentResumeToken(bookkeeping, prep.segment.ID, runResult.ResumeToken); err != nil {
			return result, fmt.Errorf("record resume token for run %d of task %s: %w", prep.run.Ordinal, prep.ref, err)
		}
		prep.segment.ResumeToken = runResult.ResumeToken
	}

	var run store.Run
	var err error
	if runResult.Outcome == agent.OutcomeInterrupted {
		run, err = finishInterruption(bookkeeping, opts, prep, runResult)
	} else {
		run, err = opts.DB.FinishRun(bookkeeping, prep.run.ID, runOutcome(runResult))
	}
	if err != nil {
		return result, fmt.Errorf("finish run %d of task %s: %w", prep.run.Ordinal, prep.ref, err)
	}
	result.Run = run

	result = publishPlanHandoff(ctx, opts, prep, result, runResult, logger)
	result = save(ctx, opts, prep, result, runResult, logger)
	if prep.promptInput.ApprovedStories != nil && result.Succeeded() {
		if err := opts.DB.MarkStoryAggregateSucceeded(bookkeeping, prep.run.ID); err != nil {
			result.Err = err
		} else if opts.StoryProgress != nil {
			if err := opts.StoryProgress.Poll(bookkeeping); err != nil {
				logger.Error("story completion remains pending", slog.Any("error", err))
			}
		}
	}
	if prep.promptInput.ApprovedStories != nil && prep.promptInput.ApprovedStories.WorkflowVersion == store.WorkflowSeparated && result.Succeeded() {
		feature, err := opts.DB.GetManagedFeature(bookkeeping, prep.ref.String())
		if err != nil {
			result.Err = err
		} else if feature.EngineeringSucceeded {
			runResult = runResult.Clone()
			runResult.ResultText += "\n\nFactory next action: all engineering and test evidence is complete and aggregate/save succeeded. You may explicitly request /validate to review acceptance. No mode transition was started."
			result.Harness = runResult.Clone()
		}
	}
	result = markManagedPlanReady(ctx, opts, prep, result, runResult)
	result = fileStoryIssues(ctx, opts, prep, result, runResult, logger)
	return finish(bookkeeping, opts, prep, result, runResult, &meta, logger)
}

// runClaude starts the subprocess, and reruns it once — cold, in this same
// turn, on this same run row — when claude refuses to resume.
//
// The refusal is not hypothetical. The runs table says a conversation was
// started under this session id; the transcript that would prove it lives
// outside the store, on the machine that wrote it and under the directory it
// ran in. A machine move, a changed worktree_root or a hand-deleted transcript
// leaves the rows behind with nothing to resume. Without this rerun the task is
// wedged permanently: every run asks to resume, is refused, and reports the
// same failure forever, and no comment on the issue can get it unstuck.
//
// Exactly one rerun, and only on [agent.ErrSessionNotResumable]. The rerun is
// this run, not a new one — same row, same ordinal, same NNNN.jsonl, which the
// harness opens with O_APPEND so the refusal stays in the log as evidence. It
// is cold, so it passes --session-id and cannot be refused for the same reason;
// if it fails it fails as an ordinary run and is reported as one.
func runClaude(
	ctx context.Context,
	prep *preparation,
	logger *slog.Logger,
) (*agent.RunResult, error) {
	if err := checkAttemptScope(ctx, prep); err != nil {
		return nil, err
	}
	result, err := runMetered(ctx, prep, logger)
	if prep.resumeOnly && errors.Is(err, agent.ErrSessionNotResumable) {
		return result, fmt.Errorf("automatic recovery conversation is unusable; resume manually: %w", err)
	}
	if !errors.Is(err, agent.ErrSessionNotResumable) {
		return result, err
	}

	logger.Warn("coding agent has no conversation to resume; rerunning this run cold",
		slog.String("session_ref", prep.request.Session.Ref),
		slog.Int("run_ordinal", prep.run.Ordinal),
		slog.String("stderr", refusalEvidence(result)),
		slog.Any("error", err))

	// The cold prompt is the full one — the header, the headless guidance, the
	// issue note, and whatever else a run that has been told nothing needs to be
	// told. Flipping Resume is the whole of the difference, which is why the
	// input is carried here rather than reassembled: everything a cold prompt
	// gains over a resumed one is assembled in exactly one place, in prepare.
	cold := prep.promptInput
	cold.Resume = false
	if err := preparePlanAttempt(prep.worktree.Path(), prep.session.Branch, prep.run, &cold, logger); err != nil {
		return nil, err
	}
	prompt, err := harness.BuildPrompt(cold)
	if err != nil {
		return nil, fmt.Errorf("assemble the cold prompt for run %d of task %s: %w",
			prep.run.Ordinal, prep.ref, err)
	}

	// The preparation is updated rather than shadowed: the metadata document is
	// built from it after this returns, and it has to record the prompt that was
	// actually sent and the path that was actually taken.
	prep.promptInput = cold
	prep.prompt = prompt
	prep.request.Prompt = prompt
	prep.request.PlanAttempt = cold.PlanAttempt
	prep.request.PlanWorkingContext = cold.PlanWorkingContext
	prep.request.Path = agent.PathColdAfterResumeRefused

	if err := checkAttemptScope(ctx, prep); err != nil {
		return nil, err
	}
	result, err = runMetered(ctx, prep, logger)
	if errors.Is(err, agent.ErrSessionNotResumable) {
		// Unreachable against a real claude: a cold run is never classified as a
		// refusal, because it is not asking to resume anything. If it happens
		// anyway there is no third attempt — one rerun is the whole budget — so
		// it is reported as the run failure it is.
		logger.Error("the cold rerun was refused as well; reporting it as a run failure",
			slog.Any("error", err))
		return result, nil
	}
	return result, err
}

// refusalEvidence renders the refused run's stderr for the log line.
//
// It is evidence for a human and nothing else. Nothing branches on this text:
// the wording belongs to another project's binary, and a release that reworded
// it would otherwise turn a handled condition back into a wedged task.
func refusalEvidence(result *harness.RunResult) string {
	if result == nil {
		return "(no run result)"
	}
	return result.Stderr
}

// turnMeta builds the run's metadata document: what the harness knows about the
// request and the result, plus the fields only a turn can know.
//
// result may be nil — a run that never started still gets a document, which is
// the point of writing it best effort — and then the log path comes from the
// row that was allocated for it rather than from a run that never opened it.
//
// Agent and SessionRef are turn-known rather than harness-known, the same way
// SessionID and Branch are: harness runs whichever tool it was given and has
// no notion of "which agent" beyond that, so the segment row — which turn
// already holds in prep — is what says it. claude_session_id is kept as a
// deprecated alias of session_ref, populated only for a Claude segment, so
// that a reader of an older meta.json and a reader of a new one agree on what
// the field means.
func turnMeta(prep *preparation, result *harness.RunResult) harness.Meta {
	meta := harness.NewMeta(prep.request, prep.prompt, result)
	meta.SessionID = prep.session.ID
	meta.Branch = prep.session.Branch
	meta.SHABefore = prep.shaBefore
	meta.Agent = prep.segment.Agent
	meta.SessionRef = prep.segment.SessionRef
	meta.ClaudeSessionID = ""
	if meta.Agent == store.AgentClaude {
		meta.ClaudeSessionID = meta.SessionRef
	}
	if meta.LogPath == "" {
		meta.LogPath = prep.run.LogPath
	}
	return meta
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
		Status:          store.RunFailed,
		ExitCode:        &exitCode,
		Error:           result.Reason,
		FinishedAt:      result.FinishedAt,
		SemanticOutcome: result.Outcome.String(),
	}
	if result.Succeeded() {
		outcome.Status = store.RunSucceeded
	}
	if result.HasCost || result.CostUSD > 0 {
		// A run that emitted no terminal event has an unknown cost, not a cost
		// of zero, so the column stays NULL (SD-3).
		cost := result.CostUSD
		outcome.CostUSD = &cost
	}
	return outcome
}

// save commits and pushes whatever the run left in the worktree, but only for
// a run that actually succeeded.
//
// A branch's history is read as finished, reviewed work. A run that failed,
// timed out, was abandoned or produced no result did not clear that bar, so
// none of the four ever reaches [commitAndPush]: the commit summary is the
// run's own result text, and a crash or a timeout is not a summary anyone
// asked for. This holds even for a run that timed out after producing most of
// a feature — the work is not thrown away, it stays in the worktree, and the
// next run on this task reuses the same worktree and commits it then, this
// time as part of a run that succeeded.
//
// Deciding that is all this does. The git itself belongs to [commitAndPush],
// which is where the clone gate is held; keeping the check outside it means a
// run that did not succeed never joins the queue at all.
func save(
	ctx context.Context,
	opts Options,
	prep *preparation,
	result Result,
	runResult *harness.RunResult,
	logger *slog.Logger,
) Result {
	if result.Err != nil || prep.promptInput.Validation != nil {
		return result
	}
	if !runResult.Succeeded() {
		result.Publication = "Not published by this run; local edits and commits are retained."
		if head, err := prep.worktree.RevParse(ctx, "HEAD", git.RevParseOptions{}); err == nil {
			result.Publication += " Retained local commit: " + head + "."
		} else {
			result.Publication += " Local commit observation unavailable: " + err.Error()
		}
		logger.Warn("run did not succeed; leaving the work uncommitted in the worktree",
			slog.String("worktree", prep.worktree.Path()),
			slog.String("outcome", runResult.Outcome.String()))
		return result
	}
	return commitAndPush(ctx, opts, prep, result, runResult, logger)
}

// commitAndPush is the turn's second critical section against the registered
// clone, and the counterpart to [prepareClone].
//
// # One acquisition, not two
//
// The commit and the push are one holder's, released once when this returns
// (AC-PAR-3.3). The push is the operation the gate is measured to be needed
// for: at parallelism 4 on git 2.50.1, 240 concurrent "push --set-upstream"
// from one clone produced no non-zero exits and still left 60 branches with no
// upstream, because git loses the race for .git/config and skips the write.
// [git.Worktree.Push] reads the upstream back afterwards and turns that into a
// failure (AC-PAR-3.4), so ungated the effect would be visible rather than
// silent — but a visible failure on 25% of concurrent pushes is not the
// outcome anyone wants, and the read-back is the check of last resort rather
// than the fix. The commit is inside the same acquisition because it and the
// push are one publication: releasing between them would let a sibling's push
// interleave with nothing gained.
//
// # Where the parallelism actually is
//
// Everything expensive in a turn — the claude run, the prompt assembly, the
// history read, the report — happens outside this section and outside
// [prepareClone]. Two turns on one clone overlap for all of it and queue only
// for the git (AC-PAR-3.2).
//
// # No deadline is imposed here
//
// As in [prepareClone], the context is passed through untouched so that the
// push's own 120 seconds start when it execs and not when this turn joined the
// queue (AC-PAR-3.6).
func commitAndPush(
	ctx context.Context,
	opts Options,
	prep *preparation,
	result Result,
	runResult *harness.RunResult,
	logger *slog.Logger,
) Result {
	// Failing to take the gate means the context was cancelled while this turn
	// queued behind a sibling's git — the daemon shutting down. Nothing is
	// lost: the run's changes are still in the worktree and the next run on
	// this task reuses it. It is recorded on the result rather than only logged
	// because a run that produced work and did not publish it is something the
	// issue is owed an answer about (AC-4.1), and it is the same shape a failed
	// commit or push already takes.
	release, err := opts.CloneGate.Acquire(ctx, prep.repo.Path)
	if err != nil {
		result.Err = fmt.Errorf("take the gate on the clone at %s: %w", prep.repo.Path, err)
		logger.Error("take the clone gate to commit and push",
			slog.String("clone", prep.repo.Path),
			slog.String("worktree", prep.worktree.Path()),
			slog.Any("error", err))
		return result
	}
	defer release()

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
	// Agent-change reporting remains relative to the prepared tree. Publication
	// eligibility below is independent: preparation/prior-attempt commits may
	// need publishing even when the agent made no changes.
	result.Changed = commit.Head != prep.shaBefore

	result.Publication = "Local commit " + commit.Head + " retained; publication not confirmed."
	remoteHead, err := prep.worktree.RemoteHead(ctx, opts.remote())
	if err != nil {
		result.Err = &git.PushError{Remote: opts.remote(), Branch: prep.worktree.Branch(), Worktree: prep.worktree.Path(), Err: fmt.Errorf("publication eligibility could not be observed; push was not attempted: %w", err)}
		return result
	}
	if remoteHead == commit.Head {
		result.Pushed = true
		result.Publication = "Commit " + commit.Head + " is already present on the remote; no push needed."
		return result
	}
	if remoteHead == "" && !result.Changed {
		// A new task with no task-specific commits is still an ordinary no-op.
		// Prior retained commits and preparation merges remain eligible for save.
		divergence, err := prep.worktree.Divergence(ctx, opts.remote()+"/"+prep.repo.BaseBranch)
		if err != nil {
			result.Err = err
			return result
		}
		if divergence.Ahead == 0 {
			result.Publication = "No unpublished task commits; no push needed."
			return result
		}
	}
	if pushErr := prep.worktree.Push(ctx, opts.remote()); pushErr != nil {
		observed, observeErr := prep.worktree.RemoteHead(ctx, opts.remote())
		var pe *git.PushError
		upstreamFailure := errors.As(pushErr, &pe) && pe.Published
		if observeErr == nil && observed == commit.Head && !upstreamFailure {
			result.Pushed = true
			result.Publication = "Commit " + commit.Head + " is confirmed on the remote after publication raced; the verified tree was unchanged."
			return result
		}
		result.Err = pushErr
		switch {
		case observeErr != nil:
			result.Publication += " Remote observation failed: " + observeErr.Error()
		case observed != remoteHead:
			result.Publication += fmt.Sprintf(" Remote task tip changed from %s to %s during publication. Resolve the task branch through the existing proposed/manual recovery and request a newly verified run; no late merge was attempted.", displayRemoteHead(remoteHead), displayRemoteHead(observed))
		case prep.sync.Task != nil && observed != prep.sync.Task.BaseSHA:
			result.Publication += fmt.Sprintf(" Remote task tip changed since preparation to %s. Resolve the task branch through the existing proposed/manual recovery and request a newly verified run; no late merge was attempted.", displayRemoteHead(observed))
		}
		logger.Error("push the task branch", slog.String("branch", prep.session.Branch), slog.Any("error", pushErr))
		return result
	}

	result.Pushed = true
	result.Publication = "Commit " + commit.Head + " published successfully."

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

func displayRemoteHead(sha string) string {
	if sha == "" {
		return "(absent)"
	}
	return sha
}
