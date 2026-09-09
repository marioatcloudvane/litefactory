package turn

import (
	"context"
	"fmt"

	"github.com/cloudvane/litefactory/internal/store"
)

// validateContinuation runs under the existing task lock before preparation.
func validateContinuation(ctx context.Context, opts Options, req Request, session store.Session) error {
	if req.Continuation == nil {
		return nil
	}
	c, err := opts.DB.GetContinuation(ctx, req.Continuation.ID)
	if err != nil {
		return err
	}
	if !opts.AutoResumeOnSessionLimit || c.Status != "reserved" || c.TriggerID() != req.Trigger.CommentID || c.SessionID != session.ID || c.Context.Task != session.TaskRef || c.Context.Worktree != session.WorktreePath || c.Context.Agent != string(req.Agent) || c.Context.Mode != req.mode() || c.Context.Request != req.Command.Text || c.Context.ResumeIdentity == "" {
		return fmt.Errorf("saved continuation no longer matches current task")
	}
	segments, err := opts.DB.ListSegments(ctx, session.ID)
	if err != nil {
		return err
	}
	if len(segments) == 0 || segments[len(segments)-1].ID != c.SegmentID {
		return fmt.Errorf("conversation segment changed")
	}
	segment := segments[len(segments)-1]
	identity := segment.ResumeToken
	if segment.Agent == store.AgentClaude {
		identity = segment.SessionRef
	}
	if identity != c.Context.ResumeIdentity || segment.SessionRef != c.Context.SessionRef {
		return fmt.Errorf("saved resume identity is unavailable or changed")
	}
	if c.Context.ApprovedGeneration != 0 {
		feature, err := opts.DB.GetManagedFeature(ctx, session.TaskRef)
		if err != nil {
			return err
		}
		if feature.Generation != c.Context.ApprovedGeneration || feature.State != "implementing" {
			return fmt.Errorf("approved story generation changed")
		}
	}
	return nil
}
