package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/turn"
)

// failingNoteReporter refuses the factory note; every other report is unused.
type failingNoteReporter struct {
	Reporter
	calls int
}

func (r *failingNoteReporter) ReportBrainstormLaunchNote(context.Context, naming.Ref, int64, int64) (turn.Posted, error) {
	r.calls++
	return turn.Posted{}, errors.New("github is down")
}

// A factory note that cannot be posted stays pending in report delivery and
// does not fail the launch step.
func TestBrainstormLaunchNoteFailureStaysPending(t *testing.T) {
	f := newLaunchFixture(t)
	reporter := &failingNoteReporter{}
	f.d.reporter = reporter
	a := f.reserved(t)
	ref := naming.Ref{Forge: "github", Owner: "acme", Repo: "api", Issue: 10}
	tk := &task{session: f.session, ref: ref, log: f.d.log}
	f.d.postBrainstormLaunchNote(t.Context(), tk, a)
	if reporter.calls != 1 {
		t.Fatalf("note post attempts = %d, want 1", reporter.calls)
	}
	key := recoveryKey(f.session.ID, a.TriggerID()) + ":brainstorm-note"
	f.d.recoveries.mu.Lock()
	pending, ok := f.d.recoveries.items[key]
	f.d.recoveries.mu.Unlock()
	if !ok || pending.State != "pending" || pending.TriggerID != a.TriggerID() {
		t.Fatalf("note recovery = %+v (found %v), want pending for trigger %d", pending, ok, a.TriggerID())
	}
}
