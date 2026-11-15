package planbrief_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// MAR-114 (#1521): a review wait stays open while the owner's revision runs.
// When that write is held by a (false) owner-edit conflict, the retry the
// failure notice offers must still reach the run that re-checks the hold. It
// was refused with "unresolved problem ... before approving", although it
// approves nothing. An approval stays refused while the hold is there.
func TestPlanWorkflow_OfferedRetryOnHeldStoryWithOpenWait(t *testing.T) {
	t.Parallel()
	const retry = "/plan Continue the unfinished step from the saved Plan state."
	held := func(t *testing.T) (planbrief.PlanState, planbrief.PlanOwnerReply) {
		s := workflowFixture(t, "a")
		r := workflowReply(t, &s, 0, 70)
		s.Stories[0].SetEffectHold("plan-request-2238-23555", "owner edits conflict with expected content revision")
		if s.Stories[0].Wait == nil || !s.Stories[0].Blocked() {
			t.Fatalf("fixture is not a held story with a wait: %+v", s.Stories[0])
		}
		return s, r
	}
	for _, body := range []string{retry, "Please try again."} {
		s, r := held(t)
		r.Body = body
		if reason := s.PlanReplyReason("a", body); reason != "" {
			t.Fatalf("%q refused: %s", body, reason)
		}
		ok, err := s.ApplyOwnerReply(r)
		if ok || err != nil || !slices.Contains(s.ConsumedReplies, r.ID) || s.Stories[0].Wait != nil || len(s.Stories[0].Approvals) != 0 {
			t.Fatalf("%q: ok=%v err=%v state=%+v", body, ok, err, s.Stories[0])
		}
		if !s.Stories[0].Blocked() {
			t.Fatal("feedback cleared the hold itself; only the run's re-check may")
		}
	}
	s, r := held(t)
	if reason := s.PlanReplyReason("a", r.Body); !strings.Contains(reason, "unresolved problem") {
		t.Fatalf("approval on a held story not refused: %q", reason)
	}
	if ok, err := s.ApplyOwnerReply(r); ok || err == nil || len(s.Stories[0].Approvals) != 0 {
		t.Fatalf("approval on a held story applied: ok=%v err=%v", ok, err)
	}
}
