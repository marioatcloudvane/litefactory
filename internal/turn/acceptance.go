package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

type acceptanceForge interface {
	UpdateIssue(context.Context, github.Repo, int, redact.Body, redact.Body) (github.Issue, error)
}
type acceptanceRenderer interface {
	RenderAcceptance(context.Context, report.AcceptanceNotice) (redact.Body, error)
	RenderAcceptanceProjection(context.Context, string, string) (redact.Body, redact.Body, error)
}

func retainAcceptance(ctx context.Context, db *store.DB, a store.ValidationAssessment) error {
	var scope agent.ValidationContext
	if err := json.Unmarshal([]byte(a.ContextJSON), &scope); err != nil {
		return err
	}
	out, err := agent.ParseValidationOutput([]byte(a.ResultJSON), &scope)
	if err != nil {
		return err
	}
	for _, result := range out.Stories {
		var source agent.ValidationStory
		for _, s := range scope.Stories {
			if s.Story.Key == result.Key {
				source = s
				break
			}
		}
		names, err := storyCriterionIDs(ctx, db, a.Parent, a.Generation, result.Key, result.Revision, source.Story.Body)
		if err != nil {
			return err
		}
		if len(names) != len(result.Criteria) {
			return fmt.Errorf("acceptance enumeration mismatch")
		}
		expected := make(map[string]bool, len(names))
		for _, name := range names {
			expected[name] = true
		}
		for _, finding := range result.Criteria {
			if !expected[finding.Name] {
				return fmt.Errorf("acceptance criterion identity mismatch")
			}
			delete(expected, finding.Name)
		}
		if len(expected) != 0 {
			return fmt.Errorf("acceptance criterion coverage mismatch")
		}
		f := store.AcceptanceFindings{}
		for _, c := range result.Criteria {
			f.Criteria = append(f.Criteria, store.AcceptanceFinding{Name: c.Name, Status: c.Status, Classification: c.Classification, Evidence: c.Evidence})
		}
		for _, c := range result.Reviews {
			f.Reviews = append(f.Reviews, store.AcceptanceFinding{Name: c.Name, Status: c.Status, Classification: c.Classification, Evidence: c.Evidence})
		}
		data, err := json.Marshal(f)
		if err != nil {
			return err
		}
		d := store.AcceptanceDecision{RunID: a.RunID, Parent: a.Parent, Generation: a.Generation, Story: result.Key, IssueNumber: source.Story.IssueNumber, IssueID: result.IssueID, ParentID: source.Story.ParentID, Revision: result.Revision, Snapshot: result.Snapshot, FrozenBody: source.Story.Body, FindingsJSON: string(data), Accepted: f.Accepted()}
		if err := db.RetainAcceptanceDecision(ctx, d); err != nil {
			return err
		}
	}
	return db.MaterializeValidationAssessment(ctx, a.RunID)
}
func (p *StoryProgress) deliverAcceptance(ctx context.Context, parent string) error {
	assessments, err := p.db.ListUnprojectedValidationAssessments(ctx)
	if err != nil {
		return err
	}
	for _, a := range assessments {
		if a.Parent == parent {
			if err := retainAcceptance(ctx, p.db, a); err != nil {
				return err
			}
		}
	}
	decisions, err := p.db.ListPendingAcceptance(ctx)
	if err != nil {
		return err
	}
	var faults []error
	blocked := map[string]bool{}
	for _, d := range decisions {
		if d.Parent != parent || blocked[d.Story] {
			continue
		}
		if err := p.deliverAcceptanceOne(ctx, d); err != nil {
			blocked[d.Story] = true
			saveErr := p.db.SaveAcceptanceDiagnostic(ctx, d.ID, err.Error())
			faults = append(faults, errors.Join(fmt.Errorf("acceptance %s: %w", d.Story, err), saveErr))
		}
	}
	return errors.Join(faults...)
}
func (p *StoryProgress) deliverAcceptanceOne(ctx context.Context, d store.AcceptanceDecision) error {
	forge, ok := p.forge.(acceptanceForge)
	if !ok {
		return fmt.Errorf("acceptance body gateway unavailable")
	}
	renderer, ok := p.renderer.(acceptanceRenderer)
	if !ok {
		return fmt.Errorf("acceptance renderer unavailable")
	}
	ref, err := naming.ParseRef(d.Parent)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(ref)
	live, err := p.forge.GetIssue(ctx, repo, d.IssueNumber)
	if err != nil {
		return err
	}
	parent, err := p.forge.GetParentIssue(ctx, repo, d.IssueNumber)
	if err != nil {
		return err
	}
	if live.ID != d.IssueID || parent.ID != d.ParentID || parent.Number != ref.Issue {
		return fmt.Errorf("acceptance target identity changed")
	}
	var f store.AcceptanceFindings
	if err := json.Unmarshal([]byte(d.FindingsJSON), &f); err != nil {
		return err
	}
	checks := map[string]bool{}
	var detail strings.Builder
	for _, c := range f.Criteria {
		checks[c.Name] = c.Status == "satisfied" || c.WaiverReason != ""
	}
	for _, group := range [][]store.AcceptanceFinding{f.Criteria, f.Reviews} {
		for _, c := range group {
			fmt.Fprintf(&detail, "%s: %s (%s): %s", c.Name, c.Status, c.Classification, c.Evidence)
			if c.WaiverReason != "" {
				fmt.Fprintf(&detail, "; OWNER-WAIVED: %s (original finding retained)", c.WaiverReason)
			}
			detail.WriteByte('\n')
		}
	}
	projected, projectionErr := projectStoryAcceptance(ctx, p.db, d, live.Body, checks)
	if projectionErr != nil {
		fmt.Fprintf(&detail, "Projection blocked: %s. Resolve the conflicting live requirement before retry; approved scope is unchanged.\n", projectionErr)
	}
	n := report.AcceptanceNotice{Parent: ref, ID: d.ID, RunID: d.RunID, OwnerExceptionID: d.ExceptionID, Generation: d.Generation, Story: d.Story, Revision: d.Revision, Snapshot: d.Snapshot, Detail: detail.String(), Accepted: d.Accepted}
	if d.CommentID == 0 {
		comments, err := p.forge.ListCommentsAfter(ctx, repo, d.IssueNumber, 0)
		if err != nil {
			return err
		}
		for _, c := range comments {
			if c.Author.ID == p.botID && exactProgressMarker(c.Body, n.Marker()) {
				d.CommentID = c.ID
				break
			}
		}
		if d.CommentID == 0 {
			body, err := renderer.RenderAcceptance(ctx, n)
			if err != nil {
				return err
			}
			c, err := p.forge.PostComment(ctx, repo, d.IssueNumber, body)
			if err != nil {
				return err
			}
			if c.ID < 1 || c.Author.ID != p.botID {
				return fmt.Errorf("acceptance comment provenance mismatch")
			}
			d.CommentID = c.ID
		}
		if err := p.db.SaveAcceptanceComment(ctx, d.ID, d.CommentID); err != nil {
			return err
		}
	}
	if projectionErr != nil {
		return projectionErr
	}
	if live.Body != projected {
		title, body, err := renderer.RenderAcceptanceProjection(ctx, live.Title, projected)
		if err != nil {
			return err
		}
		updated, err := forge.UpdateIssue(ctx, repo, d.IssueNumber, title, body)
		if err != nil {
			return err
		}
		if updated.ID != d.IssueID || updated.Number != d.IssueNumber {
			return fmt.Errorf("acceptance projection identity mismatch")
		}
	}
	// Read after update also covers a lost prior response. Recompute against the
	// frozen requirements so unrelated concurrent edits survive; never close on
	// the strength of the mutation response alone.
	confirmed, err := p.forge.GetIssue(ctx, repo, d.IssueNumber)
	if err != nil {
		return err
	}
	if confirmed.ID != d.IssueID {
		return fmt.Errorf("acceptance confirmation identity mismatch")
	}
	want, err := projectStoryAcceptance(ctx, p.db, d, confirmed.Body, checks)
	if err != nil {
		return err
	}
	if want != confirmed.Body {
		return fmt.Errorf("acceptance checkbox projection not confirmed")
	}
	if err := p.db.SaveAcceptanceProjection(ctx, d.ID); err != nil {
		return err
	}
	if d.Accepted && (confirmed.State != "closed" || confirmed.StateReason != "completed") {
		if err := p.forge.SetStoryState(ctx, repo, ref.Issue, d.ParentID, d.IssueNumber, d.IssueID, true); err != nil {
			return err
		}
		closed, err := p.forge.GetIssue(ctx, repo, d.IssueNumber)
		if err != nil {
			return err
		}
		if closed.ID != d.IssueID || closed.State != "closed" || closed.StateReason != "completed" {
			return fmt.Errorf("acceptance closure not confirmed")
		}
	}
	return p.db.DeliverAcceptance(ctx, d.ID)
}
