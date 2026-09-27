package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// AdmitMCPOwnerCommand is the offline-testable trusted intake seam. The factory
// passes a comment retrieved for this source issue and the selected mode; no
// agent tool reaches this method. Production comment intake delegates to the same authorizer.
// Authorizer construction fixes configured owner/bot IDs independently of the
// public comment body, so copied commands cannot mint someone else's approval.
func AdmitMCPOwnerCommand(ctx context.Context, authorizer *turn.CreationAuthorizer, repo github.Repo, issue int, mode string, comment github.Comment) (int64, error) {
	if authorizer == nil {
		return 0, errors.New("creation owner intake unavailable")
	}
	source := repo.Ref(issue)
	if err := source.Validate(); err != nil {
		return 0, err
	}
	return authorizer.AdmitOwnerGrantIntent(ctx, turn.OwnerGrantControl{Repository: repo.String(), SourceTask: source.String(), Mode: mode, Body: comment.Body, CommentID: comment.ID, ActorID: comment.Author.ID, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt, Bot: comment.Author.IsBot})
}

// handleMCPCreation records exact owner approval and publishes it, in any mode,
// without launching a run or changing mode.
func (d *Daemon) handleMCPCreation(ctx context.Context, t *task, c github.Comment, log *slog.Logger) error {
	mode, _, err := resolveModeLabel(t.issue.Labels)
	if err != nil {
		return d.rejectMCPCreation(ctx, t, c, log, "mode")
	}
	e, ok := d.turn.(turnExecutor)
	if !ok || e.opts.MCP == nil || e.opts.MCP.CreationAuthorizer == nil {
		return d.rejectMCPCreation(ctx, t, c, log, "disabled")
	}
	control := turn.OwnerGrantControl{Repository: t.forgeRepo.String(), SourceTask: t.ref.String(), Mode: string(mode), Body: c.Body, CommentID: c.ID, ActorID: c.Author.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Bot: c.Author.IsBot}
	if strings.HasPrefix(c.Body, "/mcp-create prepare ") {
		prepared, err := e.opts.MCP.PrepareCreationProposal(ctx, control)
		if err != nil {
			return d.creationIntakeError(ctx, t, c, log, err)
		}
		data, err := json.Marshal(prepared.Envelope)
		if err != nil {
			return err
		}
		_, err = d.reportReply(ctx, turn.ReplyRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: c.ID, Creation: &report.CreationReply{Envelope: string(data), Hash: prepared.Hash, Relationship: prepared.Envelope.Relationship, Publication: d.creationPublicationContext(ctx, t)}})
		return d.leave(ctx, t, log, "creation proposal presentation pending", err)
	}
	_, err = e.opts.MCP.AdmitCreationApproval(ctx, control)
	if err != nil {
		return d.creationIntakeError(ctx, t, c, log, err)
	}
	_, err = d.resumeCreationSuccess(ctx, t, c)
	return d.leave(ctx, t, log, "creation publication or its reply pending", err)
}

func (d *Daemon) creationIntakeError(ctx context.Context, t *task, c github.Comment, log *slog.Logger, err error) error {
	if reason, rejected := turn.CreationRejectionReason(err); rejected {
		return d.rejectMCPCreation(ctx, t, c, log, reason)
	}
	return d.leave(ctx, t, log, "creation intake remains pending", err)
}
func (d *Daemon) rejectMCPCreation(ctx context.Context, t *task, c github.Comment, log *slog.Logger, reason string) error {
	reporter, ok := d.reporter.(interface {
		ReportCreationRejection(context.Context, naming.Ref, int64, int64, string, string) (turn.Posted, error)
	})
	if !ok {
		return d.leave(ctx, t, log, "creation rejection reporting unavailable", errors.New("creation rejection reporter unavailable"))
	}
	kind := "approval"
	if strings.HasPrefix(c.Body, "/mcp-create prepare") {
		kind = "preparation"
	}
	source := store.ReportRecovery{Key: recoveryKey(t.session.ID, c.ID), SessionID: t.session.ID, TriggerID: c.ID, Task: t.ref.String()}
	_, err := d.reportAttempt(ctx, source, func() (turn.Posted, error) {
		return reporter.ReportCreationRejection(ctx, t.ref, t.session.ID, c.ID, kind, reason)
	})
	return d.leave(ctx, t, log, "creation rejection delivery pending", err)
}

// resumeCreationSuccess follows numeric identity filtering and pending delivery
// recovery, but precedes parsing and mutable mode/configuration checks.
func (d *Daemon) resumeCreationSuccess(ctx context.Context, t *task, c github.Comment) (bool, error) {
	source := store.CreationSource{Repository: t.forgeRepo.String(), SourceTask: t.ref.String(), CommentID: c.ID, OwnerID: c.Author.ID}
	if admitted, err := d.db.GetCreationAcknowledgment(ctx, source); err == nil {
		acceptance := &report.CreationAcceptance{Hash: admitted.Hash, Relationship: admitted.Relationship, CreatedAt: admitted.CreatedAt, ExpiresAt: admitted.ExpiresAt, Publication: d.creationPublicationContext(ctx, t)}
		// Publishing is idempotent per approval, so a retried comment resumes or
		// reports the same publication before its reply is (re)delivered.
		if e, ok := d.turn.(turnExecutor); ok && e.opts.MCP != nil && e.opts.MCP.Creation != nil {
			acceptance.Published, err = e.opts.MCP.PublishApprovedCreation(ctx, admitted.IntentID)
			if err != nil {
				return true, fmt.Errorf("publish approved creation proposal %s: %w", admitted.Hash, err)
			}
		}
		_, err = d.reportReply(ctx, turn.ReplyRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: admitted.Source.CommentID, CreationAcceptance: acceptance})
		return true, err
	} else if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	p, err := d.db.GetCreationPreparation(ctx, source)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = d.reportReply(ctx, turn.ReplyRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: c.ID, Creation: &report.CreationReply{Envelope: string(p.Envelope), Hash: p.Hash, Relationship: p.Relationship, Publication: d.creationPublicationContext(ctx, t)}})
	return true, err
}
