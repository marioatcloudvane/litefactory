package planbrief

import "errors"

// PlanLifecycle is an observed remote fact, independent of approval and retirement.
// Reopened remains true after a verified closed-to-open transition so historical
// closure requests cannot mistake a later open state for a failed first attempt.
type PlanLifecycle struct {
	State    string `json:"state,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Revision string `json:"revision,omitempty"`
	Reopened bool   `json:"reopened,omitempty"`
}

// Active is the shared membership rule for shaping, hashing and publication.
func (s PlanStoryState) Active() bool { return !s.Retired && s.Lifecycle.State != "closed" }

// Blocked includes debt on inactive stories: closure never settles an unknown effect.
func (s PlanStoryState) Blocked() bool {
	return len(s.EffectHolds) > 0 || s.EffectHold != "" || s.Hold != "" || s.LifecycleHold != "" || (s.Effect != "" && s.Effect != "applied")
}

// ObserveLifecycle applies only verified bindings. An unavailable or conflicting
// observation supplies hold without replacing the last verified lifecycle.
// The returned material-change flag avoids a new history row on every poll.
func (s *PlanState) ObserveLifecycle(key string, observed PlanLifecycle, hold string) (bool, error) {
	item, err := s.story(key)
	if err != nil {
		return false, err
	}
	if observed.State != "" && observed.State != "open" && observed.State != "closed" {
		return false, errors.New("invalid observed Plan lifecycle")
	}
	prior := item.Lifecycle
	if observed.State == "" {
		observed = prior
	}
	observed.Reopened = prior.Reopened || observed.Reopened || (prior.State == "closed" && observed.State == "open")
	if prior == observed && item.LifecycleHold == hold {
		return false, nil
	}
	lifecycleChanged := (prior.State != observed.State && (prior.State != "" || observed.State == "closed")) || prior.Reason != observed.Reason
	holdChanged := item.LifecycleHold != hold
	item.Lifecycle = observed
	item.LifecycleHold = hold
	if lifecycleChanged || holdChanged {
		// Invalidate follows dependencies, but lifecycle cannot clear another owner's
		// effect hold or replace its cause with the discovery outcome.
		holds := make(map[string]string, len(s.Stories))
		for _, st := range s.Stories {
			holds[st.Key] = st.Hold
		}
		s.Invalidate(key, "story", "")
		for i := range s.Stories {
			s.Stories[i].Hold = holds[s.Stories[i].Key]
		}
	}
	return true, nil
}
