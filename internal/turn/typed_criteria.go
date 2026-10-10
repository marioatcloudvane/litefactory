package turn

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// storyCriterionIDs dispatches on the immutable generation content contract.
// A typed generation requires the exact issue-derived projection; prose is never
// a fallback when that projection is absent or stale.
func storyCriterionIDs(ctx context.Context, db *store.DB, parent string, generation int, story, revision, body string) ([]string, error) {
	ids, _, err := storyCriteria(ctx, db, parent, generation, story, revision, body)
	return ids, err
}

// storyCriteria returns the frozen criterion IDs and, in the same order, those
// whose frozen text requires an executed passing check. The marker is read only
// from the frozen criterion text, never from agent output.
func storyCriteria(ctx context.Context, db *store.DB, parent string, generation int, story, revision, body string) (ids, executionRequired []string, err error) {
	contract, err := db.GetGenerationContract(ctx, parent, generation)
	if errors.Is(err, store.ErrNotFound) {
		current, e := db.GetFeatureContract(ctx, parent)
		if e == nil && current.ContentVersion == 2 {
			return nil, nil, fmt.Errorf("typed generation contract unavailable")
		}
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return nil, nil, e
		}
		return legacyStoryCriteria(body)
	}
	if err != nil {
		return nil, nil, err
	}
	if contract.ContentVersion == 1 {
		return legacyStoryCriteria(body)
	}
	projection, err := db.GetSubmissionProjection(ctx, parent, generation, story, revision)
	if err != nil {
		return nil, nil, fmt.Errorf("typed story projection unavailable: %w", err)
	}
	if projection.Body != body || planbrief.StoryHash(projection.Title, projection.Body) != revision {
		return nil, nil, fmt.Errorf("typed story source revision differs from projection")
	}
	var content planbrief.TypedStory
	if err := planbrief.DecodeTypedContent(projection.Content, &content); err != nil {
		return nil, nil, err
	}
	if content.Key != story || content.Title != projection.Title {
		return nil, nil, fmt.Errorf("typed projection identity mismatch")
	}
	ids = make([]string, len(content.AcceptanceCriteria))
	seen := map[string]bool{}
	for i, c := range content.AcceptanceCriteria {
		if !planbrief.ValidCriterionID(c.ID) || seen[c.ID] {
			return nil, nil, fmt.Errorf("invalid frozen criterion identity")
		}
		seen[c.ID] = true
		ids[i] = c.ID
		if planbrief.RequiresExecution(c.Text) {
			executionRequired = append(executionRequired, c.ID)
		}
	}
	if len(ids) < 1 || len(ids) > planbrief.MaxCriteria {
		return nil, nil, fmt.Errorf("invalid frozen criterion count")
	}
	return ids, executionRequired, nil
}

func legacyStoryCriteria(body string) (ids, executionRequired []string, err error) {
	ids, texts, err := planbrief.AcceptanceCriteriaText(body)
	if err != nil {
		return nil, nil, err
	}
	for i, id := range ids {
		if planbrief.RequiresExecution(texts[i]) {
			executionRequired = append(executionRequired, id)
		}
	}
	return ids, executionRequired, nil
}

// projectStoryAcceptance preserves legacy projection while typed decisions use
// the immutable criterion IDs retained for their exact approved revision.
func projectStoryAcceptance(ctx context.Context, db *store.DB, decision store.AcceptanceDecision, live string, checked map[string]bool) (string, error) {
	contract, err := db.GetGenerationContract(ctx, decision.Parent, decision.Generation)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	ids, err := storyCriterionIDs(ctx, db, decision.Parent, decision.Generation, decision.Story, decision.Revision, decision.FrozenBody)
	if err != nil {
		return "", err
	}
	if contract.ContentVersion == 2 {
		return planbrief.ProjectTypedAcceptance(decision.FrozenBody, live, ids, checked)
	}
	return planbrief.ProjectAcceptance(decision.FrozenBody, live, checked)
}
