// Package repos turns a clone that already exists on disk into a registered
// repository: it decides what litefactory will call it, where it will put its
// worktrees, which branch it will cut from, and whether aiteam is installed
// there at all.
//
// # Nothing is written inside the clone
//
// Registration reads. It never writes, and it cannot: every git call goes
// through [git.Clone], whose method set is the S-6 allowlist and which has no
// Commit, Checkout or Reset to reach for. The guarantee is structural, and
// AC-1.7 proves it against a deliberately dirtied fixture.
//
// # Registration makes no network call
//
// This is a deliberate property, not an accident of the current
// implementation. `repo add` must work on a plane. The origin URL comes from
// "git remote get-url" — local config — and the remote's default branch comes
// from the locally cached ref that a clone or a previous fetch already left
// behind ("git rev-parse --abbrev-ref origin/HEAD"). There is no ls-remote and
// no API call here. The only method on [git.Clone] that talks to the network
// is Fetch, and this package never calls it — which TestRegistrationIsOffline
// checks rather than trusting.
//
// # Two repositories may not create their worktrees in one directory
//
// A worktree path carries the issue number but not the repository, so two
// repositories sharing a worktree root map issue 42 to one directory. Run
// sequentially that is an annoyance; run concurrently it is a security
// exposure, because the two turns hold two correctly granted locks and nothing
// else in the design stops them. [ValidateWorktreeRoots] refuses the
// configuration at daemon startup and [ValidateNewWorktreeRoot] refuses it at
// `repo add`, before the row is written. The first of those documents why the
// collision is refused rather than designed out (AC-PAR-4.8).
//
// # aiteam detection is one rule, and it is deliberately dumb
//
// See [DetectAiteam]. It reads two explicit markers and infers nothing from
// prose. A cleverer rule would be wrong in ways nobody notices until it
// matters, and nothing at M0 consumes the team name.
package repos
