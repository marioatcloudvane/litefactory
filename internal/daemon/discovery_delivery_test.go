package daemon_test

import (
	"regexp"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

// Pending active work is durable before quota pressure arrives. Discovery may
// defer locally, but the actual client, SQLite queue and worker still deliver it.
func TestDiscoveryBudgetPendingRefinementDeliveryBypassesReserve(t *testing.T) {
	f := managedRefinementFixture(t)
	parent := f.sessionOf(testIssue)
	if err := f.db.AdmitStoryRefinement(t.Context(), store.StoryRefinement{SessionID: parent.ID, CommentID: 60, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: ownerID, ActorHandle: ownerLogin, Body: "refine the approved plan"}); err != nil {
		t.Fatal(err)
	}
	f.forge.mu.Lock()
	f.forge.quotaRemaining = 4000
	f.forge.mu.Unlock()
	f.settle()
	calls := f.turns.calls()
	if len(calls) != 1 || calls[0].Trigger.CommentID != 60 {
		t.Fatalf("queued delivery=%+v\n%s", calls, f.logs)
	}
	pending, err := f.db.ListPendingStoryRefinements(t.Context(), parent.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	cursor, err := f.db.StoryDiscoveryCursor(t.Context(), parent.ID, 200)
	if err != nil || cursor != 0 {
		t.Fatalf("deferred discovery cursor=%d err=%v", cursor, err)
	}
	if f.watermark() != 0 {
		t.Fatal("child delivery advanced parent watermark")
	}
	if !regexp.MustCompile(`requests_held_back_locally=[1-9][0-9]*`).MatchString(f.logs.String()) {
		t.Fatalf("fixture did not reach local denial\n%s", f.logs)
	}
}
