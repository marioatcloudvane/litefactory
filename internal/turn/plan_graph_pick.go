package turn

import (
	"fmt"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// planSetStage is the stage that proposes a story set. It is about no story, so
// its pick carries no story key.
const planSetStage = "set"

// planPick is the factory's choice of the next Plan work. Stage is empty when
// nothing is eligible; Key is empty at the set stage, which is about no story.
type planPick struct {
	Key, Stage string
	// Create marks a promoted story that has no draft yet.
	Create bool
}

// pickPlanStep chooses the next work from saved Plan state alone, so the same
// state always yields the same pick and a pass can ask again after every draft.
//
// A story task passes its own story key and is offered only that story's next
// stage, by the one upstream-approval rule every stage obeys. The parent passes
// no key and issued, the stories that already have an issue and so a task of
// their own: it never picks one of them, and keeps only the set stage and any
// story that has no issue yet. Of those, promoted stories owed a first draft
// come first, in stable topological order: dependencies before dependants,
// displayed order among the ready.
func pickPlanStep(s planbrief.PlanState, story string, issued map[string]bool) (planPick, error) {
	if story != "" {
		stage, create, ok := s.StoryNextStep(story)
		if !ok {
			return planPick{}, nil
		}
		return planPick{Key: story, Stage: stage, Create: create}, nil
	}
	drafted := func(key string) bool { return s.StoryDrafted(key) || issued[key] }
	if _, key, ok, err := s.StorySet.NextUndrafted(drafted); err != nil {
		return planPick{}, fmt.Errorf("select next promoted story: %w", err)
	} else if ok {
		return planPick{Key: key, Stage: "story", Create: true}, nil
	}
	if len(s.Stories) == 0 {
		if s.StorySet != nil && s.StorySet.Promoted {
			// Promoted with keys not yet reserved: there is nothing to propose again.
			return planPick{}, nil
		}
		// No set yet, or a proposed set the owner has not promoted: the set stage
		// proposes it, or a changed one under a new revision. A scope with stories
		// never reaches it, and neither does a promoted set.
		return planPick{Stage: planSetStage}, nil
	}
	for _, key := range s.EligiblePlanStories() {
		if issued[key] {
			continue
		}
		for _, item := range s.Stories {
			if item.Key == key {
				return planPick{Key: item.Key, Stage: item.Stage}, nil
			}
		}
		return planPick{}, fmt.Errorf("eligible Plan story %q missing", key)
	}
	return planPick{}, nil
}
