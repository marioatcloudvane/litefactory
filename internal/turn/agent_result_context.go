package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

func cloneResultContext(c agentresult.Context) agentresult.Context {
	c.PlanScope.Roles = slices.Clone(c.PlanScope.Roles)
	c.PlanScope.IssueRefs = slices.Clone(c.PlanScope.IssueRefs)
	c.EngineeringComplete = maps.Clone(c.EngineeringComplete)
	c.Stories = maps.Clone(c.Stories)
	for key, story := range c.Stories {
		story.Criteria = slices.Clone(story.Criteria)
		story.ExecutionRequired = slices.Clone(story.ExecutionRequired)
		story.Checks = maps.Clone(story.Checks)
		c.Stories[key] = story
	}
	return c
}

// resolveResultEngineeringContext loads exact revision-bound proof afresh for
// every validation. Provider prose and mutable launch flags never establish it.
func resolveResultEngineeringContext(ctx context.Context, db *store.DB, b store.ResultBinding, worktree *git.Worktree, generation int, base agentresult.Context) (agentresult.Context, []ResultEngineeringCheckpoint, error) {
	c := cloneResultContext(base)
	c.EngineeringComplete = map[string]bool{}
	var checkpoints []ResultEngineeringCheckpoint
	selected := make([]store.ResultStoryRevision, 0, len(c.Stories))
	for _, key := range slices.Sorted(maps.Keys(c.Stories)) {
		source := c.Stories[key]
		selected = append(selected, store.ResultStoryRevision{Story: key, Revision: source.Revision})
		source.SnapshotID, source.ImplementationDigest, source.SnapshotRetained = "", "", false
		source.Checks = map[string]agent.RetainedContributionCheck{}
		c.Stories[key] = source
		retained, err := db.FindResultEngineeringCheckpoint(ctx, b, generation, key, source.Revision)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return c, nil, fmt.Errorf("resolve retained checkpoint %s: %w", key, err)
		}
		event, err := agent.ParseStoryEvent(retained.Payload)
		if err != nil || event.Validate() != nil || event.Parent != b.Task || event.Generation != generation || event.Story != key || event.Revision != source.Revision || event.RunID != retained.RunID || event.Sequence != retained.Sequence || agent.StoryDigest(retained.Payload) != retained.Digest || event.Evidence == nil {
			return c, nil, fmt.Errorf("retained checkpoint %s has invalid attribution", key)
		}
		source.SnapshotID = event.Evidence.ImplementationDigest
		source.ImplementationDigest = event.Evidence.ImplementationDigest
		source.SnapshotRetained = true
		// Historical event-only evidence keeps its existing digest/test-name IDs.
		for _, check := range event.Evidence.Tests {
			source.Checks[check.Name] = agent.RetainedContributionCheck{Story: key, Revision: source.Revision, SnapshotID: source.SnapshotID, ImplementationDigest: check.ImplementationDigest, Executed: true, Passed: check.Passed, Retained: true}
		}
		receipt, err := db.ResultCheckpointSubmission(ctx, b, retained.RunID, retained.Sequence)
		if err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrResultAuthority) {
			return c, nil, err
		}
		if err == nil {
			var contribution agent.Contribution
			if err := json.Unmarshal(receipt.Payload, &contribution); err != nil {
				return c, nil, err
			}
			if contribution.Engineering == nil || receipt.Story != key || receipt.Revision != source.Revision {
				return c, nil, fmt.Errorf("retained contribution %s has invalid attribution", key)
			}
			snapshot, err := db.GetSubmissionSnapshot(ctx, retained.RunID, contribution.Engineering.SnapshotID)
			if err != nil {
				return c, nil, err
			}
			if snapshot.Parent != b.Task || snapshot.Generation != generation || snapshot.Story != key || snapshot.Revision != source.Revision || snapshot.Digest != source.ImplementationDigest || snapshot.Attempt != receipt.Attempt {
				return c, nil, fmt.Errorf("retained snapshot %s has invalid attribution", key)
			}
			source.SnapshotID = snapshot.Reference
			source.Checks = map[string]agent.RetainedContributionCheck{}
			for _, id := range contribution.Engineering.CheckIDs {
				check, err := db.GetSubmissionCheck(ctx, retained.RunID, id)
				if err != nil {
					return c, nil, err
				}
				var fact agent.StoryCheck
				if err := json.Unmarshal(check.Result, &fact); err != nil {
					return c, nil, err
				}
				if check.SnapshotRef != snapshot.Reference || check.Attempt != receipt.Attempt || fact.ImplementationDigest != snapshot.Digest {
					return c, nil, fmt.Errorf("retained check %s has invalid attribution", id)
				}
				source.Checks[id] = agent.RetainedContributionCheck{Story: key, Revision: source.Revision, SnapshotID: snapshot.Reference, ImplementationDigest: fact.ImplementationDigest, Executed: check.Executed, Passed: check.Passed, Retained: true}
			}
		}
		c.Stories[key] = source
		c.EngineeringComplete[key] = true
		checkpoints = append(checkpoints, ResultEngineeringCheckpoint{Story: key, RunID: retained.RunID, Sequence: retained.Sequence})
	}
	c.RequireAggregate = true
	c.AggregateValidator = ResultAggregateValidator(ctx, db, b, worktree, selected)
	return c, checkpoints, nil
}
