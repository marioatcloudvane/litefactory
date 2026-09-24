package planbrief

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// MaxPlanStorySetEntries bounds one proposed story set. A parent that needs more
// is split, not squeezed into a longer list. The graph checker derives the
// creation cycle's loop bound from it, so the bound and the limit cannot drift.
const MaxPlanStorySetEntries = 12

// ErrPlanStorySetUnorderable reports a promoted set that still has stories to
// draft but none whose dependencies are drafted: a cycle or an unknown reference
// that shape validation should have refused before promotion.
var ErrPlanStorySetUnorderable = errors.New("promoted story set cannot be ordered for drafting")

// PlanStorySetEntry is one proposed story under a temporary identity that exists
// only within its set. Real story keys are reserved by the factory at promotion.
type PlanStorySetEntry struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Intent    string   `json:"intent"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// PlanStorySetState is the saved story set of one parent Plan. Every member is
// optional in saved state: a Plan saved before story sets existed carries none
// and still loads. Entries are kept in displayed order, which is also the order
// story keys are reserved in.
type PlanStorySetState struct {
	// Revision identifies the exact proposed set the owner reviews.
	Revision string `json:"revision"`
	// Entries are the proposed stories in displayed order.
	Entries []PlanStorySetEntry `json:"entries"`
	// Keys maps each temporary entry id to the story key reserved for it.
	Keys map[string]string `json:"keys,omitempty"`
	// Promoted reports that the owner promoted PromotedRevision.
	Promoted         bool   `json:"promoted,omitempty"`
	PromotedRevision string `json:"promoted_revision,omitempty"`
}

// Clone returns a copy sharing no slice or map. A nil set clones to nil.
func (s *PlanStorySetState) Clone() *PlanStorySetState {
	if s == nil {
		return nil
	}
	c := *s
	c.Entries = make([]PlanStorySetEntry, len(s.Entries))
	for i, e := range s.Entries {
		e.DependsOn = slices.Clone(e.DependsOn)
		c.Entries[i] = e
	}
	c.Keys = maps.Clone(s.Keys)
	return &c
}

// NextUndrafted selects the next promoted story to draft. drafted reports
// whether a story key already has a draft. Selection is a stable topological
// order over the drafts still owed: a story is ready once every story it depends
// on has a draft, so dependencies receive first drafts before their dependants,
// and displayed list order breaks ties among the ready stories. Predecessor
// approval is not consulted: a draft, not an approval, satisfies a dependency.
//
// It is a pure function of the set and the drafted predicate, so a caller may
// ask again after every draft and always gets the story a fresh pass would pick.
// ok is false when the set is absent, not promoted or has nothing left to draft.
func (s *PlanStorySetState) NextUndrafted(drafted func(key string) bool) (entry PlanStorySetEntry, key string, ok bool, err error) {
	if s == nil || !s.Promoted || len(s.Entries) == 0 {
		return PlanStorySetEntry{}, "", false, nil
	}
	done := make(map[string]bool, len(s.Entries))
	for _, e := range s.Entries {
		reserved := s.Keys[e.ID]
		if reserved == "" {
			return PlanStorySetEntry{}, "", false, fmt.Errorf("promoted story %q has no reserved key", e.ID)
		}
		done[e.ID] = drafted(reserved)
	}
	owed := false
	for _, e := range s.Entries {
		if done[e.ID] {
			continue
		}
		owed = true
		ready := true
		for _, dep := range e.DependsOn {
			met, known := done[dep]
			if !known {
				return PlanStorySetEntry{}, "", false, fmt.Errorf("%w: %q depends on unknown %q", ErrPlanStorySetUnorderable, e.ID, dep)
			}
			if !met {
				ready = false
			}
		}
		if ready {
			e.DependsOn = slices.Clone(e.DependsOn)
			return e, s.Keys[e.ID], true, nil
		}
	}
	if owed {
		return PlanStorySetEntry{}, "", false, ErrPlanStorySetUnorderable
	}
	return PlanStorySetEntry{}, "", false, nil
}

// DraftOrder returns the temporary entry ids in the order NextUndrafted would
// select them if every pick were drafted in turn. It ignores reserved keys, so
// a set can be ordered for display before promotion.
func (s *PlanStorySetState) DraftOrder() ([]string, error) {
	if s == nil {
		return nil, nil
	}
	picked := map[string]bool{}
	order := make([]string, 0, len(s.Entries))
	for len(order) < len(s.Entries) {
		progressed := false
		for _, e := range s.Entries {
			if picked[e.ID] {
				continue
			}
			ready := true
			for _, dep := range e.DependsOn {
				if !slices.ContainsFunc(s.Entries, func(o PlanStorySetEntry) bool { return o.ID == dep }) {
					return nil, fmt.Errorf("%w: %q depends on unknown %q", ErrPlanStorySetUnorderable, e.ID, dep)
				}
				ready = ready && picked[dep]
			}
			if ready {
				picked[e.ID] = true
				order = append(order, e.ID)
				progressed = true
				break
			}
		}
		if !progressed {
			return nil, ErrPlanStorySetUnorderable
		}
	}
	return order, nil
}

// StoryDrafted reports whether the saved state holds a draft candidate for key.
// A story whose key is reserved but which has no candidate is still owed a draft.
func (s *PlanState) StoryDrafted(key string) bool {
	for _, item := range s.Stories {
		if item.Key == key {
			return item.Candidate != ""
		}
	}
	return false
}

// NextSetStory selects the next promoted story still owed a first draft, reading
// draft presence from the saved story states. See PlanStorySetState.NextUndrafted.
func (s *PlanState) NextSetStory() (entry PlanStorySetEntry, key string, ok bool, err error) {
	return s.StorySet.NextUndrafted(s.StoryDrafted)
}

// PromotedStory is a promoted story that is still owed its first draft: the set
// entry it came from and the dependency keys promotion saved for it, already
// translated from temporary entry ids to reserved story keys.
type PromotedStory struct {
	Entry        PlanStorySetEntry
	Dependencies []string
}

// PromotedStory reports the promoted, not yet drafted story key names. ok is false
// for a key that is not from a promoted set, and for one that already has a draft,
// because a draft's dependencies are then the story's own content.
func (s *PlanState) PromotedStory(key string) (PromotedStory, bool) {
	set := s.StorySet
	if set == nil || !set.Promoted || s.StoryDrafted(key) {
		return PromotedStory{}, false
	}
	for _, e := range set.Entries {
		if set.Keys[e.ID] != key {
			continue
		}
		for _, item := range s.Stories {
			if item.Key == key {
				e.DependsOn = slices.Clone(e.DependsOn)
				return PromotedStory{Entry: e, Dependencies: slices.Clone(item.Draft.Dependencies.StoryKeys)}, true
			}
		}
	}
	return PromotedStory{}, false
}
