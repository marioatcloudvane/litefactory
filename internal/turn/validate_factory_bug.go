package turn

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateFactoryBugFiler files one upstream factory Bug. It is the existing
// log-analysis filing path: the issue is labelled bug, and an issue updated
// since `since` that already carries marker is returned instead of a new one.
type ValidateFactoryBugFiler interface {
	FileFactoryBug(ctx context.Context, title, body redact.Body, marker string, since time.Time) (reference, url string, err error)
}

// ValidateFactoryBugActivity files every owner-confirmed factory Bug of the
// tested commit that is not filed yet. It runs only after the owner's own
// /validate-factory-bug command; the summary only proposes it. A failed
// filing is recorded with its reason and retried by repeating the command.
type ValidateFactoryBugActivity struct{ b ValidateFlowBindings }

// Execute files the pending intents.
func (h ValidateFactoryBugActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if _, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateFactoryBugAction); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	st, err := LoadValidateState(ctx, opts, h.b.Profile, req.InstanceID)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	task := ""
	if instance, e := opts.DB.GetGraphInstance(ctx, req.InstanceID); e == nil {
		if session, e := opts.DB.GetSession(ctx, instance.SessionID); e == nil {
			task = session.TaskRef
		}
	}
	now := h.b.now(opts)
	filed := map[string]string{}
	for _, row := range st.FactoryBugs {
		if row.State != store.FactoryBugIntent {
			continue
		}
		reference, url, fileErr := h.file(ctx, st, task, row)
		if ctx.Err() != nil {
			return ActivityResult{}, ctx.Err()
		}
		state, failure := store.FactoryBugFiled, ""
		if fileErr != nil {
			state, reference, url, failure = store.FactoryBugFailed, "", "", clipText(fileErr.Error(), 300)
		}
		if err := opts.DB.FinishValidateFactoryBug(context.WithoutCancel(ctx), row.ID, state, reference, url, failure, now); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		filed[row.Item] = state
	}
	return validateJSONResult("done", map[string]any{"factory_bugs": filed})
}

func (h ValidateFactoryBugActivity) file(ctx context.Context, st ValidateState, task string, row store.ValidateFactoryBug) (string, string, error) {
	if h.b.FactoryBugs == nil {
		return "", "", errors.New("no upstream factory repository is configured for filing; file it by hand from the report details")
	}
	if h.b.Delivery.Redactor == nil {
		return "", "", errors.New("factory Bug filing requires the redactor")
	}
	title, body := validateFactoryBugText(st, task, row)
	t, err := h.b.Delivery.Redactor.Apply(ctx, title, 256)
	if err != nil {
		return "", "", err
	}
	b, err := h.b.Delivery.Redactor.Apply(ctx, body, 60000)
	if err != nil {
		return "", "", err
	}
	return h.b.FactoryBugs.FileFactoryBug(ctx, t, b, row.Marker, row.CreatedAt)
}

// validateFactoryBugMarker is the stable marker line of one filing, so a
// repeated filing finds the issue it already created.
func validateFactoryBugMarker(instance int64, commit, item string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "validate-factory-bug\x00%d\x00%s\x00%s", instance, commit, item))
	return fmt.Sprintf("<!-- lf:validate-factory-bug v=1 key=%x -->", sum)
}

// validateFactoryBugText builds the upstream issue from retained facts and the
// reporter's stored diagnosis of the item.
func validateFactoryBugText(st ValidateState, task string, row store.ValidateFactoryBug) (string, string) {
	subject := row.Item
	var b strings.Builder
	fmt.Fprintf(&b, "Validate on %s, tested commit `%s`, found a failure the owner judged to be a factory or environment defect.\n\n", firstNonBlank(task, "a task"), st.Commit)
	if item, ok := st.reportItem(row.Item); ok {
		switch {
		case item.Failure != nil:
			subject = item.Failure.Command
			fmt.Fprintf(&b, "**Failing command:** `%s`\n\n", item.Failure.Command)
			if r := strings.TrimSpace(item.Failure.Reason); r != "" {
				fmt.Fprintf(&b, "**Reason:** %s\n\n", r)
			}
			if c := strings.TrimSpace(item.Failure.Cause); c != "" {
				fmt.Fprintf(&b, "**Diagnosed cause:** %s\n\n", c)
			}
			if tail := strings.TrimSpace(tailText(item.Failure.OutputTail, 4000)); tail != "" {
				fmt.Fprintf(&b, "**Output tail:**\n\n```text\n%s\n```\n\n", strings.ReplaceAll(tail, "```", "'''"))
			}
		case item.Finding != nil:
			subject = item.Finding.ID
			fmt.Fprintf(&b, "**Finding `%s`:** %s\n\n", item.Finding.ID, item.Finding.Summary)
		}
	}
	if s := st.Summary; s != nil {
		items := s.Items
		if s.Next != nil {
			items = append([]agentresult.ReportItem{*s.Next}, items...)
		}
		for _, it := range items {
			if it.Ref == row.Item {
				fmt.Fprintf(&b, "**Reporter diagnosis (%s):** %s\n\n", it.Diagnosis, it.Recommendation)
				break
			}
		}
		if s.Why != "" {
			fmt.Fprintf(&b, "**Reporter's why (%s):** %s\n\n", s.Certainty, s.Why)
		}
	}
	fmt.Fprintf(&b, "Filed by the owner's command on comment %d.\n\n%s\n", row.CommentID, row.Marker)
	return clipRunes("Validate: factory or environment defect in "+subject, 120), b.String()
}
