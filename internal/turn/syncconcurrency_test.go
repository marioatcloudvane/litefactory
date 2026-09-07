package turn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestConcurrentTurnsSyncIndependentlyOnOneClone is US-6: it does not build
// per-branch locking, it proves the existing per-clone [testGate] already
// gives every task branch an independent, correct sync under concurrency.
//
// Four issues share one registered clone and one gate, exactly the shape
// [internal/daemon]'s cloneGate is keyed for (see its doc comment's "Keyed on
// the clone, not the branch" section). Each issue's branch is cut first, one
// at a time, while the base is still where every branch started — nothing to
// sync yet. The base is then advanced once, so every branch is independently
// Behind by the same count, and all four issues' second turns are driven at
// once: each turn's own fetch sees the moved base, each syncBase merges base
// into its own branch, and each stubbed run adds a file named after its own
// issue number before committing and pushing.
//
// What would fail here if branch-keyed locking — or any second acquisition
// around the merge — were added on top of the existing model: the gate's
// acquisition count for the one registered clone would climb past
// 2 per turn (AC-6.1, AC-6.2), because a merge-specific acquisition is
// exactly the kind of "helpful" addition this story argues is unnecessary and
// would show up here as a number bigger than expected, not as a behavioural
// failure that is easy to miss.
func TestConcurrentTurnsSyncIndependentlyOnOneClone(t *testing.T) {
	t.Parallel()

	type issue struct {
		number int
		title  string
	}
	issues := []issue{
		{301, "Add request throttling"},
		{302, "Add audit logging"},
		{303, "Add feature flags"},
		{304, "Add background jobs"},
	}
	const behindCount = 2

	var f *fixture
	var mu sync.Mutex
	calls := make(map[int]int, len(issues))

	// The stub writes a file named after the issue and the call number, which
	// is what lets the assertions below tell "my branch got my own change and
	// no one else's" apart from "no error occurred".
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		mu.Lock()
		calls[req.Ref.Issue]++
		call := calls[req.Ref.Issue]
		mu.Unlock()

		name := fmt.Sprintf("issue-%d-call-%d.md", req.Ref.Issue, call)
		if err := os.WriteFile(filepath.Join(req.WorktreePath, name), []byte(name+"\n"), 0o600); err != nil {
			return nil, err
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.opts.AutoSyncBase = true

	gate, ok := f.opts.CloneGate.(*testGate)
	if !ok {
		t.Fatalf("fixture gate is %T, want *testGate", f.opts.CloneGate)
	}

	// Cut every branch sequentially, before the base moves, so every branch
	// starts from the same point and this setup phase is not itself part of
	// what is being measured for concurrency.
	sessions := make(map[int]store.Session, len(issues))
	for i, is := range issues {
		session := f.otherSession(is.number)
		sessions[is.number] = session

		req := f.requestFor(session, is.number, is.title, int64(9100+i))
		result := f.execute(req)
		if !result.Succeeded() || !result.Pushed {
			t.Fatalf("setup turn for issue %d did not push cleanly: %+v\nlogs:\n%s", is.number, result, f.logs)
		}
	}

	// Every branch above is now equally, independently Behind by the same
	// count once its own turn fetches.
	advanceSyncBase(t, f.repo, behindCount)

	before, overlapsBefore, deadlinedBefore := gate.report()

	results := make([]Result, len(issues))
	errs := make([]error, len(issues))
	var wg sync.WaitGroup
	for i, is := range issues {
		i, is := i, is
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := f.requestFor(sessions[is.number], is.number, is.title, int64(9200+i))
			results[i], errs[i] = Execute(context.Background(), f.opts, req)
		}()
	}
	wg.Wait()

	shas := make(map[string]int, len(issues))
	for i, is := range issues {
		if errs[i] != nil {
			t.Fatalf("issue %d: Execute: %v", is.number, errs[i])
		}
		result := results[i]
		if result.Disposition != DispositionRan {
			t.Errorf("issue %d: disposition = %v, reason = %q", is.number, result.Disposition, result.Reason)
		}
		if !result.Pushed {
			t.Errorf("issue %d: not pushed (err = %v, reason = %q)", is.number, result.Err, result.Reason)
		}

		runs, err := f.db.ListRuns(t.Context(), sessions[is.number].ID)
		if err != nil {
			t.Fatalf("issue %d: list runs: %v", is.number, err)
		}
		if len(runs) != 2 {
			t.Fatalf("issue %d: runs = %d, want 2 (setup + concurrent)", is.number, len(runs))
		}
		second := runs[1]
		switch {
		case second.SyncOutcome != store.SyncSynced:
			t.Errorf("issue %d: SyncOutcome = %q, want %q", is.number, second.SyncOutcome, store.SyncSynced)
		case second.SyncBehind != behindCount:
			t.Errorf("issue %d: SyncBehind = %d, want %d", is.number, second.SyncBehind, behindCount)
		case second.SyncCommitSHA == "":
			t.Errorf("issue %d: SyncCommitSHA is empty, want the merge commit sha", is.number)
		}
		shas[second.SyncCommitSHA]++

		// Cross-branch interference check: this issue's own branch on origin
		// carries its own concurrent-phase file and none of the others'.
		branch := result.Session.Branch
		if branch == "" {
			t.Fatalf("issue %d: result.Session.Branch is empty", is.number)
		}
		tree := f.repo.GitIn(f.repo.Origin, "ls-tree", "-r", "--name-only", branch)
		own := fmt.Sprintf("issue-%d-call-2.md", is.number)
		if !strings.Contains(tree, own) {
			t.Errorf("issue %d: branch %s does not contain %s", is.number, branch, own)
		}
		for _, other := range issues {
			if other.number == is.number {
				continue
			}
			theirs := fmt.Sprintf("issue-%d-call-2.md", other.number)
			if strings.Contains(tree, theirs) {
				t.Errorf("issue %d: branch %s contains %s, which belongs to issue %d: cross-branch interference",
					is.number, branch, theirs, other.number)
			}
		}
	}

	// Every merge commit is its own object: four branches independently
	// merged into, not one shared state four turns raced over.
	for sha, n := range shas {
		if n > 1 {
			t.Errorf("merge commit %s was recorded for %d issues, want each issue's sync to produce its own commit", sha, n)
		}
	}

	after, overlaps, deadlined := gate.report()
	if overlaps != overlapsBefore {
		t.Errorf("the gate was held by two turns at once %d times during the concurrent phase", overlaps-overlapsBefore)
	}
	if deadlined != deadlinedBefore {
		t.Errorf("%d acquisitions during the concurrent phase carried an ambient deadline, want 0 (AC-PAR-3.6)", deadlined-deadlinedBefore)
	}
	if len(after) != 1 {
		t.Errorf("acquisitions span %d clones, want 1: every issue here shares one registered clone", len(after))
	}
	// AC-6.1 / AC-6.2: exactly two acquisitions per concurrent turn —
	// prepareClone (drift measurement and merge, one acquisition) and
	// commitAndPush — and no further acquisition anywhere in the trace. A
	// branch-keyed lock or a second acquisition around the merge would show up
	// here as a number bigger than 2*len(issues), not as a behavioural
	// failure.
	gotDuringConcurrentPhase := after[f.storeRepo.Path] - before[f.storeRepo.Path]
	if want := 2 * len(issues); gotDuringConcurrentPhase != want {
		t.Errorf("the clone at %s was entered %d times during the concurrent phase, want %d (%d turns x 2 sections, no locking added for the merge)",
			f.storeRepo.Path, gotDuringConcurrentPhase, want, len(issues))
	}
}
