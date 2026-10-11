package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// admitWorkflow precedes mode selection so a retained standalone workflow can
// never enter classification, mode-label mutation or mode-switch cancellation.
func (d *Daemon) admitWorkflow(ctx context.Context, t *task, trigger turn.Trigger, command turn.Command) (agent.Workflow, agent.Mode, error) {
	a, err := d.db.GetTaskWorkflowAdmission(ctx, t.session.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", "", err
	}
	if err == nil && a.Family == "standalone" {
		if t.continuation != nil && (t.continuation.Context.Workflow != "bug" || t.continuation.Context.Mode != "") {
			return "", "", fmt.Errorf("bug continuation workflow binding mismatch")
		}
		if _, explicit := command.Mode(); explicit {
			return agent.WorkflowBug, "", &modeRefusal{kind: report.ReplyBugModeSwitch}
		}
		return agent.WorkflowBug, "", nil
	}
	if errors.Is(err, store.ErrNotFound) {
		issue, readErr := d.readIssue(ctx, t.forgeRepo, t.issue.Number)
		if readErr != nil {
			return "", "", fmt.Errorf("read workflow admission labels: %w", readErr)
		}
		t.issue.Labels = issue.Labels
		_, labelled, labelErr := resolveModeLabel(issue.Labels)
		if labelErr != nil {
			return "", "", labelErr
		}
		_, commanded := command.Mode()
		if !labelled && !commanded && !t.refining && !t.implementing && t.continuation == nil && slices.Contains(issue.Labels, "bug") {
			a, err = d.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: t.session.ID, Version: 1, Family: "standalone", Workflow: "bug", SourceTrigger: trigger.CommentID})
			if err != nil {
				return "", "", err
			}
			if a.Family == "standalone" {
				return agent.WorkflowBug, "", nil
			}
		}
	}
	mode, err := d.admitMode(ctx, t, trigger, command)
	if err != nil {
		return "", "", err
	}
	workflow, ok := agent.WorkflowForMode(mode)
	if !ok {
		return "", mode, nil
	} // Preserve legacy callers with no classifier.
	a, err = d.db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: t.session.ID, Version: 1, Family: "mode", Workflow: string(workflow), SourceTrigger: trigger.CommentID})
	if err != nil {
		return "", "", err
	}
	if a.Family == "standalone" {
		return agent.WorkflowBug, "", nil
	}
	return workflow, mode, nil
}
