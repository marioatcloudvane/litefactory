package planbrief

import (
	"maps"
	"slices"
)

// SetEffectHold retains independent unknown outcomes even if a newer request
// completes. A stable representative supports existing single-request consumers.
func (s *PlanStoryState) SetEffectHold(request, cause string) {
	s.EffectHolds = maps.Clone(s.EffectHolds)
	if s.EffectHolds == nil {
		s.EffectHolds = map[string]string{}
	}
	if s.EffectRequest != "" && s.EffectHold != "" {
		s.EffectHolds[s.EffectRequest] = s.EffectHold
	}
	s.EffectHolds[request] = cause
	s.selectEffectHold()
}

// ClearEffectHold removes only the named request cause, preserving unrelated debt.
func (s *PlanStoryState) ClearEffectHold(request string) {
	s.EffectHolds = maps.Clone(s.EffectHolds)
	if s.EffectRequest != "" && s.EffectHold != "" {
		if s.EffectHolds == nil {
			s.EffectHolds = map[string]string{}
		}
		s.EffectHolds[s.EffectRequest] = s.EffectHold
	}
	delete(s.EffectHolds, request)
	if s.EffectRequest == request {
		s.EffectHold = ""
	}
	if len(s.EffectHolds) > 0 {
		s.selectEffectHold()
	}
}

func (s *PlanStoryState) selectEffectHold() {
	keys := slices.Sorted(maps.Keys(s.EffectHolds))
	if len(keys) == 0 {
		return
	}
	s.Effect = "uncertain"
	s.EffectRequest = keys[0]
	s.EffectHold = s.EffectHolds[keys[0]]
}
