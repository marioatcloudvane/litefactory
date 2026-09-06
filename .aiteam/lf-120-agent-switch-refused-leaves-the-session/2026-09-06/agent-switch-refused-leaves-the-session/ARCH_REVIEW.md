# Architect: Go Architect
# Created: 2026-09-06
# Feature: agent_switch_refused must not leave a session at state=running (GH #120)
# Verdict: APPROVED WITH AMENDMENTS

---

## System Overview

### How This Feature Fits Into the Existing System

This is a bug fix inside one package: `internal/turn`, the package that executes a
single turn. It touches no package boundary and adds none. `internal/turn` knows
nothing about time or scheduling — `internal/daemon` decides *when* a turn happens,
`internal/turn` decides *what* happens in one. This fix lives entirely on the
`internal/turn` side of that line and requires no daemon change at all.

Within `internal/turn`, the change sits in `prepare` (`internal/turn/prepare.go`),
the function `Execute` calls after it has acquired the cross-process session lock
and cleared the replay check. `prepare` performs seven ordered steps — resolve the
repo, prepare the clone and worktree, resolve the session ref, get-or-create the
segment, check the agent matches, allocate the run row, build the prompt — and
returns a `*preparation` or an error. Today it writes `sessions.state = 'running'`
at step two (`prepare.go:111`), which means the *other twelve* things that can go
wrong afterwards all leave a session claiming to be running when nothing is. The
fix moves that write to the end of `prepare`, so the row is only marked `running`
once the turn is genuinely about to dispatch.

The only cross-package consumers of `sessions.state` are `internal/store`
(the write and enum validation, `internal/store/sessions.go:238-259`) and
`internal/cli/cmd_logs.go:403` (display). Nothing schedules, gates or locks on it —
`internal/store/sessions.go:21-23` says so explicitly ("M0 does not gate on this").
That is what makes moving the write a behavioural non-event outside the display line,
and it is why this fix does not need to coordinate with `internal/daemon`.

### New Packages / Components Introduced

None. No new type, no new `Disposition`, no new error, no new store accessor, no
schema change, no migration. The delta is: one statement moves inside one function,
one in-memory field assignment moves with it, three comments are corrected, one doc
comment gains an invariant, and new tests are added to the existing
`internal/turn` in-package test suite.

### Key Boundaries

| Boundary | Status under this change |
|---|---|
| `internal/turn` → `internal/store` | Unchanged. Same accessor (`SetSessionState`), same signature, same ctx-first shape (CONC-5). Only the call *position* moves. |
| `internal/turn` → `internal/daemon` | Untouched. The daemon has **zero** `SetSessionState` call sites; its three label refusals (`refuseMode`, `refuseTools`, `refuseAgent`) never write session state. RQ-1's precedent claim is verified structurally, not anecdotally. |
| `internal/turn` → `internal/report` reply path | Unchanged. `report.ReplyAgentSwitchRefused` via `ReplyRequest`/`ReportReply` stays exactly as it is; only its doc comment is corrected. |
| `Execute`'s error routing (`execute.go:492-508`) | Unchanged. `errors.As` on `*agentSwitchRefusal` then `*failure` then bare error. No new branch. This is load-bearing: AC-2 pins it. |
| `internal/store` transaction boundary | Untouched. `SetSessionState` is a single statement; `ReconcileOrphans` (`store/runs.go:475-534`) keeps its own `withTx`. No transaction is opened, widened or split by this work. |

### Data Flow

Refusal path, after the fix:

```
daemon tick → Execute → AcquireSessionLock → replay check (GetRunByTrigger)
  → prepare
      → GetRepo / prepareClone (fetch, worktree materialised on disk)
      → SetSessionWorktree            [writes sessions.worktree_path]
      → naming.SessionRef
      → GetOrCreateSegment            [stored-row-wins]
      → segment.Agent != resolvedKind → return *agentSwitchRefusal   ← state never written
  → Execute: errors.As(*agentSwitchRefusal) → agentSwitchRefused(...)
  → reportAgentSwitchRefusal → ReplyAgentSwitchRefused (or skip if issue-body trigger)
  → deferred ReleaseSessionLock (context.WithoutCancel)
  → DispositionAgentSwitchRefused, sessions.state unchanged
```

Successful path, after the fix: same prefix, then segment matches → runner resolved
→ `previousRun` → `resumeDecision` → `allocateRun` (inserts `runs` row `running`) →
`MetaPath` → `commentHistory` → `ingestAttachments` → `writeAttachmentManifest` →
`BuildPrompt` → `permissions` → **`SetSessionState(running)` + in-memory
`session.State = running`** → `logger.Info("run prepared")` → `return &preparation{…}`
→ `runAndHarvest` dispatches the subprocess → `harvest.go:417-424` writes the terminal
`idle`/`failed`.

### Concurrency & Context Notes

- No goroutine is introduced or moved. This is a straight-line reordering inside one
  synchronous function.
- The moved call keeps the request's `ctx` as its first argument (CONC-5). It must
  **not** acquire a `context.WithoutCancel` wrapper — that idiom belongs to the
  deferred lock release (`execute.go:474-479`) and to `prepareFailed`
  (`execute.go:554`), which run *after* cancellation and must still complete. The
  `running` write runs on the live request path, so a cancelled context should
  correctly abort it.
- Intra-process concurrency on a session is excluded by the daemon's in-flight set;
  cross-process concurrency by `AcquireSessionLock` (`execute.go:429-450`). Neither
  changes. Two turns cannot race this write.
- The SQLite pool is capped at one connection, so `SetSessionState` — a single
  `UPDATE` — cannot interleave or partially apply.

### Shared State and Side Effects

The important structural consequence, and the thing the requirements engineer did not
call out: **after the move, three side effects precede the `running` write that used
to follow it.**

| Side effect | Site | Nature |
|---|---|---|
| `runs` row inserted with `status='running'` | `allocateRun`, `prepare.go:192` | database write |
| Attachment bytes fetched over the network and written under `$LF_HOME` | `ingestAttachments`, `prepare.go:225` | network + filesystem |
| `.aiteam/` attachment manifest written into the worktree (`MkdirAll 0o750`, `WriteFile 0o600`) | `writeAttachmentManifest`, `prepare.go:226` | filesystem |

This creates a new, narrow window in which a `runs` row reads `running` while its
session still reads `idle` — an inversion of today's ordering. **It is safe, and the
system already self-heals it:** `ReconcileOrphans` (`store/runs.go:475-534`) selects
every run `WHERE status='running'` at daemon start, fails it with `ReasonAbandoned`,
and in the *same transaction* sets that run's session to `failed`. A crash inside the
window therefore resolves to an honest `failed`, not a stuck `running`. Nothing reads
`session.State` for control flow, so no consumer can observe the inversion and act
wrongly on it. The feature spec's Edge Cases already anticipate the crash case; this
review confirms the reasoning holds and records why.

Side effects that were already there and are unchanged: the worktree directory on
disk (`prepareClone`, `prepare.go:101`), `sessions.worktree_path`
(`SetSessionWorktree`, `prepare.go:106`), and `repos.last_fetch_at` moved by the
fetch. All three happen **before** the refusal check at `prepare.go:157` — which is
exactly why the corrected comments must be worded carefully (see AMEND-2).

---

## Verification of the Requirements Engineer's Three Resolutions

The orchestrator asked me to verify rather than re-litigate. All three hold. Findings:

### RQ-1 — "state left untouched on refusal" — CONFIRMED, and stronger than stated

A repo-wide grep for `SetSessionState` shows **no call site anywhere in
`internal/daemon`**. The daemon does not write session state at all. So "the daemon's
label refusals leave state untouched" is not a coincidence of three functions; it is
structural. `agentSwitchRefusal` is a distinct unexported type
(`internal/turn/errors.go:64-77`) answered by the same `ReplyRequest`/`ReportReply`
shape those refusals use. The refusal precedent is the right one. No deviation.

### RQ-2 — "move one statement is sufficient" — CONFIRMED, with two amendments

I traced every statement between `prepare.go:111` and `prepare`'s return
(`prepare.go:284-317`) and every error return in that span:

| Line | Call | Return kind | Post-move outcome |
|---|---|---|---|
| :115 | `worktree.RevParse` | `*failure` | `idle → failed` via `prepareFailed` (BR-4 holds) |
| :129 | `naming.SessionRef` | ordinary error | state untouched ✅ |
| :148 | `GetOrCreateSegment` | ordinary error | state untouched ✅ |
| :157 | agent-switch check | `*agentSwitchRefusal` | state untouched ✅ (US-1) |
| :168 | `opts.Runners.runner(kind)` | `*failure` | `idle → failed` via `prepareFailed` — **missing from BR-3** |
| :181 | `previousRun` | ordinary error | state untouched ✅ |
| :194 | `allocateRun` | ordinary error | state untouched ✅ |
| :198 | `harness.MetaPath` | ordinary error | state untouched ✅ |
| :203 | `commentHistory` | ordinary error | state untouched ✅ |
| :250 | `harness.BuildPrompt` | ordinary error | state untouched ✅ |
| :263-271 | `permissions` | cannot fail | — |

**Nothing in that span reads `session.State`, and nothing does compensating work
after the current `SetSessionState` call that would break if it moved later.** The
`prepareFailed` path does not read the prior state; it writes `failed`
unconditionally. The move is sufficient. The proposed insertion point — after
`permissions` at `:271`, before the `run prepared` log at `:273` — is confirmed
viable because `permissions` cannot fail, leaving **zero** error returns after the
new position, exactly as BR-1 requires.

Two amendments follow (AMEND-1, AMEND-2 below).

### RQ-3 — "correct three comments" — CONFIRMED, wording constrained

STYLE-1 makes this non-optional, not a nicety: "When your change makes a comment
untrue, updating it is part of the change." US-3 is literally STYLE-1 enforcement.
But the corrected wording must be narrower than "nothing was written" — see AMEND-2.

---

## Amendments

### AMEND-1 (spec correction, not a design change) — BR-3 undercounts: 8 sibling sites, not 7

BR-3 enumerates seven error returns after the current write. There are eight. The
missing one is the registry lookup at `prepare.go:159-169`:

```go
runner, ok := opts.Runners.runner(kind)
if !ok {
    return nil, failuref(nil, "no coding-agent-tool implementation is registered for %q", segment.Agent)
}
```

It is a `*failure`, so it routes to `prepareFailed` → `SessionFailed`. Today it
transitions `running → failed`; after the move it transitions `idle → failed`.

**This is benign and needs no code change.** The `idle → failed` transition is
already live and already covered: `TestExecuteFetchFailureAbortsWithNoWorktree`
(`execute_test.go:413-453`) asserts `SessionFailed` after a fetch failure inside
`prepareClone` (`prepare.go:101`) — which is *before* the current write, i.e. already
an `idle → failed` transition today. BR-4 holds unchanged.

**Action:** the eighth site is added to BR-3's enumeration as documentation, and
US-2's scope statement reads "eight sibling paths". No extra implementation work; one
extra sentence in the plan and one line in the US-2 acceptance criteria. This is a
count correction, not a re-litigation.

### AMEND-2 (binding constraint on US-3) — the corrected comments must claim the narrow invariant, not "nothing was written"

The three comments are false today because they claim too much. The correction must
not merely soften them — it must state the invariant that is actually true, because
STYLE-1 requires the comment to be true, and the naive fix ("prepare returned before
anything was created") is *still* false after the move.

At the moment the refusal fires (`prepare.go:157`), these have already happened:

- the worktree exists on disk (`prepareClone`, `prepare.go:101`),
- `repos.last_fetch_at` has moved (the fetch inside `prepareClone`),
- `sessions.worktree_path` has been written (`SetSessionWorktree`, `prepare.go:106`).

The true invariant is narrower and precise:

> **No session-*state* transition has occurred and no `runs` row has been allocated.
> The worktree exists on disk and `sessions.worktree_path` records it; both are
> idempotent and reused by the next turn (BR-6).**

All three corrected comments (`prepare.go:150-156`, `execute.go:565-572`,
`report.go:315-319`) must say that, not "nothing was created". The `report.go` one
keeps its true half ("no run row") and drops the blanket half, per AC-9.

### AMEND-3 (flagged, decision deferred to the owner) — a fourth comment is already false, independently of this change

`prepareFailed`'s doc comment (`internal/turn/execute.go:540-561`) claims:

> "No run row was allocated, so there is no ordinal to burn"

This is **already false today** for any `*failure` raised at or after `allocateRun`
(`prepare.go:192`) — and it stays false after this change. It is a pre-existing
inaccuracy, not one this work creates, so STYLE-1's "your change made it untrue" hook
does not fire and I am not adding it to BR-7 on my own authority.

**Recommendation:** the owner adds it to US-3's list. It is a one-line comment edit in
a file US-3 is already opening, at effectively zero cost and zero risk, and leaving a
known-false comment in the same function the plan is correcting is a poor trade.
If the owner declines, it belongs in the same follow-up issue AC-12 already requires
for orphaned `runs` rows — the two share a root cause.

**This is not a DEVIATION.** Nothing in the spec or `APP_CONSTRAINTS.md` is being
bent; it is a scope suggestion, and the plan is correct either way.

---

## Platform Constraints

Per `.claude/skills/go/platform-constraints.md`:

- **Platform: Go 1.26, single module `github.com/cloudvane/litefactory`, everything under `internal/`** — no new package, no `go.mod` change, no dependency. `internal/turn` is the only package edited; `cmd/litefactory/main.go` wiring is untouched.
- **Platform: layered split, `internal/turn` = one turn, `internal/daemon` = when turns happen** — the fix stays entirely inside `turn`. Do not introduce any timing, retry or backoff behaviour here; a failed `SetSessionState` at its new position is an ordinary error and the daemon's existing next tick is the retry.
- **Platform: `internal/store` is the only place SQL lives** — no query is written, edited or added. `SetSessionState` is reused as-is with its existing parameterised statement and enum validation (`sessions.go:238-259`).
- **Platform: SQLite via `modernc.org/sqlite`, connection pool capped at one** — a single-statement `UPDATE` cannot partially apply or interleave. No `BEGIN IMMEDIATE` transaction is needed for the moved write, and none should be added; wrapping it would serialise against `ReconcileOrphans` for no benefit.
- **Platform: `context.Context` first parameter, propagated on every request-scoped call (CONC-5)** — the moved call keeps `ctx`. Do **not** wrap it in `context.WithoutCancel`; that idiom is reserved for post-cancellation cleanup paths (`execute.go:474-479`, `:554`).
- **Platform: a bad outcome is a value, not an error (CONC-3)** — the refusal already is one (`DispositionAgentSwitchRefused` + `nil` error at `execute.go:496`). This work must not convert it to an error path or add a disposition.
- **Platform: STYLE-7 — read the package `doc.go` before touching it** — `internal/turn/doc.go` (119 lines) documents "A bad turn is not an error", the lock as the cross-process concurrency model, the three clone critical sections (count pinned by `gate_test.go`), and "post first, then commit". It records **no** session-state invariant today; BR-8's invariant goes in `prepare`'s doc comment, and `doc.go` needs no edit (the three-critical-section count is unchanged).
- **Platform: STYLE-2 lint gate — `errcheck`, `gosec`, `errorlint`, `nilerr`, `noctx`, `unparam`, no `context.Background()` outside main, no `fmt.Print*`/`log.*`** — the moved statement's `if err := …; err != nil` shape satisfies `errcheck` as written; keep the `%w` wrap for `errorlint`.
- **Platform: tests are stdlib `testing`, table-driven where it fits, no mocking framework, no new module (TEST-1, TEST-2)** — US-4 uses `newStubRunner` and the existing fixture only.
- **Platform: `internal/turn` is tested in-package and MUST NOT use the `_internal_test.go` suffix (TEST-4)** — new tests go in `internal/turn/execute_test.go` (package `turn`), not a new file with that suffix.
- **Platform: extend `fixture_test.go` rather than inventing a third faking style (TEST-3); a promise about a transaction is tested against real SQLite, never a fake store (TEST-6)** — `newFixture` already gives a real migrated SQLite DB under a real `$LF_HOME` with a real `gittest.Repo`. Every US-4 assertion reads the real `sessions` row.
- **Platform: no `time.Sleep`, `Eventually` or wall-clock polling in `daemon`/`turn` concurrency tests; rendezvous on channels with a named timeout (TEST-8)** — the BR-5 "running during the run" test needs no goroutine at all (see T-4.3); if any test does need a rendezvous, `gateTimeout` (`gate_test.go:28`) is the named timeout to use.
- **Platform: `make check` includes `-race`** — no new shared state is introduced, so `-race` has nothing new to find, but the gate still applies (AC-11).
- **Platform: litefactory commits, no agent does** — no `git add`/`commit`/`push` from any agent on this work. A live daemon runs from `dist/litefactory`; do not `make build`, do not write into `dist/`, do not restart it. Safe verbs are `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .`.

---

## Security Findings

**Scope:** the moved `SetSessionState(SessionRunning)` write in `internal/turn/prepare.go`,
the unchanged refusal path through `execute.go`/`report.go`, and the new tests. This is a
state-machine / session-lifecycle fix touching one DB write; the orchestrator asked
specifically for the concurrent-access and partial-write angles.

**Rulebook:** `.claude/skills/shared/security-scanner.md`, `.aiteam/APP_CONSTRAINTS.md`
(SEC/CONC rules), `internal/turn/doc.go`.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation / injection | ✅ | No new input surface. No new SQL, no string-built query. `SetSessionState` validates against the closed enum (`sessions.go:252-259`) and uses a parameterised statement. Unchanged. |
| 2 | AuthN / AuthZ | ✅ | The agent-switch check *is* an authorisation decision (does this segment's owning tool permit this request?) and it is strengthened, not weakened: it now precedes every state mutation as well as every run allocation. The daemon's label ladder above it is untouched. |
| 3 | Concurrent access / TOCTOU | ✅ | Two turns cannot reach the same session concurrently: `AcquireSessionLock` (`execute.go:450`) excludes other processes, the daemon's in-flight set excludes other goroutines in this one. The one-connection SQLite pool serialises everything else. The moved write has no read-then-write pair, so there is no check-then-act window to widen. |
| 4 | Partial write / atomicity | ✅ | `SetSessionState` is a single `UPDATE` on a single-connection pool — it applies or it does not. No transaction is opened, widened or split by this change. `ReconcileOrphans`' `withTx` (`store/runs.go:475-534`) is untouched. |
| 5 | State-consistency window (new) | ⚠️ | The move creates a window where a `runs` row reads `running` while its session reads `idle` (`allocateRun` at `:192` now precedes the state write). **Mitigated by design:** `ReconcileOrphans` fails every `running` run at daemon start *and* sets its session to `failed` in the same transaction, so a crash in the window resolves to an honest `failed`. Nothing gates on `running` (`sessions.go:21-23`), so no consumer can act on the inconsistency. Accept; record the reasoning in the BR-8 invariant comment. |
| 6 | Denial of service / resource exhaustion | ✅ | No new allocation, no unbounded loop, no new goroutine. The refusal path returns earlier relative to state mutation than before, i.e. strictly less work on the abusive path. |
| 7 | Secrets / credential handling | ✅ | No secret is read, written, logged or passed. The child-process environment and the existing redaction are untouched. |
| 8 | Filesystem permissions | ✅ | No new file write. The pre-existing `writeAttachmentManifest` (`MkdirAll 0o750`, `WriteFile 0o600`) now precedes the state write but is otherwise unmodified; both modes remain correct. |
| 9 | Information disclosure via error/reply | ✅ | The refusal message (`errors.go:70-77`) names only the two agent kinds — both already public in the issue's labels. Reply text is explicitly out of scope and unchanged. |
| 10 | Logging | ✅ | The `run prepared` log line (`prepare.go:273`) stays after the write; no new field, no new log call, no `fmt.Print*`/`log.*` (STYLE-2). |
| 11 | Audit trail integrity | ✅ | **Improved.** The whole point of the fix: the `sessions` row stops asserting a run that never happened, so `litefactory logs` (`cmd_logs.go:403`) reports the truth. |
| 12 | Test isolation | ✅ | New tests use the existing per-test `$LF_HOME` and per-test SQLite DB from `newFixture`; no shared global, no network, no `-race` hazard. |

**Summary:** one ⚠️, no ❌. The single finding is an accepted, self-healing consistency
window that the existing reconciliation path already resolves correctly, and it is
strictly better than the bug it replaces (a permanently stuck `running` that nothing
heals). No security work is added to the plan beyond recording the reasoning in the
BR-8 comment.

---

## Task Decomposition

Per `.claude/skills/shared/task-decomposer.md`. Every task is assigned to
`go-implementation-engineer` unless noted. Test tasks are separate entries. `—` means
no dependency.

### US-1 — a refused turn leaves session state untouched

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-1.1 | Move the `SetSessionState(SessionRunning)` write and its in-memory `session.State` assignment from `prepare.go:111` to the last statement before `return &preparation{…}` (after `permissions`, before the `run prepared` log) | logic | — | US-1 | S |
| T-1.2 | Record the BR-1/BR-8 "state write is last" invariant in `prepare`'s doc comment and at the moved statement | logic | T-1.1 | US-1 | XS |

### US-2 — the same guarantee for the sibling error paths

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-2.1 | Verify by reading that no `return nil, err` remains between the moved write and `prepare`'s return, and that all eight sibling paths (BR-3's seven plus `prepare.go:168`, per AMEND-1) now return with state untouched or via the unchanged `prepareFailed` | logic | T-1.1 | US-2 | XS |

### US-3 — correct the inaccurate comments

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-3.1 | Correct the agent-switch refusal comment at `prepare.go:150-156` to the narrow invariant (no state transition, no `runs` row; worktree exists on disk and is reused) | logic | T-1.1 | US-3 | XS |
| T-3.2 | Correct `agentSwitchRefused`'s doc comment at `execute.go:565-572` to the same narrow invariant | logic | T-1.1 | US-3 | XS |
| T-3.3 | Correct `reportAgentSwitchRefusal`'s comment at `report.go:315-319` — keep the true "no run row" half, drop/correct the false blanket half | logic | T-1.1 | US-3 | XS |

### US-4 — regression coverage

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-4.1 | Test: session `idle`, segment seeded to a different agent → `Execute` returns `DispositionAgentSwitchRefused`, `sessions.state` still `idle`, `f.runs()` empty | test | T-1.1 | US-4 | M |
| T-4.2 | Test: same scenario with the session seeded `failed` beforehand → still `failed` after the refusal (neither cleared nor invented) | test | T-4.1 | US-4 | S |
| T-4.3 | Test: on a successful turn the stub runner observes `state='running'` in the DB while it executes (pins BR-5) | test | T-1.1 | US-4 | S |
| T-4.4 | Test: assert the unchanged terminal states — a `*failure` turn still ends `failed`, a successful turn still ends `idle` (extend an existing sibling test rather than adding a new one) | test | T-1.1 | US-4 | S |
| T-4.5 | Run `go build ./...`, `go vet ./...`, `gofmt -l .` and `make check` (`-race`) clean | test | T-4.1, T-4.2, T-4.3, T-4.4, T-3.1, T-3.2, T-3.3 | US-4 | XS |

**Implementation order:** T-1.1 first — every other task depends on it directly or
transitively. T-3.x and T-4.x are independent of each other and can proceed in
parallel once T-1.1 lands. T-4.5 gates the story closed. No task exceeds `M`.

**Not decomposed here:** AC-12 ("a follow-up issue exists for orphaned `runs` rows")
is a repo-management action, not an implementation task, and issue filing is the
owner's call — it is deliberately absent from the tables. Flagging it so the
plan-orchestrator can place it deliberately rather than lose it.

---

## Task Hints

### T-1.1: Move the `running` write to the end of `prepare`
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** Straight statement relocation inside one function. Delete the
  block at `prepare.go:109-113` and reinsert it after the `permissions` construction
  (`prepare.go:263-271`), before `logger.Info("run prepared", …)` at `:273`. Keep the
  error wrap verbatim: `fmt.Errorf("mark task %s running: %w", ref, err)`.
- **Package placement:** `internal/turn/prepare.go` only. No other file changes in
  this task.
- **Key interfaces:** `opts.DB.SetSessionState(ctx, session.ID, store.SessionRunning)`
  — the store accessor is reused unchanged, ctx first (CONC-5).
- **Concurrency/context:** Pass the live request `ctx`. Do **not** wrap it in
  `context.WithoutCancel` — that belongs to post-cancellation cleanup
  (`execute.go:474-479`, `:554`), and using it here would make the write survive a
  cancelled turn, which is exactly wrong.
- **Integration points:** Move the in-memory `session.State = store.SessionRunning`
  assignment alongside the row write, before `&preparation{…}` is built at
  `prepare.go:284-317`, so `prep.session` matches the row. `result.Session` is
  populated from it at `execute.go:509` and overwritten on the completed-run path at
  `harvest.go:424`, so this is correctness-of-record rather than load-bearing —
  but do it anyway.
- **Watch out for:** `permissions` (`:263-271`) cannot fail — that is precisely why
  this insertion point works. If you find yourself inserting the write *before*
  anything that can return an error, you have chosen the wrong position and BR-1 is
  broken. The requirement is literally "zero `return nil, err` statements after this
  line".
- **NOT in scope for this task:** Do not add a `SetSessionState` call anywhere else.
  Do not touch `prepareFailed`, `agentSwitchRefused`, `Execute`'s error routing, the
  `Disposition` set, or `harvest.go`'s terminal write. Do not remove the worktree on
  refusal (BR-6). Do not add a store fault-injection seam.

### T-1.2: Record the invariant in `prepare`'s doc comment
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** `prepare`'s doc comment (`prepare.go:54-78`) already
  enumerates the seven ordered steps and closes with the error-taxonomy paragraph
  ("Everything that can go wrong … is returned as a `*failure` … A database that will
  not answer is an ordinary error"). Add the ordering invariant as a short paragraph
  after it, in the same register.
- **Package placement:** `internal/turn/prepare.go` doc comment, plus one short
  comment at the moved statement explaining *why* it is last.
- **Watch out for:** STYLE-1 — the comment explains *why*, not *what*. "The state
  write is last so that every error return above it leaves `sessions.state`
  untouched; no test can enforce this ordering structurally" is the why. "Sets the
  session state to running" is not.
- **Integration points:** `internal/turn/doc.go` needs **no** edit. It records the
  three clone critical sections (a count pinned by `gate_test.go`) and the lock model;
  neither changes. Do not add a session-state section there — the invariant is local
  to `prepare`.
- **NOT in scope for this task:** Do not restructure the seven-step enumeration. Do
  not document the `runs`-row-before-session-state window here; that belongs at the
  moved statement as a one-line note pointing at `ReconcileOrphans`, not in the
  function's contract.

### T-2.1: Verify all eight sibling paths
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** A read-and-confirm pass, not a code change. Walk
  `prepare.go` from the moved write to `return &preparation{…}` and confirm no
  `return nil, err` survives in that span.
- **Key interfaces:** The eight paths are `prepare.go:129` (`naming.SessionRef`),
  `:148` (`GetOrCreateSegment`), `:157` (the refusal), `:168`
  (`opts.Runners.runner` — **the one BR-3 omits, see AMEND-1**), `:181`
  (`previousRun`), `:194` (`allocateRun`), `:198` (`harness.MetaPath`), `:203`
  (`commentHistory`), `:250` (`harness.BuildPrompt`). Note `:115`
  (`worktree.RevParse`) is a `*failure` and was always before the write.
- **Watch out for:** `:168` returns a `*failure`, not an ordinary error, so it routes
  to `prepareFailed` → `SessionFailed`. That is correct and unchanged behaviour: it
  transitions `idle → failed` instead of `running → failed`. Do **not** "fix" it to
  leave state untouched — a missing runner registration is a real failure the issue is
  owed an explanation for (BR-4). Record the count correction in the story notes.
- **Integration points:** `TestExecuteFetchFailureAbortsWithNoWorktree`
  (`execute_test.go:413-453`) already proves `idle → failed` works today via the
  pre-write fetch path. Cite it rather than writing a new test for `:168`.
- **NOT in scope for this task:** Per-site fault-injection tests. A-3 and the spec's
  Edge Cases establish these sites are unreachable without a store seam that is
  explicitly out of scope; BR-3 is enforced by construction plus the T-1.2 invariant.

### T-3.1 / T-3.2 / T-3.3: Correct the three comments
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** All three assert the same invariant from three vantage
  points, so write the same truth three ways rather than three different truths.
  The invariant, per AMEND-2: **no session-state transition has occurred and no
  `runs` row has been allocated; the worktree exists on disk and
  `sessions.worktree_path` records it, both idempotent and reused by the next turn.**
- **Package placement:** `prepare.go:150-156` (at the refusal check),
  `execute.go:565-572` (`agentSwitchRefused`'s doc comment), `report.go:315-319`
  (`reportAgentSwitchRefusal`).
- **Watch out for:** The trap is under-correcting. "`prepare` returned before creating
  anything" is *still false* after the move — `prepareClone` (`:101`) materialised the
  worktree and moved `repos.last_fetch_at`, and `SetSessionWorktree` (`:106`) wrote a
  session column, all before the refusal at `:157`. If your corrected comment says
  "nothing was written", you have replaced one false comment with another and STYLE-1
  is still violated. Name what *did* happen, then name what did not.
- **Integration points:** `report.go:315-319` keeps its true half ("no run was
  allocated, so this takes the `ReplyRequest`/`ReportReply` shape rather than the
  run-report one") — that clause explains a real design decision and must survive
  (AC-9). Only the blanket "before anything was created" goes.
- **NOT in scope for this task:** Do not touch `internal/turn/errors.go:8-21` — RQ-3
  correctly notes it becomes *true again* as a side effect of the move and needs no
  edit. Do not change any reply text, `ReplyAgentSwitchRefused`, or the issue-body
  skip. **`prepareFailed`'s own doc comment (`execute.go:540-561`) is separately false
  and is NOT on the approved list — see AMEND-3; do not edit it unless the owner adds
  it to US-3.**

### T-4.1: Refusal from `idle` leaves state `idle`
**Agent:** go-unit-test-engineer (or go-implementation-engineer per the plan's assignment)

**Architectural Hints:**
- **Pattern to follow:** `TestExecuteRefusesACorruptedFrozenSlug`
  (`execute_test.go:520-543`) is the closest template: it seeds a store row directly
  through `f.db` and then dispatches through `f.execute`. Table-driven if T-4.1 and
  T-4.2 merge naturally; two functions is fine too (TEST-1).
- **Package placement:** `internal/turn/execute_test.go`, package `turn`, in-package.
  **TEST-4 forbids the `_internal_test.go` suffix in this package** — do not create
  one.
- **Key interfaces:** Seed the foreign-agent segment with
  `f.db.GetOrCreateSegment(ctx, f.session.ID, store.FirstSegmentOrdinal, sessionRef,
  store.AgentCodex)`; dispatch the default `f.request()`, which carries
  `agent.Kind(store.AgentClaude)`. Assert on `f.reloadSession()` for the state and
  `f.runs()` for emptiness, and on the returned `Disposition`.
- **Concurrency/context:** None needed. This is a synchronous single-turn test — no
  goroutine, no channel, no timeout, so TEST-8 is satisfied trivially.
- **Watch out for:** The refusal at `prepare.go:157` fires *before* the
  `opts.Runners.runner(kind)` lookup at `:160`, so the fixture's single-runner
  `runnerSet` is sufficient — you do **not** need to register a Codex runner. If you
  register one, you are testing a different path. Also: assert the state is `idle`
  because it was *never written*, not because something wrote `idle` — if a future
  refactor adds an explicit `SetSessionState(idle)`, this test would still pass while
  BR-2 is violated, so T-4.2 is the one that actually pins the invariant. Keep both.
- **Integration points:** No new fixture helper should be needed (A-6). If one is,
  extend `fixture_test.go` (TEST-3), never introduce a second faking style.
- **NOT in scope for this task:** Fault-injecting the store. Asserting on reply text
  or on GitHub calls. Testing the issue-body (`TriggerCommentID == 0`) skip.

### T-4.2: Refusal from `failed` stays `failed`
**Agent:** go-unit-test-engineer

**Architectural Hints:**
- **Pattern to follow:** Same shape as T-4.1, with `f.db.SetSessionState(ctx,
  f.session.ID, store.SessionFailed)` before dispatch.
- **Watch out for:** This is the test that actually enforces BR-2 — it fails both if
  someone adds a `SetSessionState(idle)` to the refusal path (inventing a success) and
  if someone adds `SetSessionState(failed)` in the wrong place. Under TEST-10 it must
  not be edited to make a later design change pass; if a change breaks it, the change
  is wrong or the design decision needs re-opening with the owner.
- **NOT in scope for this task:** Asserting anything about the previous run's rows —
  seed the state directly, do not stage a real failed run to get there.

### T-4.3: Stub runner observes `state='running'` mid-run
**Agent:** go-unit-test-engineer

**Architectural Hints:**
- **Pattern to follow:** `TestExecuteAllocatesTheRunRowBeforeTheSubprocess`
  (`execute_test.go:180-218`) is the exact template — it reads the DB *from inside the
  stub runner closure* into captured outer variables, then asserts after `f.execute`
  returns. Copy that structure with `f.db.GetSession` instead of `GetRunByTrigger`.
- **Concurrency/context:** No goroutine, no channel, no timeout — the runner closure
  runs synchronously on the calling goroutine, so there is nothing to rendezvous with
  and **TEST-8 is satisfied by construction**. Do not reach for the
  `entered`/`release` channel idiom from `concurrent_execute_test.go` here; it would
  be ceremony without a race. If some later variant genuinely needs a rendezvous,
  `gateTimeout` (`gate_test.go:28`) is the named timeout to use.
- **Key interfaces:** Read through the real SQLite DB (`f.db`), not through
  `prep.session` or `result.Session` — TEST-6 requires the promise about persisted
  state to be tested against the real store. Reading the in-memory struct would pass
  even if the row write were deleted entirely.
- **Watch out for:** This test is the guardrail against the fix over-correcting. If
  someone later moves the write past `runAndHarvest`'s dispatch, BR-5 breaks and this
  is the only thing that catches it. Name it so that intent is obvious to whoever
  sees it fail.
- **NOT in scope for this task:** Asserting the `runs` row status — that is already
  covered by `TestExecuteAllocatesTheRunRowBeforeTheSubprocess`.

### T-4.4: Pin the unchanged terminal states
**Agent:** go-unit-test-engineer

**Architectural Hints:**
- **Pattern to follow:** Prefer extending an existing test over adding a new one.
  `TestExecuteReleasesTheLockOnAFailedRun` (`execute_test.go:248-272`) already asserts
  `SessionFailed`; `TestExecuteFetchFailureAbortsWithNoWorktree` (`:413-453`) already
  asserts `SessionFailed` for a pre-write failure. AC-6 suggests extending
  `TestExecuteRefusesAnOccupiedWorktreePath` (`:547-576`) or adding a sibling
  assertion.
- **Watch out for:** Do not duplicate coverage that already exists — check the two
  tests above first and add only the assertion that is genuinely missing (the
  successful turn ending at `idle` via `harvest.go:417-424` is the likeliest gap).
  BR-4 is about proving nothing changed, so a small assertion in the right place beats
  a new test function.
- **NOT in scope for this task:** Changing `harvest.go` or `prepareFailed`. If an
  existing test needs its assertion *changed* rather than *added to*, stop — that
  means behaviour moved and the plan is wrong (TEST-10).

### T-4.5: Build, vet, format and `make check`
**Agent:** go-implementation-engineer

**Architectural Hints:**
- **Pattern to follow:** `go build ./...`, `go vet ./...`, `gofmt -l .`, then
  `make check` (which includes `-race`), per AC-11.
- **Watch out for:** A live daemon runs from `dist/litefactory`. Do **not** `make
  build`, do not write into `dist/`, do not kill or restart it. `make check` is the
  gate; a build that replaces the running binary is not.
- **NOT in scope for this task:** `git add`/`commit`/`push` — litefactory makes the
  commit itself when the run ends. Uncommitted work at the end of the run is the
  normal state.

---

## Out of Scope for This Architecture Review

No architect is rostered for frontend, infrastructure or data-platform domains in this
team, and this feature needs none of them — the change is confined to one Go package
with no HTTP, gRPC, UI or deployment surface. Nothing in this plan falls outside Go
backend architecture.

## Deviations from `.aiteam/APP_CONSTRAINTS.md`

**None.** No rule is bent, and nothing needs to be written to
`.aiteam/AC_AMENDMENTS.md`. US-3 is direct STYLE-1 enforcement; the test plan sits
inside TEST-1/2/3/4/6/8/10; the moved write satisfies CONC-5 and leaves CONC-3
untouched.

The three items above (AMEND-1, AMEND-2, AMEND-3) are amendments to the *feature
spec*, not deviations from the standards: a count correction, a wording constraint,
and a scope suggestion. None requires the owner to stop the run — AMEND-1 and AMEND-2
are absorbed into the tasks above, and AMEND-3 is a recommendation the plan is correct
without.
