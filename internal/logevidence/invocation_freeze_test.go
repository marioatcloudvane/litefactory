package logevidence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestReplacementServiceKeepsOriginalDiscoveryCutoff(t *testing.T) {
	f := newEvidenceFixture(t)
	session, err := f.db.GetOrCreateSession(t.Context(), f.repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), session.ID, 1, "active-segment", "claude")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, "active.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"result\",\"result\":\"late finish\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	active, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "claude", StartedAt: time.Now().Add(-time.Hour), LogPath: func(int) string { return path }})
	if err != nil {
		t.Fatal(err)
	}
	prior := f.run(t, f.repo, 2, "{\"type\":\"result\",\"result\":\"prior\"}\n", false)
	budget, err := NewBudget(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}}
	original := f.service(t, scope, budget)
	if _, err := f.db.FinishRun(t.Context(), active.ID, store.RunOutcome{Status: store.RunSucceeded, FinishedAt: original.cutoff.Add(time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	later := f.run(t, f.repo, 3, "{\"type\":\"result\",\"result\":\"late creation\"}\n", false)
	replacement := f.service(t, scope, budget)
	if !replacement.cutoff.Equal(original.cutoff) || replacement.highWater != original.highWater {
		t.Fatal("replacement refreshed invocation discovery boundary")
	}
	b := evidenceBroker(t, replacement)
	listed := requireEvidenceCall(t, b, "runs_list", ListRequest{})
	if len(listed.Runs) != 1 || listed.Runs[0].RunID != prior.ID {
		t.Fatalf("replacement admitted later runs: %+v", listed.Runs)
	}
	for _, id := range []int64{active.ID, later.ID} {
		result := requireEvidenceCall(t, b, "run_logs_read", ReadRequest{RunID: id})
		if result.Status != "unavailable" || len(result.Records) != 0 {
			t.Fatalf("later run %d became readable: %+v", id, result)
		}
	}
}

func TestReplacementServiceCannotWidenOrReviveInvocation(t *testing.T) {
	f := newEvidenceFixture(t)
	run := f.run(t, f.repo, 1, "{\"type\":\"result\",\"result\":\"prior\"}\n", false)
	budget, err := NewBudget(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	owner, cancel := context.WithCancel(t.Context())
	defer cancel()
	redactor := redact.New(redact.Options{Environ: []string{}})
	scope := Scope{RepositoryIDs: []int64{f.repo.ID}, RunIDs: []int64{run.ID}}
	options := Options{Store: f.db, RunsRoot: f.root, Scope: scope, Budget: budget, Redactor: redactor}
	original, err := New(owner, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	widened := options
	widened.Scope = Scope{RepositoryIDs: []int64{f.repo.ID}}
	if service, err := New(t.Context(), widened); err == nil {
		_ = service.Close()
		t.Fatal("replacement widened original grant")
	}
	moved := options
	moved.RunsRoot = t.TempDir()
	if service, err := New(t.Context(), moved); err == nil {
		_ = service.Close()
		t.Fatal("replacement changed original evidence root")
	}
	replacement, err := New(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
	// A separately alive caller context cannot revive a canceled invocation owner.
	cancel()
	if replacement.authorized(t.Context()) {
		t.Fatal("replacement ignored original owner cancellation")
	}
	revived, err := New(t.Context(), options)
	if err == nil {
		defer revived.Close()
		if revived.authorized(t.Context()) {
			t.Fatal("replacement revived canceled original owner")
		}
	}
}
