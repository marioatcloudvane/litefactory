package daemon

import (
	"context"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/confirm"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// handleConfirm dispatches a /confirm control (US-12), modeled on
// [Daemon.handleProposalControl]'s own lifecycle discipline: expire-first,
// an ownership check on both the session and the commenter's login, a
// status guard on every transition, and every refusal reported as a value
// with a reason rather than silence (T-12.5).
//
// Nothing here can bypass the confirmation gate. The only way this function
// obtains a usable confirmed proposal is a successful call to
// [turn.ValidateConfirmation]; every other path — a grammar failure, a
// database read failure, a refusal [turn.ValidateConfirmation] itself
// returned, an outcome other than [turn.ApplyOutcomeApplied] — answers the
// thread and writes nothing.
func (d *Daemon) handleConfirm(ctx context.Context, t *task, c github.Comment, command turn.Command, log *slog.Logger) error {
	if d.conflictProposalTTL > 0 {
		if _, err := d.db.ExpireConflictProposals(ctx, t.tickAt.Add(-d.conflictProposalTTL), t.tickAt); err != nil {
			return d.leave(ctx, t, log, "could not expire conflict-resolution proposals", err)
		}
	}

	request, err := confirm.Parse(command.Text)
	if err != nil {
		return d.replyConfirm(ctx, t, c, report.ReplyConfirmInvalid, report.ConfirmReply{}, log)
	}

	// Ownership is checked numerically against this daemon's one configured
	// owner identity, never against the issue's own author (D-I, SEC-8): see
	// [turn.ValidateConfirmation]'s own doc comment for why the two are not
	// the same question, and why comparing against the wrong one would
	// refuse the daemon's own owner confirming a proposal on an issue
	// somebody else opened — the ordinary case, not the rare one.
	confirmed, refusal, err := turn.ValidateConfirmation(ctx, d.db, t.session.ID, request.ProposalID,
		d.ticketIdentities(ctx).OwnerUserID, c.Author.ID, c.Author.Login)
	if err != nil {
		return d.leave(ctx, t, log, "could not validate a conflict-resolution confirmation", err)
	}
	if kind, refused := confirmationRefusalReply(refusal); refused {
		reason := ""
		if refusal == turn.ConfirmationUnsupportedFormat {
			reason = "Legacy or unknown proposal format; request regeneration and confirm the new proposal number."
		}
		return d.replyConfirm(ctx, t, c, kind, report.ConfirmReply{ProposalID: request.ProposalID, Reason: reason}, log)
	}

	if d.confirmer == nil {
		// A dependency-light daemon test that predates this feature's wiring
		// — production startup (setup.go) always supplies one. This is
		// reached only after ValidateConfirmation has already said
		// ConfirmationOK, and answers the thread with a failure rather than
		// treating a missing dependency as consent to write anything.
		log.ErrorContext(ctx, "no confirmation applier is wired; nothing was applied",
			slog.Int64("proposal_id", request.ProposalID))
		return d.replyConfirm(ctx, t, c, report.ReplyConfirmFailed, report.ConfirmReply{ProposalID: request.ProposalID}, log)
	}

	result, err := d.confirmer.ApplyConfirmedResolution(ctx, turn.ApplyRequest{
		SessionID: t.session.ID, ConfirmedCommentID: c.ID, Proposal: confirmed,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not apply a confirmed conflict-resolution proposal", err)
	}

	switch result.Outcome {
	case turn.ApplyOutcomeApplied:
		files := make([]report.ConflictProposalFile, 0, len(result.Proposal.Files))
		for _, f := range result.Proposal.Files {
			resolution := f.Resolution
			if result.Proposal.Format == store.ConflictRegionFormat {
				resolution = ""
				for _, region := range f.Details.Regions {
					resolution += region.Resolution
				}
			}
			files = append(files, report.ConflictProposalFile{Path: f.Path, Resolution: resolution})
		}
		return d.replyConfirm(ctx, t, c, report.ReplyAppliesResolution, report.ConfirmReply{
			ProposalID:  result.Proposal.ID,
			Model:       result.Proposal.Model,
			ConfirmedBy: result.Proposal.ConfirmedBy,
			Files:       files,
			CommitSHA:   result.CommitSHA,
		}, log)
	case turn.ApplyOutcomeSuperseded:
		return d.replyConfirm(ctx, t, c, report.ReplyConfirmSupersededProposal,
			report.ConfirmReply{ProposalID: result.Proposal.ID}, log)
	default:
		log.WarnContext(ctx, "could not apply a confirmed conflict-resolution proposal",
			slog.Int64("proposal_id", result.Proposal.ID), slog.String("reason", result.Reason))
		return d.replyConfirm(ctx, t, c, report.ReplyConfirmFailed,
			report.ConfirmReply{ProposalID: result.Proposal.ID, Reason: result.Reason}, log)
	}
}

// confirmationRefusalReply maps a [turn.ConfirmationRefusal] to the reply
// kind that discloses it. It reports ok=false for [turn.ConfirmationOK],
// which is not a refusal at all and never reaches this function's caller.
func confirmationRefusalReply(refusal turn.ConfirmationRefusal) (kind report.ReplyKind, ok bool) {
	switch refusal {
	case turn.ConfirmationUnknownProposal:
		return report.ReplyConfirmUnknownProposal, true
	case turn.ConfirmationNotOwner:
		return report.ReplyConfirmNotOwner, true
	case turn.ConfirmationAlreadyApplied:
		return report.ReplyConfirmAlreadyApplied, true
	case turn.ConfirmationSuperseded:
		return report.ReplyConfirmSupersededProposal, true
	case turn.ConfirmationUnsupportedFormat:
		return report.ReplyConfirmFailed, true
	case turn.ConfirmationExpired:
		return report.ReplyConfirmExpiredProposal, true
	default:
		return report.ReplyInvalid, false
	}
}

// replyConfirm posts and only then consumes a /confirm response, the same
// post-before-consume discipline [Daemon.replySchedule] already gives its
// own factory-owned control (CONC-4).
func (d *Daemon) replyConfirm(ctx context.Context, t *task, c github.Comment, kind report.ReplyKind, reply report.ConfirmReply, log *slog.Logger) error {
	posted, err := d.reportReply(ctx, turn.ReplyRequest{
		Ref: t.ref, SessionID: t.session.ID, CommentID: c.ID,
		ConfirmKind: kind, Confirm: reply,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not reply to /confirm", d.orphaned(ctx, log, posted, err))
	}
	if !posted.Skipped {
		log.InfoContext(ctx, "handled a conflict-resolution confirmation",
			slog.String("reply", kind.String()),
			slog.Int64("proposal_id", reply.ProposalID),
			slog.Int64("reply_comment_id", posted.CommentID))
	}
	return nil
}
