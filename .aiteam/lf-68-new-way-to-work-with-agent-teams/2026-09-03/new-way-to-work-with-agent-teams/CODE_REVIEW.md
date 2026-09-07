# Code Review
# Feature: Configurable Agent Teams — US-15 (multi-worktree `team update` propagation)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: ADVISORY_ONLY

---

## Scope

Belated Phase 1.5 review of US-15, run standalone against already-committed,
already-passing code (`go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test -race ./...` all clean at review time; 22 relevant `internal/cli`
tests pass with `-race` per the follow-up unit-test-engineer pass).

Files reviewed:
- `internal/cli/cmd_team_worktrees.go` (new: `discoverSiblingWorktrees`,
  `runTeamUpdateAllWorktrees`, `teamUpdateWorktreeJSON`/`teamUpdateAllJSON`,
  `writeTeamUpdateAllJSON`/`writeTeamUpdateAllTable`)
- `internal/cli/cmd_team.go` (modified: `--all-worktrees` flag registration
  and dispatch inside `runTeamUpdate`, now at lines 1226-1394; package doc
  comment)
- `internal/cli/cmd_team_worktrees_test.go` (new/extended: 6 tests covering
  AC-1 through AC-4 plus table-output rendering)

Read for context, not modified by this story: `internal/git/clone.go`
(`Clone.Open`, `Clone.WorktreeList`, `parseWorktreeList`), `internal/git/exec.go`
(`Resolve`), `internal/cli/errors.go` (`PreconditionError`/`ExitCode`),
`internal/cli/cmd_team.go`'s `resolveTeamRepoPath`, `openTeamRuntime`,
`teamSourceError`, `resolveTeamIndex`, `teamUpdateWorktree`,
`writeTeamUpdateTable`/`writeTeamUpdateJSON` (the US-11 single-worktree
functions this story reuses/parallels).

---

## Findings against the focus points

**1. Reuse of US-11's algorithm.** `runTeamUpdateAllWorktrees` calls
`teamUpdateWorktree(ctx, rt, path, force)` — the exact, unmodified US-11
function — once per discovered path, including `repoPath` itself. No
duplicated/diverged classify-then-write logic was introduced; the loop is a
thin orchestration layer around the existing single-worktree algorithm. Genuine
reuse, not a parallel implementation.

**2. `errors.Join` aggregation.** The per-worktree loop in
`runTeamUpdateAllWorktrees` (cmd_team_worktrees.go:137-151) never returns
early on a worktree's failure: it records `entry.Error`, appends a wrapped
error to `errs`, and continues to the next path unconditionally. `errors.Join(errs...)`
is called once after the loop completes, matching the required
aggregate-don't-fail-fast pattern. `TestTeamUpdateAllWorktrees_OneFailureDoesNotBlockOthers`
concretely exercises this (a broken sibling lockfile does not prevent a
healthy sibling from being updated in the same run) — verified by reading
the test, not just trusting its name. No fail-fast regression found.

**3. No concurrency.** The propagation loop is a plain sequential `for _, path
:= range paths` with no goroutines, channels, or worker pools anywhere in
`cmd_team_worktrees.go`. `context.Context` is threaded through every I/O call
(`git.Resolve(ctx)`, `binary.Open(ctx, ...)`, `clone.WorktreeList(ctx)`,
`teamUpdateWorktree(ctx, ...)`) rather than re-created with
`context.Background()`. No concurrency was introduced.

**4. `PreconditionError` never leaking from `internal/team`.** This story's
new file imports only `internal/git`, `encoding/json`, `errors`, `fmt`, `io`,
`path/filepath`, `sort`, `text/tabwriter` — it does not touch `internal/team`
at all, so the constraint is trivially satisfied here. (See finding CR-A-001
below for a related but distinct exit-code classification gap in this same
file's own precondition handling.)

**5. Symlink/path-traversal handling for `git worktree list` output.**
`discoverSiblingWorktrees` resolves each candidate via `filepath.Abs` then
`filepath.EvalSymlinks` before comparing it against `self` (`clone.Path()`,
itself always absolute and symlink-resolved by `Clone.Open`), so the dedup
comparison is canonical on both sides regardless of how a worktree path was
originally spelled. No user-controlled string is concatenated into a path or
command; every candidate comes from git's own porcelain output, parsed by the
existing NUL-delimited `parseWorktreeList`. A path this process cannot even
make absolute is excluded rather than erroring the whole discovery. No
traversal or injection issue found.

**6. General idiom/error/resource/security/performance.** No goroutines, no
new file handles held open, no raw SQL, no secrets, no `os/exec` built from
untrusted input (all git invocation goes through the existing
`internal/git` allowlisted wrapper). `paths`/`worktrees` slices are pre-sized
with `make(..., 0, len(...)+1)`. `fmt.Fprintf`/`fmt.Fprintln` write only to
`env.Stdout`/the passed `io.Writer`, never to real stdout via bare `fmt.Print*`.
Doc comments on exported/unexported identifiers alike start with the
identifier's name. No `TODO`/`FIXME`/dead code. `HTTP`/route registration is
N/A to this feature; the new `--all-worktrees` flag is registered in the same
`cmd.New` closure as every other `team update` flag
(cmd_team.go:1381-1392) and dispatched correctly inside `runTeamUpdate`
(cmd_team.go:1238-1240) — reachable, not silently orphaned.

Two advisory-level gaps found, detailed below: an exit-code classification
inconsistency for one discovery-failure path, and duplicated/incomplete table
rendering between the single- and multi-worktree table writers.

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-001: `Clone.Open` failure during discovery is not classified as `PreconditionError`
**File:** `internal/cli/cmd_team_worktrees.go:46-58`
**Story:** US-15
**Category:** Error handling / Cross-story (exit-code contract)
**Finding:** `discoverSiblingWorktrees` wraps a `git.Resolve` failure (git
binary missing) as `Precondition(err)`, correctly mapping it to exit code 3
per Shared Decision 2 ("machine not ready"). The very next call,
`binary.Open(ctx, repoPath)`, returns its error unwrapped:
```go
clone, err := binary.Open(ctx, repoPath)
if err != nil {
    return nil, err
}
```
An `Open` failure (`ErrNotARepository`, `ErrBareRepository`, or a
`resolveExisting`/`rev-parse` failure against a corrupted or tampered `.git`
entry) is exactly the "not a git worktree" / "repo not writable" category
Shared Decision 2 assigns to exit code 3, but here it falls through
`runTeamUpdateAllWorktrees`'s `fmt.Errorf("discover sibling worktrees of %s: %w", ...)`
wrap as a plain error, landing on `ExitFailure` (1) instead of
`ExitPrecondition` (3). `resolveTeamRepoPath` already checks that `.git`
exists before this code runs, so the gap is narrow (it requires a `.git`
entry that exists but that `git` itself then refuses to open), but it is a
real, reachable deviation from the exit-code contract this same story is
bound by. No test currently exercises this path either way.
**Suggestion:** Wrap the `binary.Open` error the same way the `git.Resolve`
error is wrapped, e.g. `return nil, Precondition(err)`, so both of
`discoverSiblingWorktrees`'s failure modes map to the same exit code family.

### CR-A-002: Per-worktree table rendering duplicates `writeTeamUpdateTable`'s row logic and drops its summary
**File:** `internal/cli/cmd_team_worktrees.go:184-213` vs. `internal/cli/cmd_team.go:588-613`
**Story:** US-15
**Category:** Standards (duplicated logic) / Consistency
**Finding:** `writeTeamUpdateAllTable`'s per-worktree section
(cmd_team_worktrees.go:200-211) re-implements the exact tabwriter
header/row-formatting loop that `writeTeamUpdateTable` (cmd_team.go:596-604)
already contains for the single-worktree case, instead of factoring a shared
`writeTeamUpdateRecordsTable(tw, records)`-style helper both call. Beyond the
duplication itself, the two outputs have diverged in content: the
single-worktree table prints `updated: N` / `skipped (unchanged): N` /
`refused (...): N` summary lines after the row table
(cmd_team.go:608-612); the per-worktree section in the `--all-worktrees`
table has no equivalent — a reader using the non-JSON form of
`--all-worktrees` sees each worktree's individual records but not its
per-worktree counts, even though those counts are already computed and
present in `teamUpdateWorktreeJSON.Updated/Skipped/Refused` and used in the
JSON output.
**Suggestion:** Extract the shared row-rendering loop into one helper used by
both table writers, and add the same updated/skipped/refused summary line(s)
to each worktree's section in `writeTeamUpdateAllTable` for output parity
with the single-worktree table.

### CR-A-003: Primary worktree's reported path is not symlink-canonicalized like sibling paths are
**File:** `internal/cli/cmd_team_worktrees.go:131-133`
**Story:** US-15
**Category:** Consistency
**Finding:** `runTeamUpdateAllWorktrees` reports the invoking worktree using
the raw `repoPath` argument (`filepath.Abs` only, from `resolveTeamRepoPath`),
while every sibling path in the same report has been through
`discoverSiblingWorktrees`'s `filepath.Abs` + `filepath.EvalSymlinks`
canonicalization. If `--repo` is passed as a path containing a symlink
component, the primary worktree's `Path` field in the JSON/table output can
be spelled differently from how git and the sibling entries would spell the
same physical directory, even though `discoverSiblingWorktrees`'s internal
dedup check (which compares against `clone.Path()`, always canonical) is
unaffected and correctly avoids a duplicate entry. Cosmetic only — no
worktree is double-processed or mis-attributed.
**Suggestion:** Use the canonical `self` path (already computed inside
`discoverSiblingWorktrees` via `clone.Path()`) for the primary worktree's
report entry too, e.g. by having `discoverSiblingWorktrees` also return `self`,
so every path in the report is canonicalized the same way.

---

## Cross-story summary

US-15 integrates cleanly with US-11's already-landed single-worktree `team
update`: `teamUpdateWorktree` is called verbatim, once per discovered path,
with no forked copy of its classify-then-write logic. Discovery is live via
`git worktree list` as the architecture notes require, with no persisted
repo-to-worktree schema and a symlink-safe, canonical comparison against the
worktree the command was invoked against. The `errors.Join`-aggregation
requirement — the single most common BLOCKING finding class on this issue
per three prior stories — was checked concretely by tracing the loop's
actual control flow, and it aggregates correctly rather than failing fast.
No concurrency was introduced, and `--all-worktrees` is correctly gated
behind its own flag with `team update`'s default (no flag) behavior
unchanged, per `TestTeamUpdate_AllWorktreesFlagDefaultsFalse`. The three
advisory items above are all narrow: one exit-code classification gap on an
edge case that requires an already-tampered repository past the existing
`.git`-existence precondition check, and two output-completeness/duplication
gaps confined to the human-readable (non-JSON) `--all-worktrees` table, which
do not affect the JSON contract, AC-1 through AC-4, or any write path.

---

## Verdict

ADVISORY_ONLY — 3 advisory findings noted (CR-A-001, CR-A-002, CR-A-003), no
BLOCKING findings. Proceeding to environment setup.
