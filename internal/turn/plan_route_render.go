package turn

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// predictedPlanRoute follows the captured control declarations with initial
// story-stage facts. It reports the selected activity; it does not choose a route.
func predictedPlanRoute(p *graphexec.Profile, facts map[string]string) (string, string) {
	if p == nil {
		return "normal", "legacy graph"
	}
	evidence := []string{p.Digest}
	current := p.Start
	seen := map[string]bool{}
	for len(seen) <= len(p.Nodes) {
		if seen[current] {
			break
		}
		seen[current] = true
		n, ok := p.Node(current)
		if !ok || n.Terminal {
			break
		}
		if n.Action == graphexec.PlanParallelAction {
			return "parallel", strings.Join(evidence, "; ")
		}
		if n.Action != graphexec.ResearchIfAction && n.Action != graphexec.ResearchLoopAction {
			break
		}
		condition, err := graphexec.DecodeCondition(n.Action, n.Attrs, graphexec.PlanControlPolicy())
		if err != nil {
			break
		}
		var outcome string
		if condition.Field == "response" {
			outcome, err = condition.Evaluate("story", true)
		} else {
			outcome, err = condition.EvaluatePlanFacts(facts)
			evidence = append(evidence, fmt.Sprintf("%s %s=%s [%s]", n.Name, condition.Field, facts[condition.Field], outcome))
		}
		if err != nil {
			break
		}
		next, found := graphexec.Decide(p, n.Name, outcome)
		if !found {
			break
		}
		current = next
	}
	return "normal", strings.Join(evidence, "; ")
}

func (r *planGraphRuntime) setRouteReviews(set *planbrief.PlanStorySetState) []planbrief.PlanRouteReview {
	if set == nil {
		return nil
	}
	synthetic := set.Clone()
	synthetic.Keys = map[string]string{}
	for _, entry := range synthetic.Entries {
		synthetic.Keys[entry.ID] = entry.ID
	}
	state := planbrief.PlanState{StorySet: synthetic}
	var profile *graphexec.Profile
	if r.definition != nil {
		profile = r.definition.Profile
	}
	reviews := make([]planbrief.PlanRouteReview, 0, len(set.Entries))
	for _, entry := range set.Entries {
		route, basis := predictedPlanRoute(profile, planStoryFacts(state, entry.ID))
		reviews = append(reviews, planbrief.PlanRouteReview{Story: entry.ID, Size: entry.Size, SizeReason: entry.SizeReason, Route: route, Basis: basis})
	}
	return reviews
}
