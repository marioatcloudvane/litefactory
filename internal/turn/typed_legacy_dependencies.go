package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// historicalLegacyKeys identifies only completed children retained by an explicit
// upgrade. They remain opaque read-only dependencies, never synthetic criteria.
func historicalLegacyKeys(ctx context.Context, db *store.DB, parent string, generation int) (map[string]store.ManagedStory, error) {
	out := map[string]store.ManagedStory{}
	u, err := db.GetContractUpgrade(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if u.State != "completed" || u.TargetGeneration > generation {
		return out, nil
	}
	old, err := db.ListManagedStories(ctx, parent, u.SourceGeneration)
	if err != nil {
		return nil, err
	}
	for _, story := range old {
		if story.State == "completed" {
			out[story.Key] = story
		}
	}
	return out, nil
}

func validateTypedPlanWithHistory(p planbrief.TypedPlan, scope planbrief.ContentScope, history map[string]planbrief.StoryProposal) error {
	pruned := p
	pruned.Stories = slices.Clone(p.Stories)
	for i, s := range p.Stories {
		if _, ok := history[s.Key]; ok {
			return fmt.Errorf("completed legacy dependency %s is supplied by the factory; omit it from typed contributions", s.Key)
		}
		pruned.Stories[i].Dependencies.StoryKeys = []string{}
		seen := map[string]bool{}
		for _, dep := range s.Dependencies.StoryKeys {
			if seen[dep] || dep == s.Key {
				return fmt.Errorf("story %s has duplicate or self dependency", s.Key)
			}
			seen[dep] = true
			if _, completed := history[dep]; !completed {
				pruned.Stories[i].Dependencies.StoryKeys = append(pruned.Stories[i].Dependencies.StoryKeys, dep)
			}
		}
	}
	return pruned.Validate(scope)
}

func parseTypedPlanWithHistory(raw []byte, scope planbrief.ContentScope, history map[string]planbrief.StoryProposal) (planbrief.TypedPlan, error) {
	if len(history) == 0 {
		return planbrief.ParseTypedPlan(raw, scope)
	}
	var p planbrief.TypedPlan
	if len(raw) > planbrief.MaxProposalBytes {
		return p, fmt.Errorf("typed Plan exceeds transport budget")
	}
	if err := planbrief.DecodeTypedContent(raw, &p); err != nil {
		return p, err
	}
	return p, validateTypedPlanWithHistory(p, scope, history)
}

func renderTypedPlanWithHistory(p planbrief.TypedPlan, scope planbrief.ContentScope, history map[string]planbrief.StoryProposal) ([]string, error) {
	if len(history) == 0 {
		return report.RenderTypedPlan(p, scope)
	}
	if err := validateTypedPlanWithHistory(p, scope, history); err != nil {
		return nil, err
	}
	bodies := make([]string, len(p.Stories))
	for i, s := range p.Stories {
		body, _, err := report.RenderTypedStoryParts(s, scope)
		if err != nil {
			return nil, err
		}
		bodies[i] = body
	}
	return bodies, nil
}

func normalizeTypedUpdatesWithHistory(ctx context.Context, db *store.DB, parent string, generation int, existing []store.ManagedStory, raw []byte, scope planbrief.ContentScope, clarifications map[string]map[string]string, history map[string]planbrief.StoryProposal) ([]byte, error) {
	if len(history) == 0 {
		return normalizeTypedUpdates(ctx, db, parent, generation, existing, raw, scope, clarifications)
	}
	p, err := parseTypedPlanWithHistory(raw, scope, history)
	if err != nil {
		return nil, err
	}
	deps := map[string][]string{}
	for i, s := range p.Stories {
		deps[s.Key] = s.Dependencies.StoryKeys
		p.Stories[i].Dependencies.StoryKeys = []string{}
		for _, dep := range s.Dependencies.StoryKeys {
			if _, done := history[dep]; !done {
				p.Stories[i].Dependencies.StoryKeys = append(p.Stories[i].Dependencies.StoryKeys, dep)
			}
		}
	}
	pruned, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeTypedUpdates(ctx, db, parent, generation, existing, pruned, scope, clarifications)
	if err != nil {
		return nil, err
	}
	if err := planbrief.DecodeTypedContent(normalized, &p); err != nil {
		return nil, err
	}
	for i := range p.Stories {
		p.Stories[i].Dependencies.StoryKeys = deps[p.Stories[i].Key]
	}
	return json.Marshal(p)
}

func historicalBody(story planbrief.StoryProposal, parent string) string {
	marker := "<!-- lf:story task=" + parent + " story=" + story.Key + " -->\n\n"
	return strings.TrimPrefix(story.Body, marker)
}
