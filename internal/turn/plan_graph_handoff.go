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
	for _, story := range s.Stories {
		if !story.Retired && (story.Wait != nil || story.Hold != "" || (story.Effect != "" && story.Effect != "applied")) {
			out.Reason = "planning waits for story feedback or effect reconciliation"
			return out, nil
		}
	}
	if err = s.ValidateComplete(r.scope); err != nil {
		return out, err
	}
	if s.AssembledApproval != s.PlanSetRevision() {
		ref, err := s.OpenAssembledReview(r.scope)
		if err != nil {
			return out, err
		}
		if err = r.persist(ctx, s, revision); err != nil {
			return out, err
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
	var body strings.Builder
	fmt.Fprintf(&body, "Acceptance and assembled Plan review\n\nCurrent shared decisions: %s\n\n", s.SharedRevision)
	for _, item := range s.Stories {
		if item.Retired {
			fmt.Fprintf(&body, "- Retired scope: %s.\n", item.Key)
			continue
		}
		draft, err := r.opts.DB.GetPlanStoryDraft(ctx, r.result.Ref.String(), item.Key)
		if err != nil {
			return err
		}
		fmt.Fprintf(&body, "- [%s](https://github.com/%s/%s/issues/%d): candidate %s.\n", item.Key, r.result.Ref.Owner, r.result.Ref.Repo, draft.IssueNumber, item.Candidate)
	}
	fmt.Fprintf(&body, "\nThis third-stage approval binds the exact current active story/dependency/shared-decision set. Reply `/approve-plan-set %s`.\n", ref)
	raw, err := json.Marshal(planPublication{Intent: intent, Text: body.String()})
	if err != nil {
		return err
	}
	if err = reporter.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: intent.Key, InstanceID: instance.ID, SourceAttemptID: source.ID, Node: "acceptance_review", Kind: "final", Candidate: raw}); err != nil {
		return err
	}
	_, err = reporter.deliver(ctx, intent, func() (redact.Body, error) { return reporter.renderer.RenderReportDelivery(ctx, body.String(), "") })
	return err
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
		if !item.Retired {
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
		fmt.Fprintf(&index, "| %s | %s | [#%d](https://github.com/%s/%s/issues/%d) |\n", story.Key, strings.ReplaceAll(issue.Title, "|", "&#124;"), issue.Number, r.result.Ref.Owner, r.result.Ref.Repo, issue.Number)
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
	out.Outcome = agent.OutcomeSucceeded
	return out, nil
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
