package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// Graph Plan owner notices. Each is a plain comment the factory writes when a
// Plan run stops somewhere the owner has to act or needs to know why nothing
// happened. They change no Plan state and no approval: they only read saved
// state and post. Each has a stable identity, so a repeat on any later tick
// resumes the one delivery and never posts a second comment.

// planStoryRow is one active story as the owner sees it on the parent.
type planStoryRow struct {
	Key, Name, Description string
	Issue                  int
	Dependencies           []string
	Approved               []string
	Command                string
}

// planTableCell keeps text inside one Markdown table cell.
func planTableCell(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "—"
	}
	return strings.ReplaceAll(text, "|", `\|`)
}

// planStoryRows lists the active stories in saved order with their sub-issues.
// complete is false when an active story has no sub-issue yet.
func (r *planGraphRuntime) planStoryRows(ctx context.Context, s planbrief.PlanState) (rows []planStoryRow, complete bool, err error) {
	entries := map[string]planbrief.PlanStorySetEntry{}
	if set := s.StorySet; set != nil {
		for _, e := range set.Entries {
			if key := set.Keys[e.ID]; key != "" {
				entries[key] = e
			}
		}
	}
	complete = true
	for _, item := range s.Stories {
		if !item.Active() {
			continue
		}
		row := planStoryRow{Key: item.Key, Name: item.Draft.Name, Description: item.Draft.Problem, Dependencies: item.Draft.Dependencies.StoryKeys, Command: s.PlanCurrentCommand(item.Key)}
		if e, ok := entries[item.Key]; ok {
			if row.Name == "" {
				row.Name = e.Name
			}
			if e.Intent != "" {
				row.Description = e.Intent
			}
		}
		for _, stage := range []string{"story", "technical", "acceptance"} {
			if ref, e := s.ReviewRevision(item.Key, stage); e == nil && ref != "" && item.Approvals[stage] == ref {
				row.Approved = append(row.Approved, stage)
			}
		}
		draft, e := r.opts.DB.GetPlanStoryDraft(ctx, r.result.Ref.String(), item.Key)
		switch {
		case e == nil && draft.IssueID > 0:
			row.Issue = draft.IssueNumber
		case e == nil || errors.Is(e, store.ErrNotFound):
			complete = false
		default:
			return nil, false, e
		}
		rows = append(rows, row)
	}
	return rows, complete, nil
}

func issueLink(ref naming.Ref, number int) string {
	if number < 1 {
		return "not created yet"
	}
	return fmt.Sprintf("[#%d](https://github.com/%s/%s/issues/%d)", number, ref.Owner, ref.Repo, number)
}

func dependencyCell(deps []string) string {
	if len(deps) == 0 {
		return "none"
	}
	return strings.Join(deps, ", ")
}

// renderPlanStoryTable writes the story set review table with the sub-issue of
// each story, and optionally the stages the owner has approved.
func renderPlanStoryTable(ref naming.Ref, rows []planStoryRow, stages bool) string {
	var out strings.Builder
	if stages {
		out.WriteString("| ID | Name | Description | Dependencies | Sub-issue | Approved stages |\n|---|---|---|---|---|---|\n")
	} else {
		out.WriteString("| ID | Name | Description | Dependencies | Sub-issue |\n|---|---|---|---|---|\n")
	}
	for _, row := range rows {
		fmt.Fprintf(&out, "| `%s` | %s | %s | %s | %s |", row.Key, planTableCell(row.Name), planTableCell(row.Description), dependencyCell(row.Dependencies), issueLink(ref, row.Issue))
		if stages {
			approved := "none"
			if len(row.Approved) > 0 {
				approved = strings.Join(row.Approved, ", ")
			}
			fmt.Fprintf(&out, " %s |", approved)
		}
		out.WriteString("\n")
	}
	return out.String()
}

// storiesCreatedText is the parent guide posted once the creation cycle has a
// sub-issue for every promoted story and parks at the owner's reviews.
func storiesCreatedText(ref naming.Ref, rows []planStoryRow) string {
	var out strings.Builder
	fmt.Fprintf(&out, "**%d stories are ready for your review.**\n\n", len(rows))
	out.WriteString("Each story now has its own sub-issue with a review waiting for you.\n\n")
	out.WriteString(renderPlanStoryTable(ref, rows, false))
	out.WriteString("\n**How to continue**\n\n")
	out.WriteString("1. Open each sub-issue and read its latest review.\n")
	out.WriteString("2. To approve a stage, reply on that sub-issue with the exact `/approve-plan` command shown in its latest review.\n")
	out.WriteString("3. To change a draft, reply on that sub-issue in plain words; the story is revised and reviewed again.\n")
	out.WriteString("4. Each story goes through three stages in turn: the story itself, then the technical design, then acceptance. Every stage needs your approval on the sub-issue.\n")
	out.WriteString("5. When every story has all three stages approved, a final plan review appears here on this issue. Approve it with the `/approve-plan-set` command it shows.\n")
	return out.String()
}

// planReadyText confirms the owner's final approval once the plan is saved and
// the feature is ready.
func planReadyText(ref naming.Ref, rows []planStoryRow) string {
	var out strings.Builder
	fmt.Fprintf(&out, "**Plan approved and ready.** It has %d stories:\n\n", len(rows))
	out.WriteString(renderPlanStoryTable(ref, rows, false))
	out.WriteString("\n**Next step:** reply `/implement implement the approved stories` on this issue to start building the stories.\n")
	return out.String()
}

// planWaitingText answers a parent comment that found no Plan work to do: every
// remaining step waits for the owner on a sub-issue.
func planWaitingText(ref naming.Ref, rows []planStoryRow) string {
	var out strings.Builder
	out.WriteString("**Nothing to do here right now: the plan is waiting for your reviews on the sub-issues.**\n\n")
	out.WriteString("| ID | Name | Sub-issue | Command that works now |\n|---|---|---|---|\n")
	for _, row := range rows {
		command := "no review waiting"
		if row.Command != "" {
			command = "`" + row.Command + "`"
		}
		fmt.Fprintf(&out, "| `%s` | %s | %s | %s |\n", row.Key, planTableCell(row.Name), issueLink(ref, row.Issue), command)
	}
	out.WriteString("\nReply on each sub-issue with its command to approve it, or in plain words to change it. A story with no review waiting is still being worked on or has an unresolved problem reported on its sub-issue.\n")
	return out.String()
}

// planNotice posts one factory-authored parent comment whose identity is its
// purpose alone. It answers no owner input, so it consumes nothing.
func (r *planGraphRuntime) planNotice(ctx context.Context, purpose, text string) error {
	reporter := r.opts.ResearchReporter
	if reporter == nil {
		return errors.New("graph Plan notice has no durable reporter")
	}
	intent := reporter.deliveryIntent(r.result.Ref, r.result.Ref, r.req.SessionID, 0, 0, purpose)
	intent.TriggerNamespace = store.PlanNoticeNamespace
	_, err := reporter.deliver(ctx, intent, func() (redact.Body, error) { return reporter.renderer.RenderReportDelivery(ctx, text, "") })
	return err
}

// logNotice keeps a notice failure from changing the outcome of the Plan step
// it follows. The delivery stays retained under its stable identity, and the
// next admission that reaches the same point resumes it.
func (r *planGraphRuntime) logNotice(err error, purpose string) {
	if err != nil && r.opts.Logger != nil {
		r.opts.Logger.Warn("graph Plan owner notice not delivered", slog.String("purpose", purpose), slog.String("error", err.Error()))
	}
}

// storiesCreated posts the parent guide once the creation cycle is over: every
// promoted story has a draft and a sub-issue and waits for the owner, and none
// has been approved yet. The identity is the promoted set revision, so the
// guide is posted once per promoted set whichever run reaches this point.
// applies reports whether the guide belongs to this point, posted or resumed.
func (r *planGraphRuntime) storiesCreated(ctx context.Context) (applies bool, err error) {
	s, _, err := r.state(ctx)
	if err != nil {
		return false, err
	}
	set := s.StorySet
	if set == nil || !set.Promoted || set.PromotedRevision == "" {
		return false, nil
	}
	if _, _, owed, err := s.NextSetStory(); err != nil || owed {
		return false, err
	}
	for _, item := range s.Stories {
		if item.Active() && (item.Wait == nil || len(item.Approvals) > 0) {
			return false, nil
		}
	}
	rows, complete, err := r.planStoryRows(ctx, s)
	if err != nil || !complete || len(rows) == 0 {
		return false, err
	}
	return true, r.planNotice(ctx, "plan-stories-created:"+set.PromotedRevision, storiesCreatedText(r.result.Ref, rows))
}

// waiting answers an owner comment on the parent that found no Plan work: the
// comment gets one reply naming where the owner can act.
func (r *planGraphRuntime) waiting(ctx context.Context, s planbrief.PlanState) error {
	trigger := r.req.Trigger
	if trigger.PlanReply != nil || trigger.CommentID < 1 || store.IsPrivateTrigger(trigger.CommentID) {
		return nil
	}
	rows, _, err := r.planStoryRows(ctx, s)
	if err != nil || len(rows) == 0 {
		return err
	}
	return r.planNotice(ctx, fmt.Sprintf("plan-waiting:%d", trigger.CommentID), planWaitingText(r.result.Ref, rows))
}

// planReady confirms a successful final approval. The identity is the saved
// plan revision, so a replay of the save posts nothing new.
func (r *planGraphRuntime) planReady(ctx context.Context, s planbrief.PlanState) error {
	rows, _, err := r.planStoryRows(ctx, s)
	if err != nil {
		return err
	}
	return r.planNotice(ctx, "plan-ready:"+s.SavedRevision, planReadyText(r.result.Ref, rows))
}

// planStopText explains a run that ended at a terminal with nothing posted:
// a stage loop that ran out of passes, or a stage decision that found no stage.
// ok is false for terminals the graph already reports (a failure comment, an
// owner wait, a promotion).
func planStopText(profile *graphexec.Profile, from, outcome string) (string, bool) {
	node, found := profile.Node(from)
	if !found || from == graphexec.PlanFailureNode {
		return "", false
	}
	stage := strings.TrimSuffix(from, "_loop")
	switch {
	case node.Action == graphexec.ResearchLoopAction && outcome == "false" && stage != from:
		return fmt.Sprintf("**Plan stopped: the %s stage ran out of passes in this run.**\n\nNo work was lost; everything drafted and approved so far is saved. To continue, reply on this issue. Your reply starts a fresh pass from the saved plan.", stage), true
	case node.Action == graphexec.ResearchIfAction && outcome == "false":
		return "**Plan stopped: no stage has work to do right now.**\n\nEvery story is either waiting for your review on its sub-issue or has a problem reported there. Reply on the sub-issue with the command in its latest review, or reply here in plain words to ask for a change.", true
	}
	return "", false
}

// reportStop posts the explanation for an instance that ended at the stop
// terminal without a failure comment. It is posted once per instance.
func (r *planGraphRuntime) reportStop(ctx context.Context, instanceID int64) error {
	if r.definition == nil || r.definition.Profile == nil || instanceID < 1 {
		return nil
	}
	instance, err := r.opts.DB.GetGraphInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	node, ok := r.definition.Profile.Node(instance.CurrentNode)
	if !ok || !node.Terminal || instance.CurrentNode != "stopped" {
		return nil
	}
	transitions, err := r.opts.DB.ListGraphTransitions(ctx, instanceID)
	if err != nil {
		return err
	}
	for i := len(transitions) - 1; i >= 0; i-- {
		t := transitions[i]
		if t.ToNode != instance.CurrentNode {
			continue
		}
		text, ok := planStopText(r.definition.Profile, t.FromNode, t.OnOutcome)
		if !ok {
			return nil
		}
		return r.planNotice(ctx, fmt.Sprintf("plan-stopped:%d", instanceID), text)
	}
	return nil
}
