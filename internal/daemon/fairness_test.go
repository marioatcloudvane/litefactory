package daemon_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestRepoBStartsWithinTheBoundedNumberOfTicksWhileRepoAIsSaturated is
// AC-PAR-2.2: with repo A holding continuously eligible work sufficient to
// saturate the cap, repo B's eligible issue starts within a bounded, asserted
// number of dispatch decisions — the ⌈R/S⌉ the two-level round-robin
// guarantees (doc.go, RD-1). With 2 repositories and a cap of 1 slot, that
// bound is ⌈2/1⌉ = 2 ticks, asserted here as the literal number, not as
// "eventually".
//
// Repo A is given 5 separate issues — five independent sessions, each with
// its own eligible comment — deliberately more than the 2 ticks under test,
// so that repo A never runs out of work of its own across them. If the
// round-robin were not fair across repositories (see the reasoning below the
// dispatch section for how this was verified), repo A's backlog alone would
// occupy the daemon's one slot for every one of those 5 ticks, and repo B
// would never be reached inside this test's 2-tick bound.
func TestRepoBStartsWithinTheBoundedNumberOfTicksWhileRepoAIsSaturated(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})
	repoB := f.secondRepo()

	f.plantCursor()
	repoAIssues := [5]int{401, 402, 403, 404, 405}
	for _, num := range repoAIssues {
		f.forge.addIssue(fakeIssue{Number: num, Title: fmt.Sprintf("Repo A task %d", num), User: stranger()})
	}
	const repoBIssue = 500
	f.forge.addIssueIn(repoB.Owner, repoB.Name, fakeIssue{Number: repoBIssue, Title: "Repo B task", User: stranger()})

	// Bootstrap every session before any comment exists — cap 1 and 6
	// candidates, so several fixed, deterministic settles are used rather
	// than assuming how many land in one tick's own opportunistic refill.
	for range len(repoAIssues) + 1 {
		f.settle()
	}

	for _, num := range repoAIssues {
		f.forge.addComment(num, int64(9000+num), owner(), fmt.Sprintf("/spike %d", num))
	}
	f.forge.addCommentTo(repoB.Owner, repoB.Name, repoBIssue, 9500, owner(), "/spike repo B")

	f.settle()
	f.settle()

	// The bound this criterion asks for, asserted as the actual number:
	// ⌈R/S⌉ = ⌈2/1⌉ = 2 ticks. Repo B's comment must be among what ran
	// within exactly these two — indefinite starvation behind repo A's
	// larger backlog would leave it out of this set forever, not just late.
	if got := f.turns.triggers(); !slices.Contains(got, int64(9500)) {
		t.Fatalf("turns ran for %v after the ⌈R/S⌉=2-tick bound; repo B's comment (9500) must be "+
			"among them\nlogs:\n%s", got, f.logs)
	}
}

// TestReorderingListReposDoesNotChangeWhatRuns is AC-PAR-2.3.
//
// "zzz-api" is registered first, so it holds the lower store id; "aaa-api" is
// registered second, so it holds the higher one. [store.DB.ListRepos] orders
// by owner/name/path — alphabetically, "aaa-api" first — the opposite of
// registration order. [Daemon.dispatch] explicitly re-sorts its queues by
// repository id before walking them (tick.go), precisely so that the order
// ListRepos happens to return is not what decides anything. With a cap of 1,
// whichever repository the rotation visits first wins the daemon's only slot
// on the very first tick — so if dispatch order followed ListRepos' own
// alphabetical listing instead of the id-based sort, "aaa-api" would win it.
// It must not: "zzz-api" holds the lower id, and its comment is what runs.
func TestReorderingListReposDoesNotChangeWhatRuns(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})

	zzz, err := f.db.UpsertRepo(f.t.Context(), store.Repo{
		Path:          f.repo.Path + "-zzz",
		Forge:         store.ForgeGitHub,
		Owner:         testOwnerName,
		Name:          "zzz-api",
		RemoteURL:     "git@github.com:" + testOwnerName + "/zzz-api.git",
		BaseBranch:    "main",
		FeaturePrefix: naming.DefaultFeaturePrefix,
		WorktreeRoot:  f.repo.WorktreeRoot + "-zzz",
	})
	if err != nil {
		t.Fatalf("register zzz-api: %v", err)
	}
	aaa, err := f.db.UpsertRepo(f.t.Context(), store.Repo{
		Path:          f.repo.Path + "-aaa",
		Forge:         store.ForgeGitHub,
		Owner:         testOwnerName,
		Name:          "aaa-api",
		RemoteURL:     "git@github.com:" + testOwnerName + "/aaa-api.git",
		BaseBranch:    "main",
		FeaturePrefix: naming.DefaultFeaturePrefix,
		WorktreeRoot:  f.repo.WorktreeRoot + "-aaa",
	})
	if err != nil {
		t.Fatalf("register aaa-api: %v", err)
	}
	if zzz.ID >= aaa.ID {
		t.Fatalf("zzz-api (registered first) has id %d, want it lower than aaa-api's %d", zzz.ID, aaa.ID)
	}

	f.plantCursor()
	const issueNum = 900
	f.forge.addIssueIn(zzz.Owner, zzz.Name, fakeIssue{Number: issueNum, Title: "Z", User: stranger()})
	f.forge.addIssueIn(aaa.Owner, aaa.Name, fakeIssue{Number: issueNum, Title: "A", User: stranger()})
	f.settle()
	f.settle()

	f.forge.addCommentTo(zzz.Owner, zzz.Name, issueNum, 9101, owner(), "/spike z")
	f.forge.addCommentTo(aaa.Owner, aaa.Name, issueNum, 9102, owner(), "/spike a")

	result := f.tick()
	if err := f.daemon.Quiesce(f.t.Context()); err != nil {
		t.Fatalf("Quiesce: %v\nlogs:\n%s", err, f.logs)
	}
	if result.Dispatched != 1 {
		t.Fatalf("dispatched %d turns, want 1 (the cap is 1)\nlogs:\n%s", result.Dispatched, f.logs)
	}
	if got := f.turns.triggers(); !slices.Equal(got, []int64{9101}) {
		t.Errorf("turns ran for %v, want [9101]: the repository with the lower id (zzz-api, "+
			"registered first) must be visited first, regardless of ListRepos' own alphabetical "+
			"ordering that would list aaa-api first\nlogs:\n%s", got, f.logs)
	}
}

// TestBodyPendingSessionsCannotStarveCommentDrivenWork is AC-2.11: three
// sessions that never resolve their issue-body decision — each stays
// unassigned for the whole test — sit in the same repository, same
// candidate list and same per-repo round-robin as one real, comment-driven
// session. With a cap of 1 they compete for the daemon's only slot every
// tick, and the round-robin (RD-1) is what has to answer "does the
// comment-driven session ever get one, or do the three that never finish
// crowd it out forever".
//
// The comment-driven issue is registered last on purpose, so its session id
// is the highest of the four: a rotation that simply preferred low ids over
// high ones — rather than genuinely cycling — would starve it exactly the
// way this test would catch.
func TestBodyPendingSessionsCannotStarveCommentDrivenWork(t *testing.T) {
	f := newFixture(t)
	f.daemon = f.newDaemon(daemon.Options{MaxConcurrentRuns: 1})
	f.plantCursor()

	bodyIssues := [3]int{301, 302, 303}
	for _, num := range bodyIssues {
		f.forge.addIssue(fakeIssue{
			Number: num,
			Title:  "A body-pending task",
			Body:   "/spike do the thing",
			User:   stranger(),
			// No Assignees: this issue is never assigned for the whole test, so
			// its issue-body decision never resolves either way.
		})
	}
	const commentIssue = 304
	f.forge.addIssue(fakeIssue{Number: commentIssue, Title: "A comment-driven task", User: stranger(), Body: ""})

	// Bootstrap every session individually — cap 1 and 4 candidates on the
	// very first tick, so a fixed, deterministic number of settles is used
	// rather than assuming how many land in one tick's own opportunistic
	// refill (see TestRepoBStartsWithinTheBoundedNumberOfTicksWhileRepoAIsSaturated
	// above, which this test follows the shape of).
	for range len(bodyIssues) + 1 {
		f.settle()
	}
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("ran %d turns from bootstrapping alone, want none\nlogs:\n%s", len(calls), f.logs)
	}
	for _, num := range bodyIssues {
		if !f.sessionOf(num).BodyPending {
			t.Fatalf("issue %d is not marked body_pending after its first sight refused for want "+
				"of assignment", num)
		}
		// Push it out of the listing window, so only the body_pending union —
		// exactly like a late assignment would need to — finds it again.
		f.forge.staleIssue(num, f.clock.Now().Add(-24*time.Hour))
	}

	// The comment that makes issue 304 real, contested work, added only now —
	// after every body-pending session already exists and holds a lower
	// session id.
	f.forge.addComment(commentIssue, 9304, owner(), "/spike the real work")

	// The bound this criterion asks for: 4 eligible sessions and a cap of 1
	// slot means the per-repo round-robin has to visit every one of them
	// within ⌈4/1⌉ = 4 further ticks (RD-1) — so issue 304's comment must run
	// inside that many, never indefinitely behind the three that never
	// resolve.
	for range len(bodyIssues) + 1 {
		f.settle()
	}

	if got := f.turns.triggers(); !slices.Contains(got, int64(9304)) {
		t.Fatalf("turns ran for %v after the bounded number of ticks; issue %d's comment (9304) "+
			"must be among them — body-pending sessions must not starve comment-driven work\nlogs:\n%s",
			got, commentIssue, f.logs)
	}
}
