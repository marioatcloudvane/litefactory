package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// handleJudge is reached only after authenticated owner admission and before
// Plan child routing or mode selection. The worker already owns bounded capacity.
func (d *Daemon) handleJudge(ctx context.Context, t *task, c github.Comment, qualified bool) error {
	publisher, ok := d.reporter.(interface {
		ReportJudge(context.Context, naming.Ref, int64, int64, string) (turn.Posted, error)
	})
	if !ok {
		return errors.New("judge publication requires the durable report journal")
	}
	if pending, err := d.resumeReport(ctx, t, c.ID); pending || err != nil {
		return err
	}
	options, parseErr := turn.ParseJudgeOptions(turn.ParseCommand(c.Body).Text)
	var text string
	switch {
	case parseErr != nil:
		text = "Unable to assess saved work: " + parseErr.Error() + "\nUsage: /judge [focus text] [--target <saved-reference>] [--agent claude|codex|other] [--model <model>] [--thinking <level>]"
	case d.judge == nil:
		text = "Unable to assess saved work: the judge service is unavailable."
	case !qualified:
		text = "Unable to assess saved work: a verified provider, origin, account and ticket identity is required."
	default:
		binding, err := d.db.GetSessionTicket(ctx, t.session.ID)
		if err != nil {
			return err
		}
		ref, err := binding.Ref()
		if err != nil {
			return err
		}
		key, _ := json.Marshal(ref.Identity)
		remoteCommentID := strconv.FormatInt(c.ID, 10)
		if c.QualifiedReply != nil {
			remoteCommentID = c.QualifiedReply.Identity.RemoteID
		}
		comments, err := d.readComments(ctx, t.forgeRepo, t.issue.Number, 0)
		if err != nil {
			return err
		}
		captured := make([]turn.JudgeComment, 0, len(comments))
		for _, comment := range comments {
			if (!c.CreatedAt.IsZero() && !comment.CreatedAt.After(c.CreatedAt)) || (c.CreatedAt.IsZero() && comment.ID <= c.ID) {
				captured = append(captured, turn.JudgeComment{ID: strconv.FormatInt(comment.ID, 10), URL: comment.HTMLURL, Body: comment.Body})
			}
		}
		root := t.session.WorktreePath
		if root == "" {
			root = t.repo.Path
		}
		provider := config.Kind(d.defaultAgent)
		segments, segmentErr := d.db.ListSegments(ctx, t.session.ID)
		if segmentErr != nil {
			return segmentErr
		}
		if len(segments) > 0 {
			provider = config.Kind(segments[len(segments)-1].Agent)
		}
		text, err = d.judge.Execute(ctx, turn.JudgeRequest{Ref: t.ref, SessionID: t.session.ID, CommentID: c.ID, TicketKey: string(key), RemoteCommentID: remoteCommentID, Root: root, TaskProvider: provider, Options: options, Comments: captured, Cutoff: c.CreatedAt})
		if err != nil {
			if !errors.Is(err, turn.ErrJudgeSettings) && !errors.Is(err, store.ErrJudgeSourceUnavailable) {
				return err
			}
			text = "Unable to assess saved work: " + err.Error()
		}
	}
	_, err := publisher.ReportJudge(ctx, t.ref, t.session.ID, c.ID, text)
	if err != nil {
		return fmt.Errorf("judge advisory delivery pending: %w", err)
	}
	return nil
}
