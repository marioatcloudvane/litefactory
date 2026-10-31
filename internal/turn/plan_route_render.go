package turn

import (
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// predictedPlanRoute follows the captured control declarations with initial
// story-stage facts. It reports the selected activity and why, as a reason kind
// and the graph's fast-track size; it does not choose a route.
func predictedPlanRoute(p *graphexec.Profile, facts map[string]string) (route, reason, fastTrack string) {
	if p == nil {
		return "normal", "", ""
	}
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
			return "parallel", planbrief.PlanRouteReasonFastTrack, ""
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
			reason, fastTrack = "", ""
			switch condition.Field {
			case "story.size":
				reason, fastTrack = planbrief.PlanRouteReasonSize, condition.Condition
			case "story.sizing_valid":
				reason = planbrief.PlanRouteReasonSizing
			case "story.fallback":
				reason = planbrief.PlanRouteReasonGrowth
			}
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
	return "normal", reason, fastTrack
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
		route, reason, fastTrack := predictedPlanRoute(profile, planStoryFacts(state, entry.ID))
		reviews = append(reviews, planbrief.PlanRouteReview{Story: entry.ID, Size: entry.Size, SizeReason: entry.SizeReason, Route: route, Reason: reason, FastTrackSize: fastTrack})
	}
	return reviews
}

// finalRouteReviews explains each stored route from the captured graph and the
// original sizing. The stored current route stays authoritative: the graph's
// reason is shown only when its prediction agrees with it.
func (r *planGraphRuntime) finalRouteReviews(s planbrief.PlanState) []planbrief.PlanRouteReview {
	var profile *graphexec.Profile
	if r.definition != nil {
		profile = r.definition.Profile
	}
	reviews := s.RouteReviews()
	for i := range reviews {
		if reviews[i].Growth != "" {
			continue
		}
		route, reason, fastTrack := predictedPlanRoute(profile, planStoryFacts(s, reviews[i].Story))
		if route == reviews[i].Route {
			reviews[i].Reason, reviews[i].FastTrackSize = reason, fastTrack
		}
	}
	return reviews
}
