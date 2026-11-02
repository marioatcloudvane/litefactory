package turn

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// CommentEditor replaces the body of a comment the factory posted.
type CommentEditor interface {
	EditComment(ctx context.Context, repo github.Repo, commentID int64, body redact.Body) error
}

// The states a story shows in the parent's progress summary.
const (
	planStoryDrafting        = "drafting"
	planStoryWaitingReview   = "waiting for review"
	planStoryWaitingUpstream = "waiting for upstream"
	planStoryFailed          = "failed"
	planStoryApproved        = "approved"
	planSummaryPurpose       = "plan-summary"
)

// planStoryProgress names the stage a story is at and what it is doing there. It
// reads only the saved state and whether the story's latest run failed, so the
// parent can show it without asking the story task anything.
func planStoryProgress(s planbrief.PlanState, item planbrief.PlanStoryState, lastRunFailed bool) (stage, state string) {
	stage = item.Stage
	if !s.StoryDrafted(item.Key) {
		stage = "story"
	}
	approved := true
	for _, st := range []string{"story", "technical", "acceptance"} {
		if ref, err := s.ReviewRevision(item.Key, st); err != nil || item.Approvals[st] != ref {
			approved = false
		}
	}
	switch {
	case approved:
		return "all stages", planStoryApproved
	case item.Blocked():
		return stage, planStoryFailed
	case item.Wait != nil:
		return stage, planStoryWaitingReview
	case !s.UpstreamApproved(item.Key, stage):
		return stage, planStoryWaitingUpstream
	case lastRunFailed:
		return stage, planStoryFailed
	}
	return stage, planStoryDrafting
}

// planSummaryText is the parent's progress summary: one row per live story with
// its issue, stage and state.
func planSummaryText(rows [][4]string) string {
	return planSummaryTextFor(false, rows)
}

// planSummaryTextFor renders the summary for its destination's cell contract.
func planSummaryTextFor(onLinear bool, rows [][4]string) string {
	var out strings.Builder
	out.WriteString("**Plan progress**\n\nEach story runs on its own issue. Reply there to approve or change it.\n\n")
	out.WriteString("| Story | Issue | Stage | State |\n|---|---|---|---|\n")
	for _, row := range rows {
		fmt.Fprintf(&out, "| %s | %s | %s | %s |\n", planTableCellFor(onLinear, row[0]), row[1], planTableCellFor(onLinear, row[2]), planTableCellFor(onLinear, row[3]))
	}
	return out.String()
}

// ReportPlanSummary keeps the one progress summary comment of a graph Plan
// parent current. The comment is posted once under a stable identity and then
// edited in place whenever a story's stage or state changed, read from the saved
// state and the story issues' own runs. An unchanged summary is not edited.
func (r *Reporter) ReportPlanSummary(ctx context.Context, parent naming.Ref, sessionID int64) error {
	state, _, err := r.db.GetPlanWorkflowState(ctx, parent.String())
	if err != nil {
		return err
	}
	drafts, err := r.db.ListPlanStoryDrafts(ctx, parent.String())
	if err != nil {
		return err
	}
	var rows [][4]string
	for _, draft := range drafts {
		if draft.Retired {
			continue
		}
		var found *planbrief.PlanStoryState
		for i := range state.Stories {
			if state.Stories[i].Key == draft.Key {
				found = &state.Stories[i]
			}
		}
		if found == nil || !found.Active() {
			continue
		}
		if draft.IssueID < 1 || draft.IssueNumber < 1 {
			issue := "issue unresolved"
			// Lookup is advisory: an outage must not hide the durable warning.
			if forge, ok := r.forge.(PlanStoryForge); ok {
				a := PlanStoryAuthority{Parent: parent.String(), Repo: github.RepoFromRef(parent), ParentNumber: parent.Issue}
				parentIssue, parentReadErr := forge.GetIssue(ctx, a.Repo, parent.Issue)
				a.ParentID = parentIssue.ID
				if listed, listErr := forge.ListFollowupIssues(ctx, a.Repo); listErr == nil && parentReadErr == nil && a.ParentID > 0 {
					var match *github.Issue
					matches := 0
					for i := range listed {
						if strings.HasPrefix(listed[i].Body, planStoryMarker(a, draft.Key)+"\n") {
							match = &listed[i]
							matches++
						}
					}
					if matches == 1 && match.ID > 0 && match.Number > 0 {
						observed, readErr := forge.GetIssue(ctx, a.Repo, match.Number)
						owner, parentErr := forge.GetParentIssue(ctx, a.Repo, match.Number)
						if readErr == nil && parentErr == nil && observed.ID == match.ID && observed.Number == match.Number && owner.Number == parent.Issue && owner.ID == a.ParentID && strings.HasPrefix(observed.Body, planStoryMarker(a, draft.Key)+"\n") {
							issue = displayIssue(parent, observed).Link()
						}
					}
				}
			}
			rows = append(rows, [4]string{draft.Key, issue, "story", "Not bound to its issue; cannot draft; plan assembly is blocked. No safe approval or binding-repair command is available from retained facts. Review the linked issue, if available, and report the binding problem on this parent; normal reconciliation must confirm the original identity before work can advance."})
			continue
		}
		failed, err := r.storyRunFailed(ctx, parent, draft.IssueNumber)
		if err != nil {
			return err
		}
		stage, label := planStoryProgress(state, *found, failed)
		if found.Blocked() || label == planStoryWaitingUpstream {
			label += ": " + planNoticeGuidance(state, *found)
		}
		name := found.Draft.Name
		if name == "" {
			name = draft.Key
		}
		display, err := lookupTicketDisplay(ctx, r.forge, parent, draft.IssueNumber)
		if err != nil {
			return err
		}
		rows = append(rows, [4]string{name, issueLink(parent, draft.IssueNumber, display.Link()), stage, label})
	}
	if len(rows) == 0 {
		return nil
	}
	text := planSummaryTextFor(parent.Forge == "linear", rows)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	if state.SummaryDigest == digest {
		return nil
	}
	intent := r.deliveryIntent(parent, parent, sessionID, 0, 0, planSummaryPurpose)
	intent.TriggerNamespace = store.PlanNoticeNamespace
	saved, err := r.db.GetReportDelivery(ctx, intent.Key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if _, err = r.deliver(ctx, intent, func() (redact.Body, error) { return r.renderer.RenderReportDelivery(ctx, text, "") }); err != nil {
			if errors.Is(err, ErrReportReceiptUncertain) {
				return nil
			}
			return err
		}
	case err != nil:
		return err
	default:
		if saved.CommentID == 0 || !saved.Completed {
			// The first post is not confirmed yet: finish it before editing anything.
			if _, err = r.dispatchDelivery(ctx, saved); err != nil {
				if errors.Is(err, ErrReportReceiptUncertain) {
					return nil
				}
				return err
			}
			if saved, err = r.db.GetReportDelivery(ctx, intent.Key); err != nil {
				return err
			}
		}
		editor, ok := r.forge.(CommentEditor)
		if !ok || saved.CommentID == 0 {
			return errors.New("plan summary cannot be edited in place without the posted comment")
		}
		body, err := r.renderer.RenderReportDelivery(ctx, text, intent.Marker)
		if err != nil {
			return err
		}
		if err = editor.EditComment(ctx, github.RepoFromRef(parent), saved.CommentID, body); err != nil {
			return err
		}
	}
	return r.db.UpdatePlanWorkflowState(ctx, parent.String(), func(s *planbrief.PlanState) (bool, error) {
		if s.SummaryDigest == digest {
			return false, nil
		}
		s.SummaryDigest = digest
		return true, nil
	})
}

// storyRunFailed reports whether the latest finished run of a story's own task
// failed, which is how a failed story shows in the summary before any state moves.
func (r *Reporter) storyRunFailed(ctx context.Context, parent naming.Ref, issue int) (bool, error) {
	ref := parent
	ref.Issue = issue
	session, err := r.db.GetSessionByTaskRef(ctx, ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	runs, err := r.db.ListRuns(ctx, session.ID)
	if err != nil || len(runs) == 0 {
		return false, err
	}
	latest := runs[len(runs)-1]
	return latest.Reportable() && !latest.Succeeded(), nil
}
