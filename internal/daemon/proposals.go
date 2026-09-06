package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/schedule"
	"github.com/cloudvane/litefactory/internal/store"
)

const (
	proposalLifetime = 7 * 24 * time.Hour
	maxProposalItems = 16
)

func (d *Daemon) handleProposalControl(ctx context.Context, t *task, c github.Comment, request schedule.Request, log *slog.Logger) error {
	if _, err := d.db.ExpireScheduleProposals(ctx, t.tickAt.Add(-proposalLifetime), t.tickAt); err != nil {
		return d.leave(ctx, t, log, "could not expire schedule proposals", err)
	}
	switch request.Kind {
	case schedule.KindPropose:
		return d.proposeSchedule(ctx, t, c, request, log)
	case schedule.KindAccept:
		return d.acceptScheduleProposal(ctx, t, c, request.ProposalID, log)
	case schedule.KindReject:
		return d.rejectScheduleProposal(ctx, t, c, request.ProposalID, log)
	default:
		return d.replySchedule(ctx, t, c, report.ReplyScheduleInvalid, report.ScheduleReply{}, log)
	}
}

func (d *Daemon) proposeSchedule(ctx context.Context, t *task, c github.Comment, request schedule.Request, log *slog.Logger) error {
	if d.proposals == nil {
		return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalFailed, report.ScheduleReply{}, log)
	}
	generated, err := d.proposals.ProposeSchedule(ctx, ScheduleProposalRequest{Description: request.Payload, ReceivedAt: t.tickAt.UTC()})
	if err != nil {
		log.WarnContext(ctx, "schedule proposal generator failed", slog.Any("error", err))
		return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalFailed, report.ScheduleReply{}, log)
	}
	items, err := validatedProposalItems(generated)
	if err != nil {
		log.WarnContext(ctx, "schedule proposal generator returned invalid data", slog.Any("error", err))
		return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalFailed, report.ScheduleReply{}, log)
	}
	proposal, _, err := d.db.CreateOrGetScheduleProposal(ctx, store.CreateScheduleProposalParams{
		SessionID: t.session.ID, SourceCommentID: c.ID, OwnerHandle: c.Author.Login,
		Description: request.Payload, Items: items,
	})
	if err != nil {
		return d.leave(ctx, t, log, "could not save the schedule proposal", err)
	}
	return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalReady,
		report.ScheduleReply{ProposalID: proposal.ID, Count: len(proposal.Items)}, log)
}

func (d *Daemon) acceptScheduleProposal(ctx context.Context, t *task, c github.Comment, id int64, log *slog.Logger) error {
	proposal, err := d.ownedProposal(ctx, t, c, id)
	if err != nil || (proposal.Status != store.ProposalPending && proposal.Status != store.ProposalAccepted) {
		return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalFailed, report.ScheduleReply{}, log)
	}
	schedules, _, err := d.db.AcceptScheduleProposal(ctx, id, t.tickAt)
	if err != nil {
		return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalFailed, report.ScheduleReply{}, log)
	}
	outstanding, err := d.db.CountPendingSchedules(ctx)
	if err != nil {
		return d.leave(ctx, t, log, "could not count scheduled work", err)
	}
	count := len(schedules)
	return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalAccepted,
		report.ScheduleReply{ProposalID: id, Count: count, Outstanding: outstanding}, log)
}

func (d *Daemon) rejectScheduleProposal(ctx context.Context, t *task, c github.Comment, id int64, log *slog.Logger) error {
	proposal, err := d.ownedProposal(ctx, t, c, id)
	if err != nil || (proposal.Status != store.ProposalPending && proposal.Status != store.ProposalRejected) {
		return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalFailed, report.ScheduleReply{}, log)
	}
	if _, _, err := d.db.RejectScheduleProposal(ctx, id, t.tickAt); err != nil {
		return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalFailed, report.ScheduleReply{}, log)
	}
	return d.replySchedule(ctx, t, c, report.ReplyScheduleProposalRejected, report.ScheduleReply{ProposalID: id}, log)
}

func (d *Daemon) ownedProposal(ctx context.Context, t *task, c github.Comment, id int64) (store.ScheduleProposal, error) {
	proposal, err := d.db.GetScheduleProposal(ctx, id)
	if err != nil {
		return store.ScheduleProposal{}, err
	}
	if proposal.SessionID != t.session.ID || proposal.OwnerHandle != c.Author.Login {
		return store.ScheduleProposal{}, errors.New("schedule proposal is not owned by this comment's issue and owner")
	}
	return proposal, nil
}

func validatedProposalItems(generated ScheduleProposal) ([]store.ScheduleProposalItem, error) {
	if len(generated.Items) == 0 || len(generated.Items) > maxProposalItems {
		return nil, fmt.Errorf("proposal has %d items; expected 1..%d", len(generated.Items), maxProposalItems)
	}
	items := make([]store.ScheduleProposalItem, 0, len(generated.Items))
	for _, item := range generated.Items {
		payload, err := schedule.ValidatePayload(item.Payload)
		if err != nil || item.DueAt.IsZero() {
			return nil, errors.New("proposal item does not satisfy factory schedule rules")
		}
		items = append(items, store.ScheduleProposalItem{Payload: payload, DueAt: item.DueAt.UTC()})
	}
	return items, nil
}
