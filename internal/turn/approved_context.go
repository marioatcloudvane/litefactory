package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// prepareApprovedContext reads persisted approval, never a live replacement body.
func prepareApprovedContext(ctx context.Context, opts Options, in *harness.PromptInput, runID int64) error {
	if in.Mode != "implement" {
		return nil
	}
	feature, err := opts.DB.GetManagedFeature(ctx, in.Ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if feature.State != "implementing" {
		return fmt.Errorf("managed stories are not approved for implementation")
	}
	revisions, err := opts.DB.ListApprovedStoryRevisions(ctx, feature.ParentTask, feature.Generation)
	if err != nil {
		return fmt.Errorf("read approved stories: %w", err)
	}
	bundle := &agent.ApprovedBundle{ParentTitle: in.IssueTitle, ParentBody: in.IssueBody, Parent: feature.ParentTask, RunID: runID, RunOrdinal: in.RunOrdinal, Generation: feature.Generation}
	for _, r := range revisions {
		bundle.Stories = append(bundle.Stories, agent.ApprovedStory{State: r.State, Key: r.Key, IssueNumber: r.IssueNumber, IssueID: r.IssueID, ParentID: r.ParentID, Source: fmt.Sprintf("https://github.com/%s/%s/issues/%d", in.Ref.Owner, in.Ref.Repo, r.IssueNumber), Title: r.Title, Body: r.Body, Revision: r.Hash, FetchedAt: r.FetchedAt.UTC().Format(time.RFC3339Nano), Dependencies: slices.Clone(r.Dependencies)})
	}
	selected := 0
	for _, story := range bundle.Stories {
		if story.State != "completed" {
			selected++
		}
	}
	if selected == 0 {
		if len(bundle.Stories) == 0 {
			return fmt.Errorf("managed implementation has no approved stories; no agent was launched")
		}
		// Completed children do not imply that the parent's aggregate checks or
		// save succeeded. A read-only aggregate attempt can recover that work
		// without selecting those children for implementation again.
		bundle.ParentRecovery = true
	}
	all, err := opts.DB.ListManagedStories(ctx, feature.ParentTask, feature.Generation)
	if err != nil {
		return fmt.Errorf("read completed dependency identities: %w", err)
	}
	for _, story := range all {
		if story.State == "completed" {
			bundle.CompletedDependencies = append(bundle.CompletedDependencies, agent.CompletedStory{Key: story.Key, IssueNumber: story.IssueNumber, IssueID: story.IssueID})
		}
	}
	if opts.StoryProgress != nil && !bundle.ParentRecovery {
		bundle.EventDirectory = fmt.Sprintf("%s/%d", agent.StoryEventsDirectory, runID)
	}
	in.ApprovedStories = bundle.Clone()
	return nil
}

// validateApprovedRelationships runs immediately before each attempt, including
// cold fallback. The remote body is compared for drift, never substituted.
func validateApprovedRelationships(ctx context.Context, opts Options, prep *preparation) (string, error) {
	bundle := prep.promptInput.ApprovedStories
	if bundle == nil {
		return "", nil
	}
	forge, ok := opts.SubIssueCreator.(PlanForge)
	if !ok {
		return "", fmt.Errorf("cannot validate approved story relationships: forge unavailable")
	}
	repo := github.RepoFromRef(prep.ref)
	var faults []error
	var drift []string
	for _, s := range bundle.Stories {
		live, err := forge.GetIssue(ctx, repo, s.IssueNumber)
		if err != nil {
			faults = append(faults, fmt.Errorf("approved story %s is unavailable: %w", s.Key, err))
			continue
		}
		parent, err := forge.GetParentIssue(ctx, repo, s.IssueNumber)
		if err != nil {
			faults = append(faults, fmt.Errorf("approved story %s is detached or its parent is unavailable: %w", s.Key, err))
			continue
		}
		if live.ID != s.IssueID || live.Number != s.IssueNumber || parent.ID != s.ParentID || parent.Number != prep.ref.Issue || (live.HTMLURL != "" && !strings.EqualFold(live.HTMLURL, s.Source)) {
			faults = append(faults, fmt.Errorf("approved story %s identity or parent changed; restore its relationship or explicitly replan", s.Key))
			continue
		}
		if planbrief.StoryHash(live.Title, live.Body) != s.Revision {
			drift = append(drift, fmt.Sprintf("Story %s (#%d) differs from approved revision %s; this attempt retains that revision.", s.Key, s.IssueNumber, s.Revision))
		}
	}
	return strings.Join(drift, "\n"), errors.Join(faults...)
}

// checkAttemptScope does not reload approval, so fallback retains this run's generation.
func checkAttemptScope(ctx context.Context, prep *preparation) error {
	if prep.validateScope == nil {
		return nil
	}
	drift, err := prep.validateScope(ctx)
	if drift != "" {
		prep.scopeDrift = drift
	}
	if err != nil {
		return fmt.Errorf("approved story dispatch blocked: %w", err)
	}
	return nil
}
