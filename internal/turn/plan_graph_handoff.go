package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// settled completes factory-owned stage-three review/save work without inventing
// another provider invocation. Existing child content is reobserved, never rendered
// through the legacy full-body template during handoff.
func (r *planGraphRuntime) settled(ctx context.Context) (Result, error) {
	out := r.result
	out.ResearchGraph = true
	s, revision, err := r.state(ctx)
	if err != nil {
		return out, err
	}
	// Stories run in tasks of their own, so the parent also finds a plan whose
	// stories are not all finished: it waits for them instead of assembling.
	waiting := s.ValidateComplete(r.scope) != nil
	for _, story := range s.Stories {
		waiting = waiting || story.Blocked() || (story.Active() && story.Wait != nil)
	}
	if waiting {
		out.Reason = "planning waits for story feedback or effect reconciliation"
		// The run found only owner waits: say so where the owner asked, with
		// the command that works now on each sub-issue.
		applies, noticeErr := r.storiesCreated(ctx)
		r.logNotice(noticeErr, "plan-stories-created")
		if !applies {
			r.logNotice(r.waiting(ctx, s), "plan-waiting")
		}
		return out, nil
	}
	if s.AssembledApproval != s.PlanSetRevision() {
		ref, err := s.OpenAssembledReview(r.scope)
		if err != nil {
			return out, err
		}
		// Saved on freshly read state: a story that changed meanwhile moves the
		// plan's revision, and a review of the old one is then not posted.
		stale := false
		if err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(fresh *planbrief.PlanState) (bool, error) {
			opened, e := fresh.OpenAssembledReview(r.scope)
			stale = e == nil && opened != ref
			return !stale, e
		}); err != nil {
			return out, err
		}
		if stale {
			return out, nil
		}
		return out, r.assembledComment(ctx, s, ref)
	}
	return r.finalize(ctx, s, revision)
}

func (r *planGraphRuntime) assembledComment(ctx context.Context, s planbrief.PlanState, ref string) error {
	instance, err := r.opts.DB.LatestWorkflowInstance(ctx, r.req.SessionID, "plan")
	if err != nil {
		return err
	}
	source, err := latestResearchAgentAttempt(ctx, r.opts, r.definition.Profile, instance.ID, "")
	if err != nil {
		return err
	}
	if source.ID == 0 {
		return errors.New("assembled review lacks retained source")
	}
	reporter := r.opts.ResearchReporter
	run, err := r.opts.DB.GetRun(ctx, source.RunID)
	if err != nil {
		return err
	}
	key := "plan-assembled:" + ref
	intent := reporter.deliveryIntent(r.result.Ref, r.result.Ref, r.req.SessionID, run.TriggerCommentID, run.ID, key)
	text, err := r.assembledText(ctx, s, ref)
	if err != nil {
		return err
	}
	display := store.OwnerDecisionDisplay{TaskRef: r.parentRef.String(), Action: "/approve-plan-set", Target: "/approve-plan-set " + ref, Revision: ref, InstanceID: instance.ID, CreatedAt: r.opts.MCP.Now()}
	raw, err := json.Marshal(planPublication{Intent: intent, Text: text, Display: &display})
	if err != nil {
		return err
	}
	if err = reporter.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: intent.Key, InstanceID: instance.ID, SourceAttemptID: source.ID, Node: "acceptance_review", Kind: "final", Candidate: raw}); err != nil {
		return err
	}
	posted, err := reporter.deliver(ctx, intent, func() (redact.Body, error) { return reporter.renderer.RenderReportDelivery(ctx, text, "") })
	if err != nil {
		return err
	}
	return r.opts.DB.PutOwnerDecisionDisplay(ctx, store.OwnerDecisionDisplay{TaskRef: r.parentRef.String(), Action: "/approve-plan-set", Target: "/approve-plan-set " + ref, Revision: ref, InstanceID: instance.ID, PublicationID: posted.CommentID, CreatedAt: r.opts.MCP.Now()})
}

// assembledText is the final plan review: the story set table with each
// story's sub-issue and approved stages, stories no longer in the plan, and the
// exact command that approves the plan as it stands.
func (r *planGraphRuntime) assembledText(ctx context.Context, s planbrief.PlanState, ref string) (string, error) {
	rows, _, err := r.planStoryRows(ctx, s)
	if err != nil {
		return "", err
	}
	var body strings.Builder
	body.WriteString("**Final plan review**\n\nEvery story below has all three stages approved. This is the whole plan as it stands now:\n\n")
	body.WriteString(renderPlanStoryTable(r.result.Ref, rows, true))
	body.WriteString("\n" + planbrief.RenderPlanRoutes(r.finalRouteReviews(s)))
	if graphApproved := s.GraphApprovedStages(); len(graphApproved) > 0 {
		parts := make([]string, len(graphApproved))
		for i, g := range graphApproved {
			parts[i] = "`" + g.Key + "` " + g.Stage
		}
		fmt.Fprintf(&body, "\nApproved by the graph, not by the owner: %s.\n", strings.Join(parts, ", "))
	}
	var inactive []string
	for _, item := range s.Stories {
		if !item.Active() {
			inactive = append(inactive, "`"+item.Key+"`")
		}
	}
	if len(inactive) > 0 {
		fmt.Fprintf(&body, "\nNo longer part of the plan (closed or retired): %s.\n", strings.Join(inactive, ", "))
	}
	fmt.Fprintf(&body, "\nTo approve exactly this plan, reply `/approve-plan-set %s`.\nQuestions here reach the set agent; questions on a story reach its current stage. Answers leave approvals unchanged. To change something, reply on the story's sub-issue in plain words; a changed plan gets a new final review.\n", ref)
	shortRef := store.OwnerDecisionReference(r.parentRef.String(), "/approve-plan-set", "/approve-plan-set "+ref, ref)
	fmt.Fprintf(&body, "\nShort reply: `/approve-plan-set %s`.\n", shortRef)
	return body.String(), nil
}

func (r *planGraphRuntime) finalize(ctx context.Context, s planbrief.PlanState, revision int64) (out Result, returned error) {
	out = r.result
	out.ResearchGraph = true
	req := r.req
	req.researchDefinition = r.definition
	req.researchNode = "acceptance_agent"
	policy := r.policy()
	req.workflowPolicy = &policy
	prep, err := prepare(ctx, r.opts, req, out.Session, out.Ref, r.opts.Logger)
	if err != nil {
		return out, err
	}
	run, err := r.opts.DB.StartRun(ctx, prep.run.ID)
	if err != nil {
		return out, err
	}
	prep.run = run
	out.Run = run
	out.Disposition = DispositionRan
	out.WorktreePath = prep.worktree.Path()
	out.Branch = prep.session.Branch
	// These are factory publication facts: no Harness result, cost or provider
	// output is attached to this run. The commit helper consumes only its summary.
	defer func() {
		status := store.RunSucceeded
		if returned != nil {
			status = store.RunFailed
			out.Err = returned
		}
		finished, err := r.opts.DB.FinishRun(context.WithoutCancel(ctx), run.ID, store.RunOutcome{Status: status, Error: errorText(returned), FinishedAt: r.opts.MCP.Now()})
		if err == nil {
			out.Run = finished
		}
		returned = errors.Join(returned, err)
	}()
	var drafts []planbrief.PlanDraft
	for _, item := range s.Stories {
		if item.Active() {
			drafts = append(drafts, item.Draft)
		}
	}
	typed, err := planbrief.CompletePlanDrafts("Owner-approved graph Plan", drafts, r.scope)
	if err != nil {
		return out, err
	}
	feature, err := r.opts.DB.GetManagedFeature(ctx, r.result.Ref.String())
	if err != nil {
		return out, err
	}
	if feature.State != "planning" {
		return out, errors.New("plan generation is no longer editable")
	}
	if err = r.opts.DB.BindFeatureContract(ctx, store.FeatureContract{Parent: feature.ParentTask, Generation: feature.Generation, ContentVersion: 2, SubmissionVersion: 2}); err != nil {
		return out, err
	}
	forge, err := r.storyForge()
	if err != nil {
		return out, err
	}
	var index strings.Builder
	index.WriteString("## Story Index\n\n| ID | Title | Sub-issue |\n| --- | --- | --- |\n")
	for _, story := range typed.Stories {
		binding, err := r.opts.DB.GetPlanStoryDraft(ctx, feature.ParentTask, story.Key)
		if err != nil {
			return out, err
		}
		issue, err := forge.GetIssue(ctx, github.Repo{Owner: r.result.Ref.Owner, Name: r.result.Ref.Repo}, binding.IssueNumber)
		if err != nil {
			return out, err
		}
		parent, err := forge.GetParentIssue(ctx, github.Repo{Owner: r.result.Ref.Owner, Name: r.result.Ref.Repo}, binding.IssueNumber)
		if err != nil {
			return out, err
		}
		if issue.ID != binding.IssueID || issue.State != github.StateOpen || parent.ID != r.parentID || parent.Number != r.req.Issue.Number || PlanIssueRevision(issue.Title, issue.Body) != binding.Revision {
			return out, errors.New("plan publication drift requires reconciliation")
		}
		story.Title = issue.Title
		content, err := json.Marshal(story)
		if err != nil {
			return out, err
		}
		var criteria []string
		for _, c := range story.AcceptanceCriteria {
			criteria = append(criteria, c.ID)
		}
		projection := store.SubmissionProjection{Parent: feature.ParentTask, Story: story.Key, Revision: binding.Revision, Title: issue.Title, Body: issue.Body, Generation: feature.Generation, ContentVersion: 2, IssueID: issue.ID, Content: content, Criteria: criteria}
		if err = retainTypedPublicationProjection(ctx, r.opts.DB, projection, story); err != nil {
			return out, err
		}
		if err = r.opts.DB.PutManagedStory(ctx, store.ManagedStory{ParentTask: feature.ParentTask, Generation: feature.Generation, Key: story.Key, IssueNumber: issue.Number, IssueID: issue.ID, BodyHash: binding.Revision, Dependencies: story.Dependencies.StoryKeys, State: "planning"}); err != nil {
			return out, err
		}
		// Only links and stable identifiers appear in the local index.
		fmt.Fprintf(&index, "| %s | %s | %s |\n", story.Key, strings.ReplaceAll(issue.Title, "|", "&#124;"), displayIssue(r.result.Ref, issue).Link())
	}
	s.PublishedRevision = s.PlanSetRevision()
	if err = r.persist(ctx, s, revision); err != nil {
		return out, err
	}
	revision++
	session := prep.promptInput.SessionArtifactDir
	if session == "" {
		session, err = naming.AITeamSessionDir(prep.session.Branch, run.StartedAt, prep.session.FeatureSlug)
		if err != nil {
			return out, err
		}
	}
	root, err := os.OpenRoot(prep.worktree.Path())
	if err != nil {
		return out, err
	}
	defer root.Close()
	if err = root.MkdirAll(filepath.FromSlash(session), 0o755); err != nil {
		return out, err
	}
	name := filepath.Join(filepath.FromSlash(session), planbrief.FileName)
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, writeErr := f.WriteString("# Plan\n\n## Story Index\n\n| ID | Title | Sub-issue |\n| --- | --- | --- |\n")
		if err = errors.Join(writeErr, f.Close()); err != nil {
			return out, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return out, err
	}
	// An existing file without a Story Index section gets one appended by
	// openPlanIndex/replace rather than failing every retry the same way.
	target, err := openPlanIndex(root, session)
	if err != nil {
		return out, err
	}
	defer target.dir.Close()
	if err = target.replace(root, []byte(index.String())); err != nil {
		return out, err
	}
	out = commitAndPush(ctx, r.opts, prep, out, &harness.RunResult{ResultText: "Save owner-approved graph Plan and existing story index."}, r.opts.Logger)
	if out.Err != nil {
		return out, out.Err
	}
	s.SavedRevision = s.PlanSetRevision()
	if !s.PlanReady(r.scope) {
		return out, errors.New("plan save did not establish exact readiness")
	}
	if err = r.persist(ctx, s, revision); err != nil {
		return out, err
	}
	if err = r.opts.DB.SetManagedFeatureReady(ctx, feature.ParentTask, feature.Generation, true); err != nil {
		return out, err
	}
	// The owner's final approval is confirmed on the parent with the next step.
	r.logNotice(r.planReady(ctx, s), "plan-ready")
	out.Outcome = agent.OutcomeSucceeded
	return out, nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
