package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
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

// handleMCPCreation records exact owner approval without launching or changing mode.
func (d *Daemon) handleMCPCreation(ctx context.Context, t *task, c github.Comment, log *slog.Logger) error {
	mode, found, err := resolveModeLabel(t.issue.Labels)
	if err != nil {
		return d.leave(ctx, t, log, "creation mode unavailable", err)
	}
	if !found || string(mode) != "research" {
		return d.leave(ctx, t, log, "creation requires selected Research mode", fmt.Errorf("creation requires selected Research mode"))
	}
	e, ok := d.turn.(turnExecutor)
	if !ok || e.opts.MCP == nil {
		return d.leave(ctx, t, log, "creation owner intake unavailable", fmt.Errorf("creation owner intake unavailable"))
	}
	control := turn.OwnerGrantControl{Repository: t.forgeRepo.String(), SourceTask: t.ref.String(), Mode: string(mode), Body: c.Body, CommentID: c.ID, ActorID: c.Author.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Bot: c.Author.IsBot}
	if strings.HasPrefix(c.Body, "/mcp-create prepare ") {
		prepared, err := e.opts.MCP.PrepareCreationProposal(ctx, control)
		if err != nil {
			return d.leave(ctx, t, log, "creation preparation refused", err)
		}
		data, err := json.Marshal(prepared.Envelope)
		if err != nil {
			return err
		}
		_, err = d.reportReply(ctx, turn.ReplyRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: c.ID, Creation: &report.CreationReply{Envelope: string(data), Hash: prepared.Hash, Relationship: prepared.Envelope.Relationship}})
		return d.leave(ctx, t, log, "creation proposal presentation pending", err)
	}
	_, err = e.opts.MCP.AdmitCreationApproval(ctx, control)
	if err != nil {
		return d.leave(ctx, t, log, "creation approval remains unadmitted", err)
	}
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	return d.leave(ctx, t, log, "creation approval consumption pending", err)
}
