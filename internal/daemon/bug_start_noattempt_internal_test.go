package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// TestBugStartNoAttemptPhaseFollowsStoppedRule reads the no-attempt stopped
// start through the store's evidence: the private start trigger admitted the
// Bug at the start node and the one run it launched finished before any node
// attempt. That is the state the runtime restarts from a fresh owner reply,
// so the start step classifies it S2r; with no run at all it stays S1.
func TestBugStartNoAttemptPhaseFollowsStoppedRule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		withRun bool
		want    bugStartPhase
	}{
		{name: "finished run", withRun: true, want: bugStartS2r},
		{name: "no run", want: bugStartS1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newBugStartFixture(t)
			s := fx.session(t)
			trigger := store.BugStartTrigger(1)
			admitted := fx.admitBug(t, s.ID, trigger)
			if tc.withRun {
				fx.run(t, s.ID, trigger, store.RunFailed, true)
			}
			ev, err := fx.db.BugStartEvidence(t.Context(), s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !ev.Admitted || ev.Instance.ID != admitted.Instance.ID || ev.AdmissionTrigger != trigger || ev.Instance.CurrentNode != "conversation" {
				t.Fatalf("admission evidence: %+v", ev)
			}
			if len(ev.Attempts) != 0 || len(ev.Transitions) != 0 || ev.HasDispatch || ev.HasWait || ev.HasComment {
				t.Fatalf("not the no-attempt shape: %+v", ev)
			}
			if got := len(ev.Runs) == 1; got != tc.withRun {
				t.Fatalf("runs=%+v", ev.Runs)
			}
			if ev.Stopped != tc.withRun {
				t.Fatalf("Stopped=%t", ev.Stopped)
			}
			if got := bugStartPhaseOf(ev); got != tc.want {
				t.Fatalf("phase=%d want %d", got, tc.want)
			}
		})
	}
}
