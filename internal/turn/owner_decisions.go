package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/commandtext"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResolveValidateDecision resolves shorthand once and retains the exact target
// separately from the immutable admitted owner text. Authentication belongs to
// the caller, before this method is invoked.
func ResolveValidateDecision(ctx context.Context, opts Options, req ActivityRequest, st ValidateState, wait store.GraphOwnerWait) (ValidateCommand, store.OwnerDecisionBinding, error) {
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ValidateCommand{}, store.OwnerDecisionBinding{}, err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return ValidateCommand{}, store.OwnerDecisionBinding{}, err
	}
	if saved, e := opts.DB.GetOwnerDecisionBinding(ctx, session.TaskRef, wait.ReplyID); e == nil {
		if saved.Body != wait.ReplyBody || saved.AuthorID != wait.ReplyAuthorID {
			return ValidateCommand{}, saved, errors.New("the admitted owner decision changed on replay")
		}
		c, e := ParseValidateCommand(saved.Normalized)
		return c, saved, e
	} else if !errors.Is(e, store.ErrNotFound) {
		return ValidateCommand{}, saved, e
	}
	cmd, err := ParseValidateCommand(wait.ReplyBody)
	if err != nil {
		return cmd, store.OwnerDecisionBinding{}, err
	}
	if cmd.Revision != "" {
		if cmd.Revision != st.Revision {
			return cmd, store.OwnerDecisionBinding{}, nil
		}
		// Explicit revisions remain compatible with retained legacy reports.
		d, e := opts.DB.ExplicitOwnerDecisionDisplay(ctx, session.TaskRef, cmd.Name, cmd.Revision)
		if errors.Is(e, store.ErrNotFound) {
			return cmd, store.OwnerDecisionBinding{}, nil
		}
		if e != nil {
			return cmd, store.OwnerDecisionBinding{}, e
		}
		if d.InstanceID != req.InstanceID || d.Revision != cmd.Revision {
			return cmd, store.OwnerDecisionBinding{}, errors.New("the explicit report revision is stale")
		}
		b := store.OwnerDecisionBinding{TaskRef: session.TaskRef, CommentID: wait.ReplyID, AuthorID: wait.ReplyAuthorID, Body: wait.ReplyBody, Normalized: wait.ReplyBody, Display: d, CreatedAt: wait.ClaimedAt}
		e = opts.DB.PutOwnerDecisionBinding(ctx, b)
		return cmd, b, e
	}
	d, err := opts.DB.ResolveOwnerDecisionDisplay(ctx, session.TaskRef, cmd.Name, cmd.Reference, 0)
	if err != nil {
		return cmd, store.OwnerDecisionBinding{}, err
	}
	if d.InstanceID != req.InstanceID || d.Revision != st.Revision {
		return cmd, store.OwnerDecisionBinding{}, errors.New("that displayed decision is stale; use the latest report's short reference")
	}
	head, tail := firstToken(strings.TrimSpace(commandtext.Normalize(wait.ReplyBody)))
	if cmd.Reference != "" {
		tail = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tail), cmd.Reference))
	}
	normalized := head + " " + d.Revision
	if strings.TrimSpace(tail) != "" {
		normalized += " " + strings.TrimSpace(tail)
	}
	b := store.OwnerDecisionBinding{TaskRef: session.TaskRef, CommentID: wait.ReplyID, AuthorID: wait.ReplyAuthorID, Body: wait.ReplyBody, Normalized: normalized, Display: d, CreatedAt: wait.ClaimedAt}
	if err = opts.DB.PutOwnerDecisionBinding(ctx, b); err != nil {
		return cmd, b, err
	}
	cmd, err = ParseValidateCommand(normalized)
	return cmd, b, err
}

func validateDecisionTarget(st ValidateState) string {
	return fmt.Sprintf("validate:%d:%s:%s", st.InstanceID, st.Commit, st.DeliveryCommit)
}

// ResolvePlanDecision retains a shorthand approval's exact displayed command.
// Existing full commands continue through the established Plan checks.
func ResolvePlanDecision(ctx context.Context, db *store.DB, task string, comment, author int64, body string, now time.Time) (string, error) {
	return ResolvePlanStoryDecision(ctx, db, task, comment, author, body, now, "")
}

// ResolvePlanStoryDecision scopes child shorthand while preserving exact refs.
func ResolvePlanStoryDecision(ctx context.Context, db *store.DB, task string, comment, author int64, body string, now time.Time, key string) (string, error) {
	fields := strings.Fields(commandtext.Normalize(body))
	if len(fields) == 0 || (fields[0] != "/approve-plan" && fields[0] != "/approve-plan-set") {
		return body, nil
	}
	// Only the two new short forms belong to this resolver. Preserve all
	// existing explicit commands and mixed feedback for their original policy.
	if len(fields) != 1 && (len(fields) != 2 || !strings.HasPrefix(fields[1], "d-")) {
		return body, nil
	}
	if b, e := db.GetOwnerDecisionBinding(ctx, task, comment); e == nil {
		if b.Body != body || b.AuthorID != author {
			return body, errors.New("the admitted Plan decision changed on replay")
		}
		return b.Normalized, nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return body, e
	}
	reference := ""
	if len(fields) == 2 {
		reference = fields[1]
	}
	var d store.OwnerDecisionDisplay
	var err error
	if key != "" && fields[0] == "/approve-plan" {
		d, err = db.ResolveOwnerStoryDecisionDisplay(ctx, task, fields[0], reference, 0, key)
	} else {
		d, err = db.ResolveOwnerDecisionDisplay(ctx, task, fields[0], reference, 0)
	}
	if err != nil {
		return body, err
	}
	if err = db.PutOwnerDecisionBinding(ctx, store.OwnerDecisionBinding{TaskRef: task, CommentID: comment, AuthorID: author, Body: body, Normalized: d.Target, Display: d, CreatedAt: now}); err != nil {
		return body, err
	}
	return d.Target, nil
}
