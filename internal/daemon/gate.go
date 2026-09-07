package daemon

import (
	"context"
	"sync"
)

// cloneGate serialises the git operations of concurrent turns that contend on
// one registered clone. It is keyed on store.Repo.Path, so two turns working in
// two different clones never wait for each other.
//
// # It is a measured requirement, not a precaution
//
// Probed at parallelism 4 on git 2.50.1 / macOS with litefactory's own argv
// (see the parallelization-lf spike):
//
//   - "git fetch --prune" failed 180 times out of 240 when the remote's refs
//     had moved — "cannot lock ref 'refs/remotes/origin/main': is at X but
//     expected Y" — and 0 times out of 400 when there was nothing to fetch.
//   - "git worktree add" failed 3 times out of 400, fatally and unretried:
//     "failed to read .git/worktrees/<sibling>/commondir". The contention is on
//     the shared registry under .git/worktrees, not on an index; linked
//     worktrees having their own index is true and does not save you.
//   - "git push --set-upstream" produced 0 non-zero exits out of 240, and yet
//     printed "could not lock config file .git/config" 89 times and left 60 of
//     the 240 branches with no upstream at all.
//
// Retrying is the wrong fix for the fetch, which is why this is a gate and not
// a backoff: two fetches lose a race to write the same remote-tracking ref, so
// a retry re-runs the race rather than finding a stale lockfile gone.
//
// # Why a channel and not a sync.Mutex
//
// The obvious implementation is map[string]*sync.Mutex, and it is wrong.
// sync.Mutex.Lock is uninterruptible: a shutdown arriving while a worker is
// queued behind another worker's 60-second fetch would block that worker inside
// Lock with nothing able to release it, and [Daemon.Quiesce] would sit past its
// grace budget waiting for a goroutine that cannot be told to stop. A
// 1-buffered channel acquired with select on ctx.Done() is the same mutual
// exclusion with an exit, and the exit is the point. Do not simplify this into
// a mutex.
//
// The map itself is guarded by an ordinary sync.Mutex, which is safe because it
// is only ever held across a map lookup and insert — never across a git
// command, and never while waiting for the gate.
//
// # What it does not cover
//
// The claude run deliberately sits outside every section a caller takes, which
// is the entire source of surviving parallelism (AC-PAR-3.2). And two turns in
// two *different* clones take two different gates, so this is no defence
// against two repositories mapping one issue number to one worktree directory;
// that is refused at startup and at "repo add" instead (AC-PAR-4.8).
//
// The map grows to one entry per registered clone and never shrinks. There is
// nothing to reclaim: the bound is the size of the repos table, an entry is two
// words and a channel, and a gate that outlived its last holder is exactly what
// the next turn on that clone needs.
//
// # Keyed on the clone, not the branch (US-6)
//
// internal/turn's prepareClone critical section now includes a merge of the base branch
// into the task branch, which reads as a per-branch operation and invites the
// question of whether the gate should be keyed on repo.Path plus branch, so
// that two turns syncing two different branches of the same clone could run
// their merges concurrently instead of queuing. They should not, and the
// measurements above are why: a merge still runs "git fetch" against the same
// remote-tracking refs, still resolves and moves a ref in the clone's one
// shared refs/ namespace, and still reads and writes the clone's one shared
// object database — none of that becomes branch-scoped just because the ref
// being moved has a different name. Keying on branch would still need the
// same per-clone exclusion underneath to protect the object database and the
// refs directory, so it would add a second layer of bookkeeping for
// contention that is already serialised, not remove any. Concurrent issues on
// one clone already get independent, uncorrupted syncs under the existing
// per-clone key, precisely because they are queuing on the clone, not
// racing each other on the branch — see
// internal/turn/syncconcurrency_test.go.
//
// A cloneGate is safe for concurrent use.
type cloneGate struct {
	mu    sync.Mutex
	gates map[string]chan struct{}
}

// newCloneGate builds the process's gate registry. One is made at startup and
// injected into turn.Options; the registry is per-process because the
// contention it serialises is between goroutines of one process. Two daemon
// processes on one clone are kept apart by sessions.lock_owner, not by this.
func newCloneGate() *cloneGate {
	return &cloneGate{gates: make(map[string]chan struct{})}
}

// Acquire blocks until the caller holds the gate for path, and returns the
// function that gives it back.
//
// The context is a parameter rather than a field on the registry because it
// belongs to one acquisition: a gate whose cancellation was fixed at
// construction could not be abandoned by the one worker that needs to abandon
// it. A cancelled context returns ctx.Err() and no release function; the caller
// frames that error, because the caller is the one that knows which turn was
// waiting.
//
// Every nil-error return is owed exactly one call to release, which is why
// every call site defers it on the line after the error check.
func (g *cloneGate) Acquire(ctx context.Context, path string) (func(), error) {
	gate := g.gateFor(path)

	select {
	case gate <- struct{}{}:
		// Idempotent, so that a duplicated defer — the one way this is likely
		// to be got wrong at a call site — cannot hand the gate to a second
		// holder while the first is still inside its critical section. Mutual
		// exclusion that a copy-pasted line can break is not mutual exclusion.
		var once sync.Once
		return func() { once.Do(func() { <-gate }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// gateFor returns the channel for one clone, creating it on first use.
//
// The lock is held for the lookup and the insert and nothing else. Holding it
// across the acquisition below would serialise every clone against every other
// one and reintroduce the uninterruptible wait this type exists to avoid.
func (g *cloneGate) gateFor(path string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()

	gate, ok := g.gates[path]
	if !ok {
		gate = make(chan struct{}, 1)
		g.gates[path] = gate
	}
	return gate
}
