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
// Promoted stories owed a first draft come first, in stable topological order:
// dependencies before dependants, displayed order among the ready. Only then
// does ordinary eligibility apply, which is what picks later stages of drafted
// stories and revisions the owner asked for.
func pickPlanStep(s planbrief.PlanState) (planPick, error) {
	if _, key, ok, err := s.NextSetStory(); err != nil {
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
	eligible := s.EligiblePlanStories()
	if len(eligible) == 0 {
		return planPick{}, nil
	}
	for _, item := range s.Stories {
		if item.Key == eligible[0] {
			return planPick{Key: item.Key, Stage: item.Stage}, nil
		}
	}
	return planPick{}, fmt.Errorf("eligible Plan story %q missing", eligible[0])
}
