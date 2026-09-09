package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/attach"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// preparation is everything the run needs, assembled and recorded before the
// subprocess starts. It is built by [prepare] and consumed by [runAndHarvest].
type preparation struct {
	meterDB       *store.DB
	resumeOnly    bool
	planCompleted bool

	validateScope func(context.Context) (string, error)
	scopeDrift    string
	repo          store.Repo
	session       store.Session
	ref           naming.Ref

	author   git.Author
	worktree *git.Worktree

	// shaBefore is where the branch stood before claude ran. It is read here,
	// not after the run, because that is the only moment it means anything
	// (AC-3.24).
	shaBefore string

	segment store.Segment
	run     store.Run
	// runner is the implementation this run is addressed to, resolved from
	// segment.Agent against opts.Runners while the segment is still in hand.
	// Resolving it here, rather than at the point runAndHarvest calls it, is
	// what turns a Kind with no registered implementation into a reportable
	// failure before a runs row is allocated for it.
	runner   Runner
	request  agent.Request
	prompt   string
	metaPath string

	// sync is what [syncBase] reached for this task branch (US-2), read after
	// [prepareClone] returns rather than recomputed. Rendering it into the
	// issue comment is US-3's job and persisting SyncCommitSHA is US-9's; this
	// is the one place both later stories can read the outcome off without a
	// second call to git.
	sync syncResult

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
// Steps 2 to 4 are one critical section against the registered clone and are
// taken together by [prepareClone]; everything else here runs unserialised.
//
// Everything that can go wrong because of the state of the world is returned as
// a *failure, which [Execute] turns into a reportable result rather than an
// error. A database that will not answer is an ordinary error.
//
// Marking the task running is the last thing prepare does, not an early one:
// the store write and its paired session.State assignment sit immediately
// before the "run prepared" log line, after everything above them that can
// still fail — including the agent-switch refusal — has already returned.
// Every error return above that point therefore leaves sessions.state exactly
// as the caller found it. This is an invariant about where one statement sits
// in the function body, so it is not something a test can enforce from
// outside; it can only be read, here and at the call site itself.
func prepare(
	ctx context.Context,
	opts Options,
	req Request,
	session store.Session,
	ref naming.Ref,
	logger *slog.Logger,
) (*preparation, error) {
	if err := validateContinuation(ctx, opts, req, session); err != nil {
		return nil, failuref(err, "automatic recovery requires manual intervention")
	}
	if err := prepareManagedReplan(ctx, opts, req, session, ref); err != nil {
		return nil, err
	}
	if err := invalidatePlanReadiness(ctx, opts, ref, req.mode()); err != nil {
		return nil, err
	}

	repo, err := opts.DB.GetRepo(ctx, session.RepoID)
	if err != nil {
		return nil, fmt.Errorf("read the repository of task %s: %w", ref, err)
	}
	var selected *teamsource.Snapshot
	if repo.ExternalTeam != "" {
		selected, err = teamsource.Capture(ctx, opts.Paths, repo.ExternalTeam, req.mode())
		if err != nil {
			return nil, failuref(err, "external team instructions could not be captured")
		}
	}

	clone, err := opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return nil, failuref(err, "the registered clone at %s could not be opened", repo.Path)
	}
	author, err := resolveAuthor(ctx, opts, clone)
	if err != nil {
		return nil, err
	}

	session, branch, worktree, sync, _, err := prepareClone(ctx, opts, req, clone, session, repo, author, logger, nil)
	if err != nil {
		return nil, err
	}
	// AC-4.1: a blocked merge preview stops the turn here, before anything
	// else below is created or mutated — no segment, no runs row, no
	// run-artifact directory — and the branch itself is untouched, because
	// [git.Worktree.MergePreview] never performed one (US-2). syncBase
	// produces the blocked outcome regardless of this flag (US-2's own scope
	// boundary); [Options.BlockRunOnConflict] is what turns it into a refusal
	// rather than a run that merely discloses the conflict and goes ahead.
	// Distance refuses unconditionally: unlike a conflict, there is no
	// BlockRunOnConflict-style "disclose and carry on" reading of a cap. An
	// operator who wants the run to go ahead raises the cap or sets it to 0.
	if sync.Outcome == syncOutcomeTooFarBehind {
		return nil, &syncRefusal{
			tooFarBehind: true,
			behind:       sync.Behind,
			maxBehind:    opts.AutoSyncMaxBehind,
			branch:       sync.Branch, base: sync.Base,
		}
	}
	if sync.Outcome == syncOutcomeBlocked && opts.BlockRunOnConflict {
		return nil, &syncRefusal{
			conflicts: append([]string(nil), sync.Conflicts...),
			branch:    sync.Branch, base: sync.Base,
			files:            sync.ConflictFiles,
			snapshots:        sync.ConflictSnapshots,
			collectionReason: sync.ConflictCollectionReason,
			excluded:         sync.ConflictExcluded,
			baseSHA:          sync.BaseSHA, branchSHA: sync.BranchSHA,
		}
	}
	if session.WorktreePath != worktree.Path() {
		if err = opts.DB.SetSessionWorktree(ctx, session.ID, worktree.Path()); err != nil {
			return nil, fmt.Errorf("record the worktree of task %s: %w", ref, err)
		}
		session.WorktreePath = worktree.Path()
	}

	shaBefore, err := worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if err != nil {
		return nil, failuref(err, "the head of %s could not be read", branch)
	}
	deliveryPlan, err := planDelivery(opts, req, selected, worktree.Path())
	if err != nil {
		return nil, failuref(err, "external team instructions could not be prepared")
	}

	// The one place in the tree that derives a session ref. Every later
	// consumer — the harness, the run log, the metadata document — reads it back
	// off the segment below, because GetOrCreateSegment only uses this value
	// when it creates the row and returns an existing row as stored. Its doc
	// comment says the stored value wins if the two ever disagree; a resumed run
	// is what makes that real, since the stored id is the only one that names a
	// conversation claude can still find.
	sessionRef, err := naming.SessionRef(ref, store.FirstSegmentOrdinal)
	if err != nil {
		return nil, fmt.Errorf("derive the session ref for task %s: %w", ref, err)
	}
	// resolvedKind is what the daemon's label ladder decided (internal/daemon's
	// Daemon.resolveAgent, stage 1), carried in on req.Agent. The daemon has
	// already replaced an absent label with the first configured agent, so this
	// layer never invents a vendor-specific fallback.
	resolvedKind := req.Agent

	// The task lock serializes segment selection and creation. Effective
	// instruction changes rotate same-vendor segments; vendor changes still
	// return the stored segment unchanged for the refusal below.
	segment, delivery, err := prepareDeliverySegment(ctx, opts, req, session, ref, sessionRef, deliveryPlan)
	if err != nil {
		return nil, fmt.Errorf("record the conversation segment of task %s: %w", ref, err)
	}
	if segment.Agent != string(resolvedKind) {
		// A session with no segment yet always matches — GetOrCreateSegment
		// just created one under resolvedKind — so this is reachable only
		// when a prior run already committed this conversation to a
		// different tool (AC-3.3, AC-3.1). No session-state transition has
		// occurred and no runs row has been allocated; the worktree already
		// exists on disk and sessions.worktree_path already records it, and
		// both are idempotent and reused by the next turn.
		return nil, &agentSwitchRefusal{owning: segment.Agent, requested: string(resolvedKind)}
	}
	kind := agent.Kind(segment.Agent)
	runner, ok := opts.Runners.runner(kind)
	if !ok {
		// Registry-driven, not a hardcoded switch (AC-5.6): a Kind with no
		// entry in opts.Runners is refused here, before a runs row is
		// allocated for it, rather than reaching a nil Runner three steps
		// later. Reportable rather than a bare error, because the issue is
		// owed an explanation the same way any other preparation failure is
		// (AC-4.1).
		return nil, failuref(nil, "no coding-agent-tool implementation is registered for %q", segment.Agent)
	}

	// Read before the row for this run is inserted, so that the run being
	// prepared cannot be found as its own predecessor. The terminal-status
	// filter would exclude it anyway — it is inserted as running — and both are
	// kept: one of them is an ordering that a later edit could quietly reverse.
	//
	// Both stay exact with N turns in flight, because neither depends on how
	// many there are: the rows a sibling worker writes are never in this result
	// set. See [previousRun] for why.
	previous, found, err := previousRun(ctx, opts, session, segment)
	if err != nil {
		return nil, err
	}
	// Tool-aware, and evaluated on its own rather than trusted from the
	// switch-refusal check above (US-3, AC-3.4): a segment whose agent does
	// not match resolvedKind never reaches this line today, because that
	// mismatch already returned an *agentSwitchRefusal — but resumeDecision
	// reads segment.Agent for itself rather than assuming that invariant, so
	// the resume decision stays correct even if the two checks are ever
	// reordered or one of them is reused on its own.
	resume := resumeDecision(found, segment, resolvedKind)
	if req.Continuation != nil {
		if segment.ID != req.Continuation.SegmentID {
			return nil, failuref(nil, "automatic recovery conversation changed; resume manually")
		}
		resume = true
		segment.ResumeToken = req.Continuation.Context.ResumeIdentity
	}

	run, err := allocateRun(ctx, opts, req, session, segment, ref)
	if err != nil {
		return nil, err
	}
	// US-9: sync was decided earlier, inside prepareClone's call into
	// syncBase, but there was no runs row yet for the outcome to attach to —
	// allocateRun just above is what creates it. Persisting it here, before
	// anything else below that can fail, keeps the invariant the comment on
	// the SetSessionState call further down relies on: every error return
	// from this point on must leave sessions.state untouched, and that is
	// only true if this write cannot itself be skipped by a later failure.
	if _, err = opts.DB.RecordRunSync(ctx, run.ID, store.RunSyncOutcome{
		Outcome:   store.SyncOutcome(sync.Outcome),
		Behind:    sync.Behind,
		Ahead:     sync.Ahead,
		CommitSHA: sync.SyncCommitSHA,
	}); err != nil {
		return nil, fmt.Errorf("record the base-sync outcome for run %d of task %s: %w", run.Ordinal, ref, err)
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

	// Derive current and previous question permission with the same mode policy.
	mayAsk := questionPermission(req.mode(), req.Command)

	// Attachments are fetched here: after the worktree and the runs row
	// exist — the landing directory is named for this run's ordinal — and
	// before the prompt is assembled, because the prompt is where they are
	// handed off. A failure to fetch never fails the turn (see
	// [ingestAttachments]).
	attachments := ingestAttachments(ctx, opts, req, ref, run.Ordinal, logger)
	writeAttachmentManifest(ctx, worktree.Path(), ref, run.Ordinal, attachments, logger)

	// Both fields are populated only when resume is true: a cold run has no
	// predecessor whose transcript needs correcting, and a zero store.Run's
	// own Succeeded() would read as success anyway, which is not the fact a
	// cold prompt could ever need. resumeDecision's own three conditions
	// already guarantee found is true whenever resume is true, so previous is
	// always the real predecessor row here, never the zero value.
	var previousFailed bool
	var previousReason string
	if resume {
		previousFailed = !previous.Succeeded()
		previousReason = previous.Error
	}

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
		Attachments:       attach.PromptSection(attachments.Accepted),
		PreviousFailed:    previousFailed,
		PreviousReason:    previousReason,
	}
	if sync.Outcome == syncOutcomeDeferredDirty {
		promptInput.DeferredSync = harness.DeferredSyncContext{
			Deferred: true, Branch: sync.Branch, Base: sync.Base,
			Behind: sync.Behind, Ahead: sync.Ahead,
		}
	}
	if err := prepareManagedScope(ctx, opts, req, ref); err != nil {
		return nil, err
	}
	if err := preparePlanHandoff(ctx, opts, ref, req.mode(), &promptInput); err != nil {
		return nil, err
	}
	if err := preparePlanAttempt(worktree.Path(), session.Branch, run, &promptInput, logger); err != nil {
		return nil, err
	}
	if err := prepareRefinementContext(ctx, opts, req, &promptInput); err != nil {
		return nil, err
	}
	if err := prepareApprovedContext(ctx, opts, &promptInput, run.ID); err != nil {
		return nil, err
	}
	prompt, err := harness.BuildPrompt(promptInput)
	if err != nil {
		return nil, fmt.Errorf("assemble the prompt for run %d of task %s: %w", run.Ordinal, ref, err)
	}
	if delivery != nil {
		prompt = "Instruction bundle: " + delivery.Root + "\n\n" + prompt
		if string(req.Agent) == "codex" {
			prompt = string(deliveryPlan.files["bootstrap.md"]) + "\n" + prompt
		}
	}

	path := agent.PathCold
	if resume {
		path = agent.PathResume
	}

	// Permissions carries no vendor-named field crossing this boundary
	// (AC-5.2). The daemon resolves a mode profile before dispatch and its
	// payload is narrowed into harness.ToolSurface here and
	// nowhere else, because Claude's --tools/--allowedTools vocabulary is
	// exactly the shape this package must not otherwise know.
	var permissions agent.Permissions
	if req.Agent == agent.Kind(store.AgentCodex) && req.CodexSandbox != "" {
		permissions = agent.Permissions{Profile: req.ToolProfile, Payload: codex.Sandbox{Mode: req.CodexSandbox}}
	} else if len(req.Tools) > 0 || req.ToolProfile != "" {
		permissions = agent.Permissions{
			Profile: req.ToolProfile,
			Payload: harness.ToolSurface{Tools: req.Tools, AllowedTools: req.AllowedTools},
		}
	}

	// This is prepare's last store write, deliberately: every error
	// return above this line — the agent-switch refusal (AC-3.1) included —
	// leaves sessions.state exactly as it was when this call started, because
	// nothing after it can fail. That ordering is what this comment records
	// rather than what a test enforces: a test asserting "no write happened
	// on the refusal path" cannot tell "correctly last" apart from "moved
	// back up by a later edit" without re-deriving this same argument, so it
	// is read here, not checked there.
	//
	// Moving the write this late does open a window where allocateRun's runs
	// row (and the attachment manifest written above) can be observed as
	// running before its session is marked running. That window is accepted,
	// not mitigated: ReconcileOrphans already fails an orphaned running run
	// and its session together at daemon start, and nothing else in this
	// tree branches control flow on session.state.
	if err = opts.DB.SetSessionState(ctx, session.ID, store.SessionRunning); err != nil {
		return nil, fmt.Errorf("mark task %s running: %w", ref, err)
	}
	session.State = store.SessionRunning

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
		resumeOnly:  req.Continuation != nil,
		repo:        repo,
		session:     session,
		ref:         ref,
		author:      author,
		worktree:    worktree,
		shaBefore:   shaBefore,
		segment:     segment,
		run:         run,
		runner:      runner,
		prompt:      prompt,
		metaPath:    metaPath,
		promptInput: promptInput,
		sync:        sync,
		request: agent.Request{
			Delivery:           delivery,
			PlanProposalPath:   promptInput.PlanProposalPath,
			PlanOutcomePath:    promptInput.PlanOutcomePath,
			PlanGeneration:     promptInput.PlanGeneration,
			PlanRunID:          promptInput.PlanRunID,
			PlanAttempt:        promptInput.PlanAttempt,
			PlanContextPath:    promptInput.PlanContextPath,
			PlanWorkingContext: promptInput.PlanWorkingContext,
			MayAsk:             promptInput.MayAsk,
			PreviousMayAsk:     promptInput.PreviousMayAsk,
			Ref:                ref,
			RunOrdinal:         run.Ordinal,
			SegmentOrdinal:     segment.Ordinal,
			// Ref is the segments row's own value, never recomputed: if it and
			// a fresh derivation ever disagree the stored one wins, because it
			// is the one the tool has already seen. Resume is the tool's own
			// reported handle, absent until a real run reports one — Claude
			// has none of its own, since its --session-id already is Ref, so
			// this stays empty for every segment this tree can produce today.
			Session: agent.SessionRef{
				Ref:    segment.SessionRef,
				Resume: segment.ResumeToken,
			},
			Mode:         req.mode(),
			Path:         path,
			WorktreePath: worktree.Path(),
			Prompt:       prompt,
			Permissions:  permissions,
		},
	}, nil
}

// prepareClone is the turn's first critical section against the registered
// clone: fetch, freeze the slug, create or reuse the branch and the worktree.
//
// # One acquisition, not three
//
// The gate is taken once, at the top, and given back by one deferred release
// when this function returns (AC-PAR-3.3). Gating each git call separately
// would look tidier and would be wrong: [git.EnsureWorktree] resolves the
// branch start from the remote-tracking refs and then runs "worktree add"
// against it, so a sibling's fetch landing between the two moves the ref the
// start was resolved from. That time-of-check-to-time-of-use window is
// precisely what this section exists to close, and it is only closed if the
// fetch, the resolution and the add are one holder's.
//
// [freezeSlug] is a database call sitting inside a git critical section, which
// is worth being deliberate about. It stays inside because the branch name it
// returns is an input to the worktree creation below, and taking the gate again
// afterwards would reopen the window above. It is a single indexed UPDATE …
// RETURNING against a local SQLite file — microseconds against the seconds a
// fetch takes — so what it costs the turns queued behind it is not measurable.
//
// # confirmed (US-12)
//
// confirmed is nil for every ordinary turn and non-nil only for
// [ExecuteApplyResolution]'s agentless apply turn. [syncBase] itself only
// runs when confirmed is nil: a confirmed resolution already knows exactly
// what it is merging and carries its own re-verification, so a second,
// independent auto-sync attempt over the same branch would risk a second,
// unconfirmed merge commit (AC-12.5) and could move the branch tip out from
// under the TOCTOU check below before that check ever runs (S-15). When
// confirmed is set, applying the confirmed proposal is instead this
// section's fourth step, immediately after [ensureWorktree]: the stored
// base/branch shas are re-verified against values read no earlier than this
// same gate acquisition (S-15), and — only when they still match —
// [git.Worktree.WriteResolvedFiles], [git.Worktree.StageResolvedFiles],
// [git.Worktree.CompleteMerge] and [git.Worktree.Push] run right here,
// inline, rather than through a separately named helper. That is
// deliberate, not merely convenient: internal/turn/gate_test.go's static
// analysis attributes a gated call to the literal function it is written
// in, and the whole point of doing this here is that none of those four
// calls exists anywhere else in the package — see gate_test.go's own
// "gated" map, which names this function as their one allowed home. There is
// no claude run between this step and [ensureWorktree] — the confirmation
// grammar never starts one — so, unlike an ordinary turn's separate
// [commitAndPush] critical section, nothing here needs to give the gate back
// and take it a second time.
//
// # No deadline is imposed here
//
// The context is passed through untouched. Every git command starts its own
// deadline when it execs (internal/git/exec.go), so time a turn spends waiting
// for the gate is not spent out of the fetch's 60 seconds or the push's 120
// (AC-PAR-3.6). Wrapping this section in a context.WithTimeout would silently
// undo that, and would do it worst on exactly the busy clone where the wait is
// longest.
func prepareClone(
	ctx context.Context,
	opts Options,
	req Request,
	clone *git.Clone,
	session store.Session,
	repo store.Repo,
	author git.Author,
	logger *slog.Logger,
	confirmed *confirmedProposal,
) (store.Session, string, *git.Worktree, syncResult, *confirmedApplyResult, error) {
	// A cancelled context here is the daemon shutting down while this turn
	// queued behind a sibling's fetch. It is reported the same way a failed
	// fetch is — as a *failure the issue hears about — because from the
	// thread's point of view nothing was attempted and the comment is owed an
	// answer either way (AC-4.1).
	release, err := opts.CloneGate.Acquire(ctx, repo.Path)
	if err != nil {
		return session, "", nil, syncResult{}, nil, failuref(err, "the gate on the clone at %s could not be taken", repo.Path)
	}
	defer release()

	if err := fetch(ctx, opts, clone, repo, logger); err != nil {
		return session, "", nil, syncResult{}, nil, err
	}

	session, branch, err := freezeSlug(ctx, opts, req, session, repo)
	if err != nil {
		return session, "", nil, syncResult{}, nil, err
	}

	worktree, err := ensureWorktree(ctx, opts, clone, session, repo, branch, logger)
	if err != nil {
		return session, "", nil, syncResult{}, nil, err
	}

	if confirmed == nil {
		sync, err := syncBase(ctx, opts, req, worktree, repo, author, logger)
		if err != nil {
			return session, "", nil, syncResult{}, nil, err
		}
		return session, branch, worktree, sync, nil, nil
	}

	// US-12: see the "confirmed" section of this function's own doc comment
	// above for why this runs inline rather than through a helper, and for
	// why [syncBase] itself does not run at all on this path. There is no
	// meaningful syncResult for this turn, so every return below carries the
	// zero value.
	stored := confirmed.proposal
	currentBranchSHA, revErr := worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if revErr != nil {
		return session, branch, worktree, syncResult{}, nil, failuref(revErr, "the head of %s could not be read", worktree.Branch())
	}
	baseRef := opts.remote() + "/" + repo.BaseBranch
	currentBaseSHA, revErr := worktree.RevParse(ctx, baseRef, git.RevParseOptions{})
	if revErr != nil {
		return session, branch, worktree, syncResult{}, nil, failuref(revErr, "%s could not be resolved", baseRef)
	}
	if currentBranchSHA != stored.BranchSHA || currentBaseSHA != stored.BaseSHA {
		logger.Info("confirmed proposal is stale; will be superseded rather than applied",
			slog.Int64("proposal_id", stored.ID),
			slog.String("stored_branch_sha", stored.BranchSHA), slog.String("current_branch_sha", currentBranchSHA),
			slog.String("stored_base_sha", stored.BaseSHA), slog.String("current_base_sha", currentBaseSHA))
		return session, branch, worktree, syncResult{}, &confirmedApplyResult{Superseded: true, Proposal: stored}, nil
	}

	ref, refErr := naming.ParseRef(session.TaskRef)
	if refErr != nil {
		return session, branch, worktree, syncResult{}, nil, fmt.Errorf("task ref %q on session %d: %w", session.TaskRef, session.ID, refErr)
	}

	files := make([]git.ResolvedFile, 0, len(stored.Files))
	for _, f := range stored.Files {
		files = append(files, appliedRegionFile(stored, f, opts.ConflictContentMaxBytes))
	}
	if err := worktree.WriteResolvedFiles(ctx, baseRef, files); err != nil {
		return session, branch, worktree, syncResult{}, nil, failuref(err, "the confirmed resolution for proposal %d could not be written", stored.ID)
	}
	if err := worktree.StageResolvedFiles(ctx, files); err != nil {
		return session, branch, worktree, syncResult{}, nil, failuref(err, "the confirmed resolution for proposal %d could not be staged", stored.ID)
	}
	mergeResult, err := worktree.CompleteMerge(ctx, git.CompleteMergeOptions{
		Task: ref.String(), Mode: "confirm", Author: author,
		Files: files, ProposalID: stored.ID, ConfirmedBy: confirmed.confirmedBy, Model: stored.Model,
	})
	if err != nil {
		return session, branch, worktree, syncResult{}, nil, failuref(err, "the merge confirmed by proposal %d could not be completed", stored.ID)
	}
	if err := worktree.Push(ctx, opts.remote()); err != nil {
		return session, branch, worktree, syncResult{}, nil, failuref(err, "the commit applying proposal %d could not be pushed", stored.ID)
	}

	return session, branch, worktree, syncResult{}, &confirmedApplyResult{Proposal: stored, CommitSHA: mergeResult.Head}, nil
}

// confirmedApplyResult is what [prepareClone] reached when confirmed was
// non-nil (US-12): either the proposal turned out to be stale and was left
// for the caller to mark superseded, or it was applied and CommitSHA is the
// merge commit [git.Worktree.CompleteMerge] produced. It is nil whenever
// confirmed was nil, and whenever an error is also returned — a git mutator
// that failed reports through the ordinary error return, the same as every
// other step of this function.
type confirmedApplyResult struct {
	// Superseded is true when the stored base/branch shas no longer matched
	// what a fresh read found (AC-12.4). Proposal is set; CommitSHA is not.
	Superseded bool
	// Proposal is the proposal as [prepareClone] read it before deciding —
	// carried back so the caller does not have to re-fetch it to know which
	// proposal this result is about.
	Proposal store.ConflictProposal
	// CommitSHA is the merge commit produced, set only when Superseded is
	// false.
	CommitSHA string
}

// syncOutcome names the result syncBase reached (US-2). It is logged on every
// path and carried on [preparation.sync] for the later stories that render it
// (US-3) or persist it (US-9); today it is otherwise only observable in
// daemon.log.
type syncOutcome string

const (
	syncOutcomeUpToDate      syncOutcome = "up_to_date"
	syncOutcomeSynced        syncOutcome = "synced"
	syncOutcomeBlocked       syncOutcome = "blocked"
	syncOutcomeSkipped       syncOutcome = "skipped"
	syncOutcomeDeferredDirty syncOutcome = "deferred_dirty"
	// syncOutcomeTooFarBehind is distance alone: further behind than
	// [Options.AutoSyncMaxBehind] allows, so no merge was ever previewed.
	// It refuses the run; see [syncRefusal.tooFarBehind].
	syncOutcomeTooFarBehind syncOutcome = "too_far_behind"
)

// syncResult is what [syncBase] reached for one task branch: the outcome, the
// divergence it was measured from, the merge commit's sha when one was made,
// and the conflicting paths when the preview was unclean.
//
// It is deliberately a plain data value with no behaviour: US-3 reads Outcome
// and Conflicts to render the comment, US-9 reads SyncCommitSHA to persist it,
// and neither has to call back into internal/git to get there.
type syncResult struct {
	// Outcome is the synchronization decision syncBase reached.
	Outcome syncOutcome
	// Behind and Ahead are the [git.Divergence] syncBase measured before
	// deciding. Behind is 0 exactly when Outcome is [syncOutcomeUpToDate];
	// Ahead is independent of Outcome and may be nonzero for any outcome.
	Behind, Ahead int
	// SyncCommitSHA is the merge commit [git.Worktree.MergeBase] produced,
	// non-empty only when Outcome is [syncOutcomeSynced] and the merge
	// actually moved the branch ([git.MergeResult.Merged]).
	SyncCommitSHA string
	// Conflicts lists the paths [git.Worktree.MergePreview] reported, non-nil
	// only when Outcome is [syncOutcomeBlocked].
	Conflicts []string
	// Reason is why sync did not complete, phrased to read directly after
	// "because ", and set on [syncOutcomeSkipped] and
	// [syncOutcomeDeferredDirty]. US-3 renders it; AC-3.2 is why a skip records one
	// at all, since a skip that cannot say why is the silent staleness this
	// whole feature exists to prevent.
	Reason string

	// Branch and Base are the exact ref names Outcome was decided against,
	// carried alongside Conflicts so a suggested-resolution advisor (US-11)
	// can be given proper names without reaching back into git for them.
	Branch, Base string
	// BranchSHA and BaseSHA are Branch and Base resolved to object ids at
	// the moment [git.Worktree.MergePreview] evaluated them, still inside
	// the same [Options.CloneGate] acquisition (US-12, S-15). Non-empty
	// only when Outcome is [syncOutcomeBlocked] — they are what a later
	// confirmation's own fresh reads are compared against before anything
	// is applied.
	BranchSHA, BaseSHA string
	// ConflictFiles and ConflictExcluded are the conflicting content
	// [collectConflictContent] gathered inside this same [Options.CloneGate]
	// acquisition (US-11, S-10), non-nil only when
	// [Options.SuggestConflictResolution] was true and Outcome reached
	// [syncOutcomeBlocked].
	ConflictCollectionReason string
	ConflictSnapshots        []git.ConflictSnapshot
	ConflictFiles            []agent.ConflictFileContent
	ConflictExcluded         []string
}

// syncEnabled resolves whether auto-sync runs for this turn: a non-nil
// [Request.SyncOverride] — the per-issue "Sync:" label's resolved decision
// (US-7) — always wins; a nil override leaves the configured
// [Options.AutoSyncBase] default in force (AC-7.1).
func syncEnabled(opts Options, req Request) bool {
	if req.SyncOverride != nil {
		return *req.SyncOverride
	}
	return opts.AutoSyncBase
}

// withinSyncCap reports whether behind commits is close enough to attempt
// auto-sync. [Options.AutoSyncMaxBehind] of 0 means no cap, exactly as
// [config.Settings.AutoSyncMaxBehind] documents.
func withinSyncCap(opts Options, behind int) bool {
	return opts.AutoSyncMaxBehind == 0 || behind <= opts.AutoSyncMaxBehind
}

// reportSync renders one [syncResult] as the value [report.Run.Sync] takes,
// or nil when there is nothing a run report should say. Up-to-date is the
// nil case by design: AC-3.1 requires a report for a branch that was already
// current to be byte-identical to one from before this feature existed.
//
// Blocked never reaches here either, for a different reason — a blocked run
// is refused before it starts and answered by [report.ReplyRefusesSync], so
// there is no run report to carry it.
func reportSync(sync syncResult) *report.Sync {
	switch sync.Outcome {
	case syncOutcomeUpToDate, syncOutcomeBlocked:
		return nil
	}
	return &report.Sync{
		Outcome:   report.SyncOutcome(sync.Outcome),
		Behind:    sync.Behind,
		Ahead:     sync.Ahead,
		Base:      sync.Base,
		CommitSHA: sync.SyncCommitSHA,
		Reason:    sync.Reason,
	}
}

// syncOffLabel is the label a per-issue opt-out carries, named here only so a
// skip can say what caused it. internal/daemon owns the label's resolution and
// its spelling; this is the rendered name, not a second parser.
const syncOffLabel = "`Sync: Off`"

// syncBase merges the latest base branch into the task branch when that merge
// is clean (US-2), as the fourth and last step of [prepareClone] — after
// [ensureWorktree], still inside the one [Options.CloneGate] acquisition that
// function took at its top. There is no separate gate for this step: the same
// time-of-check-to-time-of-use reasoning that keeps fetch, freezeSlug and
// ensureWorktree together applies here too, since a sibling's fetch landing
// between Divergence and MergeBase would let this compute a preview against a
// base that has already moved.
//
// It never returns an error for a merge that is merely unclean: a conflicted
// [git.Worktree.MergePreview] is the "blocked" outcome, not a failure, and the
// branch is left exactly as [ensureWorktree] produced it. An error return here
// means the merge could not even be attempted or attempting it failed in a way
// [git.Worktree.MergeBase] could not roll back on its own — both are reported
// to the issue the same way every other prepareClone step's failure is.
func syncBase(
	ctx context.Context,
	opts Options,
	req Request,
	worktree *git.Worktree,
	repo store.Repo,
	author git.Author,
	logger *slog.Logger,
) (syncResult, error) {
	baseRef := opts.remote() + "/" + repo.BaseBranch

	divergence, err := worktree.Divergence(ctx, baseRef)
	if err != nil {
		return syncResult{}, failuref(err, "how far %s has diverged from %s could not be measured", worktree.Branch(), baseRef)
	}

	logFields := []any{
		slog.String("branch", worktree.Branch()),
		slog.String("base", baseRef),
		slog.Int("behind", divergence.Behind),
		slog.Int("ahead", divergence.Ahead),
	}

	if divergence.Behind == 0 {
		logger.Debug("base sync: up to date", append(logFields, slog.String("outcome", string(syncOutcomeUpToDate)))...)
		return syncResult{Outcome: syncOutcomeUpToDate, Behind: divergence.Behind, Ahead: divergence.Ahead}, nil
	}

	if !syncEnabled(opts, req) {
		reason := "auto-sync is turned off for this repository"
		if req.SyncOverride != nil {
			reason = "this issue carries a " + syncOffLabel + " label"
		}
		logger.Info("base sync: skipped",
			append(logFields, slog.String("outcome", string(syncOutcomeSkipped)), slog.String("reason", reason))...)
		return syncResult{Outcome: syncOutcomeSkipped, Behind: divergence.Behind, Ahead: divergence.Ahead, Reason: reason}, nil
	}
	// Over the cap refuses the run rather than skipping past it. A skip would
	// let the run proceed against a branch known to be stale, disclosed only
	// in a report nobody has to read; the cap says this branch is too far
	// gone to merge unattended, and stopping is the honest answer to that.
	// Being switched off, just above, stays a skip: an operator who turned
	// sync off asked for the run to go ahead.
	if !withinSyncCap(opts, divergence.Behind) {
		logger.Info("base sync: too far behind",
			append(logFields, slog.String("outcome", string(syncOutcomeTooFarBehind)),
				slog.Int("max_behind", opts.AutoSyncMaxBehind))...)
		return syncResult{
			Outcome: syncOutcomeTooFarBehind, Behind: divergence.Behind, Ahead: divergence.Ahead,
			Branch: worktree.Branch(), Base: repo.BaseBranch,
		}, nil
	}

	// Retained edits must be available to a recovery run even when the
	// committed tips conflict (AC-171-1). Observe them before preview, but
	// after explicit policy decisions; never stage or park them here.
	changes, err := worktree.Status(ctx)
	if err != nil {
		return syncResult{}, failuref(err, "uncommitted changes on %s could not be inspected before base synchronization", worktree.Branch())
	}
	if len(changes) > 0 {
		const reason = "uncommitted changes were preserved for this run to inspect and finish"
		logger.Info("base sync: deferred to preserve uncommitted changes",
			append(logFields, slog.String("outcome", string(syncOutcomeDeferredDirty)), slog.String("reason", reason))...)
		return syncResult{
			Outcome: syncOutcomeDeferredDirty, Behind: divergence.Behind, Ahead: divergence.Ahead,
			Branch: worktree.Branch(), Base: baseRef, Reason: reason,
		}, nil
	}

	preview, err := worktree.MergePreview(ctx, baseRef)
	if err != nil {
		return syncResult{}, failuref(err, "whether %s could be merged into %s could not be previewed", baseRef, worktree.Branch())
	}
	if !preview.Clean {
		logger.Info("base sync: blocked",
			append(logFields, slog.String("outcome", string(syncOutcomeBlocked)), slog.Any("conflicts", preview.Conflicts))...)
		blocked := syncResult{
			Outcome: syncOutcomeBlocked, Behind: divergence.Behind, Ahead: divergence.Ahead, Conflicts: preview.Conflicts,
			Branch: worktree.Branch(), Base: repo.BaseBranch,
		}
		// US-12: the branch and base tips this preview was evaluated against
		// are captured here, still inside the same gate acquisition, so a
		// later confirmation has something to re-verify its own fresh reads
		// against (S-15) — unconditional, the same way Branch and Base
		// themselves are, since a proposal may or may not end up being
		// generated from this refusal but the starting point it would need
		// is cheapest to read right here, right now.
		branchSHA, err := worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
		if err != nil {
			return syncResult{}, failuref(err, "the head of %s could not be read", worktree.Branch())
		}
		baseSHA, err := worktree.RevParse(ctx, baseRef, git.RevParseOptions{})
		if err != nil {
			return syncResult{}, failuref(err, "%s could not be resolved", baseRef)
		}
		blocked.BranchSHA, blocked.BaseSHA = branchSHA, baseSHA
		// US-11: the conflicting content is collected here, still inside
		// prepareClone's one [Options.CloneGate] acquisition, and only when
		// the feature is configured on and there is a comment to eventually
		// attach a suggestion to — a disabled turn, or one triggered by the
		// issue body (which draws no reply at all, AC-2.16c), must not
		// spend the extra git reads this loop costs (AC-11.3).
		if opts.SuggestConflictResolution && req.Trigger.CommentID != 0 {
			snapshots, collectionErr := worktree.CollectConflictSnapshots(ctx, preview.TreeSHA, branchSHA, baseSHA, preview.Conflicts, opts.ConflictContentMaxBytes)
			if collectionErr != nil {
				blocked.ConflictCollectionReason = collectionErr.Error()
				blocked.ConflictExcluded = append([]string(nil), preview.Conflicts...)
				logger.Warn("conflict collection refused", slog.Any("error", collectionErr))
			} else {
				blocked.ConflictSnapshots = snapshots
				blocked.ConflictFiles = conflictAdvisorFiles(snapshots)
			}
		}
		return blocked, nil
	}

	result, err := worktree.MergeBase(ctx, baseRef, git.MergeOptions{Author: author})
	if err != nil {
		return syncResult{}, failuref(err, "%s could not be merged into %s", baseRef, worktree.Branch())
	}
	logger.Info("base sync: synced",
		append(logFields, slog.String("outcome", string(syncOutcomeSynced)), slog.String("head", result.Head), slog.Bool("merged", result.Merged))...)

	sync := syncResult{Outcome: syncOutcomeSynced, Behind: divergence.Behind, Ahead: divergence.Ahead}
	if result.Merged {
		sync.SyncCommitSHA = result.Head
	}
	return sync, nil
}

// previousRun finds the last run of this segment that claude was actually
// invoked for — one of the three things [resumeDecision] weighs to answer
// "is there a conversation to continue?" (US-3, AC-3.4).
//
// The evidence is the runs table and nothing else. A row on this segment in a
// terminal status means a subprocess was started with the segment's claude
// session id, and claude keeps a transcript for every session id it is given —
// so a conversation should exist and this run can resume it. There is no column
// for this and there does not need to be one: runs.segment_id and runs.status
// already say it, and a column would be a second copy of the same fact to keep
// in step.
//
// A row still marked running is not evidence, and the argument for that has to
// be made against the real concurrency model rather than the one this code was
// written under. It is not "one turn at a time in this process" any more; it is
// one worker per session, with up to max_concurrent_runs sessions in flight at
// once. Under that model a running row can be three things, and none of them is
// a predecessor.
//
// It can be this daemon's own run, which cannot be its own predecessor. The
// call site reads before the row is inserted, and the terminal-status filter
// would exclude it even if a later edit reversed that order.
//
// It can be a run a killed daemon left behind, which becomes failed — and so
// terminal, and so eligible — the moment reconciliation sees it (D-B).
//
// It can be a sibling worker's run, and that one cannot reach this result set
// at all. ListRuns is scoped to this session, the loop below narrows again to
// run.SegmentID == segment.ID, and a segment belongs to exactly one session: a
// concurrent worker is by construction on a *different* session and therefore
// touching different rows entirely. A sibling on the *same* session does not
// exist to worry about — the daemon's in-flight set admits one worker per
// session, and the cross-process store lock refuses a second process besides.
// Adding workers does not weaken any of this; it only makes it worth writing
// down.
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

// resumeDecision is the tool-aware cold-vs-resume decision (US-3, AC-3.4):
// this run continues the previous run's conversation only when all three
// hold —
//
//   - found: a prior terminal run exists for this segment ([previousRun]);
//   - segment.Agent equals resolvedKind: this run addresses the same
//     coding-agent tool the conversation already belongs to;
//   - segment.ResumeToken is non-empty: the tool has actually reported a
//     conversation handle to resume, which only a real run leaves behind.
//
// Any one of them failing means a cold start. The agent check is read
// directly off segment rather than trusted as an invariant [prepare]'s
// switch-refusal check already established earlier in the same function —
// a session whose segment does not match resolvedKind is refused before this
// is ever called today, but this function answers correctly on its own,
// which is what makes it independently testable (T-3.3) rather than a claim
// about call order.
func resumeDecision(found bool, segment store.Segment, resolvedKind agent.Kind) bool {
	if !found || segment.Agent != string(resolvedKind) {
		return false
	}
	// Claude's own resumable handle is SessionRef, chosen before the first
	// process starts. Codex discovers a separate handle from thread.started.
	if resolvedKind == agent.Kind(store.AgentClaude) {
		return true
	}
	return segment.ResumeToken != ""
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
// It uses the previous mode and instruction through the same policy function
// as the current request, including Plan defaults and explicit revocation.
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
	return questionPermission(previous.Mode, ParseCommand(body))
}

// ingestAttachments fetches the attachments referenced by the run's trigger
// and issue body, and reports what landed.
//
// # It never fails the turn
//
// There is no error return, and that is the design rather than a shortcut. An
// attachment is extra context, not a precondition: a run whose screenshot
// would not download is a run that proceeds without the screenshot, exactly
// as every run before this feature existed did. Turning a flaky CDN into a
// failed turn would trade a small loss of context for a total one. Individual
// refusals become [attach.Rejection] values, which US-7's manifest will
// render; a failure of the whole operation is logged here and the turn
// carries on with nothing.
//
// # Two gates, both required
//
// [Request.AttachmentsAllowed] is authority — the resolved tool profile said
// this run may ingest (US-5). A non-nil [Options.AssetFetcher] is capability.
// Neither implies the other, and a turn missing either fetches nothing. The
// check is here rather than inside internal/attach because the permission is
// a property of this turn, not of ingestion.
//
// # Why the trigger is scanned first
//
// The comment that started the run is scanned before the issue body, so when
// a turn's ceilings can only admit some of the attachments, the ones the
// reporter just attached win over ones that have been sitting on the issue
// since it was filed. [attach.Scan] collapses duplicates by URL, so an image
// referenced in both places is fetched once and counted once — including the
// case where the trigger *is* the issue body, which scans the same text
// twice by construction.
func ingestAttachments(
	ctx context.Context,
	opts Options,
	req Request,
	ref naming.Ref,
	ordinal int,
	logger *slog.Logger,
) attach.Result {
	if !req.AttachmentsAllowed || opts.AssetFetcher == nil {
		return attach.Result{}
	}
	refs := attach.Scan(req.Trigger.Body, req.Issue.Body)
	if len(refs) == 0 {
		return attach.Result{}
	}

	dir, err := naming.AttachmentsDir(opts.Paths.Home(), ref, ordinal)
	if err != nil {
		logger.WarnContext(ctx, "attachments were skipped: the landing directory could not be derived",
			slog.String("error", err.Error()))
		return attach.Result{}
	}
	result, err := attach.Ingest(ctx, opts.AssetFetcher, dir, refs, opts.AttachmentLimits)
	if err != nil {
		logger.WarnContext(ctx, "attachments were skipped", slog.String("error", err.Error()))
		return attach.Result{}
	}
	logger.InfoContext(ctx, "attachments ingested",
		slog.Int("accepted", len(result.Accepted)),
		slog.Int("rejected", len(result.Rejected)),
	)
	return result
}

// aiteamDirName is the team's directory at the worktree root, where the
// attachment manifest is written. internal/repos keeps its own unexported
// copy for the registration check; this is the one this package writes into,
// and the two are deliberately independent — a change to what makes a
// repository count as aiteam-installed should not silently relocate a run's
// artefacts.
const aiteamDirName = ".aiteam"

// writeAttachmentManifest writes attachments.md into the worktree's .aiteam
// directory (US-7).
//
// # Why the worktree, and not $LF_HOME
//
// The landed bytes go to $LF_HOME, deliberately, so an attachment never
// shows up in the repository's own git status. The manifest goes the other
// way for the opposite reason: it is meant to be committed. litefactory
// commits the worktree at the end of every run, so putting the record there
// makes its history git's history — which is the entire durability story
// this story is allowed, since the spec's Out of Scope list rules out a
// table and a migration for it (AC-7.4).
//
// It is rewritten per run rather than appended to. A run's manifest
// describes that run's attachments, and the previous run's version is one
// git log away; appending would grow a file nobody prunes and make "what did
// this run see" a search rather than a read.
//
// # Not a reply
//
// Nothing here passes through internal/redact, and that is correct rather
// than an omission (AC-7.3): the redactor guards text on its way to a forge
// comment, and this is a worktree artefact. What keeps it safe is how the
// records were built — internal/attach strips the query string from every
// URL at construction, so the signed credential never reaches a Record in
// the first place, and no record ever carries attachment bytes.
//
// # It never fails the turn
//
// Like ingestion itself, a manifest that could not be written is logged and
// stepped over. The attachments are already fetched and already named in the
// prompt; failing the run at this point would throw away work that
// succeeded in order to report that the paperwork did not.
func writeAttachmentManifest(
	ctx context.Context,
	worktreePath string,
	ref naming.Ref,
	ordinal int,
	result attach.Result,
	logger *slog.Logger,
) {
	body := attach.Manifest(ref.String(), ordinal, result.Records)
	if body == "" {
		return
	}
	dir := filepath.Join(worktreePath, aiteamDirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		logger.WarnContext(ctx, "the attachment manifest was not written",
			slog.String("error", err.Error()))
		return
	}
	path := filepath.Join(dir, attach.ManifestFile)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		logger.WarnContext(ctx, "the attachment manifest was not written",
			slog.String("path", path), slog.String("error", err.Error()))
		return
	}
	logger.InfoContext(ctx, "attachment manifest written",
		slog.String("path", path), slog.Int("records", len(result.Records)))
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
		SessionID: session.ID,
		SegmentID: segment.ID,
		Mode:      req.mode(),
		// The segment's own value, never req.Agent recomputed: by the time
		// this is called, [prepare] has already confirmed the two agree —
		// segment.Agent is what the switch-refusal check compared resolvedKind
		// against — and the segment is the record a resume reads back later
		// (US-3).
		Agent:            segment.Agent,
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
