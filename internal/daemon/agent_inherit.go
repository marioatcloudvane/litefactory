package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// inheritParentAgent gives a story issue its parent's Coding Agent label when
// the story has none of its own, so a parent labelled Codex builds its stories
// with Codex. The label is written to the story issue, which keeps GitHub the
// record and lets the owner change it per story. A story with its own label, an
// issue that is not a story, or a parent without a label are left alone.
func (d *Daemon) inheritParentAgent(ctx context.Context, t *task) error {
	if hasAgentLabel(t.issue.Labels) {
		return nil
	}
	parent, ok, err := d.storyParent(ctx, t)
	if err != nil || !ok {
		return err
	}
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return fmt.Errorf("story parent: %w", err)
	}
	issue, err := d.readIssue(ctx, t.forgeRepo, ref.Issue)
	if err != nil {
		return fmt.Errorf("read the story's parent: %w", err)
	}
	choice := d.resolveAgent(issue.Labels)
	if choice.refused() || choice.Agent == "" {
		return nil
	}
	label := agentLabel(issue.Labels)
	labels, ok := d.ticketLabels()
	if !ok {
		return errors.New("story agent labels unavailable")
	}
	if err := labels.AddIssueLabel(ctx, t.forgeRepo, t.issue.Number, label); err != nil {
		return fmt.Errorf("give the story its parent's agent label: %w", err)
	}
	t.issue.Labels = append(t.issue.Labels, label)
	return nil
}

// storyParent names the parent task of a story issue: a graph Plan story or an
// Implement story worker.
func (d *Daemon) storyParent(ctx context.Context, t *task) (string, bool, error) {
	story, ok, err := d.db.GraphPlanStoryAt(ctx, t.repo.ID, t.issue.Number)
	if err != nil {
		return "", false, err
	}
	if ok {
		return story.Parent, true, nil
	}
	row, err := d.db.ImplementStoryByWorkerSession(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.Parent, true, nil
}

// hasAgentLabel reports whether any label carries the Coding Agent prefix in
// any casing; a malformed one is the story's own and is judged as such.
func hasAgentLabel(labels []string) bool {
	return agentLabel(labels) != ""
}

// agentLabel returns the first label carrying the Coding Agent prefix in any
// casing, as written.
func agentLabel(labels []string) string {
	for _, label := range labels {
		if len(label) >= len(agentLabelPrefix) && strings.EqualFold(label[:len(agentLabelPrefix)], agentLabelPrefix) {
			return label
		}
	}
	return ""
}
