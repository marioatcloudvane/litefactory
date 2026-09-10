package turn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// invalidatePlanReadiness runs immediately after authorized replan admission,
// before clone synchronization, context reads or any possible subprocess failure.
func invalidatePlanReadiness(ctx context.Context, o Options, ref naming.Ref, mode string) error {
	if mode != "plan" {
		return nil
	}
	if _, ok := o.SubIssueCreator.(PlanForge); !ok {
		return nil
	}
	if _, ok := o.SubIssueRenderer.(CompleteStoryRenderer); !ok {
		return nil
	}
	feature, err := o.DB.GetManagedFeature(ctx, ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return o.DB.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref.String(), Generation: 1, State: "planning"})
	}
	if err != nil {
		return err
	}
	if feature.State != "planning" {
		return fmt.Errorf("user stories are locked after promotion to Implement")
	}
	return o.DB.SetManagedFeatureReady(ctx, ref.String(), feature.Generation, false)
}

func preparePlanHandoff(ctx context.Context, o Options, ref naming.Ref, mode string, in *harness.PromptInput) error {
	if mode != "plan" {
		return nil
	}
	if _, ok := o.SubIssueCreator.(PlanForge); !ok {
		return nil
	}
	if _, ok := o.SubIssueRenderer.(CompleteStoryRenderer); !ok {
		return nil
	}
	generation := 1
	feature, err := o.DB.GetManagedFeature(ctx, ref.String())
	if err == nil {
		if feature.State != "planning" {
			return fmt.Errorf("user stories are locked after promotion to Implement")
		}
		generation = feature.Generation
		if err := o.DB.SetManagedFeatureReady(ctx, ref.String(), generation, false); err != nil {
			return err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil {
		forge, ok := o.SubIssueCreator.(PlanForge)
		if !ok {
			return fmt.Errorf("plan forge unavailable")
		}
		stories, e := o.DB.ListManagedStories(ctx, ref.String(), generation)
		if e != nil {
			return e
		}
		var contextText strings.Builder
		for _, story := range stories {
			issue, e := forge.GetIssue(ctx, github.RepoFromRef(ref), story.IssueNumber)
			if e != nil {
				return e
			}
			if issue.ID != story.IssueID {
				return fmt.Errorf("story %s identity changed", story.Key)
			}
			fmt.Fprintf(&contextText, "Story %s; state %s (include completed stories with intent unchanged as read-only dependencies; new scope requires a successor); issue #%d; database ID %d; expected_base %s\nTitle: %s\n%s\n\n", story.Key, story.State, issue.Number, issue.ID, planbrief.StoryHash(issue.Title, issue.Body), issue.Title, issue.Body)
			if contextText.Len() > planbrief.MaxProposalBytes {
				return fmt.Errorf("managed Plan context exceeds supported size")
			}
		}
		in.PlanStoryContext = contextText.String()
	}
	if errors.Is(err, store.ErrNotFound) {
		if err := o.DB.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref.String(), Generation: generation, State: "planning"}); err != nil {
			return err
		}
	}
	in.PlanProposalPath = planbrief.ProposalFileName
	in.PlanGeneration = generation
	return nil
}

// publishPlanHandoff runs before save, so readiness and the index are part of
// the successful run's save. Failure preserves the transport for inspection.
func publishPlanHandoff(ctx context.Context, o Options, prep *preparation, result Result, run *harness.RunResult, log *slog.Logger) Result {
	if prep.promptInput.PlanProposalPath == "" || prep.run.Mode != "plan" || !run.Succeeded() {
		return result
	}
	fail := func(err error) Result {
		result.Err = fmt.Errorf("publish managed plan: %w", err)
		log.Error("managed plan publication blocked", slog.Any("error", result.Err))
		return result
	}
	root, err := os.OpenRoot(prep.worktree.Path())
	if err != nil {
		return fail(err)
	}
	defer root.Close()
	outcomeData, err := readPlanArtifact(root, planbrief.OutcomeFileName, planbrief.MaxOutcomeBytes)
	if err != nil {
		return fail(fmt.Errorf("current Plan outcome required: %w", err))
	}
	outcome, err := planbrief.ParseOutcome(outcomeData)
	if err != nil {
		return fail(err)
	}
	if outcome.Parent != prep.ref.String() || outcome.Generation != prep.promptInput.PlanGeneration || outcome.RunID != prep.run.ID || outcome.Attempt != prep.promptInput.PlanAttempt {
		return fail(fmt.Errorf("plan outcome does not match current parent, generation, run and attempt"))
	}
	if outcome.Outcome == "discussion" {
		if _, err := root.Lstat(planbrief.ProposalFileName); !errors.Is(err, os.ErrNotExist) {
			return fail(fmt.Errorf("discussion must not supply a proposal: %w", errors.Join(err, errors.New("contradictory plan transport"))))
		}
		data, err := readPlanArtifact(root, filepath.FromSlash(prep.promptInput.PlanContextPath), planbrief.MaxContextBytes)
		if err != nil {
			return fail(err)
		}
		working, err := planbrief.ParseWorkingContext(data)
		if err != nil {
			return fail(err)
		}
		if working.Parent != prep.ref.String() || working.Branch != prep.session.Branch || working.Feature != prep.session.FeatureSlug {
			return fail(fmt.Errorf("planning context identity does not match admitted task, branch and feature"))
		}
		if err := root.Remove(planbrief.OutcomeFileName); err != nil {
			return fail(err)
		}
		return result
	}
	data, err := readPlanArtifact(root, planbrief.ProposalFileName, planbrief.MaxProposalBytes)
	if err != nil {
		return fail(err)
	}
	proposal, err := planbrief.ParseProposal(data)
	if err != nil {
		return fail(err)
	}
	dir, ok := locateSessionDir(prep, log)
	if !ok {
		return fail(fmt.Errorf("plan brief session missing"))
	}
	// Legacy files are not converted or deleted. A new managed proposal must
	// not create a second editable story specification beside the issues.
	for _, story := range proposal.Stories {
		storyPath := filepath.Join(filepath.FromSlash(dir), story.Key+".md")
		if _, e := root.Lstat(storyPath); e == nil {
			return fail(fmt.Errorf("story %s: per-story Markdown files are unsupported in new managed plans; no legacy conversion is performed", story.Key))
		} else if !errors.Is(e, os.ErrNotExist) {
			return fail(e)
		}
	}
	// Validate and pin the index destination before any forge side effects.
	indexFile, err := openPlanIndex(root, filepath.FromSlash(dir))
	if err != nil {
		return fail(err)
	}
	defer indexFile.dir.Close()
	forge, ok := o.SubIssueCreator.(PlanForge)
	if !ok {
		return fail(fmt.Errorf("managed plan forge unavailable"))
	}
	renderer, ok := o.SubIssueRenderer.(CompleteStoryRenderer)
	if !ok {
		return fail(fmt.Errorf("complete story renderer unavailable"))
	}
	bot, err := o.DB.GetIdentity(ctx, prep.ref.Forge, store.RoleBot)
	if err != nil {
		return fail(err)
	}
	// Parsing and index validation must not allow a changed transport to be
	// published under evidence from the earlier descriptor.
	for _, transport := range []struct {
		path    string
		content []byte
	}{{planbrief.OutcomeFileName, outcomeData}, {planbrief.ProposalFileName, data}} {
		current, err := readPlanArtifact(root, transport.path, planbrief.MaxProposalBytes)
		if err != nil {
			return fail(err)
		}
		if !bytes.Equal(current, transport.content) {
			return fail(fmt.Errorf("plan transport changed before publication"))
		}
	}
	index, err := PublishPlan(ctx, PublicationOptions{DB: o.DB, Forge: forge, Renderer: renderer, Parent: prep.ref, Generation: prep.promptInput.PlanGeneration, BotID: bot.UserID}, proposal)
	if err != nil {
		return fail(err)
	}
	if err := indexFile.replace(root, index); err != nil {
		return fail(err)
	}
	if err := root.Remove(planbrief.ProposalFileName); err != nil {
		return fail(err)
	}
	if err := root.Remove(planbrief.OutcomeFileName); err != nil {
		return fail(err)
	}
	prep.planCompleted = true
	return result
}

func markManagedPlanReady(ctx context.Context, o Options, prep *preparation, result Result, run *harness.RunResult) Result {
	if !prep.planCompleted || !run.Succeeded() || result.Err != nil {
		return result
	}
	if err := o.DB.SetManagedFeatureReady(ctx, prep.ref.String(), prep.promptInput.PlanGeneration, true); err != nil {
		result.Err = fmt.Errorf("record managed Plan readiness: %w", err)
		return result
	}
	const advice = "\n\nPlan is ready and remains selected. To begin approved implementation, comment `/implement <instruction>` with the work to perform. An ordinary reply continues Plan."
	run.ResultText += advice
	if result.Harness != nil {
		result.Harness.ResultText = run.ResultText
	}
	return result
}
