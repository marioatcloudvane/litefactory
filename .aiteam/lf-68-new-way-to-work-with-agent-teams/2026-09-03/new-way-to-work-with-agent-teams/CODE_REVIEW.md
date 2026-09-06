# Code Review
# Feature: Configurable Agent Teams — US-4 (CLI scaffold + `team list`/`team show`, with `team add` wired ahead of schedule)
# Reviewed by: go-code-reviewer
# Date: 2026-09-06
# Status: BLOCKING

---

## Scope

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

## BLOCKING findings

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

---

## Checklist walkthrough (summary)

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

## Cross-story summary

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

## Verdict

BLOCKING — 2 issues must be resolved before testing. Routing to the engineer responsible for
`team add` (Report 2 / `internal/cli/cmd_team.go`'s `runTeamAdd`, `parseAgentSource`,
`hashSourceBytes`): CR-B-001, CR-B-002. `team list`/`team show` (Report 1) have no BLOCKING
findings and need no changes.
