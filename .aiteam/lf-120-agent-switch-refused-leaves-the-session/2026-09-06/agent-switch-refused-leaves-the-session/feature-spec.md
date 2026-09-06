# Feature Spec: agent_switch_refused must not leave a session at state=running
# Status: APPROVED
# Date: 2026-09-06

## Goal

A turn that ends without running a coding agent must leave `sessions.state` honest, so `litefactory logs` never shows a task as `running` when nothing is running — fixing GH #120 (the agent-switch refusal) and the seven sibling error paths in `prepare` that share its cause.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an operator, I want a turn refused for agent-switch (`agentSwitchRefusal`, US-3/AC-3.1) to leave the task's state exactly as it was before the turn, so `litefactory logs` does not show a permanently `running` task that no process is working on |
| US-2 | As an operator, I want the same guarantee for every other error `prepare` can return after the point where the session was previously marked `running` (7 sites, see Business Rules BR-3), so the whole bug class is closed rather than the one reported instance |
| US-3 | As a maintainer, I want the three code comments that claim "nothing was created / no session state was touched" to describe what the code actually does, so the next reader is not misled into re-introducing this bug |
| US-4 | As a maintainer, I want regression tests asserting session state after a refusal and after a successful dispatch, so the ordering this fix depends on cannot be silently reversed |

## Resolutions to the Research Brief's Open Questions

### RQ-1 — refusal ends at `idle` or `failed`? → **Neither is written; the state is left untouched (in practice `idle`)**

| Consideration | Finding |
|---|---|
| Existing precedent for *failures* | `prepareFailed` (`internal/turn/execute.go:546-561`) writes `SessionFailed`. Applies to `*failure`: the world broke a turn that was supposed to run |
| Existing precedent for *refusals* | `Daemon.refuseMode` (`internal/daemon/mode.go:100`), `Daemon.refuseTools` (`internal/daemon/tick.go:1591`), `Daemon.refuseAgent` (`internal/daemon/tick.go:1633`) answer a label-driven permission question and touch **no** session state. Verified: `SetSessionState` has **zero** call sites in `internal/daemon` |
| Which precedent this belongs to | Refusal. `agentSwitchRefusal` is deliberately a separate type from `failure` (`internal/turn/errors.go:64-77`), is answered by `report.ReplyAgentSwitchRefused` via `ReplyRequest`/`ReportReply` — the same reply shape the three daemon refusals use — not by a run report |
| Why not `failed` | `failed` means "the last run failed (S-9)" (`internal/store/sessions.go:21-23`). No run existed. Writing `failed` would make `litefactory logs` (`internal/cli/cmd_logs.go:403`) report a broken task when the only thing that happened is that the owner used the wrong Coding Agent label; the next correctly-labelled comment runs normally |
| Why not an explicit `idle` write | An explicit `idle` write would *clear* a genuine prior `failed` from an earlier run that really did fail — inventing a success signal out of a refusal. Leaving the row untouched satisfies the reporter's stated `idle` expectation for every session that was `idle` (the normal case) without falsifying the one that was not |

**Decision:** the refusal writes no session state at all. This is achieved structurally (BR-1), not by adding a `SetSessionState` call to `agentSwitchRefused`. Consequence: `Execute`'s error routing (`execute.go:492-508`), the `Disposition` set and `report.go`'s reply path are all unchanged — which also resolves the brief's Open Question 2 in favour of "`agentSwitchRefusal` stays a separate type; no `prepareFailed` reuse".

### RQ-2 — are the 7 sibling sites in scope? → **Yes, in scope, because the chosen fix closes them at no extra cost**

| Consideration | Finding |
|---|---|
| Chosen fix shape | Move the single `SetSessionState(..., SessionRunning)` statement from `internal/turn/prepare.go:111` to the last statement before `prepare` returns its `*preparation` (after `permissions` is built, immediately before the `logger.Info("run prepared", …)` at `prepare.go:273`) |
| Effect on scope | One moved statement leaves **zero** error returns between the `running` write and the return. All 8 sites (the refusal plus the 7 in BR-3) are fixed by that one move; fixing only the refusal would cost the same edit and deliberately leave 7 known-worse instances behind |
| Rejected alternative A | Add `SetSessionState(SessionFailed)` in `agentSwitchRefused` only — smallest diff, but leaves the 7 sites, contradicts RQ-1, and preserves the misleading write ordering the next bug will grow from |
| Rejected alternative B | Blanket state reset in `Execute`'s fallthrough `return result, err` branch (`execute.go:507`) — fires for errors raised *before* the `running` write too (`prepare.go:89` GetRepo, `:107` SetSessionWorktree), flipping an untouched `idle` session to `failed` spuriously; and needs new plumbing for `Execute` to know whether the write happened. Strictly larger and riskier |
| Risk of the chosen move | Verified low: nothing reads `sessions.state` for control flow. The only non-store readers in the tree are `internal/cli/cmd_logs.go:403` (display) and `internal/turn/harvest.go:417-424` (the terminal write). `internal/store/sessions.go:21-23` states "M0 does not gate on this". Dispatch is gated by `lock_owner` plus the daemon's in-flight set (`execute.go:429-449`), not by state |

**Decision:** the 7 sites are in scope, delivered by US-2 as the same one-statement move. Scope is not grown: no new error types, no error-routing changes, no schema change.

### RQ-3 — correct the three inaccurate comments? → **Yes**

- Zero runtime risk, and each one asserts the exact invariant this fix restores; leaving them would document the bug as intended behaviour.
- A fourth, related comment becomes *true again* as a result of the move and needs no edit: the error-taxonomy doc at `internal/turn/errors.go:8-21` ("an ordinary error ... happened before any mutation").

## Data Model

No schema change. No migration. Existing shape, for reference:

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| sessions | state | TEXT (`store.SessionState`) | yes | closed enum `idle` \| `running` \| `failed`, validated by `SessionState.valid()` (`internal/store/sessions.go:13-23, 252-259`); default `idle` at insert (`:103`); unchanged by this work |
| sessions | lock_owner | TEXT NULL | no | unchanged — released on every `Execute` exit path by the unconditional defer (`execute.go:474-479`) |
| runs | status | TEXT | yes | unchanged — no run row is allocated on a refused turn |
| segments | agent | TEXT | yes | unchanged — read-only in this work; `GetOrCreateSegment`'s stored-row-wins semantics are what the refusal check compares against |

### Relationships
- sessions → runs via `runs.session_id` (a refused turn creates none)
- sessions → segments via `segments.session_id` (a refused turn mutates none)

## API / Interface Changes

No exported API changes. No CLI, HTTP or store signature changes. Internal call-order changes only:

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| `turn.prepare` | `SetSessionState(ctx, session.ID, store.SessionRunning)` moves from `prepare.go:111` to the last statement before `return &preparation{…}` (`prepare.go:~273`, before the `run prepared` log line) | n/a | Error handling of that call is unchanged: still `fmt.Errorf("mark task %s running: %w", …)`, still an ordinary error, still retried on the next tick |
| `turn.prepare` | in-memory `session.State = store.SessionRunning` set alongside the row write, before `preparation.session` is populated | n/a | Makes `prep.session` match the row; not load-bearing today (`result.Session.State` is overwritten at `harvest.go:424`) |
| `turn.agentSwitchRefused` | unchanged behaviour; doc comment corrected | n/a | Must **not** gain a `SetSessionState` call — see RQ-1 |
| `turn.Execute` | error routing at `execute.go:492-508` unchanged | n/a | No new branch, no new disposition |
| `turn.Reporter.reportAgentSwitchRefusal` | unchanged behaviour; doc comment corrected | n/a | Reply shape and issue-body skip stay as they are |

## Business Rules

- **BR-1** — `sessions.state` is written to `running` by `prepare` **only after every error return in `prepare` has been passed**. It is the last store write `prepare` makes; a `prepare` that returns an error never leaves the row modified.
- **BR-2** — an `*agentSwitchRefusal` leaves `sessions.state` at whatever value it held when the turn started: `idle` stays `idle`, `failed` stays `failed`. No `SetSessionState` call exists on the refusal path.
- **BR-3** — the same guarantee covers all seven sibling error returns identified in the brief, which today sit after the `running` write and get no state reset of any kind: `prepare.go:129` (`naming.SessionRef`), `:148` (`GetOrCreateSegment`), `:181` (`previousRun`/`ListRuns`, built at `:434`), `:194` (`allocateRun`, `:945-951`), `:198` (`harness.MetaPath`), `:203` (`commentHistory`/`ListComments`, `:524`), `:250` (`harness.BuildPrompt`).
- **BR-4** — a `*failure` still ends at `SessionFailed` via `prepareFailed`; a completed run still ends at `SessionIdle`/`SessionFailed` via `harvest.go:417-424`. Neither path changes.
- **BR-5** — the session must be `running` in the database for the whole time the coding-agent subprocess is executing. The write moves later inside `prepare`, never past the point where `runAndHarvest` dispatches.
- **BR-6** — the worktree created at `prepare.go:101` is **not** removed on a refusal. It is idempotent and reused by the next turn; removal would delete a directory a concurrent or subsequent turn legitimately wants.
- **BR-7** — comments corrected to state that the worktree exists on disk and that the `running` write now happens after the check, at these three sites: `prepare.go:150-156`, `execute.go:565-572`, `report.go:315-319`.
- **BR-8** — the "state write is last" ordering is stated as an invariant in `prepare`'s doc comment (`prepare.go:76-78` area) and at the moved statement, because no test can enforce it structurally (see Edge Cases).

## Edge Cases & Error Handling

- Session already `failed` from a previous run, then a turn is refused → stays `failed`; the refusal reply is still posted; no run row, no state write.
- Session `idle`, turn refused → stays `idle` (the reporter's expected behaviour in #120).
- Refusal triggered by the issue body (`TriggerCommentID == 0`) → unchanged: nothing is posted (`report.go:335-340`), and the session state is likewise untouched.
- `SetSessionState(SessionRunning)` itself fails at its new position → ordinary error, session left as it was, run row already allocated stays `running` and is cleaned by `Reconcile` at the next daemon start. Same class of orphan as today, not made worse (see Out of Scope).
- Errors after `allocateRun` (`prepare.go:198`, `:203`, `:250`) → after this fix the session is untouched but an orphaned `runs` row with `status='running'` remains until the next daemon start. Pre-existing, unchanged by this work, tracked separately.
- Daemon crash between `allocateRun` and the moved state write → `runs` row `running` + session `idle`; `ReconcileOrphans` (`store/runs.go:475-534`) fails the run and fails its session as a side effect. Correct outcome, no change needed.
- Concurrent second process reaching the same session → refused at `AcquireSessionLock` before `prepare` runs; unaffected.
- Fault injection for the 7 sibling sites is not testable today: `turn.Options.DB` is a concrete `*store.DB` (`execute.go:83`), so a failing `ListRuns`/`ListComments` cannot be simulated. BR-3 is therefore enforced by construction (there is no code path left between the write and the return) plus the BR-8 invariant comment, not by a per-site test. Adding a store seam is explicitly out of scope.

## Out of Scope

- Adding an interface/fake seam for `store.DB` to fault-inject the seven sibling sites.
- Orphaned `runs` rows left `status='running'` by errors after `allocateRun` — real, pre-existing, unchanged here; file as a separate issue.
- Any change to `lock_owner` handling, `AcquireSessionLock`/`ReleaseSessionLock`, or `ReconcileOrphans`' run-row-and-lock-based detection (confirmed working as designed for this scenario).
- Making `Reconcile` key off `session.state` directly.
- Gating dispatch or scheduling on `session.state`.
- Any new `SessionState` value, any migration, any session-state history table.
- Removing or cleaning the worktree on a refused turn (BR-6).
- Merging `agentSwitchRefusal` into `failure` / `prepareFailed`, or adding a new `Disposition`.
- Changing the refusal reply text, `report.ReplyAgentSwitchRefused`, or the issue-body skip behaviour.
- The daemon-level label-ladder refusals (`refuseAgent`, `refuseMode`, `refuseTools`) — already correct.

## Acceptance Criteria

- [ ] `internal/turn/prepare.go` contains exactly one `SetSessionState(..., store.SessionRunning)` call, and it is the last store write before `prepare` returns its `*preparation`; no `return nil, err` statement follows it.
- [ ] `internal/turn/execute.go`'s `agentSwitchRefused` contains no `SetSessionState` call, and `Execute`'s error routing at `execute.go:492-508` is byte-for-byte unchanged in behaviour (no new branch, no new disposition).
- [ ] New test: a session whose segment is already owned by a different agent, driven through `Execute`, returns `DispositionAgentSwitchRefused` **and** the `sessions` row still reads `state='idle'` afterwards; `f.runs()` is empty.
- [ ] New test: the same scenario with the session seeded at `state='failed'` before the turn still reads `state='failed'` afterwards (the refusal neither clears nor invents a failure).
- [ ] New test: on a successful turn, the stub runner observes `state='running'` in the database while it is executing (pins BR-5 — the write must not drift past dispatch).
- [ ] Existing behaviour preserved: a `*failure` turn still ends at `state='failed'` (e.g. `TestExecuteRefusesAnOccupiedWorktreePath` extended or a sibling assertion added), and a successful turn still ends at `state='idle'`.
- [ ] `prepare.go:150-156`'s comment no longer claims "nothing above this point wrote anything the switch needs to undo"; it states that the worktree exists on disk and that no session-state write has happened yet.
- [ ] `execute.go:565-572`'s comment no longer claims "`prepare` returned before creating anything"; it states that the worktree exists and that the session state was deliberately never written.
- [ ] `report.go:315-319`'s comment no longer claims "No run was ever allocated — `prepare` returned before anything was created" as a blanket statement; it keeps the true half (no run row) and drops/corrects the false half.
- [ ] `prepare`'s doc comment records the BR-1/BR-8 invariant: the `running` write is the last thing `prepare` does, so every error return above it leaves `sessions.state` untouched.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` clean and `make check` passes (`-race` included).
- [ ] A follow-up issue exists for orphaned `runs` rows left `running` by post-`allocateRun` errors.

## Assumptions

| # | Assumption | Based on |
|---|---|---|
| A-1 | "Leave the state untouched" satisfies the #120 reporter's stated `idle` expectation | A session that reaches a turn is `idle` in every case except one whose previous run failed; the untouched-state rule gives `idle` there and preserves the truth in the exception |
| A-2 | Nothing outside display reads `sessions.state`, so moving the write later is behaviourally safe | Grep of `internal/**` (non-test) for `SessionRunning`/`SessionIdle`/`SessionFailed`/`.State`: only `cmd_logs.go:403`, `harvest.go:417-424`, `execute.go:554`, `prepare.go:111`, and the store itself; plus `internal/store/sessions.go:21-23` "M0 does not gate on this" |
| A-3 | The 7 sibling sites need no individual tests | They cannot be reached without a store fault-injection seam that does not exist, and after the move there is no code path between the write and the return for them to exercise |
| A-4 | Setting `session.State` in memory alongside the row write breaks nothing | `result.Session.State` is unconditionally overwritten at `harvest.go:424` on the completed-run path, and `prep.session` has no other state reader |
| A-5 | The refusal keeps its own type and reply path rather than reusing `prepareFailed` | Brief Open Question 2; RQ-1's precedent analysis makes reuse wrong on semantics, and the chosen fix needs no routing change at all |
| A-6 | No regression-test scaffolding beyond the existing `newFixture(t, runner)` / `f.execute(f.request())` / `f.runs()` helpers is required | `internal/turn/execute_test.go:520-576` shows the fixture already seeds store rows directly (`f.db.FreezeSessionSlug`), which is what seeding a foreign-agent segment needs |
