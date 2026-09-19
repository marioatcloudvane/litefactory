# Architect: Go Architect
# Created: 2026-09-19
# Feature: #313 — expose the creation proposer (agent-originated story-creation proposals)
# Spec: `feature-spec.md` (APPROVED) · Research: `RESEARCH_TECHNICAL.md` f1–f14

## System Overview

### How This Fits

Nothing new enters the package map. Every change lands in four existing
packages, along the established dependency direction
(`daemon → turn → {submission, store, report}`):

| Package | Role in this feature |
|---|---|
| `internal/submission` | Owns the immutable family catalog: one new `familyTool` entry, one `familyAllowed` arm, the `FamilyCatalogVersion` bump. Stays a leaf; knows nothing about runs or proposals. |
| `internal/store` | Owns the new `mcp_creation_presentations` row and the transaction that writes it together with the existing proposal retention. |
| `internal/turn` | Owns the MCP-facing service (`CreationProposalService`), its registration in `prepareMCP`, the guidance line, the receipt projection, and the new `Reporter` presentation method. |
| `internal/daemon` | Owns *when* the presentation is attempted (`completeTurn`) and the retry/backoff bookkeeping key. |

`internal/report` is **unchanged**: `ReplyCreationProposal` and
`report.CreationReply` already render the envelope plus the approval command
(f4, `internal/report/reply.go:737`), and the renderer already re-checks the
48000-byte bound and the hash (`internal/report/reporter.go:121-129`, `:150-159`).

No new package, no new module dependency, no new config key (the gate reuses
`Settings.AllowMCPStoryCreation`, already wired at `internal/daemon/setup.go:282`
into `CreationAuthorizer.enabled`). `internal/config/template.go` and CFG-3 are
therefore untouched.

### New Components

| Component | File | One-sentence responsibility |
|---|---|---|
| `store.CreationPresentation` + accessors | `internal/store/mcp_creation_presentations.go` (new) | Durable per-run bookkeeping of "this run retained a proposal that the owner has not been shown yet". |
| migration `0055_creation_presentations.sql` | `internal/store/migrations/` (new) | The table; `0055` is free and `//go:embed migrations/*.sql` picks it up with no registration step. |
| `turn.CreationProposalService` | `internal/turn/creation_proposal.go` (new) | The `issues_create_prepare` family owner: decodes drafts, fills factory fields, prepares/retains through the existing preparer, returns a grant-free result. |
| `(*turn.Reporter).ReportCreationPresentation` | `internal/turn/creation_presentation.go` (new) | Mints the presentation's own delivery identity and posts it through the existing delivery journal. |
| `(*daemon.Daemon).presentCreationProposal` | `internal/daemon/creation_presentation.go` (new), called from `tick.go` `completeTurn` | Decides, once per finished turn, whether a presentation is owed, and routes failure into the existing recovery/backoff machinery. |

### Key Boundaries

1. **Agent → factory (MCP family call).** In-process through
   `submission.Broker` → `ToolDispatcher.dispatchFamily`
   (`internal/turn/mcp_families.go:144`). The tool is a *registration*, not a
   grant: it must be appended to `families.Registrations` **without** setting
   `families.AllowCreation` and **without** calling
   `CreationAuthorizer.BindCreationGrant`. Those two lines are the entire
   difference between "may draft" and "may publish" (AC-4).
2. **Tool → retention.** `CreationProposalService` must reuse
   `CreationPublicationService.prepare` (`internal/turn/creation_publication.go:105`)
   rather than re-implementing redaction, the `lf:mcp-create` marker, the bounds
   check or the canonical envelope. It supplies a deterministic nonce and
   `presentationLimit = 48000`, exactly as the owner path does at
   `internal/turn/publication_recovery.go:152-155`.
3. **Retention → bookkeeping.** One SQLite transaction inside
   `internal/store`. The turn layer never writes the presentation row with a
   second call; "retained but no row" and "row but no proposal" must both be
   unreachable (AC-11, AC-13, AC-14).
4. **Run → owner comment.** The existing report-delivery journal
   (`store.EnsureReportDelivery` → `MarkReportDeliverySent` → `Confirm` →
   `Complete`). The presentation is a **new purpose** on the run's existing
   `(session, trigger)` source, with `RunID = 0` so completion cannot
   re-finalize the run (`internal/store/report_deliveries.go:176-184`).
5. **Failure → retry.** `daemon.reportAttempt` with a new recovery key
   `creation:<session>:<trigger>`, the same shape as `stopped:`
   (`internal/daemon/report_recovery.go:34`, `:53`).

### Data Flow

```
Research agent
  └─ issues_create_prepare(drafts)                       [native MCP, research only]
      └─ ToolDispatcher.dispatchFamily  → receipts.Begin
          └─ CreationProposalService.HandleDraft
              ├─ decode strict drafts → submission.CreationEnvelope
              │    (factory fills repository/source_task/version/relationship/parent_issue,
              │     appends the source issue to every link_issues)
              ├─ CreationPublicationService.prepare(env, deterministicNonce, 48000)
              │    ├─ redact + marker + CanonicalCreation
              │    └─ retain:  store tx { mcp_creation_proposals ⊕ mcp_creation_current
              │                           ⊕ mcp_creation_presentations(run_id) }
              └─ receipts.Finish(confirmed, {creation_proposal, run_id})
run ends
  └─ daemon.completeTurn                                  [graph branch and legacy branch]
      └─ presentCreationProposal
          ├─ store.PendingCreationPresentation(runID)      → ErrNotFound ⇒ nothing owed
          ├─ FindPendingReportDelivery(session, trigger)   → another open purpose ⇒ defer
          ├─ Reporter.ReportCreationPresentation           → report.ReplyCreationProposal body
          │    └─ EnsureReportDelivery(RunID=0, purpose creation-proposal:<hash>) → POST → confirm → complete
          └─ store.MarkCreationPresented(runID)
owner
  └─ /mcp-create <hash> linked|attach                      [unchanged, 30-min TTL, intent only]
```

### Concurrency & Context

- No new goroutine. CONC-2's budget is unchanged.
- Every new store accessor takes `ctx` first (CONC-5) and runs inside
  `db.withTx`; no transaction spans forge I/O (CONC-10, CONC-18). The
  presentation POST happens strictly outside any transaction, between
  `MarkReportDeliverySent` and `ConfirmReportDelivery`, which is what the
  existing journal already guarantees.
- Ordering is CONC-4: render → durable intent → post → confirm → complete →
  only then `presented = 1` (R-19). A crash at any point leaves the row with
  `presented = 0` and a recoverable delivery key.
- The presentation runs on the worker goroutine inside `completeTurn`, under
  the tick context that already carries `recoveryTickKey`
  (`internal/daemon/tick.go:135`), so backoff is deterministic clock-driven and
  tests need no sleeps (TEST-8).
- One graph run advances at most one agent node
  (`internal/turn/research_workflow.go:66-78`), so `PRIMARY KEY(run_id)` is a
  true "one proposal per run" constraint on both paths.

### Shared State and Side Effects other packages must know about

- **`FamilyCatalogVersion` is in every attempt's permission digest**
  (`internal/turn/mcp_families.go:121`). Bumping it to `families-v3` changes the
  digest of *every* attempt, in every mode. It is pinned in
  `internal/turn/activity_runtime_io_test.go:102` — a TEST-10 test: update it
  deliberately, as a design change.
- **Supersession is global per task, not per run.** `RetainProposal` bumps
  `mcp_creation_current.revision` (`internal/store/mcp_creation.go:118`), so a
  hand-written `/mcp-create prepare` between the tool call and the presentation
  makes the presented hash unapprovable. That is the existing, intended
  behaviour (d7); do not add a second supersession mechanism.
- **The run report and the presentation share one `(session, trigger)` source.**
  `FindPendingReportDelivery` hard-errors on two unresolved purposes
  (`internal/store/report_deliveries.go:228-231`), and `daemon.resumeReport`
  calls it on every later tick. An unresolved presentation therefore occupies
  the task's single delivery slot until it completes.

---

## Cross-story decisions

These bind more than one story. Engineers must not re-litigate them per task.

**D-1 — The atomic retention seam lives in `internal/store`, and `prepare`
takes an injected retainer.** `CreationPublicationService.prepare` currently
calls `s.authorizer.RetainProposal` (`creation_publication.go:154`). Split
`CreationStore.RetainProposal` into an unexported `retainProposalTx(ctx, tx, p)`
plus two exported entry points: the existing `RetainProposal` and a new
`RetainProposalForRun(ctx, CreationProposal, CreationPresentation) error` that
calls `retainProposalTx` and the presentation upsert inside one `withTx`. On the
turn side, give `prepare` an injected retain function (STYLE-4 function-field
seam): the owner path passes `authorizer.RetainProposal`, the tool path passes a
closure onto a new `CreationAuthorizer.RetainProposalForRun`. Duplicating the
redaction/marker/bounds logic to get atomicity is the wrong trade.

**D-2 — `session_id` is derived in SQL, never supplied.** The presentation
insert resolves `session_id` from `runs.session_id` and refuses when the run's
session `task_ref` differs from `source_task`. No caller — least of all a tool
handler — names a session.

**D-3 — The source-issue auto-link becomes one shared helper.** R-7's "append
the source issue to every `link_issues`" exists today only in
`MCPRuntime.PrepareCreationProposal` (`publication_recovery.go:137-142`). Extract
it into an unexported helper in `internal/turn/creation_publication.go` and call
it from both paths, so AC-9 and AC-25 can never drift apart.

**D-4 — The presentation carries its own delivery identity and its own recovery
key, and must not reuse `ReportReply`.** `ReportReply` skips when the trigger is
already consumed (`internal/turn/report.go:624-631`, f12) — which is always true
for a finished run's trigger. The new `ReportCreationPresentation` therefore has
no `consumed` check, mints `deliveryIntent(ref, ref, sessionID, run.TriggerCommentID,
0, "creation-proposal:"+hash)`, and renders through
`report.Reply{Kind: ReplyCreationProposal, Creation: …}`.

**D-5 — Exempt the presentation purpose from the legacy-runless ambiguity
probe.** `dispatchDelivery` runs `legacyRunlessConflict` for every `RunID == 0`
delivery (`internal/turn/report_delivery.go:157-168`). On any task that carries a
pre-journal bot comment (a `lf:status` or `lf:reply` marker without
`lf:delivery`), that probe would declare the presentation ambiguous, set
`possibly_sent`, and then wedge permanently in `recoverDelivery` ("no trustworthy
receipt found"). The probe exists to disambiguate *legacy task-only replies*; a
purpose that did not exist before the journal cannot be ambiguous. Guard it with
the purpose (e.g. skip when `delivery.Purpose` has the `creation-proposal:`
prefix) and say so in the comment. This is the one change this feature makes to
shared delivery code; it is deliberate and it needs its own test.

**D-6 — The receipt projection is registered under the reference kind, not only
under `tool:`.** `ReceiptService.Get` looks up `projections[Reference.Kind]`
first and only falls back to `projections["tool:"+Tool]` when the kind is empty
(`internal/turn/family_receipts.go:141-145`). With reference kind
`creation_proposal`, a projection registered only as
`projections["tool:issues_create_prepare"]` would never run and AC-15 would fail.
Register the same `FamilyProjection` under **both** keys — `"creation_proposal"`
for the normal path and `"tool:issues_create_prepare"` for a crash before the
reference was linked. (`mcp_family_catalog_test.go:132` shows the dual-key shape
already in use for comments and publications.)

**D-7 — The daemon hook keys off durable bookkeeping, not configuration.**
`presentCreationProposal` reads `PendingCreationPresentation(run.ID)`;
`ErrNotFound` means nothing is owed. Do not add a second
`AllowMCPStoryCreation` check in the daemon: a row can only exist if the tool was
installed, and a second gate can drift from the first. This is what makes AC-2,
AC-19 and "proposal retained, run then failed" fall out structurally.

**D-8 — Extend the daemon's `Reporter` through an optional capability
assertion, not by widening the interface.** `daemon.Reporter`
(`internal/daemon/daemon.go:137-142`) is declared at the point of consumption
(TEST-5); `resumeReport` already shows the house pattern of asserting an
optional method (`report_recovery.go:273-278`). Follow it — but when a pending
presentation row exists and the reporter does **not** implement the method,
fail loudly rather than silently skipping (CONC-3: a failed attempt is a
reported outcome, not silence).

### Spec corrections carried by these decisions

The approved spec stands; these three lines describe *mechanism* the spec named
slightly off, and the corrected form is what the acceptance criteria require:

1. `RetainRunProposal(ctx, CreationPresentation)` cannot retain atomically — it
   carries no envelope. Use `RetainProposalForRun(ctx, CreationProposal,
   CreationPresentation)` (D-1). The spec's four accessor *behaviours* are
   unchanged.
2. The projection must also be registered under `"creation_proposal"` (D-6).
3. `presentCreationProposal` is called *from* `completeTurn` but lives in its own
   file; `tick.go` gains two call sites, not a new method body.

No rule in `.aiteam/APP_CONSTRAINTS.md` has to bend for this feature. **No
standard deviation is requested.**

---

## Architecture Notes (platform lens)

- Platform: project layout — all four touched packages are under `internal/`; no
  `pkg/`, no new top-level package, no new `cmd/` binary.
- Platform: error handling — the refusal surface is existing sentinels
  (`store.ErrCreationAuthority`, `store.ErrNotFound`, `store.ErrCreationConflict`);
  add no new sentinel unless the daemon must branch on it. The presentation's
  failure type is the existing typed `turn.DeliveryError`, which
  `daemon.recoveryFailure` already reads with `errors.As`.
- Platform: concurrency — no new goroutine; every blocking call takes `ctx`
  first; no transaction wraps a POST.
- Platform: interfaces — one new consumer-side capability assertion (D-8); no
  interface next to its implementation.
- Platform: dependencies — no new module. `go.mod` keeps its four direct
  requires (TEST-2).
- Platform: HTTP layer — not applicable (no listener; the forge client is
  existing).
- Platform: database access — one new migration file, parameterised SQL only,
  `ctx` on every query; `Open` still does not migrate.
- Platform: testing — stdlib `testing`, table-driven, hand-written fakes;
  `turn`/`daemon` tests use the existing real-SQLite + fake-forge fixtures
  (TEST-3) and must not use `*_internal_test.go` in `turn` (TEST-4).
- Platform: logging — the daemon's injected `*slog.Logger`; no package-level
  logger, no `fmt.Print*` (STYLE-2).
- Platform: linting — `make check` includes race tests; the digest bump touches
  a pinned test (TEST-10).

## Security Findings

**Scope:** the planned change only (forward-looking), not an audit of existing
creation code. **Rulebook:** `.aiteam/APP_CONSTRAINTS.md` §1 `SEC`.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ✅ | Strict schema (`additionalProperties:false`) plus `submission.CanonicalCreation` before *and* after preparation; `repository`/`source_task`/`version` have no input field (R-5). |
| 2 | Authorisation | ✅ | Installation is not a grant: no `AllowCreation`, no intent lookup, no `BindCreationGrant` on this path (AC-4). Approval stays numeric-owner-only and forge-mediated (SEC-8). |
| 3 | Authorisation | ⚠️ | The digest must include the new tool *before* `Authority.Bind` (`mcp_runtime.go:319-330`) and the installed catalog must still equal `spec.PermittedTools()` (`:358-365`). Registering after the digest, or registering in a mode whose `familyAllowed` filters the tool out, silently desynchronises the lease. |
| 4 | Untrusted text | ✅ | Agent draft text is redacted and bounded before hashing (`prepare`), and the renderer refuses any body whose redaction changes a byte. SEC-5/SEC-6 hold: nothing bypasses `redact.Redactor.Apply`. |
| 5 | Untrusted text | ⚠️ | The tool result must never contain `/mcp-create` or an approval command (R-12): the agent must be structurally unable to quote an approval into its own report. Keep the result struct fieldless of any command text. |
| 6 | Secrets / config | ✅ | No new config key, no secret in any new struct; the presentation body carries only already-redacted bytes. CFG-4 untouched. |
| 7 | Error handling | ✅ | `DeliveryError` exposes key/state, not body text; `recoveryFailure` redacts the reason to 1024 bytes before logging. |
| 8 | Data handling | ⚠️ | An envelope that renders once must render identically forever: the delivery body is stored and re-checked byte-for-byte on recovery (`report_delivery.go:169-175`). Never mutate the rendered presentation body after the intent is saved. |

**Summary:** the posture is sound because the feature adds *drafting* to a
pipeline whose approval, quota and publication gates are unchanged. The highest
priority item is #3 — registration-order and catalog equality, which is the one
mistake that would fail closed loudly but is easy to introduce while moving the
branch at `mcp_runtime.go:301`.

---

## Suggested decomposition — 4 independently deliverable stories

Dependency order: **S1 → S2**, **S1 → S3 → S4**. S4's end-to-end criteria also
want S2 merged; S4 can still be built and tested against a hand-seeded
presentation row, so it is not blocked.

| Story | Title | Delivers | Depends on | AC covered | Test shape |
|---|---|---|---|---|---|
| S1 | Per-run proposal bookkeeping | migration `0055`, `store.CreationPresentation`, the four accessors, the `retainProposalTx` split and `RetainProposalForRun` | — | (foundation for AC-11/13/14) + AC-25, AC-27 regression | **Unit only**, in-package `internal/store` against real migrated SQLite (TEST-6) |
| S2 | The `issues_create_prepare` family tool | `familyTool`/`familyAllowed`/`families-v3`, `CreationProposalService`, registration in `prepareMCP` for `Invocation == ""` **and** `InvocationResearchNode`, guidance line, receipt projection | S1 | AC-1…AC-15, AC-28 | **Unit** (`internal/submission` catalog table; `mcpGuidance`) + **in-process integration** (`internal/turn` dispatcher over real SQLite and a fake MCP-supporting runner) |
| S3 | Presentation delivery identity | `Reporter.ReportCreationPresentation`, the `creation-proposal:<hash>` purpose, the D-5 carve-out in `dispatchDelivery` | S1 | AC-16, AC-18, AC-19, AC-21, AC-23 | **Unit** (delivery-identity determinism, purpose shape) + **integration** (`internal/turn` fixture: real store, fake forge; crash-between-POST-and-confirm case) |
| S4 | Post-run presentation hook and recovery | `presentCreationProposal`, both `completeTurn` branches, recovery key `creation:<session>:<trigger>`, R-17 deferral | S1, S3 (S2 for end-to-end) | AC-17, AC-20, AC-22, AC-24, AC-26 regression | **Integration** (`internal/daemon` fixtures: httptest GitHub, real store, fake executor; deterministic `tickAt`, no sleeps — TEST-8) |

Each story's definition of done includes AC-28 (`go build ./...`, `make check`).

### Task tables

**S1 — Per-run proposal bookkeeping**

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-1.1 | Migration `0055_creation_presentations.sql` | data | — | US-3 | XS |
| T-1.2 | `store.CreationPresentation` + `PendingCreationPresentation` / `BindCreationPresentationDelivery` / `MarkCreationPresented` | data | T-1.1 | US-3, US-5 | S |
| T-1.3 | Split `RetainProposal` into `retainProposalTx` + add `RetainProposalForRun` | data | T-1.1 | US-3 | S |
| T-1.4 | Store tests: upsert/supersession, task mismatch refusal, presented-run refusal, idempotent mark | test | T-1.2, T-1.3 | US-3 | M |

**S2 — The `issues_create_prepare` family tool**

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-2.1 | `familyTool` entry + `familyAllowed` arm + `FamilyCatalogVersion = families-v3` | api | S1 | US-1, US-4 | S |
| T-2.2 | `CreationProposalService`: input type, factory field filling, deterministic nonce, `prepare(…, 48000)`, grant-free result | logic | T-2.1 | US-1, US-7 | M |
| T-2.3 | Injected retainer on `prepare` + shared source-issue auto-link helper (D-1, D-3) | logic | T-2.2 | US-1 | S |
| T-2.4 | Registration in `prepareMCP` for both invocation kinds + receipt projection under both keys (D-6) | api | T-2.2 | US-1, US-6 | S |
| T-2.5 | `mcpGuidance` creation-proposal line with the split advice | logic | T-2.1 | US-1 | XS |
| T-2.6 | Catalog/profile unit tests + existing catalog-test updates (incl. the `families-v2` pin) | test | T-2.1 | US-1, US-4 | M |
| T-2.7 | Dispatcher tests: retention, refusals, replay, identical/changed input, disabled and non-research | test | T-2.4 | US-1, US-3, US-4 | L |

**S3 — Presentation delivery identity**

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-3.1 | `Reporter.ReportCreationPresentation` (intent, render, deliver) | api | S1 | US-2 | M |
| T-3.2 | Legacy-runless probe carve-out for the presentation purpose (D-5) | logic | T-3.1 | US-2, US-5 | S |
| T-3.3 | Delivery tests: `RunID == 0` non-finalisation, no consumed-skip, POST-then-crash recovery, no second comment | test | T-3.1, T-3.2 | US-2, US-5 | L |

**S4 — Post-run presentation hook and recovery**

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-4.1 | `presentCreationProposal` + both `completeTurn` call sites | logic | S3 | US-2, US-6 | M |
| T-4.2 | Recovery key `creation:<session>:<trigger>` through `reportAttempt`, plus R-17 deferral | logic | T-4.1 | US-5 | S |
| T-4.3 | Daemon tests: legacy path, graph path, forge outage + retry, competing open purpose, two proposals in one run | test | T-4.2 | US-2, US-5, US-6 | L |

---

## Per-task hints

### T-1.1: Migration `0055_creation_presentations.sql`
**Agent:** go-implementation-engineer

- **Pattern:** plain embedded SQL file; `//go:embed migrations/*.sql`
  (`internal/store/migrate.go:20`) picks it up with no registration and no
  version list to update. `0055` is the next free number (`0054` is the latest).
- **Package placement:** `internal/store/migrations/0055_creation_presentations.sql`.
- **Key interfaces:** columns exactly as the spec's data model —
  `run_id` PK → `runs(id)`, `session_id` → `sessions(id)`, `source_task`,
  `proposal_hash` → `mcp_creation_proposals(hash)`, `relationship` CHECK IN
  (`linked`,`attach`), `artifacts` CHECK BETWEEN 1 AND 5, `retained_ns`,
  `delivery_key` DEFAULT `''`, `presented` DEFAULT 0 CHECK IN (0,1).
- **Watch out for:** each migration runs in its own transaction and SQLite DDL is
  transactional — do not split this across two files. Follow the FK/`STRICT`
  conventions of `0046_creation_publication.sql` rather than inventing a new
  table style.
- **Not in scope:** any change to `mcp_creation_proposals`, `_current`,
  `_intents`, `_grants` or `_operations` (d8).

### T-1.2: Presentation type and read/bind/mark accessors
**Agent:** go-implementation-engineer

- **Pattern:** one file per entity —
  `internal/store/mcp_creation_presentations.go`. Read
  `internal/store/store.go`'s header comment and `mcp_creation.go` first
  (STYLE-7); mirror the `scanX` + `withTx` shape used there.
- **Package placement:** accessors on `*DB` (per the spec's table); the
  retention accessor belongs on `*CreationStore` (T-1.3) because it shares that
  type's bookkeeping clock.
- **Key interfaces:** `PendingCreationPresentation(ctx, runID) (CreationPresentation, error)`
  returning `ErrNotFound` when absent **or already presented**;
  `BindCreationPresentationDelivery(ctx, runID, key)` refusing a *different*
  key once set (idempotent for the same key);
  `MarkCreationPresented(ctx, runID)` idempotent and refusing when no delivery
  key is bound. `ctx` first on all of them (CONC-5).
- **Watch out for:** `ErrNotFound` is the daemon's "nothing owed" signal — do
  not invent a second sentinel, and do not return a zero struct with a nil
  error. Ordinary errors must keep their cause (STYLE-5).
- **Not in scope:** any forge call, any delivery-journal write.

### T-1.3: Atomic retention (`retainProposalTx` + `RetainProposalForRun`)
**Agent:** go-implementation-engineer

- **Pattern:** extract the body of `CreationStore.RetainProposal`
  (`internal/store/mcp_creation.go:93-121`) into an unexported
  `retainProposalTx(ctx, tx, p)`; keep `RetainProposal` as a one-transaction
  wrapper so its behaviour is bit-for-bit unchanged; add
  `RetainProposalForRun(ctx, CreationProposal, CreationPresentation) error`
  that calls both `retainProposalTx` and the presentation upsert in one `withTx`.
- **Package placement:** the split stays in `mcp_creation.go`; the new exported
  method may live in `mcp_creation_presentations.go`.
- **Key interfaces:** the upsert is `ON CONFLICT(run_id) DO UPDATE`, which is
  what makes "the last proposal of a run wins" structural (d7). Derive
  `session_id` from `runs.session_id` in SQL and refuse when that session's
  `task_ref` ≠ `source_task` (D-2). Refuse when the existing row has
  `presented = 1`.
- **Watch out for:** an identical re-retention must **not** bump
  `mcp_creation_current.revision` — that is already handled by the `CASE WHEN
  proposal_hash<>excluded.proposal_hash` at `mcp_creation.go:118`; leave it
  alone. Keep `retained_ns` sourced from the store's injected clock, never from
  an argument.
- **Not in scope:** changing intent/grant/operation logic; approval TTL; quota.

### T-1.4: Store tests
**Agent:** go-implementation-engineer

- **Pattern:** table-driven, `package store`-internal as `mcp_creation_test.go`
  already is; real migrated SQLite, never a fake (TEST-6).
- **Key interfaces:** cover — retention creates exactly one row; a second,
  different proposal in the same run replaces it and bumps the revision; an
  identical re-retention leaves revision and row untouched; a task/session
  mismatch refuses with `ErrCreationAuthority` and writes nothing; a presented
  run refuses a new proposal; `MarkCreationPresented` is idempotent and refuses
  without a bound key.
- **Watch out for:** assert the *absence* of the bookkeeping row on every
  refusal path — that is the half of AC-11 a happy-path test misses.
- **Not in scope:** anything in `turn` or `daemon`.

### T-2.1: Catalog entry, mode gate and `families-v3`
**Agent:** go-implementation-engineer

- **Pattern:** one `case` in `familyTool` and one arm in `familyAllowed`
  (`internal/submission/family_tools.go:30-84`). `internal/submission` is a leaf:
  it must not learn about runs, proposals or the creation gate.
- **Key interfaces:** `familyAllowed` returns `mode == "research"` for this name
  and **must not** consult the `creation` boolean — that flag means "a live
  grant exists", which this tool never has. Effect label `input`. Bump
  `FamilyCatalogVersion` to `families-v3`.
- **Watch out for:** `ToolProfile.List()` filters by `familyAllowed`, while the
  turn layer's `handlers` map does not — a registration installed in a mode the
  profile filters out desynchronises `spec.PermittedTools()` from
  `dispatcher.profile.List()` and trips the guard at `mcp_runtime.go:363`.
  Register only inside the research branch (T-2.4). The digest bump changes every
  attempt's permission digest; `internal/turn/activity_runtime_io_test.go:102`
  pins the old value and is a TEST-10 test — change it as a design change, and
  check that no in-flight lease digest is compared across a binary upgrade.
- **Not in scope:** the input schema's *semantic* validation (that is
  `CanonicalCreation`'s job); anything about grants.

### T-2.2: `CreationProposalService`
**Agent:** go-implementation-engineer

- **Pattern:** the `IssuePublicationService` shape
  (`internal/turn/issue_publication.go:29-75`) is the model: concrete struct,
  validating constructor (STYLE-3), `Binding()` + `PermissionMaterial()` to
  satisfy `FamilyOwner`, a single `Registration()`.
- **Package placement:** new `internal/turn/creation_proposal.go`. Read
  `internal/turn/doc.go` first (STYLE-7).
- **Key interfaces:** implement `FamilyOwner`
  (`internal/turn/mcp_families.go:18`); `PermissionMaterial()` marshals
  `{Enabled, BotID, SourceIssue}` only — no intent id, no proposal hash (those
  belong to the publication service's material and would make the digest depend
  on state this tool must not read). Decode with `decodeMCPArguments` into a
  **new** input struct (never `submission.CreationEnvelope` directly), so
  `repository`/`source_task`/`version` are refused by strict decoding (R-5).
  Fill `relationship` default `linked`, and for `attach` fill `parent_issue`
  with the source issue number, refusing any other supplied value (R-6).
  Finish the receipt yourself with
  `store.FamilyReference{Kind: "creation_proposal", ID: runID}`.
- **Concurrency/context:** the handler is called from the broker's worker pool;
  it holds no mutable state beyond immutable construction fields. No guard
  binding, no `BindGuard`, no lease context needed — this tool performs no
  remote effect.
- **Watch out for:** the nonce must be exactly
  `sha256("task:runID:attempt:canonicalInput")[:16]` over the tool's *raw
  argument bytes*, so an identical call on the same attempt reproduces the same
  marker, envelope and hash (R-9, AC-13) — hash the bytes you were handed, not a
  re-marshalled struct. The result must contain no `/mcp-create` text and no
  approval command (R-12, US-7). The dispatcher finishes a failed call as
  `rejected` unless the tool is on its "uncertain" list
  (`mcp_families.go:194-197`) — do **not** add this tool there: retention is
  local and transactional, so a failure means nothing was retained.
- **Not in scope:** publication, grants, quota, `issues_create_batch`.

### T-2.3: Injected retainer and shared auto-link helper
**Agent:** go-implementation-engineer

- **Pattern:** function-field / parameter injection (STYLE-4), not a new
  interface (TEST-5 — one production implementation each).
- **Key interfaces:** `CreationPublicationService.prepare` gains a retain
  function; the owner path (`publication_recovery.go:155`) keeps today's
  behaviour by passing `authorizer.RetainProposal`. Add
  `CreationAuthorizer.RetainProposalForRun(ctx, envelope, presentation) (string, error)`
  next to `RetainProposal` (`internal/turn/mcp_creation.go:67`), reusing
  `creationProposal(e)` for the canonical conversion.
- **Watch out for:** `prepare` validates `CanonicalCreation` before *and* after
  redaction — keep both calls. The auto-link helper must not duplicate a target
  already present and must leave the post-append list ≤16 (R-7, AC-9); the owner
  path must keep producing byte-identical envelopes after the extraction
  (AC-25).
- **Not in scope:** changing the 48000 limit, the marker format or the redaction
  budgets.

### T-2.4: Registration in `prepareMCP` and the receipt projection
**Agent:** go-implementation-engineer

- **Pattern:** a sibling branch to the creation-grant block at
  `internal/turn/mcp_runtime.go:301`, *without* the
  `req.Invocation != agent.InvocationResearchNode` exclusion (a7) and *without*
  the `EligibleCreationIntent` lookup.
- **Package placement:** the branch in `mcp_runtime.go`; the projection function
  next to the service in `creation_proposal.go`.
- **Key interfaces:** condition is exactly R-1 — `req.Mode == "research"`,
  native MCP supported, `!runtime.DisableMCP`,
  `runtime.CreationAuthorizer != nil && .enabled`, `runtime.Creation != nil`.
  Append to `families.Registrations` only; never touch `families.AllowCreation`.
  Register the projection in the `projections` map (around `:239-242`) under
  **both** `"creation_proposal"` and `"tool:issues_create_prepare"` (D-6).
- **Integration points:** the registration must happen before
  `FamilyPermissionDigest(families)` at `:319` and before `Authority.Bind` at
  `:327`; the catalog-equality guard at `:358-365` then proves the launch spec
  matches. The graph path reaches the same function through
  `research_workflow.go:139` with `session == nil` — nothing in the new branch
  may dereference `session`.
- **Watch out for:** the existing `Invocation != InvocationResearchNode` guards
  on lines 292 and 301 stay as they are; this feature adds a branch, it does not
  relax the comment service or the publication grant for graph runs.
- **Not in scope:** changing when `issues_create_batch` is registered.

### T-2.5: Guidance line
**Agent:** go-implementation-engineer

- **Pattern:** one `if has("issues_create_prepare")` block in `mcpGuidance`
  (`internal/turn/mcp_guidance.go:53-70`), rendered from the installed catalog
  only — the prompt and the provider configuration read the same list.
- **Key interfaces:** carry the split recommendation as advice (R-14): no
  threshold, no enforcement, never a refusal. Keep the existing tone: name the
  call shape, say what it does not grant.
- **Watch out for:** SEC-20/CONC-23 — this is factory text; it must not imply the
  agent may approve, publish, or quote an approval command.
- **Not in scope:** `.claude/**` and `.codex/**` files (explicitly out of scope,
  so the protected-instruction story split does not apply to this feature).

### T-2.6 / T-2.7: Catalog and dispatcher tests
**Agent:** go-implementation-engineer

- **Pattern:** extend the existing suites rather than adding parallel ones —
  `internal/submission/family_tools_test.go`, `family_catalog_test.go`,
  `family_journal_transport_test.go`; `internal/turn/mcp_family_catalog_test.go`,
  `mcp_guidance_test.go`, `mcp_runtime_test.go` (TEST-3). `turn` is tested
  in-package: no `_internal_test.go` suffix there (TEST-4).
- **Key interfaces:** the turn-side test must drive the registered handler
  through `NewFamilyToolDispatcher` against real migrated SQLite with a session
  row present (`RetainProposal` joins `sessions`/`repos`), using a fake runner
  reporting `SupportsMCP() == true`.
- **Watch out for:** AC-1 needs the catalog asserted for **both**
  `Invocation == ""` and `Invocation == agent.InvocationResearchNode` — one
  table with the invocation as a column. AC-11's refusal list is long; drive it
  table-driven and assert "nothing retained **and** no bookkeeping row" for each.
- **Not in scope:** any test that launches a real `claude`/Codex process
  (EXT-1, EXT-5). Live host invocation of this tool is unverifiable in this
  sandbox — do not write an assertion, a comment or a story claim that implies
  it was observed.

### T-3.1: `Reporter.ReportCreationPresentation`
**Agent:** go-implementation-engineer

- **Pattern:** `ReportReply` (`internal/turn/report.go:598-655`) is the template
  for structure and `deliver(...)` usage — but not for its `consumed` early
  return, which must not be copied (D-4, f12).
- **Package placement:** new `internal/turn/creation_presentation.go`.
- **Key interfaces:** `deliveryIntent(ref, ref, sessionID, run.TriggerCommentID,
  0, "creation-proposal:"+hash)` then `r.deliver(intent, render)` where render
  builds `report.Reply{Ref, Kind: report.ReplyCreationProposal,
  Creation: &report.CreationReply{Envelope, Hash, Relationship}}` through
  `r.renderer.RenderReply`. Read the envelope bytes from the retained proposal,
  not from an argument the caller assembled.
- **Integration points:** `EnsureReportDelivery` validates the session/task
  match; `CompleteReportDelivery` with `RunID == 0` runs `consumeReportSource`,
  which is safe here — `advanceWatermark` is `MAX(...)` and the run's trigger is
  already at or below the watermark, and a private graph trigger is skipped
  entirely (`store.IsPrivateTrigger`). Say so in a comment (STYLE-1); it is the
  non-obvious part of AC-23.
- **Watch out for:** `RunID` must stay 0 or completion re-finalizes the run and
  overwrites `runs.result_comment_id` (AC-18). Bind the delivery key into the
  bookkeeping row (`BindCreationPresentationDelivery`) **before** the POST, and
  set `presented = 1` only after the delivery completes (R-19).
- **Not in scope:** deciding *when* to present (that is S4), and any change to
  `internal/report`.

### T-3.2: Legacy-runless probe carve-out
**Agent:** go-implementation-engineer

- **Pattern:** a purpose-scoped guard on the existing branch at
  `internal/turn/report_delivery.go:157-168` (D-5).
- **Watch out for:** this is shared delivery code on the path of every runless
  reply — keep the change to the narrowest possible condition, and update the
  surrounding comment to say why a post-journal purpose cannot be ambiguous
  (STYLE-1). A regression test must prove ordinary replies still take the probe.
- **Not in scope:** any other change to `dispatchDelivery`'s ordering, the
  `MarkReportDeliverySent` claim or `recoverDelivery`'s matching rules.

### T-3.3: Delivery tests
**Agent:** go-implementation-engineer

- **Pattern:** follow `internal/turn/report_delivery_db_test.go` and
  `research_failure_publication_test.go`; real store + fake forge.
- **Key interfaces:** cover — completion does not touch `runs.result_comment_id`;
  a run whose trigger was consumed still presents; a delivery marked
  `possibly_sent` with a matching remote comment confirms rather than re-posting
  (AC-21); a task carrying a legacy pre-journal bot comment still presents
  (the D-5 regression).
- **Watch out for:** TEST-8 — rendezvous and injected clocks only; no
  `time.Sleep`, no polling.
- **Not in scope:** daemon scheduling behaviour.

### T-4.1: `presentCreationProposal` and the two `completeTurn` call sites
**Agent:** go-implementation-engineer

- **Pattern:** a small method in a new `internal/daemon/creation_presentation.go`,
  called from `completeTurn` (`internal/daemon/tick.go:1910`); `handleMCPCreation`
  (`internal/daemon/mcp_creation.go:46-57`) is the closest existing shape for
  "prepare, report, leave".
- **Key interfaces:** graph branch — after `d.reporter.Consume(...)` succeeds,
  attempt the presentation and route its error through `d.leave`; legacy branch
  — after `d.reportTurn(...)` succeeds, before the "turn complete" log. Use
  `turnResult.Run.ID`; a zero run id means nothing is owed. `d.db` gives the
  bookkeeping reads (D-7); the reporter capability is an optional assertion
  (D-8) that must fail loudly when a row exists.
- **Integration points:** `internal/daemon/stopped_results.go:104` also calls
  `completeTurn`, so repaired stopped results inherit the hook for free —
  intentional, and worth a line of comment.
- **Watch out for:** `d.leave` swallows a `*recoveryPending` into a deferred
  worker status and returns nil; that is exactly the shape a deferred or failed
  presentation should have, so return the error from `reportAttempt` unchanged
  rather than logging and discarding it (CONC-3). Do not let a presentation
  failure rewrite the run's outcome or un-consume its trigger.
- **Not in scope:** posting anything when no row exists; a second
  `AllowMCPStoryCreation` check.

### T-4.2: Recovery key and deferral
**Agent:** go-implementation-engineer

- **Pattern:** `d.reportAttempt(ctx, store.ReportRecovery{Key:
  "creation:" + recoveryKey(session, trigger), …}, attempt)` — the `stopped:`
  prefix at `internal/daemon/report_recovery.go:53`, `tick.go:1873` is the
  precedent for a prefixed variant.
- **Key interfaces:** before minting, call
  `FindPendingReportDelivery(session, trigger)`; if it returns a *different*
  unresolved key, defer to the next tick instead of minting a second open
  purpose (R-17, AC-22) — the store hard-errors on two
  (`report_deliveries.go:228-231`). Failure feeds `recoveryFailure`, which
  already reads `*turn.DeliveryError` for its state and escalates at three
  attempts.
- **Watch out for:** `sessionRecoveryHeld` will now hold a session for an unposted
  presentation — intended (the draft is owed), but say so, because it means a
  forge outage defers the task's next comment too. Also note that
  `resumeReport` can pick the presentation up on a later tick through the
  generic `ReportPending` path under the `source:` key; both routes must be
  idempotent, which the delivery journal already guarantees.
- **Not in scope:** changing backoff constants, escalation thresholds or
  `refreshRecoveries`' `stopped:` migration logic.

### T-4.3: Daemon tests
**Agent:** go-implementation-engineer

- **Pattern:** extend the existing daemon fixtures — httptest GitHub, real
  store, fake executor (TEST-3), following `report_recovery_db_test.go`.
- **Key interfaces:** cover AC-17 (graph Research node retained a proposal →
  one comment), AC-20 (forge failure → `presented = 0`, debt under
  `creation:…`, next eligible tick posts exactly one), AC-22 (competing open
  purpose defers), AC-24 (two proposals in one run → one comment showing the
  later hash).
- **Watch out for:** TEST-8 and TEST-9 — drive time through the tick clock, never
  a sleep or an `Eventually`; do not invent a worker-slot observable, use a real
  subsequent tick. Count comments on the fake forge rather than trusting the
  returned `Posted`.
- **Not in scope:** proving a real GitHub outage; the fake proves the factory's
  behaviour, not the forge's (record it that way).

---

## Assumptions recorded (no questions permitted this run)

1. **A1** — `0055` is the correct next migration number; verified free on this
   branch (`0054_graph_dispatch_attempts.sql` is the latest). A concurrent
   feature taking `0055` first is a rename, not a redesign.
2. **A2** — The daemon hook is unconditional on configuration (D-7): a proposal
   retained while the feature was enabled is still presented if the flag is
   turned off before the run ends. This reads as correct (the draft exists and
   the owner should see it) and is compatible with US-4, which concerns runs
   where the tool was never installed.
3. **A3** — Bumping `FamilyCatalogVersion` invalidates no persisted state: leases
   are bound per attempt and a resumed launch mints a new attempt. If an
   upgraded binary is found comparing a pre-upgrade digest, that is a
   fail-closed refusal, not corruption — but it should be checked in T-2.1.
4. **A4** — `docs/decisions/nested-agent-evidence.md` does not exist in this
   worktree (as the spec records). The evidence obligations are read from
   repository convention: offline Go tests, no integration tags, `$LF_HOME`-scoped
   state. Live host invocation of `issues_create_prepare` and real forge outage
   behaviour remain **unverified obligations** and must not be claimed as
   demonstrated anywhere in the stories (EXT-4, EXT-5).
5. **A5** — No domain outside Go/backend is touched. No architect is rostered
   for frontend, infra topology, CI/CD, ML or mobile, and none is needed here.

## Gaps noted, not invented

- The spec's `Out of Scope` excludes issue #314 (queue wedging). D-5 and R-17
  both touch the delivery-slot behaviour that #314 is about; this feature must
  not be extended to fix #314, but its tests should not *depend* on the wedge
  either.
