# Implement-mode notes

## 2026-09-05 — Run halted before Phase 1: schema drift judged a severe blocker

Read in full before starting: `plan_brief.md`, `MANIFEST.md`, `US-1.md`, `US-2.md`,
`US-17.md`, `SPIKE.md`. Read `RESEARCH_BRIEF.md` through its fourth revision
pass (the section describing the schema change directly).

**Finding.** `plan_brief.md`'s Shared Decision 1 — which the plan explicitly
tells Implement mode to treat as settled — specifies a flat top-level schema:

```go
Index{SchemaVersion int, Agents map[string]AgentEntry, Shared []string, Teams []TeamEntry}
```

US-1 (the first story, gating all 17 others directly or transitively) builds
exactly this shape: its Architecture Notes quote the same struct verbatim, and
its highest-risk task (T-03, sized L, called out as "the single biggest sizing
risk in this story") is a two-phase strict decoder written against it. US-17's
worked JSON example and `SPIKE.md`'s reference JSON both instantiate the same
flat `agents` map.

`RESEARCH_BRIEF.md`'s fourth revision pass (2026-09-05, ~17:28 — the most
recent accepted design decision, made in direct response to the sponsor's own
comment on issue #68 that a flat top-level `agents` map "will become
complicated" past 20-30 teams) removes that top-level map entirely: `shared`
becomes its own key→`{source,dest}` map, each team's `agents` field becomes a
key→`{source,dest}` map scoped to that team's own non-shared agents, and a new
structural invariant is fixed — an agent key lives in exactly one place in the
whole index (inline under one team, XOR once in `shared`); a key appearing in
more than one place is a loader error, not a merge.

MANIFEST.md's own history entry for that pass independently corroborates that
this is unreconciled: it records, in the project's own words, that
"plan_brief.md, all 18 US-*.md sub-issues (especially US-17.md), and SPIKE.md
all still embed the old flat-map shape... both left for a future Plan
reconciliation pass."

**Judgment.** This is not an ordinary scope ambiguity resolvable on "the most
reasonable reading of the plan" — the two schemas are structurally
incompatible (presence vs. absence of a top-level map; key-string lists vs.
inline full definitions; no uniqueness invariant vs. an exactly-once
invariant), and the incompatibility sits in the one story every other story in
the feature depends on. Two paths were open and both are foreclosed:

1. **Implement the old (plan_brief/US-1) schema as written.** This means
   hand-building the highest-risk piece of the whole feature — a strict
   two-phase decoder — against a shape the sponsor has already rejected on the
   record, on exactly the scaling concern (20-30 teams) this feature exists to
   solve. That is building known-to-be-wrong foundational code and calling it
   delivered.
2. **Implement the new (RESEARCH_BRIEF.md fourth-pass) schema instead**,
   silently substituting it for US-1's spec. That is re-deriving the
   architecture inside Implement mode, which this mode is explicitly not
   authorized to do — reconciling a design document against new research is
   Plan mode's job.

Both are ruled out by the same fact: the frozen artifact (`plan_brief.md`/
`US-1.md`) and the latest accepted design (`RESEARCH_BRIEF.md`) disagree on the
central data structure of the feature, and no reading of either document makes
them fit together. Per the Escalating rubric, this is a **material plan
change**: finishing US-1 (and everything built on it) as currently specified
would contradict the sponsor's own most recent, on-the-record feedback.

**Decision: escalate. No implementation agent was invoked. No code was
written.** The worktree is unchanged from session start except for this note.

See the run's final report (returned to the dispatcher) for the formal
escalation write-up.

## 2026-09-06 — Run 19: US-6, US-7, US-8 (the three renderers), Workflow step 5

**Scope for this run** (per sponsor's issue #68 comment): only US-6, US-7, US-8,
run in parallel, building on US-5's already-landed renderer contract
(`internal/render/`, T-13). US-7 (Codex) closes fully. US-6 (Claude) and US-8
(opencode) implement the standalone-file output path only — their
marker-tagged-section paths wait for US-18, not started this run. US-4, US-9,
US-18 explicitly untouched.

**Verification performed before starting** (per sponsor's own instruction to
verify rather than trust the run-18 note on faith): read `plan_brief.md`'s
Workflow (steps 5–6) and Story Index, and US-5/US-6/US-7/US-8's own sub-issue
files. Confirmed via `ls`/`git status --short`/`git log --oneline` (structure
only, no content read for this check) that `internal/render/` already contained
only the US-5 contract files (`doc.go, errors.go, registry.go, render.go,
render_smoke_test.go, types.go`) with no `claude.go`/`codex.go`/`opencode.go`
yet, and that `internal/team/`, `internal/teamsource/` (US-1/US-2/US-3/US-19)
were present and committed (clean git status). This matched the sponsor's claim
exactly. Also confirmed the schema-drift blocker recorded above (2026-09-05
entry) is moot for this run: the worktree's `internal/team/` types reflect the
reconciled (post-4th-pass) schema, not the old flat-map shape — the escalation
from that earlier run was resolved by a subsequent Plan reconciliation pass
before run 18 started.

**Phase 1.** Three `go-implementation-engineer` invocations in parallel, one
per story, each scoped explicitly to standalone-file output only (US-6, US-8)
or full closure (US-7). All three reported DONE. All three independently
registered their renderer under a distinct harness key (`claude`, `codex`,
`opencode`) via `init()` + `render.Register`, per `registry.go`'s documented
convention. `go build ./...`, `go vet ./...`, and `go test
./internal/render/... -v` all passed (22/22) after all three landed side by
side — no registration collisions, no import-boundary violations (all three
files import only stdlib + `gopkg.in/yaml.v3` + `internal/render` itself).

No other package in the repo imports `internal/render` yet (`go list -f
'{{.ImportPath}}: {{.Imports}}' ./...` shows no importer), so these changes are
fully isolated from the rest of the tree. A full-repo `go test ./...` run timed
out at 2 minutes; since nothing imports the touched package, this is judged to
be pre-existing slow/hanging behavior elsewhere in the repo, unrelated to this
run's changes, and not investigated further (out of this run's scope).

**Phase 1.5 — Code Review.** First pass: BLOCKING (CR-B-001) — the three
renderers, written in parallel with no visibility into each other's code, each
handled the "empty `Frontmatter.Name`" precondition differently (one bare
unwrapped error, two divergently-named sentinels). Dispatched one
`go-implementation-engineer` to unify all three onto a single shared sentinel,
`render.ErrMissingAgentName`, added to `errors.go` next to
`ErrUnsupportedField`/`ErrUnknownHarness`. Re-review (scoped to the touched
files only) confirmed the fix and returned ADVISORY_ONLY. Full findings in
`$session/CODE_REVIEW.md`.

## Code Review Advisories (carried forward, non-blocking)

- **CR-A-001**: The Extra-key sort-and-report block (for the "unsupported field"
  error path) is duplicated verbatim across `claude.go`, `codex.go`, and
  `opencode.go`. Candidate for a shared helper in the `render` package if a
  fourth harness is ever added.
- **CR-A-002**: `codex.go` hardcodes the string literal `"codex"` in four places
  instead of a named const, unlike `claude.go`/`opencode.go` which each define
  one for their own harness key.
- **CR-A-003**: Claude's empty-body path produces a trailing blank line
  (`---\n\n`) that Codex's and opencode's renderers explicitly guard against
  (and test for, via `TestCodexRenderEmptyBody`/`TestOpencodeRenderEmptyBody`);
  Claude has no equivalent guard or test.
- **CR-A-004**: Minor inconsistency — Claude calls `yaml.Marshal(&header)`
  (pointer) where its siblings pass the struct by value; harmless but worth
  aligning for consistency if this file is touched again.

None of these block correctness or the acceptance criteria for US-6/7/8 as
scoped to this run; recorded for a future pass (e.g. when US-18 lands and
these renderers are extended for the marker-section path).

## 2026-09-06 — Run 20: US-9 (write engine) + US-18 (root-file composition), Workflow steps 6-7 as one delivery window

**Scope for this run** (per sponsor's issue #68 comment): US-9 and US-18 only,
built as a single combined delivery given their explicit two-way hard coupling
in `plan_brief.md`'s Workflow section ("Hard couplings, stated once" —
US-9 supplies the atomic-write primitives US-18's marker upsert reuses; US-18
supplies the root-file write path US-9's install needs). US-4, US-10, US-11,
US-13, US-17 explicitly untouched this run.

**Deviation from standard Phase 1 fan-out**: instead of one
`go-implementation-engineer` per story in parallel, a single engineer built
both stories together in one pass, given the two-way coupling — avoids an
interface mismatch that two independent parallel implementations would risk
(e.g. one side assuming a `RenderedFile` shape the other didn't build).

**Assumption (deferred CLI wiring)**: US-9's T-20 (repo-identifier resolution
CLI integration via US-19) is explicitly excluded from this run per the task
instruction — the engine's entry point, `team.Add(root string, renders
[]AgentRender) (*AddResult, error)`, is delivered as the complete contract for
a future caller (US-4's `cmd_team.go`, or `internal/teamsource` glue) to build
against. No `internal/cli` changes were made.

**Engineer's own documented assumptions** (reasonable-reading judgment calls):
1. No-op short-circuit path tested via black-box `os.Stat` mtime-unchanged
   check rather than mocking the filesystem.
2. Concurrent-serialization test (`TestAddSerializesConcurrentCalls`, 8
   concurrent `Add` calls under `-race`) asserts indirectly via final
   lockfile-record count and error-free completion, rather than instrumenting
   internal lock acquisition directly.
3. `Add` takes no `context.Context` parameter — read as consistent with the
   plan's "no concurrency beyond the specified per-repo flock" constraint,
   since nothing else in this engine's call chain (renderers, flock) is
   ctx-aware either.

**Phase 1.5 — Code Review.** First pass: BLOCKING (CR-B-001) — `Add`'s
validation loop failed fast on the first bad harness/key/path/secret instead
of aggregating every validation problem in the batch via `errors.Join`, per
`plan_brief.md`'s Cross-Story Constraints (stated under both Platform and
Security, the latter explicitly naming `team add`). `doc.go` had documented
the fail-fast behavior as intentional, which itself conflicted with the plan.
Dispatched one `go-implementation-engineer` to fix: the loop now collects
every validation error into a slice and joins them once via the package's
existing `joinSorted` helper (already used by `decode.go`), returning nothing-
written unless the whole batch validates cleanly; individual sentinel types
remain reachable via `errors.Is`/`errors.As` against the joined result
(native `errors.Join` behavior). `doc.go` corrected. New test
`TestAddAggregatesAllValidationFailuresInOneBatch` added, proving both
error-completeness (two distinct problems, both reported) and atomicity
(nothing written) in the multi-failure case. Re-review (scoped to
`write.go`/`doc.go`/`write_test.go` only) independently confirmed the fix —
re-ran `go build`, `go vet`, `go test -race`, `gofmt -l` — and returned CLEAN.
Full findings in `$session/CODE_REVIEW.md`.

## Code Review Advisories (Run 20, carried forward, non-blocking)

- **CR-A-001**: `lock.go`'s `acquireRepoLock` has an inline symlink check that
  duplicates a narrower version of `writefile.go`'s `refuseSymlink`; candidate
  for reuse via the shared helper.
- **CR-A-002**: `Add` takes no `context.Context`, though `plan_brief.md`
  describes a `ctx.Err()` check for write paths generally; flagged for
  awareness ahead of US-4's CLI wiring (which will presumably thread a context
  in from the command layer) — not judged a defect in `internal/team` as
  delivered, since nothing in this engine's own call chain is ctx-aware.

Both non-blocking; not fixed this run per Phase 1.5 handling of
ADVISORY-only findings.

## 2026-09-06 — Implement run 21 (retroactively documented): US-4 `team add` CLI wiring

No entry for this run existed in this file before now — it is added retroactively
from the surviving artifacts (`CODE_REVIEW.md`'s original BLOCKING findings,
`git log`, and the committed `internal/cli/cmd_team.go`) because the run that
did this work ended (commit 76f3159) without completing its own close-out steps.

**Scope, as far as can be reconstructed:** `team add`'s CLI wiring in
`internal/cli/cmd_team.go` (791 lines, new file) plus a 6-line addition to
`internal/cli/commands.go` registering the subcommand — the caller-side glue
between US-4's CLI scaffold and US-9/US-18's already-landed `team.Add` engine.
`team list`/`team show` were built in the same file with no BLOCKING findings
against them.

**Phase 1.5 — Code Review.** First pass returned **BLOCKING**, with two
findings against `team add` specifically (full text preserved verbatim in
`CODE_REVIEW.md`):

- **CR-B-001**: each `team.AgentRender` was built with `Teams:
  []string{teamName}` — always just the current invocation's team, never
  merged with contributing teams already recorded for a shared agent. This
  defeated US-18 AC-2 (one deduplicated marker block per shared agent, listing
  every contributing team): a second team adding a shared agent would silently
  drop the first team's membership from the marker's `team=` list.
- **CR-B-002**: rendering/hashing/team-collection logic (`parseAgentSource`,
  `hashSourceBytes`, and the render loop) was built inside `internal/cli`
  instead of `internal/teamsource`, violating the package-boundary contract
  documented in `internal/team/doc.go` and `write_types.go`, which assign this
  responsibility to `internal/teamsource` — the same layering US-9/US-18's
  engine already follows.

The run stopped after this review pass without dispatching a fix cycle, and
without any of its own close-out steps (no `DRIFT_REPORT.md`, no MANIFEST.md
History entry, no notes.md entry, no `APP_CONTEXT.md`/`APP_CONSTRAINTS.md`
update). That is the incomplete state this session (run 22, below) was asked
to close out.

## 2026-09-06 — Implement run 22: close out run 21 — fix CR-B-001/CR-B-002, add missing tests, complete bookkeeping

**Scope** (per sponsor's issue #68 comment): fix exactly the two BLOCKING
findings left open against `team add` in run 21, then complete the
review/test/bookkeeping steps that run skipped. Explicitly out of bounds this
run: US-10, US-11, US-13, US-17, and US-1's `manage` subcommand beyond its
existing stub. No new user-story scope was started.

**Phase 1 — Fix.** A single `go-implementation-engineer` fixed both findings
together in one pass (per `CODE_REVIEW.md`'s own instruction that they belong
at the same layer):

- New file `internal/teamsource/render.go`: a `RosterEntry` type and a
  `RenderTeam(root string, fsys fs.FS, teamName string, roster
  []RosterEntry, harnessNames []string) ([]team.AgentRender, error)` entry
  point. Relocates `parseAgentSource` (now package-private inside
  `internal/teamsource`) from `cmd_team.go`. For each (harness, agent key)
  pair, calls the pre-existing `team.ExistingTeams(root, harness, agentKey)`
  read accessor to discover prior contributing teams, unions the result with
  the current `teamName`, and passes the full set through to the renderer and
  into the returned `team.AgentRender.Teams` — closing CR-B-001 without
  inventing new durable state, since `ExistingTeams` already existed for
  exactly this purpose. Converts `render.RenderedFile` → `team.RenderedFile`
  and aggregates per-entry errors via `errors.Join`.
- `internal/cli/cmd_team.go`: `parseAgentSource`/`hashSourceBytes` deleted;
  `runTeamAdd`'s render loop now builds `[]teamsource.RosterEntry` from the
  existing roster result and calls `teamsource.RenderTeam`, passing its
  output straight to `team.Add` — closing CR-B-002. Now-unused imports
  (`crypto/sha256`, `encoding/hex`, `gopkg.in/yaml.v3`, `internal/render`)
  removed.
- `internal/team/hash.go`'s unexported `hashBytes` exported as `HashBytes`
  (taking CR-A-001's advisory since this boundary was already being touched),
  with call-site renames in `marker.go`, `write.go`, and the two existing
  `_test.go` files in that package to keep them compiling.

Build, vet, gofmt, and the existing test suite all passed clean after the
fix.

**Phase 1.5 — Re-review.** `go-code-reviewer` re-reviewed, scoped only to the
seven touched files, independently re-ran build/vet/gofmt/test/grep checks,
and confirmed both CR-B-001 and CR-B-002 resolved. Verdict: **ADVISORY_ONLY**
(no third review cycle needed) — treated as satisfying "confirm CLEAN" for
gating purposes, per this orchestrator's own rule that ADVISORY_ONLY is a
terminal, non-blocking state equivalent to CLEAN. CR-A-001 (the original
`hashSourceBytes` duplication advisory) is now moot, since the duplicated
code was deleted rather than kept side by side. CR-A-002 (lockfile-exists
short-circuit skips harness/index validation ordering) is unchanged/open.
One new advisory recorded:

## Code Review Advisories (Run 22, carried forward, non-blocking)

- **CR-A-002** (carried from run 21's review, still open): the lockfile-exists
  short-circuit in `team add` skips harness/index validation ordering —
  non-functional, documentation-only suggestion.
- **CR-A-003** (new): `teamsource.RenderTeam` calls `team.ExistingTeams` once
  per (harness, roster entry) pair rather than loading the lockfile once and
  querying it in memory — a performance nit on repeated per-entry lockfile
  reads, not a correctness issue at today's team/agent-count scale.

Neither blocks correctness or this run's acceptance criteria; recorded for a
future pass.

**Phase 2 — Environment setup.** Skipped. `team add`'s implementation
(`internal/cli/cmd_team.go`, `internal/teamsource`, `internal/team`) is
filesystem-only — no database, external HTTP call, or message queue — matching
the same judgment already recorded for run 20. No `TEST_BASE_URL`/
`TEST_DATABASE_URL` lookup was needed.

**Phase 3 — Testing.** `go-unit-test-engineer` added the tests run 21 never
wrote, scoped narrowly to `team add` and the newly-relocated `teamsource`
logic (not a full-story re-test pass, and not touching `team list`/`team
show`, which the engineer separately flagged as themselves still having no
test coverage in this worktree — out of scope for this run, noted here as an
open gap for a future pass):

- `internal/teamsource/render_test.go` — `TestParseAgentSource`,
  `TestRenderTeam_SingleHarnessSingleAgent`,
  `TestRenderTeam_MultipleHarnesses`,
  `TestRenderTeam_FirstEverAdd_TeamsIsJustCurrentTeam`,
  `TestRenderTeam_SharedAgentWithExistingContributingTeam_UnionsTeams` (direct
  regression coverage for CR-B-001 — asserts the union, not an overwrite, via
  a real prior `team.Add` seeding a lockfile),
  `TestRenderTeam_MalformedFrontmatterSource_ReturnsWrappedError`,
  `TestRenderTeam_UnknownHarness_ReturnsError`.
- `internal/cli/cmd_team_test.go` (new) — `TestTeamAddRejections` (missing
  team argument, unknown `--harness` value, no `.git` at the repo path, an
  already-existing lockfile → the four `UsageError`/`PreconditionError`/plain-
  error exit-code paths) and `TestTeamAddSuccess` (full happy path: real
  temp-dir fixtures for the team index, config, and target repo; asserts
  stdout summary, on-disk rendered file content, and post-add lockfile
  presence).

`go test ./internal/teamsource/... ./internal/cli/... -race -cover`: all
passing, `-race` clean, `go vet` clean. Coverage 44.6% (`teamsource`), 73.2%
(`cli`, whole package). No bug loop was needed — zero test failures.

**Assumptions made this run** (headless, no clarifying question put to a
human):
1. ADVISORY_ONLY satisfies the sponsor's "confirm CLEAN" instruction, per this
   orchestrator's own Phase 1.5 handling rule that both verdicts gate
   identically. Recorded here explicitly since the sponsor's prose named
   "CLEAN" specifically.
2. No new `internal/team` read accessor was needed for CR-B-001 — the
   pre-existing `team.ExistingTeams` already covered the "prior contributing
   teams for this (harness, agent key)" query the fix needed.
3. `team list`/`team show`'s pre-existing lack of test coverage (discovered by
   the unit-test engineer, not by this orchestrator) is left as-is — fixing it
   was not part of this run's scope (fixing CR-B-001/CR-B-002 plus their
   missing tests), and is noted here as an open item for a future pass rather
   than silently expanded into.

No commits or pushes made this run — worktree left for litefactory to inspect
and commit.

## 2026-09-06 — Implement run 25: US-10 (`team status` drift detection) + closing the `team list`/`team show` test-coverage gap

**Scope** (per sponsor's request on issue #68): US-10 only, plus the open
`team list`/`team show` test-coverage gap flagged by run 22 and independently
noted again by run 24. Explicitly out of bounds this run: US-11, US-13, US-14,
US-15, US-16, US-17, and any extension of `team manage` beyond its existing
stub. Headless run — no clarifying questions were put to a human; ambiguities
were settled on the most reasonable reading of the plan and recorded below.

**Pre-run state read as ground truth, not re-verified**: `main` had been merged
into this branch (`9530f3b`) shortly before this run started, restructuring
config into a nested `Settings` struct (`TeamConfig` → `settings.team_config`
across `config.go`, `validate.go`, `template.go`, `cmd_team.go`); `go test
-race ./...` was reported green (26 packages) at that point.

**Decision required by the sponsor and recorded here, as instructed — CLI
surface for unimplemented subcommands.** With `team status` now real, `team
update` and `team manage` remain registered stubs whose help text says "X is
not implemented yet. This command currently only validates its flags."
Decision: **keep advertising all six `team` subcommands** (`list`, `show`,
`add`, `status`, `update`, `manage`) rather than hiding the two remaining
stubs from `--help` until they land. Reasoning:
1. `plan_brief.md`'s Shared Decision 2 architects the CLI surface as "six
   commands, one `Command` constructor each, one line each in
   `internal/cli/commands.go`'s `Commands()` — no switch, no mutable
   registry." That statically pre-commits to registering all six regardless
   of implementation status; conditionally hiding a subcommand would require
   a registration mechanism the plan explicitly rules out.
2. No story in the 18-story plan describes building dynamic/conditional
   command registration, so building one now would be scope not asked for.
3. The stub pattern (`"X is not implemented yet. This command currently only
   validates its flags."`) is itself the plan's existing, honest way of
   telling a user a subcommand isn't real yet — `team status` used exactly
   this pattern before this run, and `team update`/`team manage` continue to
   use it now. This is judged an acceptable ordinary-ambiguity resolution,
   not a severe blocker, since a full worked precedent (the six-command
   architecture) already existed to read from.

**Phase 1 — Development.** One `go-implementation-engineer` invocation
(single-story fan-out; the `team list`/`show` test gap is testing work, not
new implementation, so it was scoped to Phase 3 instead). Delivered:
- New `internal/team/status.go`: `Status` enum (`StatusInSync`,
  `StatusLocallyModified`, `StatusUpstreamUpdated`, `StatusConflict`,
  `StatusMissing`) with `String()`; exported `LoadLockfile`/`DecodeTeams`
  wrappers; `Classify(root, rec, freshSourceHash, freshTeams, freshBody)` —
  the five-state byte-comparison classifier from US-10's Architecture Notes,
  handling both standalone targets and root-file marker-span targets;
  read-only (only ever `os.ReadFile`, reuses the existing `refuseSymlink`
  helper rather than a second one). Imports stdlib only — no
  `internal/render`/`internal/teamsource` import, preserving `internal/team`'s
  import-boundary contract.
- New `internal/team/status_test.go`: one reproduction per classification
  state (including the missing-file and both-sides-changed-conflict cases
  T-29 calls for), plus two root-file-target variants and a `Status.String()`
  table test.
- Modified `internal/teamsource/render.go`: added `RenderCurrent(fsys,
  harnessName, sourcePath) (sourceHash string, files []team.RenderedFile,
  err error)` — a narrow, team-membership-independent re-render of one
  agent's current source, giving `team status` the "fresh render" half of
  each comparison without writing anything. `RenderTeam` itself untouched.
- Modified `internal/cli/cmd_team.go`: replaced the `team status` stub with
  `runTeamStatus`/`newTeamStatusCommand` plus JSON/table report helpers,
  reusing the same `resolveTeamRepoPath`/`openTeamRuntime`/`resolveTeamIndex`
  seam `team list`/`team show` already use. `team status` binds its own
  `--repo` flag directly (no `--harness` — each lockfile record already
  names its own harness, so exposing one would be misleading).
  `team update`/`team manage` stubs confirmed unchanged.

**Phase 1.5 — Code Review.** First pass: **BLOCKING** (CR-B-001) —
`runTeamStatus`'s loop over lockfile records returned immediately from inside
the loop on the first render/classify error, aborting the whole command and
silently dropping every other record from the report — undermining AC-1
("every tracked file classified") and AC-3 ("a missing-file record is
classified Missing, not silently skipped") for realistic failure modes (e.g.
a record whose source was since deleted/renamed), and diverging from this
codebase's established aggregate-via-`errors.Join` convention (the very
`teamsource.RenderTeam` this code calls into already follows it). Dispatched
one `go-implementation-engineer` to fix: per-record render/classify failures
are now isolated to that record's own report entry (`Status: "error"`, `Note`
set to the wrapped error), the loop continues to every other record, and all
per-record errors are aggregated via `errors.Join` alongside the existing
"N of M not in sync" summary — exit-code contract preserved (still a plain
error, never `UsageError`/`PreconditionError`). Re-review (scoped to
`cmd_team.go`'s `runTeamStatus` only) independently re-ran build/vet/gofmt/
test and confirmed the fix. Verdict: **ADVISORY_ONLY** — treated as
satisfying "not ADVISORY_ONLY with open blockers" per this orchestrator's
standing rule that ADVISORY_ONLY and CLEAN gate identically (both are
terminal, non-blocking states); recorded explicitly since the sponsor's
prose named "CLEAN" specifically. Full findings in `$session/CODE_REVIEW.md`.

## Code Review Advisories (Run 25, carried forward, non-blocking)

- **CR-A-001**: `RenderCurrent` is called once per lockfile record with no
  caching across records sharing the same `(harness, sourcePath)` pair — a
  performance nit, not a correctness issue at today's scale.
- **CR-A-002** (new): the fix for CR-B-001 introduces a sixth `Status:
  "error"` outcome for records whose current source can no longer be
  re-rendered/classified, but `newTeamStatusCommand`'s `--help` text and
  `runTeamStatus`'s doc comment still describe only the original five drift
  states — a documentation-completeness gap, not a functional defect.

Neither blocks correctness or this run's acceptance criteria; recorded for a
future pass.
