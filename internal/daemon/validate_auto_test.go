package daemon

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

const validateAutoHead = "1111111111111111111111111111111111111111"

// finishImplement records a finished (or, with running, a resting) Implement
// coordinator instance on the parent and merges every story.
func (f *implementDaemonFixture) finishImplement(t *testing.T, status string) store.GraphInstance {
	t.Helper()
	for _, key := range []string{"story-001", "story-002", "story-003"} {
		f.move(key, store.ImplementRunning, store.ImplementDone)
		row := f.row(t, key)
		if _, err := f.d.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: implementDaemonParent, Generation: 1, Key: key, From: store.ImplementDone, To: store.ImplementMerged, ExpectedRevision: row.Revision, MergeCommit: validateAutoHead, At: implementDaemonClock}); err != nil {
			t.Fatal(err)
		}
	}
	f.move("story-004", store.ImplementWaiting, store.ImplementRunning)
	f.move("story-004", store.ImplementRunning, store.ImplementDone)
	row := f.row(t, "story-004")
	if _, err := f.d.db.TransitionImplementStory(t.Context(), store.ImplementTransition{Parent: implementDaemonParent, Generation: 1, Key: "story-004", From: store.ImplementDone, To: store.ImplementMerged, ExpectedRevision: row.Revision, MergeCommit: validateAutoHead, At: implementDaemonClock}); err != nil {
		t.Fatal(err)
	}
	pin, err := f.d.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("ab", 32), TeamSnapshotDigest: strings.Repeat("cd", 32)})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := f.d.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.parent.ID, DefinitionID: pin.ID, CurrentNode: "ready", StartedAt: implementDaemonClock})
	if err != nil {
		t.Fatal(err)
	}
	if status != store.GraphRunning {
		if err := f.d.db.SetGraphInstanceStatus(t.Context(), inst.ID, status, time.Time{}, implementDaemonClock); err != nil {
			t.Fatal(err)
		}
	}
	f.d.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	f.d.validateHead = func(context.Context, store.Session) (string, error) { return validateAutoHead, nil }
	return inst
}

func (f *implementDaemonFixture) offered(t *testing.T) []*candidate {
	t.Helper()
	f.byRepo[f.repo.ID].candidates = nil
	if err := f.d.graphValidateStarts(t.Context(), f.byRepo, map[int64]*candidate{}); err != nil {
		t.Fatal(err)
	}
	return f.byRepo[f.repo.ID].candidates
}

func TestValidateAutoStartOffersNothingWhileTheSettingIsOff(t *testing.T) {
	f := newImplementDaemonFixture(t)
	f.finishImplement(t, store.GraphSucceeded)
	f.d.validateAutoStart = false
	if got := f.offered(t); len(got) != 0 {
		t.Fatalf("manual is the default: %d offered", len(got))
	}
	if _, owed, err := f.d.validateAutoOwed(t.Context(), f.parent); err != nil || owed {
		t.Fatalf("owed=%v err=%v", owed, err)
	}
}

func TestValidateAutoStartOffersEachFinishedParentOncePerHead(t *testing.T) {
	f := newImplementDaemonFixture(t)
	f.finishImplement(t, store.GraphSucceeded)
	f.d.validateAutoStart = true
	got := f.offered(t)
	if len(got) != 1 || got[0].session.ID != f.parent.ID || !slices.Contains(got[0].sources, "validate_auto") {
		t.Fatalf("the finished parent is offered: %+v", got)
	}
	head, owed, err := f.d.validateAutoOwed(t.Context(), f.parent)
	if err != nil || !owed || head != validateAutoHead {
		t.Fatalf("head=%q owed=%v err=%v", head, owed, err)
	}

	// The entry is saved and the start attempted: the same head is not offered again.
	entry, _, err := f.d.db.EnsureValidateEntry(t.Context(), store.ValidateEntry{SessionID: f.parent.ID, Commit: head, Branch: "lf/1-x", Source: store.ValidateSourceAuto}, implementDaemonClock)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.offered(t); len(got) != 1 {
		t.Fatalf("a saved but not admitted entry is still owed after a restart: %d", len(got))
	}
	if err := f.d.db.AdmitValidateEntry(t.Context(), entry.ID, 5, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	if got := f.offered(t); len(got) != 0 {
		t.Fatalf("an admitted entry for the current head owes nothing: %d", len(got))
	}
	// A new head commit owes a new start.
	f.d.validateHead = func(context.Context, store.Session) (string, error) { return strings.Repeat("2", 40), nil }
	if got := f.offered(t); len(got) != 1 {
		t.Fatalf("a new head is a new result: %d", len(got))
	}
}

func TestValidateAutoStartWaitsForImplementToFinishWithEveryStoryMerged(t *testing.T) {
	f := newImplementDaemonFixture(t)
	f.d.validateAutoStart = true
	f.finishImplement(t, store.GraphRunning)
	if got := f.offered(t); len(got) != 0 {
		t.Fatalf("Implement still resting: %d offered", len(got))
	}
}

func TestValidateAutoStartIgnoresStoryWorkerSessions(t *testing.T) {
	f := newImplementDaemonFixture(t)
	f.finishImplement(t, store.GraphSucceeded)
	f.d.validateAutoStart = true
	worker, err := f.d.db.GetOrCreateSession(t.Context(), f.repo, 11)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := f.d.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("ab", 32), TeamSnapshotDigest: strings.Repeat("cd", 32)})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := f.d.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: worker.ID, DefinitionID: pin.ID, CurrentNode: "ready", StartedAt: implementDaemonClock})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.db.SetGraphInstanceStatus(t.Context(), inst.ID, store.GraphSucceeded, time.Time{}, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	ids, err := f.d.db.ListFinishedImplementParents(t.Context())
	if err != nil || !slices.Equal(ids, []int64{f.parent.ID}) {
		t.Fatalf("only the managed parent is a finished Implement parent: %v %v", ids, err)
	}
}

func TestValidateAutoIDIsOneIdentityPerParentAndHead(t *testing.T) {
	f := newImplementDaemonFixture(t)
	a := validateAutoID(f.parent, validateAutoHead)
	if a != validateAutoID(f.parent, validateAutoHead) || a == validateAutoID(f.parent, strings.Repeat("2", 40)) {
		t.Fatal("the same result is the same start; a new head is a new one")
	}
	if !strings.HasPrefix(turn.ValidateAutoPayload, "/validate ") {
		t.Fatal("the automatic start is the /validate command with a note")
	}
}
