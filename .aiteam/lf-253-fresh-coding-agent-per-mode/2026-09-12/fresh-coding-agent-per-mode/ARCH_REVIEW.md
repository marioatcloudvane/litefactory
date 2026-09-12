# Architect: Go Architect
# Created: 2026-09-12
# Feature: Fresh coding agent per mode (issue #253, supersedes #227)

**Verdict: APPROVED — buildable as specified, one story, three tasks. No major
deviation. No new package, no new store accessor, no migration, no new
dependency, no goroutine, no interface.**

---

## System Overview

### How This Feature Fits

Two existing seams change, both already on the single turn-preparation path.

`internal/daemon` admits the mode (`daemon/mode.go:admitMode`) and passes it as
`turn.Request.Mode`; `internal/turn.prepare` (`internal/turn/prepare.go:109`) is
the only place a conversation segment is selected, a runs row is allocated and a
prompt is assembled. Segment selection itself lives in exactly one function,
`prepareDeliverySegment` (`internal/turn/delivery.go:295`), which today rotates a
segment only when the effective instruction-bundle identity changed. That
function is where the mode-change reset belongs, and nowhere else.

The prompt is assembled by `harness.BuildPrompt`
(`internal/harness/prompt.go:378`), which both runner adapters
(`internal/harness/runner.go`, `internal/codex/runner.go`) go through — the
existing single construction point that makes BR-17/AC-12 true for free.

No package-map edge changes. `turn → store, naming, harness` and
`harness → agent, naming` are all pre-existing. `internal/daemon`,
`internal/store`, `internal/meter`, `internal/report`, `internal/teamsource`,
`internal/team`, `internal/planbrief` and `internal/cli` are untouched.

### New Packages / Components

None. Three additions inside existing packages:

- `internal/turn/session_artifacts.go` (new file, existing package) — resolves
  the repository-relative session artifact directory and the mode-relevant brief
  for one run, and populates the prompt input.
- `internal/harness/prompt.go` — two new optional `PromptInput` string fields and
  one `write…` helper, in the shape `writeAttachments` already establishes.
- `internal/naming/aiteam.go` — one exported constant for `MANIFEST.md` beside
  `ResearchBriefName` (spec A-7). `naming` stays pure and I/O-free.

### Key Boundaries

- **daemon → turn:** unchanged. The admitted mode already crosses as
  `turn.Request.Mode`; `Request.mode()` (`internal/turn/execute.go:438`) is the
  single reader, and it is the same string `allocateRun` writes to `runs.mode`.
  Nothing in this feature writes, retains or suggests a mode — CONC-21's
  machinery is read-only to this story.
- **turn → store:** in-process calls on existing accessors only —
  `ListSegments`, `GetOrCreateSegment`, `SegmentDelivery`, `SetSegmentDelivery`,
  `ListRuns` (through the existing `previousRun` helper). No new accessor, no
  SQL, no schema (AC-14).
- **turn → harness:** the `PromptInput` struct, by value. The new fields are two
  plain strings. No per-artifact type, no filesystem handle and no locator
  crosses into `harness` — the same boundary discipline the `Attachments` field's
  doc comment records.
- **harness → both runners:** unchanged. The artifact section is written inside
  `BuildPrompt`, so neither adapter learns it exists (AC-12).

### Data Flow

Reset: `daemon.admitMode` → `turn.Request.Mode` → `prepare` →
`prepareDeliverySegment` (lists segments, reads the candidate's last terminal
non-refused run, compares that run's `mode` with `req.mode()`) → rotate or reuse
→ `GetOrCreateSegment` → `previousRun` against the *selected* segment →
`resumeDecision` → `agent.PathCold` or `agent.PathResume`.

Pointer: `prepare` → `findSessionDir` (`internal/turn/sessiondir.go:38`, rooted
read of the worktree) → `PromptInput.{dir, brief}` → `harness.BuildPrompt` →
prompt on stdin (SEC-2) → either runner.

### Concurrency & Context

Nothing new. No goroutine, no channel, no shared mutable state (CONC-2 budget
unchanged). Segment selection is already serialized by the per-task lock and by
the daemon's one-worker-per-session admission; the added `ListRuns` read sits
inside that same critical section and is session-scoped, so a sibling worker's
rows are unreachable by construction (the reasoning is already written out above
`previousRun`, `internal/turn/prepare.go:1000-1023`). Every new call takes `ctx`
first (CONC-5). No retry, no sleep, no clock read is added below the daemon
(CONC-6).

### Shared State and Side Effects

- `segments` gains rows, never edits them (BR-2). A rotation is
  `last.Ordinal + 1` + `naming.SessionRef(ref, ordinal)` + empty resume token.
- Bundle retention is append-only and stays that way: `writeDelivery`
  (`internal/turn/delivery.go:190`) allocates an exclusive `MkdirTemp` directory
  per new segment and no code path deletes or rewrites an older root (AC-4).
- The prompt grows by one optional section. Every other consumer of the prompt —
  the metadata document, the run log, the report — takes it as bytes.
- `internal/turn/report.go:827` and `internal/turn/subissues.go:343` also call
  `findSessionDir`. Its signature and behavior must not change, or their links
  move with it.

---

## Cross-cutting decisions (binding on all three tasks)

**D-1 — One selection site, one predicate ladder.** All reset logic lives in
`prepareDeliverySegment`. The `p == nil && !hadDelivery` early return at
`delivery.go:318-327` must be folded into the common `ListSegments` path, so a
repository with no instruction bundle traverses the same ladder as one with a
bundle (BR-7, AC-3 — that early return *is* today's "no reset ever" bug). The
fold is behavior-preserving for the non-reset cases because `GetOrCreateSegment`
returns an existing row as stored, which is the same row the general path's
`last` already is.

**D-2 — Fixed predicate order, documented in the function's comment:**
continuation (return recorded segment, refuse on bundle change) → vendor
mismatch (return `last` unchanged so `prepare` raises `agentSwitchRefusal`) →
rotate when *(bundle identity changed **OR** mode changed)* → otherwise reuse.
The OR is what makes BR-5 one segment rather than two, and the vendor check
staying first is what makes BR-6 true without mentioning mode on that path.

**D-3 — Mode evidence is `previousRun`, not a new column or accessor.** Compare
`req.mode()` with the `Mode` of the last terminal, non-refused run *of the
candidate segment*, obtained by calling the existing
`previousRun(ctx, opts, session, last)`. Not found means no previous mode and
never a reset (BR-4, AC-7). `infer` participates as an ordinary value (BR-3).

**D-4 — Accept the second `ListRuns` read; do not thread a cache through.**
`prepare` calls `previousRun` again after selection, and it must, because after a
rotation it has to return *not found* against the new segment — that is what
delivers `agent.PathCold` with no new branch in either runner (BR-10, AC-5).
Reusing the pre-rotation result would silently resume a fresh conversation. Two
session-scoped reads inside one already-serialized critical section is the right
trade.

**D-5 — The rotation needs no idempotency flag.** Its idempotency key is "the
candidate segment has no terminal, non-refused run": after a failed prelaunch the
new segment row is already durable and carries no terminal run, so the second
`prepare` reuses it and `allocateRun`'s prelaunch match (`SegmentID` + `Mode`,
`prepare.go:1520`) still holds (AC-7). Adding a "reset already happened" marker
would be exactly the extra state BR-2 rules out.

**D-6 — Location in `turn`, wording in `harness`.** `turn` owns the worktree, the
branch→slug derivation and the filesystem; `harness` owns the prompt's voice and
its framing sentences. So the new `PromptInput` fields are two plain strings (the
repository-relative directory, the brief file name) and *not* a pre-rendered
section. This deliberately differs from `Attachments`, which arrives pre-composed
because `internal/attach` owns a per-attachment vocabulary that must stay out of
`harness`; here there is no vocabulary to keep out.

**D-7 — Populate on every path, render only when `!in.Resume`.** `prepare`
fills the fields unconditionally; `BuildPrompt` writes the section only on the
cold path (BR-15, AC-11). This is what keeps `harvest.go:228-233`'s
resume-refused rerun (`cold := prep.promptInput; cold.Resume = false`) correct
with no edit to `harvest.go`: flipping `Resume` stays the whole of the difference
between the two prompts, exactly as `PromptInput.History`'s doc comment records.
Gating the *population* on `resume` in `turn` would break that and is wrong.

**D-8 — Empty means the section does not exist.** No heading, no blank line, no
trailing newline — the discipline `writeAttachments`
(`internal/harness/prompt.go:512-538`) documents, and the whole of AC-10.

**D-9 — Rooted reads only.** Presence of the directory and of the brief is
established through the locator's existing rooted, symlink-refusing checks
(`sessionDirOn` / `checkStoryComponents` + `Lstat` + `IsRegular`), never a bare
`os.Stat` or `filepath.Join` + read. A symlinked or non-regular marker is "not
found" — never an error, never a refusal (SEC-21, spec edge table).

**D-10 — The rotation must be diagnosable.** An unexpected reset is otherwise
invisible until someone reads two transcripts. Log it once, at info, with the
previous mode, the admitted mode, the old and new ordinal. Use the injected
`*slog.Logger` (STYLE-2; no package-level logger, no `fmt.Print*`) — `prepare`
already holds one at the call site; `Options` does not carry one, so either pass
the existing parameter down or return the reason and log at the call site. The
engineer picks the seam; the observable is required.

---

## Suggested task decomposition

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-001.1 | Mode-change segment reset in `prepareDeliverySegment` | logic | — | US-1, US-2, US-4 | M |
| T-001.2 | Session-artifact pointer: locator in `turn`, section in `harness` | logic | — | US-3 | M |
| T-001.3 | Focused regression tests for AC-1..AC-14 | test | T-001.1, T-001.2 | US-1..US-4 | M |

T-001.1 and T-001.2 touch disjoint code and may run in parallel; they meet only
in `prepare`, at two different statements.

---

## Per-task hints

### T-001.1: Consistent mode-change resets
**Agent:** go-implementation-engineer

- **Pattern:** extend the existing predicate ladder in one function. This is the
  same shape as the bundle-identity rotation already there — read the last
  segment, decide reuse-or-rotate, write at most one new row.
- **Package placement:** `internal/turn/delivery.go`, inside
  `prepareDeliverySegment`. Nothing goes in `internal/store`, `internal/daemon`
  or a new file. Read `internal/turn/doc.go` first (STYLE-7).
- **Key interfaces:** `opts.DB` accessors already used here (`ListSegments`,
  `SegmentDelivery`, `GetOrCreateSegment`, `SetSegmentDelivery`) plus
  `ListRuns` via the package-local `previousRun` helper
  (`internal/turn/prepare.go:1024`). `naming.SessionRef(ref, ordinal)` derives
  the new conversation identity — it is the only derivation site, and
  `store.Segment.SessionRef`'s doc comment says the stored value wins.
- **Integration points:** upstream, `daemon.admitMode` supplies the mode and
  `Request.mode()` reads it; downstream, `prepare`'s `previousRun` +
  `resumeDecision` + `allocateRun` all key off the segment this function
  returns. Do not touch any of them: the reset works precisely because a fresh
  segment has no terminal run and no resume token (BR-10).
- **Watch out for:**
  - The `p == nil && !hadDelivery` early return is the bug (D-1). Folding it away
    must keep the non-reset outcome byte-for-byte identical for a first run.
  - Keep the vendor check first and return `last` *unchanged* on mismatch —
    creating a segment there would turn a refusal into a silent switch (BR-6,
    AC-6). Nothing may be written before that check.
  - Bundle identity change and mode change must share one rotation branch, not
    two sequential ones (BR-5, AC-4).
  - When a bundle exists, `writeDelivery` still runs before the row is created
    and `SetSegmentDelivery` after — preserve that order and never remove or
    rewrite a retained root (AC-4, and `writeDelivery`'s own comment).
  - The continuation branch returns before any of this. A continuation carries
    its own recorded mode and must not be compared (BR-8, AC-8, CONC-14).
  - `runs.mode` is the only recorded run mode; do not infer a mode from labels,
    from the bundle's `bootstrap.md` or from `harness.InferMode` heuristics.
  - A task whose mode label is removed mid-flight will alternate
    label-mode/`infer` and reset each time. That follows from the approved rule
    and is accepted — D-10's log line is how an operator sees it.
  - STYLE-1: the function's doc comment ("only changes in effective instructions
    start another same-vendor segment") becomes false. Update it in the same
    commit.
- **Not in scope:** any change to `daemon/mode.go`, `store/modes.go`, mode
  admission, label handling, bundle construction, `planDelivery`'s identity
  inputs, or the incidental mode component of `bootstrap.md` (spec A-8).

### T-001.2: Existing-artifact pointer in the prompt
**Agent:** go-implementation-engineer

- **Pattern:** the optional-section pattern `writeAttachments` establishes —
  an optional field on `PromptInput`, one small writer, empty writes nothing.
- **Package placement:** the locator and the mode→brief policy in a new
  `internal/turn/session_artifacts.go`; the two fields and the writer in
  `internal/harness/prompt.go`; the `MANIFEST.md` constant in
  `internal/naming/aiteam.go` beside `ResearchBriefName`. Do not put the
  mode→brief mapping in `harness` — that package knows the mode only as a header
  line, and it must stay that way.
- **Key interfaces:** `findSessionDir(worktreeDir, branch, issue, day, marker,
  log)` unchanged; `naming.ResearchBriefName`, `planbrief.FileName`, the new
  manifest constant; `harness.PromptInput`.
- **Integration points:** call it from `prepare` alongside the other
  `prepare…Context` helpers (`internal/turn/prepare.go:376-395`), after
  `allocateRun` so `run.StartedAt` is the day handed to the locator — the same
  input `preparePlanAttempt` uses. `harness.BuildPrompt` renders it for both
  adapters.
- **Watch out for:**
  - Populate unconditionally, render on `!in.Resume` only (D-7). Do not add a
    second resolution call in `harvest.go`.
  - Markers are tried in the fixed BR-13 order and the first that resolves wins;
    the brief is named only if that file is actually present, checked through the
    same rooted path discipline (D-9). A located directory with no brief names
    the directory alone.
  - Reuse `findSessionDir`; do not write a new scan or a walk, and do not widen
    it — `report.go` and `subissues.go` depend on its current behavior.
  - Repository-relative, forward slashes. No absolute worktree path and nothing
    under `$LF_HOME` may appear in the prompt: the prompt is copied into the run
    metadata document, and a runtime path in it leaks layout for no gain
    (CFG-8's spirit).
  - The section is litefactory's own assertion, placed with the attachment
    section — above `## Context`, because these are files this run may open, not
    third-party text to weigh. It must read as a location, not as authority: it
    grants no tool, no permission and no scope (BR-18, SEC-20, SEC-3).
  - Nothing untrusted may reach the path. The slug comes from
    `naming.SlugFromBranch` over the task branch; issue and comment text must not
    influence it.
  - `"- Mode: %s\n"` in `writeHeader` must not move or change wording — STYLE-8
    pairs it with `CLAUDE.md`'s opening line and no test catches the other side.
  - `preparePlanAttempt` already runs a two-marker lookup for the plan context
    path. Leave it alone; it fires only when a plan proposal path is set and
    answers a different question.
  - Leave the `PreviousMode` branch in `writeResumeHeader` in place (spec A-9),
    but its doc comment now describes an unreachable case — say so in the comment
    rather than leaving a comment that reads as current behavior (STYLE-1).
- **Not in scope:** recording which artifacts a run read, verifying artifact
  contents, any handoff/context-transfer mechanism, reading an old transcript,
  or any change under `.claude/**`, `.codex/**` or `teams/source/**`.

### T-001.3: Focused regression tests
**Agent:** go-implementation-engineer

- **Pattern:** table-driven, stdlib `testing`, hand-written fakes (TEST-1,
  TEST-2). Extend the existing fixture files rather than adding a parallel
  harness (TEST-3).
- **Package placement:** `internal/turn` and `internal/harness` are tested
  in-package throughout — no `_internal_test.go` suffix in either (TEST-4).
  Segment/run assertions belong beside `internal/turn/prepare_test.go` and
  `resume_test.go`; prompt byte assertions beside
  `internal/harness/prompt_test.go` and `prompt_attachments_test.go`; the
  locator cases beside `internal/turn/sessiondir_test.go`.
- **Key interfaces:** turn's real-git + migrated-SQLite + fake-runner fixture,
  `internal/gittest` for worktrees, and the store directly for reading back
  ordinals, session refs and resume tokens. A promise about a transaction is
  tested against real SQLite, never a fake store (TEST-6).
- **Integration points:** AC-5's routing half is asserted through the existing
  runner fixtures (`harness/runner_test.go`, `codex/runner_test.go`) as
  `agent.RunPath` and argv shape. AC-14 extends the existing migration inventory
  assertion.
- **Watch out for:**
  - AC-10's byte identity is only meaningful against output built from the same
    input with the new fields empty — compare two `BuildPrompt` results, not a
    hand-copied literal that will rot.
  - No sleeps, no `Eventually`, no wall-clock polling in `turn` (TEST-8). None of
    this is concurrent, so there is nothing to wait for; if a test wants to wait,
    the design is wrong.
  - Never launch a real `claude` or Codex, and never reach the network (EXT-1,
    EXT-5). Everything here is offline.
  - Sequence cases (`research → plan → implement`, and a return to an
    earlier mode) must assert ordinals 1,2,3,4 and distinct recomputable session
    refs — the forward-only property is the one a future edit is most likely to
    break.
  - Assert the negatives as hard as the positives: same mode resumes; no terminal
    run never resets; vendor change refuses with *no* segment and *no* run row;
    continuation resumes its recorded segment.
  - Treat a failure in `config/purity_test.go`, `git/surface_test.go`,
    `github/surface_test.go`, `turn/gate_test.go`, `daemon/clock_test.go` or the
    `store` migration test as a design change to stop and report, not a test to
    adjust (TEST-10).
  - `make check` (fmt, vet, pinned golangci-lint v2, race) is the gate, not
    `go test ./...` alone (AC-15).
- **Not in scope:** any live-agent capture, CI changes, sandbox permission
  changes, or a new test module/assertion library.

---

## Security notes (forward-looking, this plan only)

**Scope:** the planned change. **Rulebook:** `.aiteam/APP_CONSTRAINTS.md`.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ✅ | The artifact path is derived from the task branch via `naming.SlugFromBranch`; no issue, comment or story text reaches it. SEC-20 holds — untrusted text stays inside its existing note framing. |
| 2 | Filesystem boundary | ⚠️ → controlled | New reads must stay on the locator's rooted, symlink-refusing path (D-9). A bare `os.Stat` for the brief-presence check would be the one regression here (SEC-21). |
| 3 | Authorisation | ✅ | The pointer names files; it grants nothing. Tool surface (SEC-3), sandbox and question permission (CONC-23) are untouched. Mode admission and frozen managed scope (CONC-9, CONC-12) are read-only to this story. |
| 4 | Secrets & config | ✅ | No config key, no credential, no `$LF_HOME` path in the prompt. CFG-1..CFG-8 unaffected. |
| 5 | Data handling | ✅ | No schema, no SQL, no new query. Existing parameterised accessors only (AC-14). |
| 6 | Dependencies | ✅ | No new dependency; `go.mod`'s four direct requires stay as they are (TEST-2). |
| 7 | Error handling | ✅ | An unlocatable session directory is a value, not an error, and never a refusal (CONC-3 read the right way round: there is no failed operation to report). Store and locator errors keep `%w` wrapping. |
| 8 | Process boundary | ✅ | The prompt still goes in on stdin, never argv (SEC-2); the child environment allowlist is untouched (SEC-4). |

## Platform constraints (Go lens)

- Layout — applies: everything lands under `internal/`; no new top-level package,
  nothing promoted to `pkg/`.
- Error handling — applies: no new sentinel is warranted; "no session directory"
  is a `(string, bool)`, matching `findSessionDir`'s existing shape.
- Concurrency — not applicable (no goroutine, no shared state); `ctx` stays first
  on every store call.
- Interfaces — not applicable: no new interface. Do not add one for the locator;
  a single concrete implementation with no substitution need is premature
  (TEST-5 also forbids declaring one next to its implementation).
- Dependencies — not applicable (none added).
- HTTP — not applicable.
- Database — applies only as a prohibition: no new table, column or migration.
- Testing — applies: table-driven, in-package per TEST-4, hand-written fakes,
  `-race` via `make check`.
- Logging — applies: the rotation log line uses the injected `*slog.Logger`.
- Lint — applies: new prompt text is a Go string literal; run `make check` before
  calling the task done.

## Evidence obligations carried into the story

The spec's evidence table is correct and must survive into the story's
acceptance evidence rather than being simplified:

- **AC-5, live half** — that real `claude` and real Codex actually begin a new
  conversation on reset: **unavailable** under the current sandbox
  (`docs/decisions/nested-agent-evidence.md`, EXT-4). Proof is limited to
  litefactory's own invocation inputs (path, session id, absent resume token).
  Do not claim end-to-end conversation-lifecycle verification.
- **AC-9, comprehension half** — that a reset agent opens and uses the named
  brief: **unavailable**. The criterion is met by the prompt naming directory and
  brief, not by observed agent behavior.

Both stay recorded as unsatisfied obligations. A passing offline suite does not
convert either (EXT-4, EXT-5).

## Deviations

None major; nothing here requires an owner decision.

One tension resolved within my level: the spec places deletion of the
now-unreachable `PreviousMode` branch in `writeResumeHeader` out of scope (A-9),
while STYLE-1 requires comments to stay true. Resolution: keep the branch,
update its doc comment to record that it is unreachable and why. Same for
`prepareDeliverySegment`'s own doc comment. No `.aiteam/AC_AMENDMENTS.md` entry
is needed.

## Gaps outside my specialization

None. This change is entirely Go under `internal/`; no frontend, infrastructure,
CI/CD, ML or mobile surface is involved, and no instruction-tree story is needed
(`.claude/**` and `.codex/**` are untouched, so the protected-tree split does not
apply).
