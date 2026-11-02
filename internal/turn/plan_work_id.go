package turn

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// PlanWorkID is the identity of one piece of factory-started Plan work. It is a
// pure function of what the work is, so the same work is the same schedule: a
// stage that failed is not started again until the owner replies, and work whose
// state moved on is new work. The band keeps it clear of every forge comment id.
func PlanWorkID(kind string, parts ...string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind + "\x00" + strings.Join(parts, "\x00")))
	return int64(h.Sum64()&(1<<56-1)) | 1<<56
}

// StoryWorkID names the work a story task has in the saved Plan state, if any:
// the next stage it may run, or an effect it has to reconcile. One rule decides
// it for the offer and for the run, so a story is offered exactly when its run
// would find something to do.
func StoryWorkID(state planbrief.PlanState, key string) (int64, bool) {
	return StoryWorkIDAfter(state, key, false)
}

// StoryWorkIDAfter is StoryWorkID for a story whose latest Plan agent attempt
// may have failed on an upstream story (upstreamFailed). Only then does the
// identity also name each dependency's candidate and effect, so the stage is
// offered again by itself once an upstream's next candidate or effect lands,
// and not before. Every other story keeps the plain identity, so a stage that
// failed for any other reason waits for the owner's reply as before.
func StoryWorkIDAfter(state planbrief.PlanState, key string, upstreamFailed bool) (int64, bool) {
	if stage, create, ok := state.StoryNextStep(key); ok {
		candidate := ""
		var deps []string
		for _, item := range state.Stories {
			if item.Key == key {
				candidate, deps = item.Candidate, item.Draft.Dependencies.StoryKeys
			}
		}
		parts := []string{key, stage, candidate, strconv.FormatBool(create)}
		if upstreamFailed {
			for _, dep := range deps {
				for _, up := range state.Stories {
					if up.Key == dep {
						parts = append(parts, "upstream", dep, up.Candidate, up.Effect, up.EffectRequest)
					}
				}
			}
		}
		return PlanWorkID("story", parts...), true
	}
	// Explicit effect holds need resolution through retained recovery, not a
	// scheduled turn. Other waits must still admit unheld reconciliation debt.
	for _, item := range state.Stories {
		if item.Key == key && item.Active() && item.Effect != "" && item.Effect != "applied" && item.EffectHold == "" && len(item.EffectHolds) == 0 {
			return PlanWorkID("effect", key, item.EffectRequest), true
		}
	}
	return 0, false
}
