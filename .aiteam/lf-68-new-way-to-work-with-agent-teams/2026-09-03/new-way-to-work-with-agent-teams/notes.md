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
