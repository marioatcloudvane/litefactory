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

// admitWorkflow precedes mode selection so a bug-labelled task never enters
// classification, mode-label mutation or mode-switch cancellation.
func (d *Daemon) admitWorkflow(ctx context.Context, t *task, trigger turn.Trigger, command turn.Command) (agent.Workflow, agent.Mode, error) {
	// The Doctor runs beside the task's own workflow: it changes no mode
	// selection or label and never touches the task's admission.
	if t.doctoring {
		return agent.WorkflowDoctor, agent.ModeDoctor, nil
	}
	if doctor, err := d.doctorRoute(ctx, t, trigger, command); err != nil || doctor {
		return agent.WorkflowDoctor, agent.ModeDoctor, err
	}
	a, err := d.db.GetTaskWorkflowAdmission(ctx, t.session.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", "", err
	}
	if err == nil && a.Family == "standalone" {
		if t.continuation != nil && t.continuation.Context.Mode != string(agent.ModeBug) {
			return "", "", fmt.Errorf("bug continuation workflow binding mismatch")
		}
		if _, explicit := command.Mode(); explicit {
			return agent.WorkflowBug, agent.ModeBug, &modeRefusal{kind: report.ReplyBugModeSwitch}
		}
		return agent.WorkflowBug, agent.ModeBug, nil
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
				return agent.WorkflowBug, agent.ModeBug, nil
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
		return agent.WorkflowBug, agent.ModeBug, nil
	}
	return workflow, mode, nil
}
