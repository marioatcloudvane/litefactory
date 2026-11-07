package daemon

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// CountBugStartHandOffsForTest wraps the start step's real hand-off with a
// counter, so a test can prove the step did not hand off its private trigger.
func CountBugStartHandOffsForTest(t testing.TB) *atomic.Int32 {
	n := &atomic.Int32{}
	original := bugStartHandOff
	bugStartHandOff = func(d *Daemon, ctx context.Context, repo store.Repo, session store.Session, f store.BugFiling, a store.BugFilingStartAttempt) (bool, error) {
		n.Add(1)
		return original(d, ctx, repo, session, f, a)
	}
	t.Cleanup(func() { bugStartHandOff = original })
	return n
}
