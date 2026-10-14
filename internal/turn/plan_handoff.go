package turn

import (
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
	if o.SubIssueRenderer == nil {
		return nil
	}
	return invalidateManagedPlanReadiness(ctx, o, ref)
}

// invalidateManagedPlanReadiness is shared by admitted graph and legacy Plan.
// Graph Plan owns its renderer and does not require the legacy renderer seam.
func invalidateManagedPlanReadiness(ctx context.Context, o Options, ref naming.Ref) error {
	feature, err := o.DB.GetManagedFeature(ctx, ref.String())
	if errors.Is(err, store.ErrNotFound) {
		if err := o.DB.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: ref.String(), Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}); err != nil {
			return err
		}
		return o.DB.BindFeatureContract(ctx, store.FeatureContract{Parent: ref.String(), Generation: 1, ContentVersion: 2, SubmissionVersion: 2})
	}
	if err != nil {
		return err
	}
	if feature.State != "planning" {
		return fmt.Errorf("user stories are locked after promotion to Implement")
	}
	contract, err := o.DB.GetFeatureContract(ctx, ref.String())
	if err != nil {
		return err
	}
	if err := o.DB.BindFeatureContract(ctx, contract); err != nil {
		return err
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
	if o.SubIssueRenderer == nil {
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
			fmt.Fprintf(&contextText, "Story %s; state %s (include completed stories with intent unchanged as read-only dependencies; new scope requires a successor); issue %s; database ID %d; expected_base %s\nTitle: %s\n%s\n\n", story.Key, story.State, displayIssue(ref, issue).Link(), issue.ID, planbrief.StoryHash(issue.Title, issue.Body), issue.Title, issue.Body)
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
	// A local factory failure recorded ahead of this call (AC-005: cleanup,
	// finalization or scoped submission recovery, attached in harvestStopped)
	// must block publication the same way it blocks save — the subprocess's
	// own success does not make the turn's overall outcome successful.
	if result.Err != nil || prep.promptInput.PlanProposalPath == "" || prep.run.Mode != "plan" || !run.Succeeded() {
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
	// Checked first, so the run that wrote a malformed or foreign context
	// reports it, instead of the next turn's preparation refusing it (#289).
	if prep.promptInput.PlanContextPath != "" {
		var refusal *planContextRefusal
		if _, err := readPlanningContext(root, prep.promptInput.PlanContextPath, prep.promptInput); errors.As(err, &refusal) {
			return fail(err)
		}
	}
	var recordedOutcome, recordedProposal, recordedWorking []byte
	var haveRecorded bool
	if prep.typedPlan != nil {
		recordedOutcome, recordedProposal, recordedWorking, haveRecorded = prep.typedPlan.recorded()
	}
	outcomeData := recordedOutcome
	if !haveRecorded {
		outcomeData, err = readPlanArtifact(root, planbrief.OutcomeFileName, planbrief.MaxOutcomeBytes)
		if err != nil {
			return fail(fmt.Errorf("current Plan outcome required: %w", err))
		}
	}
	outcome, err := planbrief.ParseOutcome(outcomeData)
	if err != nil {
		return fail(err)
	}
	if outcome.Parent != prep.ref.String() || outcome.Generation != prep.promptInput.PlanGeneration || outcome.RunID != prep.run.ID || outcome.Attempt != prep.promptInput.PlanAttempt {
		return fail(fmt.Errorf("plan outcome does not match current parent, generation, run and attempt"))
	}
	if outcome.Outcome == "discussion" {
		data := recordedWorking
		if !haveRecorded {
			if _, err := root.Lstat(planbrief.ProposalFileName); !errors.Is(err, os.ErrNotExist) {
				return fail(fmt.Errorf("discussion must not supply a proposal: %w", errors.Join(err, errors.New("contradictory plan transport"))))
			}
			data, err = readPlanArtifact(root, filepath.FromSlash(prep.promptInput.PlanContextPath), planbrief.MaxContextBytes)
			if err != nil {
				return fail(err)
			}
		}
		working, err := planbrief.ParseWorkingContext(data)
		if err != nil {
			return fail(err)
		}
		if err := checkPlanningIdentity(working, prep.ref.String(), prep.session.Branch, prep.session.FeatureSlug); err != nil {
			return fail(err)
		}
		if err := root.Remove(planbrief.OutcomeFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		return result
	}
	data := recordedProposal
	if !haveRecorded {
		data, err = readPlanArtifact(root, planbrief.ProposalFileName, planbrief.MaxProposalBytes)
		if err != nil {
			return fail(err)
		}
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
	renderer := o.SubIssueRenderer
	if renderer == nil {
		return fail(fmt.Errorf("complete story renderer unavailable"))
	}
	bot, err := o.DB.GetIdentity(ctx, prep.ref.Forge, store.RoleBot)
	if err != nil {
		return fail(err)
	}
	publication := PublicationOptions{DB: o.DB, Forge: forge, Renderer: renderer, Parent: prep.ref, Generation: prep.promptInput.PlanGeneration, BotID: bot.UserID}
	if prep.typedPlan != nil {
		publication.TypedContent = prep.typedPlan.content
		publication.TypedScope = prep.typedPlan.scope
	}
	index, err := PublishPlan(ctx, publication, proposal)
	if err != nil {
		return fail(err)
	}
	if err := indexFile.replace(root, index); err != nil {
		return fail(err)
	}
	if err := root.Remove(planbrief.ProposalFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	if err := root.Remove(planbrief.OutcomeFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
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
	if prep.typedPlan != nil {
		if err := o.DB.FinalizeTypedPlanPublication(ctx, prep.typedPlan.receiptID); err != nil {
			result.Err = fmt.Errorf("record typed Plan publication receipt: %w", errors.Join(err, o.DB.SetManagedFeatureReady(ctx, prep.ref.String(), prep.promptInput.PlanGeneration, false)))
			return result
		}
	}
	const advice = "\n\nPlan is ready and remains selected. To begin approved implementation, comment `/implement <instruction>` with the work to perform. An ordinary reply continues Plan."
	run.ResultText += advice
	if result.Harness != nil {
		result.Harness.ResultText = run.ResultText
	}
	return result
}
