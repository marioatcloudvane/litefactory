package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// ConfirmationRefusal enumerates why a confirmation could not proceed, when
// it could not (S-12, S-14). Every value ValidateConfirmation returns is one
// of these — there is no bool a future change could forget to check, and no
// path that reaches [ExecuteApplyResolution] without one of them having been
// [ConfirmationOK].
type ConfirmationRefusal int

const (
	// ConfirmationOK means every gate passed: a [confirmedProposal] was
	// constructed and the caller may proceed to [ExecuteApplyResolution].
	ConfirmationOK ConfirmationRefusal = iota
	// ConfirmationUnknownProposal means the id named no proposal on this
	// session at all — including a proposal that exists but belongs to a
	// different session, which is reported identically so a confirmation
	// can never be used to probe which ids exist elsewhere.
	ConfirmationUnknownProposal
	// ConfirmationNotOwner means the proposal exists on this session but
	// the commenter is not the issue owner (watch-out #4 of US-12): the
	// proposal's status is left exactly as it was.
	ConfirmationNotOwner
	// ConfirmationAlreadyApplied means this proposal was already applied by
	// an earlier confirmation (AC-12.9): a second confirmation is a no-op,
	// never a second commit.
	ConfirmationAlreadyApplied
	// ConfirmationSuperseded means a later proposal for this session has
	// already superseded this one, or a previous application attempt found
	// the branch or base had moved (AC-12.4).
	ConfirmationSuperseded
	// ConfirmationExpired means this proposal's TTL passed before it was
	// confirmed.
	ConfirmationExpired
	// ConfirmationUnsupportedFormat requires regeneration and fresh confirmation.
	ConfirmationUnsupportedFormat
)

// String renders the refusal for logs and comment rendering.
func (r ConfirmationRefusal) String() string {
	switch r {
	case ConfirmationOK:
		return "ok"
	case ConfirmationUnknownProposal:
		return "unknown_proposal"
	case ConfirmationNotOwner:
		return "not_owner"
	case ConfirmationAlreadyApplied:
		return "already_applied"
	case ConfirmationSuperseded:
		return "superseded"
	case ConfirmationUnsupportedFormat:
		return "unsupported_format_regenerate"
	case ConfirmationExpired:
		return "expired"
	default:
		return "invalid"
	}
}

// confirmedProposal is a [store.ConflictProposal] that has passed every gate
// a confirmation must pass (S-12): it is only ever constructed by
// [ValidateConfirmation], and [ExecuteApplyResolution] requires one as a
// parameter of [ApplyRequest]. There is no other way to obtain a usable
// value of this type from outside this package — every field is unexported
// and there is no exported constructor — which is what makes "did this
// confirmation pass every gate" a compile-time property of the call rather
// than a runtime check a future change could accidentally skip.
type confirmedProposal struct {
	proposal    store.ConflictProposal
	confirmedBy string
}

// ValidateConfirmation is the whole of the confirmation gate (S-12, S-14).
//
// It never touches git, the worktree, or the index — everything here is a
// database read plus one numeric comparison — so it can run, and refuse,
// long before any [Options.CloneGate] is ever acquired. The one check it
// deliberately does *not* make is whether the stored base/branch shas still
// match reality: that is S-15's TOCTOU guard, and it is re-verified fresh,
// inside the gate, immediately before [ExecuteApplyResolution] writes
// anything — never here, where the answer could go stale before a worktree
// is even opened.
//
// ownerUserID is this daemon's single configured owner identity — the same
// numeric id [Identities.OwnerUserID] carries and the same one
// internal/daemon's own "assigned to me" gate compares against
// (Daemon.runIssueBody) — and commenterUserID is the numeric id of whoever
// wrote the confirmation comment. The two are
// compared numerically and never by handle (D-I, SEC-8): a login is
// renameable, and the daemon's admission filter has, by construction,
// already refused every comment whose author is not this same owner id
// before a body is ever classified as a confirmation control at all, so an
// issue's own author — a stranger, an assignee, anyone but the configured
// owner — is deliberately not what this compares against. Either id being
// zero, or the two differing, refuses as [ConfirmationNotOwner] without
// moving the proposal's status at all (watch-out #4).
//
// commenterLogin carries no authorization weight at all: it is recorded
// verbatim as the confirming handle for [confirmedProposal.confirmedBy],
// which becomes this story's disclosure and commit trailer, and
// internal/report screens it again before ever echoing it into a comment
// (S-16, SEC-5) — this function trusts it for storage only.
//
// A proposal id that does not resolve at all, and one that resolves to a
// different session, are refused identically as [ConfirmationUnknownProposal]:
// a confirmation must never be usable to learn whether some other session
// has a proposal with a given id.
func ValidateConfirmation(ctx context.Context, db *store.DB, sessionID, proposalID int64, ownerUserID, commenterUserID int64, commenterLogin string) (confirmedProposal, ConfirmationRefusal, error) {
	proposal, err := db.GetConflictProposal(ctx, proposalID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return confirmedProposal{}, ConfirmationUnknownProposal, nil
		}
		return confirmedProposal{}, ConfirmationUnknownProposal, fmt.Errorf("read conflict proposal %d: %w", proposalID, err)
	}
	if proposal.SessionID != sessionID {
		return confirmedProposal{}, ConfirmationUnknownProposal, nil
	}
	if ownerUserID == 0 || commenterUserID == 0 || ownerUserID != commenterUserID {
		return confirmedProposal{}, ConfirmationNotOwner, nil
	}

	switch proposal.Status {
	case store.ConflictProposalPending:
		if proposal.Format != store.ConflictRegionFormat {
			return confirmedProposal{}, ConfirmationUnsupportedFormat, nil
		}
		return confirmedProposal{proposal: proposal, confirmedBy: commenterLogin}, ConfirmationOK, nil
	case store.ConflictProposalApplied:
		return confirmedProposal{}, ConfirmationAlreadyApplied, nil
	case store.ConflictProposalSuperseded:
		return confirmedProposal{}, ConfirmationSuperseded, nil
	case store.ConflictProposalExpired:
		return confirmedProposal{}, ConfirmationExpired, nil
	default:
		return confirmedProposal{}, ConfirmationUnknownProposal, nil
	}
}

// ApplyRequest is the input to [ExecuteApplyResolution]: which session, which
// confirming comment, and the [confirmedProposal] a successful
// [ValidateConfirmation] call produced. There is no field here a caller
// could fill in to skip validation — the only way to get a non-zero
// Proposal is to call ValidateConfirmation first.
type ApplyRequest struct {
	// SessionID is the task the confirmed proposal belongs to.
	SessionID int64
	// ConfirmedCommentID is the forge id of the comment that confirmed the
	// proposal, recorded on the proposal once applied (US-12).
	ConfirmedCommentID int64
	// Proposal is the confirmation [ValidateConfirmation] produced.
	Proposal confirmedProposal
}

func (r ApplyRequest) validate() error {
	switch {
	case r.SessionID <= 0:
		return invalidRequest("a session id is required")
	case r.ConfirmedCommentID <= 0:
		return invalidRequest("a confirming comment id is required")
	case r.Proposal.proposal.ID <= 0:
		return invalidRequest("a confirmed proposal is required")
	default:
		return nil
	}
}

// ApplyOutcome is what [ExecuteApplyResolution] actually did.
type ApplyOutcome int

const (
	// ApplyOutcomeInvalid is the zero value and never a real outcome.
	ApplyOutcomeInvalid ApplyOutcome = iota
	// ApplyOutcomeApplied means the confirmed files were written, staged,
	// the merge was completed, and the branch was pushed (AC-12.5).
	ApplyOutcomeApplied
	// ApplyOutcomeSuperseded means the branch or base tip re-verified
	// inside the gate no longer matched what the proposal was computed
	// against (AC-12.4): nothing was written, and the proposal is now
	// [store.ConflictProposalSuperseded].
	ApplyOutcomeSuperseded
	// ApplyOutcomeFailed means the application could not be completed —
	// prepareClone's own steps failed, or a git mutator failed and rolled
	// back (AC-12.6). The proposal's status is left exactly where
	// [ValidateConfirmation] found it: pending, so a fresh confirmation
	// comment may retry in its own, later, independently terminal run
	// (AC-12.10) — this run never retries itself.
	ApplyOutcomeFailed
)

// String renders the outcome for logs and comment rendering.
func (o ApplyOutcome) String() string {
	switch o {
	case ApplyOutcomeApplied:
		return "applied"
	case ApplyOutcomeSuperseded:
		return "superseded"
	case ApplyOutcomeFailed:
		return "failed"
	default:
		return "invalid"
	}
}

// ApplyResult is what [ExecuteApplyResolution] reached.
type ApplyResult struct {
	// Outcome says what happened.
	Outcome ApplyOutcome
	// Proposal is the proposal as it stands once this call returns: applied
	// with its commit sha recorded, superseded, or — on a failure —
	// unchanged from what [ValidateConfirmation] read.
	Proposal store.ConflictProposal
	// CommitSHA is the merge commit produced, non-empty only when Outcome
	// is [ApplyOutcomeApplied].
	CommitSHA string
	// Reason is a human-readable account of why Outcome is
	// [ApplyOutcomeFailed]. Empty otherwise.
	Reason string
}

// ExecuteApplyResolution is US-12's agentless apply turn: given a
// [confirmedProposal] — obtainable only from [ValidateConfirmation] — it
// reuses [prepareClone]'s own gate/fetch/freeze/ensureWorktree sequence
// (unchanged, and with no second [Options.CloneGate] acquisition), re-
// verifies the proposal's stored base/branch shas fresh inside that one
// gate acquisition (S-15), and — only when they still match — writes
// exactly the confirmed files, stages them, completes the merge, and
// pushes.
//
// It never runs [syncBase]: a confirmed resolution already knows exactly
// what it is merging and has its own re-verification, so a second,
// independent auto-sync attempt over the same branch would be redundant at
// best and a second, differently-decided merge at worst.
//
// A turn that could not be completed — prepareClone's own steps failing, or
// a git mutator failing and rolling back — is not a Go error: it is
// [ApplyOutcomeFailed], because every one of those owes the thread a
// disclosure (the same reasoning [Execute] documents for its own [Result]).
// A non-nil error means the call could not even be attempted: invalid
// options, an invalid request, or a database that would not answer.
func ExecuteApplyResolution(ctx context.Context, opts Options, req ApplyRequest) (ApplyResult, error) {
	if err := opts.validate(); err != nil {
		return ApplyResult{}, err
	}
	if err := req.validate(); err != nil {
		return ApplyResult{}, err
	}
	stored := req.Proposal.proposal

	logger := opts.Logger.With(
		slog.Int64("session_id", req.SessionID),
		slog.Int64("proposal_id", stored.ID),
		slog.String("instance_id", opts.InstanceID))

	session, err := opts.DB.GetSession(ctx, req.SessionID)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("read task %d: %w", req.SessionID, err)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("task ref %q on session %d: %w", session.TaskRef, session.ID, err)
	}
	logger = logger.With(slog.String("task", ref.String()))

	if session.FeatureSlug == "" || session.Branch == "" {
		// A conflict proposal only ever comes from a run that already froze
		// a branch (US-11 runs inside syncBase, which prepareClone reaches
		// only after freezeSlug). Reaching here with no frozen branch means
		// the session's state and the proposal's own existence disagree, so
		// this refuses rather than guessing at a slug from nothing.
		return ApplyResult{}, fmt.Errorf("task %s has no frozen branch to apply a confirmed resolution on", ref)
	}

	repo, err := opts.DB.GetRepo(ctx, session.RepoID)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("read the repository of task %s: %w", ref, err)
	}
	clone, err := opts.Git.Open(ctx, repo.Path)
	if err != nil {
		return applyFailed(stored, fmt.Errorf("open the registered clone at %s: %w", repo.Path, err)), nil
	}
	author, err := resolveAuthor(ctx, opts, clone)
	if err != nil {
		return applyFailed(stored, err), nil
	}

	// prepareClone is reused for its gate/fetch/freezeSlug/ensureWorktree
	// sequence and, because req.Proposal is non-nil here, also performs
	// this story's own confirmed-application step as the fourth and last
	// part of that same critical section (S-15) — [syncBase] itself is
	// skipped for this turn shape (CR-B-003) — see prepareClone's own doc
	// comment for why that step is written inline there rather than through
	// a helper this file could call directly.
	minimalReq := Request{SessionID: req.SessionID, Issue: Issue{Number: session.IssueNumber}}
	_, _, _, _, applyResult, err := prepareClone(ctx, opts, minimalReq, clone, session, repo, author, logger, &req.Proposal)
	if err != nil {
		return applyFailed(stored, err), nil
	}
	if applyResult == nil {
		// Unreachable in practice: req.Proposal is always non-nil here
		// because ApplyRequest.validate rejects a zero Proposal, and
		// prepareClone only returns a nil *confirmedApplyResult alongside a
		// nil error when confirmed itself was nil.
		return ApplyResult{}, fmt.Errorf("apply confirmed resolution: prepareClone reported no outcome")
	}

	if applyResult.Superseded {
		superseded, err := opts.DB.SupersedeConflictProposal(ctx, stored.ID, time.Now().UTC())
		if err != nil {
			return ApplyResult{}, fmt.Errorf("supersede stale conflict proposal %d: %w", stored.ID, err)
		}
		return ApplyResult{Outcome: ApplyOutcomeSuperseded, Proposal: superseded}, nil
	}

	appliedAt := time.Now().UTC()
	applied, err := opts.DB.MarkConflictProposalApplied(ctx, stored.ID, req.Proposal.confirmedBy, req.ConfirmedCommentID, applyResult.CommitSHA, appliedAt)
	if err != nil {
		// The commit is real, already pushed, and cannot be un-pushed: only
		// the bookkeeping failed. This is still reported as applied — the
		// disclosure is about what actually happened to the repository —
		// with the failure logged for an operator to notice; a proposal
		// stuck at "pending" despite a real commit is a data-quality
		// problem to fix by hand, not a reason to tell the owner nothing
		// happened when something did.
		logger.Error("conflict proposal applied but could not be marked applied in the store", slog.Any("error", err))
		applied = stored
		applied.Status = store.ConflictProposalApplied
		applied.ConfirmedBy = req.Proposal.confirmedBy
		applied.ConfirmedCommentID = req.ConfirmedCommentID
		applied.AppliedCommitSHA = applyResult.CommitSHA
		applied.AppliedAt = appliedAt
	}

	logger.Info("confirmed conflict-resolution proposal applied", slog.String("commit", applyResult.CommitSHA))
	return ApplyResult{Outcome: ApplyOutcomeApplied, Proposal: applied, CommitSHA: applyResult.CommitSHA}, nil
}

// applyFailed builds the [ApplyOutcomeFailed] result every failing step
// above returns: the proposal is reported back exactly as
// [ValidateConfirmation] found it — still pending — because nothing here
// ever changes a proposal's status on a failure path (AC-12.6, AC-12.10:
// this run does not retry itself, and a fresh confirmation is what a later,
// independently terminal run would be).
func applyFailed(proposal store.ConflictProposal, err error) ApplyResult {
	if errors.Is(err, git.ErrWorktreeUncommittedChanges) {
		// Confirmation must not overwrite unfinished work. An ordinary run can
		// now defer synchronization and let the owner recover it (AC-171-5).
		return ApplyResult{
			Outcome: ApplyOutcomeFailed, Proposal: proposal,
			Reason: "The confirmed resolution was not applied because the worktree has uncommitted changes. " +
				"Your work is preserved and the proposal remains pending. " +
				"Post an ordinary owner comment asking the agent to finish the retained work; an eligible recovery run can proceed with base synchronization deferred. " +
				"After recovery is saved, request another normal run to synchronize the clean branch. " +
				"If the branch or base revision changes, this confirmation becomes stale; any new conflict proposal requires a fresh confirmation.",
		}
	}
	return ApplyResult{Outcome: ApplyOutcomeFailed, Proposal: proposal, Reason: err.Error()}
}
