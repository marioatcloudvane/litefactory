package planbrief

import (
	"errors"
	"fmt"
	"slices"
)

// ErrPlanStorySetMissing refuses a promotion when no set was ever proposed.
var ErrPlanStorySetMissing = errors.New("there is no proposed story set to promote")

// ErrPlanStorySetStaleRevision refuses a promotion bound to a revision other than
// the one currently proposed: the set changed after the owner's review, so the
// approval covers a different set than the one now shown.
var ErrPlanStorySetStaleRevision = errors.New("the approval names a story set revision that is not the current one")

// ErrPlanStorySetRevisionMismatch reports a saved set whose content no longer
// hashes to its recorded revision, which promotion never accepts.
var ErrPlanStorySetRevisionMismatch = errors.New("the saved story set does not match its recorded revision")

// NextStoryKey is the factory-reserved semantic key of the next story of this
// Plan. Reservation is monotonic over every story ever saved, retired ones
// included, so a key is never reused and later additions continue one allocator.
func (s *PlanState) NextStoryKey() string {
	return fmt.Sprintf("story-%03d", len(s.Stories)+1)
}

// PromoteStorySet promotes the proposed set for the exact revision the owner
// approved. It reserves one story key per entry in displayed order, records the
// temporary-id-to-key mapping, and saves each reserved story with its
// dependencies translated to those keys, so every dependency key exists before
// any issue does and dependency-ordered drafting never renumbers a story.
//
// Promotion is bound to the current revision: a revision that is not the
// proposed one, or that names an older set, is refused with
// ErrPlanStorySetStaleRevision. Promoting the already-promoted revision again
// changes nothing and reports changed false, so a repeat, a lost reply and a
// restart never allocate twice. The authority to promote is the caller's: this
// function trusts that only authenticated owner evidence reached it.
func (s *PlanState) PromoteStorySet(revision string) (changed bool, err error) {
	set := s.StorySet
	if set == nil || set.Revision == "" || len(set.Entries) == 0 {
		return false, ErrPlanStorySetMissing
	}
	if revision != set.Revision {
		return false, fmt.Errorf("%w: it names %s but the proposed set is %s", ErrPlanStorySetStaleRevision, revision, set.Revision)
	}
	if set.Promoted {
		return false, nil
	}
	if PlanStorySetRevision(set.Entries) != set.Revision {
		return false, ErrPlanStorySetRevisionMismatch
	}
	if len(s.Stories) > 0 {
		return false, ErrPlanStorySetHasStories
	}
	if _, err = set.DraftOrder(); err != nil {
		return false, err
	}
	keys := make(map[string]string, len(set.Entries))
	for i, e := range set.Entries {
		keys[e.ID] = fmt.Sprintf("story-%03d", len(s.Stories)+i+1)
	}
	stories := make([]PlanStoryState, len(set.Entries))
	for i, e := range set.Entries {
		deps := make([]string, 0, len(e.DependsOn))
		for _, id := range e.DependsOn {
			key, ok := keys[id]
			if !ok {
				return false, fmt.Errorf("%w: %q depends on unknown %q", ErrPlanStorySetUnorderable, e.ID, id)
			}
			if !slices.Contains(deps, key) {
				deps = append(deps, key)
			}
		}
		key := keys[e.ID]
		stories[i] = PlanStoryState{Key: key, Stage: "story", Approvals: map[string]string{}, Draft: PlanDraft{Key: key, Dependencies: DependencyContent{StoryKeys: deps, IssueRefs: []string{}}}}
	}
	s.Stories = append(s.Stories, stories...)
	set.Keys = keys
	set.Promoted = true
	set.PromotedRevision = revision
	s.invalidateAssembly()
	return true, nil
}
