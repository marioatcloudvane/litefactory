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
	contract, err := db.GetGenerationContract(ctx, parent, generation)
	if errors.Is(err, store.ErrNotFound) {
		current, e := db.GetFeatureContract(ctx, parent)
		if e == nil && current.ContentVersion == 2 {
			return nil, fmt.Errorf("typed generation contract unavailable")
		}
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return nil, e
		}
		return planbrief.AcceptanceCriteria(body)
	}
	if err != nil {
		return nil, err
	}
	if contract.ContentVersion == 1 {
		return planbrief.AcceptanceCriteria(body)
	}
	projection, err := db.GetSubmissionProjection(ctx, parent, generation, story, revision)
	if err != nil {
		return nil, fmt.Errorf("typed story projection unavailable: %w", err)
	}
	if projection.Body != body || planbrief.StoryHash(projection.Title, projection.Body) != revision {
		return nil, fmt.Errorf("typed story source revision differs from projection")
	}
	var content planbrief.TypedStory
	if err := planbrief.DecodeTypedContent(projection.Content, &content); err != nil {
		return nil, err
	}
	if content.Key != story || content.Title != projection.Title {
		return nil, fmt.Errorf("typed projection identity mismatch")
	}
	ids := make([]string, len(content.AcceptanceCriteria))
	seen := map[string]bool{}
	for i, c := range content.AcceptanceCriteria {
		if !planbrief.ValidCriterionID(c.ID) || seen[c.ID] {
			return nil, fmt.Errorf("invalid frozen criterion identity")
		}
		seen[c.ID] = true
		ids[i] = c.ID
	}
	if len(ids) < 1 || len(ids) > planbrief.MaxCriteria {
		return nil, fmt.Errorf("invalid frozen criterion count")
	}
	return ids, nil
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
