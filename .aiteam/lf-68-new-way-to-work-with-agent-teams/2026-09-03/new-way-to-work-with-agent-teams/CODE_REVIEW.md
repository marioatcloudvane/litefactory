# Code Review
# Feature: Configurable Agent Teams — US-4 (CLI scaffold + `team list`/`team show`, with `team add` wired ahead of schedule)
# Reviewed by: go-code-reviewer
# Date: 2026-09-06 (original review) / 2026-09-06 (re-review after fix pass)
# Status: CLEAN (post re-review) — see "Re-review" section below for the current verdict.
#          Original verdict on first pass was BLOCKING; superseded, history preserved below.

---

## Scope (original review)

Files delivered this run:
- `internal/cli/cmd_team.go` (NEW, 791 lines)
- `internal/cli/commands.go` (MODIFIED, +6 registration lines)

Read as supporting context (pre-existing, not under review in their own right, except where
`cmd_team.go`'s new code interacts with their documented contracts): `internal/cli/errors.go`,
`internal/cli/command.go`, `internal/render/{types,registry,render}.go`,
`internal/team/{doc,write,write_types,marker,lockfile,errors,paths,hash}.go`,
`internal/teamsource/{doc,resolve,errors}.go`.

Independently verified (not just trusting the task reports):
```
go build ./...                          # OK
go vet ./internal/cli/...               # OK, no findings
gofmt -l internal/cli/cmd_team.go internal/cli/commands.go   # no output — clean
grep context.Background|context.TODO in cmd_team.go          # no matches
grep fmt.Print|log.Print/Fatal/Panic in cmd_team.go           # no matches
grep RenderedFile|render\.|AgentRender in internal/teamsource # no matches
```

---

## BLOCKING findings (original review — see Re-review section for resolution status)

### CR-B-001: `team add` loses a shared agent's prior contributing teams on every subsequent add
**File:** `internal/cli/cmd_team.go:646` (inside `runTeamAdd`)
**Story:** US-4 (unofficial `team add` wiring; formally owned by US-9/US-18)
**Category:** Cross-story consistency / Error handling correctness (violates a documented invariant)
**Finding:** `runTeamAdd` builds each `team.AgentRender` as:
```go
renders = append(renders, team.AgentRender{
    AgentKey:   entry.Key,
    Harness:    harnessName,
    Teams:      []string{teamName},
    SourcePath: entry.Agent.Source,
    SourceHash: hashSourceBytes(raw),
    Files:      teamFiles,
})
```
`Teams` is always the single team being added in *this* invocation. It is never merged with any
team(s) that previously contributed the same shared agent's root-file section.

`internal/team/doc.go` (lines 113-121) and `internal/team/write_types.go`'s `AgentRender` doc
comment both state this explicitly and unconditionally as a load-bearing invariant: "the caller
(internal/teamsource) is responsible for collecting every contributing team into one AgentRender
before calling Add, so this package never has to merge two calls' worth of contributions itself."
`internal/team/marker.go`'s `upsertMarkerSection` (confirmed by reading it in full) locates an
existing section by agent key alone and **fully replaces** the section — including its BEGIN
line's `team=` list — using only the caller-supplied `teams` argument; it never reads or merges
the existing section's team list. `internal/team/lockfile.go`'s `LockRecord` (confirmed by
reading it in full) has no field recording the team list either, so there is no other mechanism
anywhere in `internal/team` that reconstructs "who else already contributes this agent."

Consequence: if Team A runs `litefactory team add team-a` and a shared agent's section is written
to `CLAUDE.md` with `team=team-a`, then Team B runs `litefactory team add team-b` for the *same*
shared agent, `upsertMarkerSection` will overwrite the section with `team=team-b`, silently
dropping `team-a`'s attribution. This is not caught by `Add`'s no-op short-circuit — `markerSpanHash`
deliberately incorporates the team list (per `marker.go`'s own doc comment: "a change in the set of
contributing teams alone... still invalidates a previously recorded RenderedHash"), so the new,
incomplete team list hashes as a legitimate change and is written without any warning or error.
This is exactly the multi-team shared-agent scenario the marker/lockfile design in `internal/team`
was built to support (US-18 AC-2), and the current wiring defeats it silently.
**Required fix:** Before constructing `AgentRender.Teams` for a shared agent, the caller must
determine the full, current set of contributing teams and pass all of them. Since `internal/team`
does not expose a way to read a section's existing team list (by design — `parseBeginLine`/
`locateMarkerSection` are unexported), this requires either (a) `internal/team` exposing a read-only
accessor for a root file's existing team list per agent key, or (b) the caller tracking contributing
teams via some other durable state (e.g. index metadata) rather than only ever supplying the current
invocation's team. Resolve this at the same layer decided in CR-B-002 below, since the fix and the
placement question are the same piece of work.

**Re-review resolution: RESOLVED.** See "Re-review" section below.

### CR-B-002: Render/convert/collect-teams orchestration built in `internal/cli` instead of `internal/teamsource`
**File:** `internal/cli/cmd_team.go:538-676` (`parseAgentSource`, `hashSourceBytes`, and the
render loop inside `runTeamAdd`)
**Story:** US-4 (unofficial `team add` wiring; formally owned by US-9/US-18/US-19)
**Category:** Cross-story consistency / Standards (package placement)
**Finding:** Two pre-existing, in-tree doc comments assign this responsibility to
`internal/teamsource`, not `internal/cli`:
- `internal/team/doc.go:59-61`: "This package never renders anything itself and never imports
  internal/render — a caller (internal/teamsource) renders first, converts render.RenderedFile
  into this package's own RenderedFile, and calls Add."
- `internal/team/write_types.go`'s doc comment on `RenderedFile`: "internal/teamsource, which does
  import both, is what converts a render.RenderedFile into this type before calling Add."

`internal/teamsource` today has no rendering-related code at all (confirmed by grep: no match for
`RenderedFile`, `render.`, or `AgentRender` anywhere in the package). `runTeamAdd` instead performs
frontmatter parsing (`parseAgentSource`), source hashing (`hashSourceBytes`, a byte-for-byte
duplicate of `internal/team/hash.go`'s unexported `hashBytes`), harness lookup (`render.Lookup`),
rendering (`renderer.Render`), and the `render.RenderedFile` → `team.RenderedFile` conversion
directly inside the CLI command handler.

This is a genuine architectural deviation from a documented contract (not an inferred style
preference): it puts business logic that later stories (US-11 `team update`, US-19 registered-repo
resolution) are expected to reuse into a package (`internal/cli`) those stories have no reason to
depend on, and it is the direct structural cause of CR-B-001 — the "collect every contributing team"
responsibility that `doc.go` assigns to `teamsource` was simply dropped rather than relocated.
**Required fix:** Move `parseAgentSource`, the render loop, and the `render.RenderedFile` →
`team.RenderedFile` conversion into `internal/teamsource` (e.g. a `teamsource.Render`/`RenderTeam`
entry point that returns `[]team.AgentRender` ready for `team.Add`), with `internal/teamsource`
importing `internal/render` for this purpose. `internal/cli/cmd_team.go`'s `runTeamAdd` should call
that entry point rather than performing rendering itself, consistent with how `resolveTeamIndex`
already delegates index resolution to `internal/teamsource`. Fold the CR-B-001 fix (collecting
existing contributing teams) into this same relocated code, since `teamsource` sits above both
`internal/team` and `internal/render` and is positioned to read prior state before rendering.

**Re-review resolution: RESOLVED.** See "Re-review" section below.

---

## ADVISORY findings

### CR-A-001: `hashSourceBytes` duplicates `internal/team/hash.go`'s unexported `hashBytes`
**File:** `internal/cli/cmd_team.go:562-565`
**Story:** US-4
**Category:** Standards (duplicated logic)
**Finding:** `hashSourceBytes` (sha256 + hex) is a byte-for-byte reimplementation of
`internal/team/hash.go`'s `hashBytes`, done because the original is unexported. Two independent
implementations of the same hashing scheme can silently diverge (e.g. one gets swapped to a
different digest algorithm and the other doesn't) with no compiler signal.
**Suggestion:** Export `hashBytes` as `HashBytes` from `internal/team` (or, once CR-B-002 is
resolved, keep this concern out of `internal/cli` entirely) rather than maintaining a second
implementation.

**Status as of re-review:** Resolved as a side effect of the CR-B-002 fix — `hashBytes` is now
exported as `HashBytes` (`internal/team/hash.go`), `hashSourceBytes` was deleted from
`internal/cli/cmd_team.go`, and `internal/teamsource/render.go` calls `team.HashBytes` directly.
No duplicate implementation remains.

### CR-A-002: `team add`'s "lockfile already exists" short-circuit skips harness/index validation
**File:** `internal/cli/cmd_team.go` (`runTeamAdd`, `team.HasLockfile(repoPath)` check, ahead of
`openTeamRuntime`/`resolveTeamIndex`)
**Story:** US-4
**Category:** Standards / minor UX consistency
**Finding:** `HasLockfile` is checked before harness resolution or index resolution, so a repo that
already has a lockfile gets the "already has a team lockfile; run `team update` instead" error even
if `--harness` or `--repo` would otherwise have been invalid. This is efficient and not incorrect
(the operator still needs to switch commands regardless), but it means this error path never
benefits from the same input validation the rest of the command gets.
**Suggestion:** No functional change required; note this ordering choice in the command's doc
comment so a future reader does not assume `--harness`/`--repo` are always validated before this
check fires.

**Status as of re-review:** Unchanged — this finding's file/lines were not touched by the fix pass
(the `HasLockfile` check still runs before `resolveTeamIndex`, unaffected by the `RenderTeam`
relocation). Still advisory, still open, out of scope for this pass.

---

## Checklist walkthrough (original review, summary)

1. **AC-3 (six commands, one constructor + one `Commands()` line each, no switch/registry):**
   Satisfied — confirmed by reading `commands.go` (15-entry literal, last 6 are the new team
   commands) and `cmd_team.go` (six `newTeamXxxCommand()` constructors, each following the
   documented pattern in `command.go`'s package doc).
2. **Exit-code contract (`UsageError`→2, `PreconditionError`→3, plain error→1):** Satisfied
   throughout — `resolveTeamRepoPath` returns `Preconditionf` on a missing `.git`;
   `resolveTeamHarness`/`findTeam` return `&UsageError{}` for bad input; `teamSourceError` maps
   `teamsource`'s tier-2/tier-3 "missing" errors to `Precondition`; `team add`'s "already has a
   lockfile" case correctly returns a plain error (exit 1), matching `plan_brief.md`'s exit-code
   table, which does not list "already installed" under Usage or Precondition.
3. **Atomicity of `team add`:** Confirmed no partial state reaches `team.Add` — all rendering and
   error collection happens into local `renders`/`renderErrs` slices; `errors.Join(renderErrs...)`
   is checked and returned before `team.Add` is ever called if any render failed.
4. **`errors.Join` aggregation:** Used correctly (aggregating, not fail-fast) in `parseHarnessList`
   and `runTeamAdd`'s render-error collection loop.
5. **Security (symlink refusal, path validation, secret scanning):** All live entirely inside
   `internal/team/write.go` and `internal/team/lock.go`; `cmd_team.go` does not duplicate or
   re-implement any of this, and only performs a read (`fs.ReadFile(resolved.FS, ...)`) on the
   source side, which is not a write path.
6. **`env.Stdout`/`env.Logger` convention (STYLE-2):** Clean — grep across `cmd_team.go` found no
   bare `fmt.Print*` or `log.*`/`Fatal`/`Panic` calls; all output goes through
   `fmt.Fprintf(env.Stdout, ...)` / writer-targeted table/JSON writers, consistent with
   `command.go`'s documented exception for `env.Stdout`.
7. **Context threading:** No `context.Background()`/`context.TODO()` found in `cmd_team.go`; `ctx`
   is threaded from the handler through `openTeamRuntime`/`resolveTeamIndex`.
8. **gofmt/go vet/go build:** Independently re-ran; all clean, corroborating both task reports'
   claims.
9. **Dead code / TODO/FIXME:** None found in the reviewed file.
10. **`internal/render` import asymmetry (report 1 vs report 2):** Not an inconsistency — `team
    list`/`team show` only read/present index metadata (no rendering needed); `team add` genuinely
    needs to render. No finding.

---

## Cross-story summary (original review)

`team list`/`team show` (the formally in-scope US-4 deliverable) integrate cleanly with
`internal/teamsource` and `internal/team`: signatures match, error mapping respects the
`cli -> domain` dependency direction (`internal/team`/`internal/teamsource` never import
`internal/cli`), and the exit-code contract from `plan_brief.md` Shared Decision 2 is applied
correctly throughout. The risk is entirely concentrated in the ahead-of-schedule `team add` wiring:
it is not wrong about atomicity or exit codes, but it builds rendering/conversion/team-collection
logic directly in `internal/cli` where two other in-tree packages' doc comments explicitly say that
logic belongs in `internal/teamsource`, and the resulting shortcut produces a real, silent
data-loss bug (CR-B-001) for the shared-agent-across-multiple-teams scenario that US-18's own
marker/hash design exists specifically to support. Both issues are best fixed together, at the
`internal/teamsource` layer, before `team add` is exercised against a repo with more than one team
sharing an agent.

---

## Verdict (original review)

BLOCKING — 2 issues must be resolved before testing. Routing to the engineer responsible for
`team add` (Report 2 / `internal/cli/cmd_team.go`'s `runTeamAdd`, `parseAgentSource`,
`hashSourceBytes`): CR-B-001, CR-B-002. `team list`/`team show` (Report 1) have no BLOCKING
findings and need no changes.

---
---

# Re-review (2026-09-06, after fix pass)

## Scope of this pass

Per the orchestrator's instruction, this pass reviews only the files touched by the fix, and
confirms CR-B-001 and CR-B-002 specifically. `team list`/`team show` and everything else already
found clean above are out of scope and were not re-read.

Files reviewed in full:
- `internal/teamsource/render.go` (NEW)
- `internal/cli/cmd_team.go` (MODIFIED)
- `internal/team/hash.go` (MODIFIED)
- `internal/team/marker.go` (MODIFIED)
- `internal/team/write.go` (MODIFIED)
- `internal/team/marker_test.go`, `internal/team/write_test.go` (MODIFIED, mechanical rename only)
- `internal/team/lockfile.go` (read for context — not modified by this fix pass; its `Teams` field,
  `encodeTeams`/`decodeTeams`, and `ExistingTeams` predate this pass and were already present at
  `HEAD`, confirmed via `git diff HEAD~3 -- internal/team/lockfile.go` showing the whole file as one
  addition prior to this fix, and `git diff HEAD -- internal/team/write.go` showing this pass added
  nothing to `write.go` beyond the `hashBytes`→`HashBytes` rename)

Independently run (not just trusting the engineer's report):
```
go build ./...                                                                    # OK
go vet ./...                                                                      # OK, no findings
gofmt -l internal/teamsource/render.go internal/cli/cmd_team.go \
      internal/team/hash.go internal/team/marker.go internal/team/write.go \
      internal/team/marker_test.go internal/team/write_test.go                    # no output — clean
go test -count=1 ./internal/team/... ./internal/teamsource/... ./internal/cli/... # all pass
                                                                                   # (teamsource has
                                                                                   # no test files —
                                                                                   # out of scope per
                                                                                   # review remit, but
                                                                                   # noted)
grep '"github.com/cloudvane' internal/team/*.go (excl. _test.go)                  # only internal/config
grep parseAgentSource|hashSourceBytes|render\.Lookup|render\.RenderedFile \
     internal/cli/cmd_team.go                                                     # no matches
```

## CR-B-001 — verified RESOLVED

`internal/teamsource/render.go`'s `RenderTeam` now does, per `(harness, agent key)` pair, inside the
inner loop over `roster` (line ~111):
```go
existingTeams, existingErr := team.ExistingTeams(root, harnessName, entry.Key)
if existingErr != nil {
    errs = append(errs, fmt.Errorf("existing teams for %s (%s): %w", entry.Key, harnessName, existingErr))
    continue
}
teams := make([]string, 0, len(existingTeams)+1)
teams = append(teams, existingTeams...)
teams = append(teams, teamName)

renders = append(renders, team.AgentRender{
    ...
    Teams: teams,
    ...
})
```
This runs unconditionally for every roster entry × harness combination — there is no early return or
skip path that bypasses it for some entries but not others, and the resulting `AgentRender.Teams` is
what actually gets appended to `renders`. Confirmed (a).

(b) Merge correctness: `team.ExistingTeams` (in `internal/team/lockfile.go`, unmodified by this pass,
confirmed pre-existing at `HEAD`) already unions every `LockRecord.Teams` matching the `(harness,
agentKey)` pair across the whole lockfile, deduplicated and sorted, via `sortedUniqueTeams`. `RenderTeam`
then appends `teamName` on top, which can produce a duplicate entry if `teamName` already appears in
`existingTeams` (e.g. running `team add` twice for the same team) — but this cannot cause a spurious
diff or a lost team: both consumers of `AgentRender.Teams` normalize before comparing —
`internal/team/write.go`'s `encodeTeams(ar.Teams)` (which the lockfile record stores) and
`internal/team/marker.go`'s `markerBeginLine`/`buildMarkerSection` (which the written file and its hash
are built from) both call `sortedUniqueTeams` internally before use. So the on-disk BEGIN line, the
hash, and the lockfile's `Teams` string are all order- and duplicate-insensitive by construction, and
no team is ever dropped by this union. Confirmed (b) — no lost team, no duplicate-induced diff.

(c) First-ever-add degradation: `ExistingTeams` returns `(nil, nil)` both when `root` has no lockfile
yet (`loadLockfile` returns `ErrNoLockfile`, explicitly handled and mapped to `(nil, nil)`) and when no
record matches the `(harness, agentKey)` pair. `RenderTeam`'s `teams := make([]string, 0,
len(existingTeams)+1); teams = append(teams, existingTeams...); teams = append(teams, teamName)`
correctly degrades to `[]string{teamName}` in both cases. Confirmed (c).

The specific scenario CR-B-001 was raised against — Team A's `team add team-a` writing `team=team-a`,
then Team B's `team add team-b` for the same shared agent — is now handled: `RenderTeam` reads the
lockfile before rendering, finds `team-a` already recorded for that `(harness, agentKey)`, and unions
it with `team-b`, so `upsertMarkerSection` receives `teams=[team-a, team-b]` instead of just
`[team-b]`. US-18 AC-2's invariant (one marker block, opening marker lists every contributing team)
holds.

**CR-B-001: RESOLVED.**

## CR-B-002 — verified RESOLVED

(a) `git diff HEAD -- internal/cli/cmd_team.go` confirms `parseAgentSource` and `hashSourceBytes` were
deleted outright from `cmd_team.go` (not left behind unused), along with their now-unneeded imports
(`crypto/sha256`, `encoding/hex`, `gopkg.in/yaml.v3`, `internal/render`). A grep for
`parseAgentSource|hashSourceBytes|render\.Lookup|render\.RenderedFile` across `cmd_team.go` returns no
matches. `runTeamAdd` now only builds a `[]teamsource.RosterEntry` from its own `teamRosterEntry` list
and calls `teamsource.RenderTeam(repoPath, resolved.FS, teamName, sourceRoster, harness.names)`,
performing no rendering/conversion itself. Confirmed (a).

(b) `internal/teamsource/render.go` imports `internal/render` and `internal/team`. `go build ./...`
succeeds with no import-cycle error, and a grep of `internal/render`'s own source for any import of
`internal/teamsource` or `internal/team` finds none — the dependency direction `internal/cli ->
internal/teamsource -> (internal/team, internal/render)` is intact and acyclic. Confirmed (b).

(c) Grepped every non-test `.go` file in `internal/team` for `"github.com/cloudvane` imports: the only
hit across the package is `internal/team/write.go`'s pre-existing `internal/config` import. `hash.go`,
`marker.go`, and `write.go`'s diffs against `HEAD` are each a one-line rename
(`hashBytes`→`HashBytes`) with no new imports added. The relocation did not leak an import of
`internal/render` or `internal/teamsource` into `internal/team`. Confirmed (c).

(d) Behavioral comparison of `runTeamAdd` before/after (via `git diff HEAD`): error mapping is
unchanged (`teamsource.RenderTeam`'s returned error, like the old inline loop's
`errors.Join(renderErrs...)`, is returned bare from `runTeamAdd` and falls through to the same
exit-code path as before — a plain error, exit 1, exactly matching the pre-fix behavior since neither
version wraps it as `UsageError`/`PreconditionError`); atomicity is unchanged (`RenderTeam` aggregates
every read/parse/render/lockfile-read error via `errors.Join` and returns before appending anything
useful, so a non-nil error still means `team.Add` is never called — matching the "render-all-then-write-
all-or-nothing" contract the original review verified). No new validation, flag, or output-format
change was introduced; `HasLockfile`, `openTeamRuntime`, `resolveTeamIndex`, `findTeam`, and the
stdout summary lines are byte-for-byte unchanged. Confirmed (d) — this was a pure move plus the
CR-B-001 bug fix, not a behavior change to the command's contract.

**CR-B-002: RESOLVED.**

## New problems introduced by the fix — none found

- `RenderTeam`'s per-entry `team.ExistingTeams` call is an extra lockfile read per roster entry per
  harness (O(n) reads of the same file within one `RenderTeam` call rather than one read reused
  across entries). This is a performance nit, not a correctness problem — `loadLockfile` is cheap
  (a small JSON file, no lock contention since `Add`'s repo flock is acquired later, inside
  `team.Add`, not here) — and is noted as a new ADVISORY below rather than blocking.
- No goroutines, no shared mutable state across goroutines, no new resource-management footguns:
  `RenderTeam` is a single-goroutine, synchronous function; every `fs.ReadFile` result is consumed
  immediately with no leaked handle (`fs.FS`'s `ReadFile` doesn't return a `Closer`).
  `errors.Join(errs...)` is checked before use.
  `gofmt`/`go vet` both clean on every touched file.
- No stray `fmt.Println`/`log.*` introduced; no `TODO`/`FIXME`; no panics on recoverable paths.
- Doc comments on the new exported `RenderTeam` and `RosterEntry` both start with the identifier's
  name and accurately describe the new team-collection behavior, including why `ExistingTeams`
  degrades to `(nil, nil)` on first add — no stale documentation left over from the pre-fix version.

## New ADVISORY finding

### CR-A-003: `RenderTeam` re-reads the lockfile once per roster entry per harness
**File:** `internal/teamsource/render.go:111` (inside the roster loop, per harness)
**Story:** US-4/US-9/US-18 (`team add` render/collect path)
**Category:** Performance
**Finding:** `team.ExistingTeams(root, harnessName, entry.Key)` is called once per `(harness, roster
entry)` pair, each call independently doing `loadLockfile(root)` — a full read + JSON-decode of
`.litefactory/team.lock.json` — even though the lockfile's contents do not change during the course
of one `RenderTeam` call. For a team with many agents across multiple harnesses this is an
N-times-redundant file read and JSON unmarshal of the same file.
**Suggestion:** Load the lockfile once at the top of `RenderTeam` (or have `team` expose a
`Lockfile`-scoped variant of `ExistingTeams` that takes an already-loaded lockfile) and reuse it
across the roster/harness loops, rather than reloading it per entry. Not required before proceeding —
the file is small and this runs in a CLI command invoked interactively, not a hot service path — but
worth folding in if `RenderTeam` is later reused by `team update` (US-11) against larger teams.

## Re-review verdict

**CLEAN** relative to the original two BLOCKING findings: CR-B-001 and CR-B-002 are both resolved,
verified against the architect's ground truth (US-18 AC-2, the dependency-direction contract, and the
exit-code/atomicity contract), and the fix introduced no new BLOCKING issue. One new ADVISORY
(CR-A-003, performance) was found; CR-A-002 (pre-existing, out of scope for this pass) remains open
and unaffected. `go build`, `go vet`, `gofmt`, and `go test` all pass on every touched package.

Proceeding to environment setup / Phase 2 is appropriate; no third review cycle is needed.
