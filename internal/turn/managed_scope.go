package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// ManagedReplan is an explicit parent control; incidental mode labels and child
// comments never carry permission to discard the approved generation.
func ManagedReplan(command Command) bool {
	if mode, explicit := command.Mode(); explicit && mode == "plan" {
		return true
	}
	return command.Kind == KindFreeText && strings.EqualFold(strings.TrimSpace(command.Text), "replan")
}

type scopeForge interface {
	PlanForge
	ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error)
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
}
type scopeRenderer interface {
	RenderStoryScope(context.Context, report.StoryScopeNotice) (redact.Body, error)
}

func prepareManagedReplan(ctx context.Context, o Options, req Request, session store.Session, ref naming.Ref) error {
	f, err := o.DB.GetManagedFeature(ctx, ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ManagedReplan(req.Command) || req.mode() != "plan" || f.State == "planning" {
		return nil
	}
	if _, err := o.DB.GetStoryRefinement(ctx, session.ID, req.Trigger.CommentID); !errors.Is(err, store.ErrNotFound) {
		if err != nil {
			return err
		}
		return fmt.Errorf("child discussion cannot reopen planning")
	}
	return o.DB.ReplanManagedFeature(ctx, f.ParentTask, f.Generation)
}

func prepareManagedScope(ctx context.Context, o Options, req Request, ref naming.Ref) error {
	f, err := o.DB.GetManagedFeature(ctx, ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if req.mode() == "validate" && f.WorkflowVersion == store.WorkflowSeparated {
		mode, explicit := req.Command.Mode()
		if !explicit || mode != "validate" || f.State != "implementing" {
			return fmt.Errorf("managed Validate requires explicit owner /validate and approved scope")
		}
		return nil
	}
	if f.State == "planning" && req.mode() != "implement" {
		return deliverScopeNotices(ctx, o, ref, f, true)
	}
	if req.mode() != "implement" {
		return fmt.Errorf("approved stories remain locked; comment replan on the quiescent parent to reopen Plan")
	}
	if f.State == "planning" {
		revisions, err := readScopeForApproval(ctx, o, ref, f)
		if err != nil {
			return err
		}
		if err := o.DB.ApproveManagedStories(ctx, ref.String(), f.Generation, revisions); err != nil {
			return err
		}
		f.State = "freezing"
	}
	if f.State == "freezing" {
		if err := deliverScopeNotices(ctx, o, ref, f, false); err != nil {
			return err
		}
		if err := o.DB.ActivateManagedScope(ctx, ref.String(), f.Generation); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func readScopeForApproval(ctx context.Context, o Options, ref naming.Ref, f store.ManagedFeature) ([]store.ApprovedStoryRevision, error) {
	forge, ok := o.SubIssueCreator.(PlanForge)
	if !ok {
		return nil, fmt.Errorf("managed scope requires GitHub reads")
	}
	repo := github.RepoFromRef(ref)
	parent, err := forge.GetIssue(ctx, repo, ref.Issue)
	if err != nil {
		return nil, err
	}
	if parent.ID <= 0 || parent.Number != ref.Issue {
		return nil, fmt.Errorf("invalid managed parent identity")
	}
	stories, err := o.DB.ListManagedStories(ctx, ref.String(), f.Generation)
	if err != nil {
		return nil, err
	}
	children, err := forge.ListSubIssues(ctx, repo, ref.Issue)
	if err != nil {
		return nil, err
	}
	attached := map[int]int64{}
	for _, child := range children {
		attached[child.Number] = child.ID
	}
	var out []store.ApprovedStoryRevision
	var faults []error
	total := 0
	for _, s := range stories {
		issue, err := forge.GetIssue(ctx, repo, s.IssueNumber)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		direct, err := forge.GetParentIssue(ctx, repo, s.IssueNumber)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		if issue.ID != s.IssueID || issue.Number != s.IssueNumber || attached[s.IssueNumber] != s.IssueID || direct.ID != parent.ID || direct.Number != parent.Number {
			faults = append(faults, fmt.Errorf("story %s is no longer the indexed direct child", s.Key))
			continue
		}
		if s.State == "completed" {
			continue
		}
		if planbrief.StoryHash(issue.Title, issue.Body) != s.BodyHash {
			contract, err := o.DB.GetGenerationContract(ctx, ref.String(), f.Generation)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				faults = append(faults, err)
				continue
			}
			if contract.ContentVersion != 2 {
				faults = append(faults, fmt.Errorf("story %s changed on GitHub since Plan publication; refresh and publish the parent Plan against the current issue before promotion so dependency metadata is approved with the body", s.Key))
				continue
			}
			issue, err = reconcilePresentationIssue(ctx, o.DB, forge, ref, s, issue)
			if err != nil {
				faults = append(faults, fmt.Errorf("story %s: %w", s.Key, err))
				continue
			}
			s.BodyHash = planbrief.StoryHash(issue.Title, issue.Body)
		}
		if issue.State != github.StateOpen || strings.TrimSpace(issue.Title) == "" || len(issue.Title) > 256 {
			faults = append(faults, fmt.Errorf("unfinished story %s is not a valid open issue", s.Key))
			continue
		}
		if _, err := storyCriterionIDs(ctx, o.DB, ref.String(), f.Generation, s.Key, planbrief.StoryHash(issue.Title, issue.Body), issue.Body); err != nil {
			faults = append(faults, fmt.Errorf("story %s: %w", s.Key, err))
			continue
		}
		if err := planbrief.ValidateStoryBody(issue.Body); err != nil {
			faults = append(faults, fmt.Errorf("story %s: %w", s.Key, err))
			continue
		}
		total += len(issue.Title) + len(issue.Body)
		out = append(out, store.ApprovedStoryRevision{ManagedStory: s, ParentID: parent.ID, Title: issue.Title, Body: issue.Body, Hash: planbrief.StoryHash(issue.Title, issue.Body), FetchedAt: time.Now().UTC()})
	}
	contract, contractErr := o.DB.GetGenerationContract(ctx, ref.String(), f.Generation)
	if contractErr != nil && !errors.Is(contractErr, store.ErrNotFound) {
		faults = append(faults, contractErr)
	}
	if contractErr == nil && contract.ContentVersion == 2 {
		// Confirm the exact observed bytes again at the approval boundary. The
		// forge offers no atomic compare-and-swap across this read and storage.
		for _, revision := range out {
			current, err := forge.GetIssue(ctx, repo, revision.IssueNumber)
			if err != nil {
				faults = append(faults, err)
				continue
			}
			if current.ID != revision.IssueID || current.Title != revision.Title || current.Body != revision.Body {
				faults = append(faults, fmt.Errorf("story %s changed before typed approval", revision.Key))
			}
		}
	}
	if total > planbrief.MaxProposalBytes {
		faults = append(faults, fmt.Errorf("approved scope exceeds context limits"))
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	return out, nil
}

func deliverScopeNotices(ctx context.Context, o Options, ref naming.Ref, f store.ManagedFeature, planning bool) error {
	stories, err := o.DB.ListManagedStories(ctx, ref.String(), f.Generation)
	if err != nil {
		return err
	}
	kind := "freeze"
	if planning {
		kind = "planning"
	}
	var faults []error
	for _, s := range stories {
		if s.State == "completed" {
			continue
		}
		delivered, err := o.DB.StoryScopeNoticeDelivered(ctx, ref.String(), f.Generation, s.Key, kind)
		// The initial Plan has no availability transition to announce.
		if planning && errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			faults = append(faults, err)
			continue
		}
		if delivered {
			continue
		}
		if err := deliverScopeNotice(ctx, o, ref, f, s, planning); err != nil {
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}

func deliverScopeNotice(ctx context.Context, o Options, ref naming.Ref, f store.ManagedFeature, s store.ManagedStory, planning bool) error {
	forge, ok := o.SubIssueCreator.(scopeForge)
	if !ok {
		return fmt.Errorf("scope announcement forge unavailable")
	}
	renderer, ok := o.SubIssueRenderer.(scopeRenderer)
	if !ok {
		return fmt.Errorf("scope announcement renderer unavailable")
	}
	bot, err := o.DB.GetIdentity(ctx, ref.Forge, store.RoleBot)
	if err != nil {
		return err
	}
	if bot.UserID <= 0 {
		return fmt.Errorf("scope announcement requires verified bot identity")
	}
	n := report.StoryScopeNotice{Parent: ref, Generation: f.Generation, Key: s.Key, Digest: s.BodyHash, Planning: planning}
	body, err := renderer.RenderStoryScope(ctx, n)
	if err != nil {
		return err
	}
	comments, err := forge.ListCommentsAfter(ctx, github.RepoFromRef(ref), s.IssueNumber, 0)
	if err != nil {
		return err
	}
	found := false
	for _, c := range comments {
		if c.Author.ID == bot.UserID && strings.Contains(c.Body, n.Marker()) && c.Body == body.String() {
			found = true
			break
		}
	}
	if !found {
		if _, err := forge.PostComment(ctx, github.RepoFromRef(ref), s.IssueNumber, body); err != nil {
			return err
		}
	}
	kind := "freeze"
	if planning {
		kind = "planning"
	}
	return o.DB.DeliverStoryScopeNotice(context.WithoutCancel(ctx), ref.String(), f.Generation, s.Key, kind)
}
