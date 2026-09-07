# Architecture Review — litefactory#126

# Architect: Go Architect
# Created: 2026-09-06
# Input: `$session/feature-spec.md` (Status: APPROVED, 2026-09-06)
# Verdict: **APPROVE WITH CONDITIONS**

The spec is unusually well grounded. Every one of VF-1..VF-7 was re-verified
against the source in this review and every one holds character for character.
The four conditions below are guardrails on *how* the change lands, not
objections to *what* it does. No story needs re-scoping and no product decision
(D-1..D-4) is re-opened.

---

## System Overview

### How This Feature Fits Into the Existing System

This is not a new capability. It is a correction to a decision that is made in
exactly one place today and read in three others. `internal/turn` decides
whether a run's worktree becomes a commit; `internal/harness` decides what the
next run is told about its predecessor; `internal/report` decides what the
human is told about both. All three currently answer as if "the run finished"
and "the run succeeded" were the same fact. The feature separates them.

Nothing moves between packages. No package is created. The dependency direction
recorded in `.aiteam/APP_CONTEXT.md` is untouched: `turn` continues to import
`harness`, `store`, `git` and `report` continues not to import `turn`. The
whole change is four gate/branch edits, two new struct fields, one new derived
method, and one new package-level string constant — plus their tests.

The pivot is that litefactory already holds the truth it needs and simply does
not consult it. `agent.RunResult.Succeeded()` exists
(`internal/agent/result.go:171`, pointer receiver, nil-safe).
`store.Run.Status` and `store.Run.Error` are both already on the row
(`internal/store/runs.go`), which is what makes D-3 (no schema change) sound
rather than a compromise. The feature is a consultation of existing facts, not
an acquisition of new ones.

### New Packages / Components Introduced

None. The additions are:

| Symbol | Home | Responsibility |
|---|---|---|
| `store.Run.Succeeded() bool` | `internal/store/runs.go` | Derived predicate over the persisted row: true iff `Status == RunSucceeded`, or `Status == RunReported && Error == ""`. The *only* new exported symbol. |
| `harness.PromptInput.PreviousFailed bool` | `internal/harness/prompt.go` | Resume-path-only fact: the predecessor run did not succeed. |
| `harness.PromptInput.PreviousReason string` | `internal/harness/prompt.go` | Resume-path-only fact: the predecessor's one-line reason. |
| a corrective-note constant | `internal/harness/prompt.go` | litefactory's own voice, sited beside `askingRule` / `askingRevokedRule`. |

`turn.preparation` gets no new field (spec is right: `previous` is already in
scope at the `PromptInput` literal, `internal/turn/prepare.go:235-254`).

### Key Boundaries

Four boundaries carry this change, and each has exactly one owner:

1. **The commit gate — `internal/turn/harvest.go:save`.** The single place that
   decides "worktree becomes commit". US-1 widens its guard from
   `Outcome == OutcomeAbandoned` to `!runResult.Succeeded()`. `commitAndPush`
   below it is unchanged and stays the only commit-side `CloneGate.Acquire`
   call site.

2. **The second commit gate — `internal/turn/subissues.go:fileStoryIssues`.**
   The write-back that stages with `git add --all`. Its own source comment
   already says a commit failure here would "sweep claude's still-uncommitted
   work into a commit no runs row names" — which is precisely what US-1 alone
   would cause. This is a boundary, not an afterthought.

3. **The prompt contract — `harness.PromptInput` → `BuildPrompt`.** `turn`
   populates a value; `harness` renders it purely (no clock, no filesystem, no
   env). The two new fields cross this boundary as data only.

4. **The report contract — `turn.Result` → `internal/report`.** Read-only. US-3
   and US-6 change rendering in `internal/report/run.go` and require *no*
   change to what `turn` hands over.

### Data Flow

Unchanged in shape; changed in one branch and one field:

```
run subprocess (harness)
  → classify() → Outcome + Reason
  → FinishRun (durable, S-9)          ← runs.status + runs.error land here
  → save()      ── gate: succeeded? ──→ commitAndPush   (US-1 changes the gate)
                        └── no ───────→ log + leave in worktree
  → fileStoryIssues() ── gate: succeeded? ─→ git add --all write-back  (US-2)
  → finish()    (SetRunCommit only when CommitSHA != "" — already correct)
  → report      (US-3 "nothing committed, work is at <path>"; US-6 cost)

next turn, same segment:
  previousRun() → store.Run
  → resumeDecision()  (unchanged — a failed Claude predecessor still resumes, D-2)
  → PromptInput{PreviousFailed: !previous.Succeeded(), PreviousReason: previous.Error}
  → writeResumeHeader() → corrective note → stdin of the child process
```

The one genuinely **new** data flow in this feature is the last arrow but one:
`runs.error` of run N becomes prompt text of run N+1. It is the only place
where data crosses a run boundary that did not cross it before, and it is where
the security lens concentrates.

### Concurrency & Context Notes

- `runAndHarvest` runs its bookkeeping under
  `bookkeeping := context.WithoutCancel(ctx)` because bookkeeping must outlive
  cancellation (D-B, AC-4.1). **The widened `save` gate sits inside that
  region.** Returning early from `save` must not skip anything the
  `WithoutCancel` region exists to guarantee — `finish` and the deferred meta
  write both still run. Verified: `save` already has an early-return arm for
  abandonment and it is safe; the new arm is the same shape.
- **CONC-7 is adjacent to the US-4 edit.** `SetSessionState(ctx, session.ID,
  store.SessionRunning)` must remain prepare's last statement before a
  successful return and must not be wrapped in `context.WithoutCancel`
  (`internal/turn/prepare.go:280-298`). The two new field assignments belong in
  the `PromptInput` literal ~45 lines above it. Do not relocate that call.
- No goroutine is introduced by any story. No `sync` primitive is introduced.
  There is no new lifetime to own.

### Shared State and Side Effects

- **Removed side effect, by design:** a non-success run no longer produces a
  commit or a push. Downstream consumers of `result.CommitSHA` /
  `result.Changed` / `result.Pushed` must tolerate the empty/false case for a
  failed run. Verified they already do: `finish` guards `SetRunCommit` on a
  non-empty SHA, `FinishRun` `COALESCE`s an empty SHA rather than clearing, and
  `internal/report/run.go`'s Commit branch already has a `case run.Branch != ""`
  arm for "committed nothing".
- **`turn.Result.Succeeded()` does not change.** It is
  `Disposition == DispositionRan && Outcome == OutcomeSucceeded && Err == nil`
  (`internal/turn/result.go:236`). Widening `save`'s gate leaves it untouched,
  so session state (`SessionIdle` vs `SessionFailed`), `Reportable()` and
  `ConsumesComment()` do not regress. This was the largest ripple risk in the
  plan and it is ruled out.
- **`runs.error` gains a second reader.** It has been a report-and-log field;
  it becomes prompt input. That is a widening of its blast radius and should be
  documented in its doc comment as part of US-4 (STYLE-1).

---

## Lens 1 — Engineering

### Feasibility

High confidence. Every edit site named in the spec's interface-change table was
opened and matches the spec's description exactly. Three acceptance criteria
turn out to be satisfied by code that already exists and need a *test* rather
than a *change*:

- "empty `commit_sha` for a failed run" — `finish` already guards
  `SetRunCommit` on `result.CommitSHA != ""`, and `store.FinishRun`
  `COALESCE`s an empty SHA.
- "`resumeDecision` still returns true for a failed Claude predecessor" —
  `resumeDecision` returns true unconditionally for `agent.Kind(store.AgentClaude)`
  once `found && segment.Agent` matches; failure is not consulted.
  `terminalRunStatus` already counts `RunFailed`.
- "the two new fields are inert on the cold path" — `runClaude`'s cold rerun
  copies the whole input (`cold := prep.promptInput; cold.Resume = false`), and
  `BuildPrompt` routes to `writeHeader`, which reads neither field.

Engineers should be told this explicitly so they do not "fix" working code.

### Implementation order

```
US-6 ─────────────────────────────────────────► (independent, ship anytime)
US-1 ──┐
       ├─ must ship together ──► US-3 ──► (report can then tell the truth)
US-2 ──┘
US-4 ──► US-5
```

- **US-1 and US-2 are one shipping unit.** VF-7/A-7: `fileStoryIssues` stages
  with `git add --all`. Land US-1 without US-2 and the failed run's work is
  still committed, now under litefactory's *own* commit message
  (`subIssueCommitSummary`) and attached to no runs row at all. That is strictly
  worse than the bug being fixed. **Do not merge US-1 alone.**
- **US-3 after US-1/US-2.** Its whole point is to describe a state that only
  exists once the gate is widened. Shipping US-3 first makes the report claim
  "nothing was committed" while a commit exists.
- **US-4 before US-5.** US-5 renders fields US-4 populates. US-5 can be *written*
  against the field definitions in parallel, but its end-to-end test needs US-4.
- **US-6 is fully independent** — a three-way branch in
  `internal/report/run.go:582-585`, touching nothing else. Good first task, good
  parallel task.

### Dependency risks — assessed

| Risk | Assessment |
|---|---|
| Widening `save` changes session state for failed runs | **Ruled out.** `finish` derives state from `result.Succeeded()`, which is unchanged. |
| Widening `save` breaks TEST-10 / `turn/gate_test.go` | **Low.** The pinned invariant is *which call sites* acquire the clone gate. `commitAndPush` is not moved and no site is added or removed. Run the test; do not edit it (TEST-10). |
| `!runResult.Succeeded()` nil-derefs | **Ruled out.** Pointer receiver, nil-safe (`internal/agent/result.go:171`). In `fileStoryIssues` the existing `runResult == nil` check becomes redundant — keep it anyway for readability; deleting it is a needless diff. |
| Two `Succeeded()` methods get conflated | **Real.** See cross-story decision C. |
| `writeResumeHeader` output drifts for a succeeded predecessor | **Real but containable.** See cross-story decision D. |
| STYLE-8 tripwire | **Real.** See cross-story decision E. |
| `TimedOut` runs lose work | **Not a regression, but a behaviour change worth naming.** Today a two-hour timeout commits what it produced; after US-1 it does not. This is exactly D-1 and it is approved — but US-3's report text must be genuinely good for this case, because it is the case where a human most needs to be told where the work is. `describeRunOutcome`'s TimedOut arm already says "The work it had done up to that point is still in the worktree" — it must now also carry the path, as the other arms will. |

### One correction to the spec's framing

The spec's data-model section presents `store.Run.Succeeded()` as if it were a
shared prerequisite. It is not: the two turn-side gates (US-1, US-2) operate on
a live `*harness.RunResult` and use the **already-existing**
`agent.RunResult.Succeeded()`. `store.Run.Succeeded()` is needed only by
`prepare` (US-4), which reads a persisted row. This is a clarification, not a
disagreement — but it materially changes the critical path: US-1/US-2 have **no**
blocking dependency on the new store method and can start immediately.

---

## Lens 2 — Platform

Per `.claude/skills/go/platform-constraints.md`:

- **Platform: single Go module, everything under `internal/`, `cmd/` is wiring
  only — no implication.** Every edit lands in an existing `internal/` package.
  No new module, no `pkg/` candidate, no `cmd/` change.
- **Platform: package dependency direction is acyclic and `internal/report`
  must not import `internal/turn` — respected.** US-3 and US-6 change rendering
  inside `internal/report/run.go` using data already on the view type. No new
  import edge is required in either direction.
- **Platform: `internal/turn` orchestrates one turn and has no clock;
  `internal/daemon` owns time — respected.** Nothing in this feature reads a
  clock. `BuildPrompt` stays pure (no clock, no filesystem, no env), which the
  US-5 constant preserves.
- **Platform: `internal/harness` is the only place that execs `claude` — not
  applicable (unchanged).** No new subprocess, no new flag, no change to the
  `--session-id`/`--resume` contract.
- **Platform: fourteen packages carry a `doc.go` that is the real spec for the
  package — actionable.** `internal/turn/doc.go` and `internal/harness/doc.go`
  both describe the commit and prompt behaviour this feature changes. Updating
  them is part of the change, not follow-up (STYLE-1).
- **Platform: a live daemon runs from `dist/litefactory`; do not `make build`
  and do not write into `dist/` — actionable.** Verification is
  `go build ./...`, `go test ./...` (`make test` adds `-race`), `go vet ./...`,
  `gofmt -l .`. `make check` is the full gate.
- **Platform: no build tags and no integration tests exist in the tree — has
  implication.** Every test in this feature is a package unit test with
  hand-written fakes (TEST-2). Do not propose an integration harness; there is
  nothing to hang it on and introducing one is out of scope.
- **Platform: SQLite pool capped at one connection, hand-rolled embedded
  migrations — not applicable (no schema change).** D-3 holds; `Status` and
  `Error` are already columns. Explicitly: **no new migration file.**
- **Platform: NULL mapping — numbers whose zero is a real value are pointers —
  directly relevant to US-6.** `runOutcome` already stores cost as `*float64`
  guarded by `HasCost || CostUSD > 0` (SD-3). The persistence side is correct;
  only the *display* side is wrong. US-6 must not touch `runOutcome`.
- **Platform: `make check` = fmt vet lint test — no implication beyond running
  it.**

---

## Lens 3 — Security (forward-looking)

Per `.claude/skills/shared/security-scanner.md`.

**Scope:** the change proposed by `$session/feature-spec.md` — the two commit
gates, the prompt-input plumbing, the resume corrective note, and the cost
display. Existing behaviour is assessed only where this change alters its
exposure.

**Rulebook:** `.aiteam/APP_CONSTRAINTS.md` (SEC-2, SEC-5, SEC-6, SEC-7, CONC-3,
CONC-4) plus the prompt-injection defences documented in
`internal/harness/prompt.go`.

| Area | Finding | Detail |
|---|---|---|
| Input validation | ⚠️ **Concern** | `PreviousReason` is sourced from `runs.error`. One arm of `harness.classify` fills that column with **agent-authored text**: `"claude reported is_error on its terminal result event: " + excerpt(c.result.Result)` (`internal/harness/runner.go:880-882`). `excerpt` strips CR/LF and caps at `reasonExcerptLength` = 200 bytes — it is explicitly documented as "not redaction… length and newline control". That string then lands in `writeResumeHeader`, which is **litefactory's own authoritative voice**: unlike every other quoted body in the prompt it is not wrapped in a `<note>` frame, not passed through `neutraliseNoteTags`, and not capped by `capText`. Net effect: up to 200 bytes of model-authored prose gets to speak as litefactory to the next run. See "Guardrail" below. |
| Input validation | ✅ Compliant | The new prompt text travels to the child on **stdin** via `harness.Request.Prompt`, never argv (**SEC-2**). No ARG_MAX concern and no process-table exposure. |
| Auth / authz | ✅ Compliant | No authentication, authorization, token or permission surface is touched. `DefaultSettings`, `defaultAllowedTools()` and the Codex sandbox mode are all out of scope (spec's Out of Scope, and VF-2/VF-3 are recorded as accepted risk, not as this feature's work). |
| Secrets & configuration | ✅ Compliant | No secret is read, written, logged or rendered. No config key is added. The corrective note is a compile-time constant. |
| Data handling | ✅ Compliant, with a note | `runs.error` gains a reader but not a new sink: it already reaches the forge comment via `internal/report`, where **SEC-6** applies (renderers return `redact.Body`; `github.PostComment` is the sole consumer). The prompt is persisted verbatim in `NNNN.meta.json`, which is 0600 and local — the same accepted exposure `historyNote` already documents, and the local `$LF_HOME/logs` copy deliberately stays unredacted per SEC-6. **No redaction change is needed and none should be made.** |
| Data handling | ✅ **Improvement** | US-1 *reduces* exposure. Today a failed run's output is committed and **pushed to the remote** under a summary marked "Untrusted: claude wrote it". After US-1 that content stays in a local worktree until a human looks at it. Gating commits on outcome does not introduce risk; it removes a path by which unreviewed, failure-state output reached `origin` automatically. |
| Data handling | ✅ Compliant | **SEC-7** is not weakened. The change only ever makes litefactory write *less*; `git.Clone`'s method set (the allowlist — no `Commit`/`Checkout`/`Reset`/`Merge`/`Pull`) is untouched, and **TEST-10**'s `turn/gate_test.go` continues to pin the three `CloneGate.Acquire` call sites. |
| Dependencies & supply chain | ✅ Compliant | No dependency added. No `go.mod` / `go.sum` change. **TEST-2** (no mocking framework, no new module) is satisfiable with hand-written fakes throughout. |
| Error handling | ✅ Compliant | **CONC-3** holds: a failed run stays a *value* (`Outcome` + `Reason`), and `Result.Err` remains nil for a run that merely failed — the widened gate returns the result, it does not manufacture an error. **CONC-4** (post first, then commit) is untouched: the reporting order in `runAndHarvest` does not move. Silence remains forbidden (AC-4.1) — US-3 is what keeps the report loud about the new state. |
| Error handling | ⚠️ **Concern (minor)** | `fileStoryIssues`'s doctrine is that *every* failure is a log line and an early return — never a mutation of `result`, never a returned error. The widened guard must keep that shape. A guard that logs at `Error` level for the ordinary case of "the run failed, so no sub-issues" would turn a normal outcome into alarm fatigue. Log at `Info`/`Warn` and say why, matching `save`'s existing abandonment log. |

**Summary (2 sentences).** The feature is net security-positive: it closes an
automatic path by which the output of a failed, unreviewed run reached the
remote, and it adds no dependency, secret, or authorization surface. The single
new risk is that a 200-byte, agent-influenceable excerpt from `runs.error`
enters the next run's prompt inside litefactory's unframed authoritative voice
— which the guardrail below closes.

### Guardrail for US-5 (binding)

`PreviousReason` **MUST NOT** be interpolated raw into the resume header. The
engineer has two acceptable options and must pick one and say which in the task
notes:

- **Preferred:** render the reason inside the same `<note>` framing every other
  quoted body uses, passing it through `neutraliseNoteTags` and `capText`, so
  the child sees it as quoted material and not as litefactory's instruction.
- **Acceptable:** keep it in litefactory's voice but pass it through
  `neutraliseNoteTags` at minimum, and add a test asserting that a
  `PreviousReason` containing `</note>`, a fake `- Mode:` line, or a fake
  `# litefactory run` heading cannot forge header structure.

"`excerpt` already caps it at 200 bytes" is **not** sufficient on its own: 200
bytes is ample room for a forged directive, and `excerpt` bounds length, not
meaning. Its own doc comment says so.

---

## Lens 4 — Task Decomposition

Per `.claude/skills/shared/task-decomposer.md`. One task = one deliverable;
tests are separate entries; no task exceeds `L`; `—` means no dependency. This
is a single-layer team, so every task is assigned to
**`go-implementation-engineer`**. These tables are per-story content and belong
in the story sub-issues, **not** in `plan_brief.md`.

### US-1 — a non-success run commits nothing

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-1.1 | Widen `save`'s gate in `internal/turn/harvest.go` to skip `commitAndPush` for any non-success outcome | logic | — | US-1 | S |
| T-1.2 | Rewrite `save`'s doc comment, which currently argues the opposite behaviour, and update `internal/turn/doc.go` where it describes commit-on-failure (STYLE-1) | logic | T-1.1 | US-1 | XS |
| T-1.3 | Table-driven unit tests over all five outcomes: only `Succeeded` reaches `commitAndPush`; the other four leave the worktree uncommitted and log the worktree path | test | T-1.1 | US-1 | M |
| T-1.4 | Test that a failed run leaves `Result.CommitSHA` empty, `Changed`/`Committed`/`Pushed` false, `Result.Err` nil, and `Result.Succeeded()` false — the non-regression pin | test | T-1.1 | US-1 | S |
| T-1.5 | Test that `finish` writes no `commit_sha` and that `store.FinishRun` leaves the column untouched for a failed run (pins existing behaviour; no production change expected) | test | T-1.1 | US-1 | S |
| T-1.6 | Run `turn/gate_test.go` (TEST-10) unmodified and confirm the three `CloneGate.Acquire` call sites are unchanged | test | T-1.1 | US-1 | XS |

### US-2 — the sub-issue phase skips a non-success run

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-2.1 | Widen the `fileStoryIssues` guard in `internal/turn/subissues.go` to `runResult == nil \|\| !runResult.Succeeded()`, preserving the log-and-early-return doctrine (no `result` mutation, no returned error) | logic | — | US-2 | S |
| T-2.2 | Update the guard's comment to state the real reason: `git add --all` would otherwise sweep the failed run's uncommitted work into a commit no runs row names (VF-7) | logic | T-2.1 | US-2 | XS |
| T-2.3 | Tests: for each non-success outcome the story-index write-back does not run, `result` is returned byte-identical, and no error is returned | test | T-2.1 | US-2 | M |
| T-2.4 | Regression test for the relocation bug: a failed run with a dirty worktree produces **no** commit from either `save` or `fileStoryIssues` | test | T-1.1, T-2.1 | US-2 | M |

### US-3 — the report says plainly that nothing was committed and where the work is

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-3.1 | Change the "none — this run committed nothing" arm in `internal/report/run.go` to name the worktree path for a non-success run | logic | — | US-3 | S |
| T-3.2 | Extend `describeRunOutcome`'s Failed / NoResult arms (and confirm TimedOut) so each says the work is still in the worktree, following the existing `withFailure` wording | logic | T-3.1 | US-3 | M |
| T-3.3 | Golden-ish rendering tests per outcome: the failed report names the worktree exactly once, does not claim a commit, and the succeeded report is unchanged | test | T-3.1, T-3.2 | US-3 | M |
| T-3.4 | Test that the worktree path is rendered through the package's existing sanitiser/span helpers and cannot break the comment's markdown (SEC-5) | test | T-3.1 | US-3 | S |

### US-4 — a resumed run knows its predecessor failed

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-4.1 | Add `store.Run.Succeeded()` to `internal/store/runs.go` with a doc comment stating the two-arm rule and why `RunReported && Error == ""` is the durable success signal (VF-6) | logic | — | US-4 | S |
| T-4.2 | Extend `runs.error`'s doc comment to record its new second reader — it is now prompt input, not only report/log text (STYLE-1) | logic | T-4.1 | US-4 | XS |
| T-4.3 | Add `PreviousFailed` / `PreviousReason` to `harness.PromptInput`, doc-commented in the same "read only on the resume path" style as `PreviousMode` | api | — | US-4 | S |
| T-4.4 | Populate both fields from `previous` in `prepare`'s `PromptInput` literal, leaving the CONC-7 `SetSessionState` call untouched as prepare's last statement | logic | T-4.1, T-4.3 | US-4 | S |
| T-4.5 | Table tests for `store.Run.Succeeded()` across all four statuses × empty/non-empty `Error` | test | T-4.1 | US-4 | S |
| T-4.6 | Tests that `prepare` sets both fields correctly for a succeeded, failed, timed-out and absent predecessor, and that `resumeDecision` still returns true for a failed Claude predecessor (pins existing behaviour) | test | T-4.4 | US-4 | M |
| T-4.7 | Test that CONC-7 still holds: `SetSessionState(..., SessionRunning)` remains prepare's last store write and is not wrapped in `context.WithoutCancel` | test | T-4.4 | US-4 | S |

### US-5 — the prompt states the predecessor failed and disowns its claims

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-5.1 | Add the corrective-note constant beside `askingRule` / `askingRevokedRule` in `internal/harness/prompt.go`, in litefactory's voice, saying the predecessor failed and that its unverified claims are not this run's accomplishments | logic | T-4.3 | US-5 | S |
| T-5.2 | Emit the note in `writeResumeHeader` after the `- Feature slug:` line and before the infer-guidance block, gated on `PreviousFailed`, without touching the `"- Mode: %s\n"` literal (STYLE-8) | logic | T-5.1 | US-5 | M |
| T-5.3 | Apply the security guardrail to `PreviousReason` — `<note>` framing plus `neutraliseNoteTags`/`capText`, or at minimum `neutraliseNoteTags` — and document which was chosen and why | logic | T-5.2 | US-5 | M |
| T-5.4 | Byte-for-byte test that a resume prompt with a **succeeded** predecessor is unchanged from today, across the infer-guidance and ask-permission combinations | test | T-5.2 | US-5 | M |
| T-5.5 | Byte-for-byte test that the cold-start prompt is unchanged, and that the cold rerun after a resume refusal carries the fields inertly | test | T-5.2 | US-5 | S |
| T-5.6 | Injection test: a `PreviousReason` containing `</note>`, a forged `- Mode:` line and a forged `# litefactory run` heading cannot forge header structure | test | T-5.3 | US-5 | M |
| T-5.7 | Test that `BuildPrompt` stays pure with the new branch — no clock, no filesystem, no env — and that the note appears in the persisted `NNNN.meta.json` prompt | test | T-5.2 | US-5 | S |

### US-6 — an unknown cost renders "unknown"

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-6.1 | Replace the `res.CostUSD > 0` branch in `internal/report/run.go:582-585` with a `HasCost`/`HasResult`-driven three-way, matching the sibling "Exit status: unknown" and "Duration: unknown" patterns already in the function | logic | — | US-6 | S |
| T-6.2 | Table tests: reported non-zero cost renders the amount; reported zero cost renders `$0.0000`, not "unknown"; no terminal result renders "unknown"; the Cost row is never silently omitted | test | T-6.1 | US-6 | M |

**Totals.** 27 tasks, 12 logic/api and 15 test, none above `M`. The
implementation surface is genuinely small; the weight is in the tests, which is
the correct shape for a correctness fix to a decision this load-bearing.

---

## Cross-Story Decisions (binding)

**A. US-1 and US-2 ship as one unit.** Merging US-1 alone relocates the bug into
`fileStoryIssues`'s `git add --all` write-back, where the failed work is
committed under litefactory's own message and named by no runs row. Strictly
worse than the status quo. T-2.4 is the test that proves they landed together.

**B. There are two `Succeeded()` predicates and they must not be collapsed.**
`agent.RunResult.Succeeded()` (already exists, pointer receiver, nil-safe) is
the live-outcome predicate for the two turn-side gates, US-1 and US-2.
`store.Run.Succeeded()` (new, T-4.1) is the persisted-row predicate for the
prepare-side plumbing, US-4. Different sources of truth, different lifetimes,
different failure modes. Do not add a conversion helper between them and do not
route one through the other. Corollary: **US-1 and US-2 have no blocking
dependency on the new store method** and can start on day one.

**C. `turn.Result.Succeeded()` is unchanged and must stay unchanged.** It is the
load-bearing predicate for session state, `Reportable()` and
`ConsumesComment()`. If a task finds itself editing
`internal/turn/result.go:236`, it has left the plan — stop and escalate.

**D. Byte-for-byte prompt tests are required on both sides.** The resume header
for a **succeeded** predecessor and the cold-start prompt must be provably
unchanged (T-5.4, T-5.5). The spec's insertion point — after `- Feature slug:`,
before the infer-guidance block — is the only one that preserves output, because
both existing conditional blocks either return early or fall off the end of
`writeResumeHeader`. Insert anywhere else and the guarantee is lost.

**E. STYLE-8 binds US-5.** `internal/harness/prompt.go`'s `"- Mode: %s\n"`
literal and `CLAUDE.md`'s `## Classify` trust of that exact literal must change
together in the same commit, and **no test catches the CLAUDE.md side
drifting**. The corrective note is added *below* that line, so STYLE-8 is a
watch-out rather than a trigger — provided the literal itself is not touched. If
a task finds a reason to touch it, that is an escalation, not a judgement call.

**F. `PreviousReason` gets neutralise/cap treatment before it enters the
prompt.** See the security guardrail above. This is the one place the plan adds
attack surface and the one place it must be closed deliberately rather than by
appeal to `excerpt`'s 200-byte bound.

**G. No schema change, no migration, no new dependency.** D-3 is sound because
`Status` and `Error` are already columns. TEST-2 holds: hand-written fakes only.
A task that reaches for a migration file or a mocking library has misread the
plan.

---

## Deviations

**None.** No rule in `.aiteam/APP_CONSTRAINTS.md` needs to be bent for this
feature, and no approved product decision in the spec needs re-opening.
Consequently there is nothing to record in `.aiteam/AC_AMENDMENTS.md`.

For completeness, the rules this feature runs closest to, all of which it
satisfies as planned: SEC-2 (stdin, satisfied), SEC-6 (no redaction change
needed), SEC-7 (strengthened, not weakened), CONC-3 (a bad outcome stays a
value), CONC-7 (untouched, and pinned by T-4.7), STYLE-1 (comment updates are
tasks T-1.2, T-2.2, T-4.2), STYLE-8 (decision E), TEST-2 / TEST-10 (decision G,
task T-1.6).

## Out-of-domain notes

None. Every story in this spec is Go backend architecture — package boundaries,
commit gating, prompt contracts and report rendering inside a single Go module.
No task requires frontend, infrastructure or data-science guidance, so no gap
needs to be flagged against this lightweight roster.
