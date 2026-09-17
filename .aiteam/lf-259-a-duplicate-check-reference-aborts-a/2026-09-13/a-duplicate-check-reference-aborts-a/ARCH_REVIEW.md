# Architect: Go Architect
# Created: 2026-09-17

Verdict: APPROVED for the three retained repairs. No deviations or unresolved architectural decisions.

## System Overview

### How This Feature Fits

Keep the repair in existing `internal/store` and `internal/turn`, with daemon maintenance remaining the global caller. Source inspected at `5ebdbd93f020b94429b07648c1f9c1984ecd5913`. The current check reader uses only run/reference (`internal/store/submission_evidence.go:181`); session cleanup joins global recovery into the caller's error (`internal/turn/submission_runner.go:53`, `internal/turn/submissions.go:182`); harvest still substitutes a startup failure for any runner error (`internal/turn/harvest.go:107`).

### New Packages / Components

None. Add one embedded SQL migration and narrowly extend existing store/recovery interfaces. No protocol, service, dependency or snapshot-handle redesign.

### Key Boundaries

- Store owns SQL, migration and exact evidence identity `(run_id, snapshot_ref, reference)`; turn supplies an already authorized snapshot. Attempt remains execution provenance and authorization, not an additional check-key dimension.
- Submission sessions supply trusted run/attempt identity to foreground recovery; daemon ticks explicitly retain global maintenance.
- Harvest separates actual subprocess facts from factory cleanup/finalization failures. Failed finalization still blocks success-only save/publication (CONC-3, CONC-8, STYLE-10).

### Data Flow

Bound contribution → authorized retained snapshot → exact snapshot/check write or lookup → existing receipt/evidence processing. Foreground cleanup selects only its run/attempt; global maintenance visits all eligible receipts, attributes each failure, and continues. Harvest retains available execution facts and attaches the failing factory phase before ordinary reporting.

### Concurrency & Context

Reuse existing StoryProgress mutex and cross-process parent lock. Keep the single SQLite connection, context-first access and existing transaction boundaries; never hold a transaction over external I/O (CONC-5, CONC-10). Add no goroutines, retry loops or turn-owned clock. Scope discovery and processing consistently, including reloads under locks.

### Shared State and Side Effects

Preserve retained byte accounting, receipt/request identity, historical evidence and foreign keys. Keep immutable replay checks. Foreground `Finish` currently discovers all runs, effects and parents (`internal/turn/story_progress.go:107`); restricting only submission receipt recovery is insufficient. Drain the authorized foreground run while leaving unrelated maintenance for daemon polling; do not widen scope to every run of the same parent. Preserve existing ordered effect delivery and Plan publication.

## Structural Hints for the Three Repair Areas

Task IDs and typed task ownership are assigned by the Plan orchestrator; the following hints map directly to the three existing feature-spec areas.

### Snapshot-scoped check identity

**Agent:** go-implementation-engineer

- **Pattern / placement:** Additive embedded migration under `internal/store/migrations`; evidence access stays in `internal/store/submission_evidence.go`.
- **Key interfaces:** All retained-check readers and writers take exact snapshot identity. Update `internal/turn/submission_records.go`, `submissions.go` and `agent_result_context.go` together with the store API; inventory remaining callers before implementation.
- **Watch out for:** Never offer an ambiguous run/check fallback. Snapshot ownership, frozen revision and aggregate-versus-story provenance remain independently checked (SEC-21). Migration preserves existing bytes/counters rather than recapturing evidence.
- **Not in scope:** Agent naming rules, extra issue columns, historical receipt repair.

### Foreground recovery isolation

**Agent:** go-implementation-engineer

- **Pattern / placement:** Store-owned filtered selection; turn-owned reconciliation in `submission_recovery.go`, `submissions.go`, `submission_runner.go` and `story_progress.go`.
- **Key interfaces:** Explicit scoped foreground entry points versus global daemon entry points. Use session binding run/attempt for submission, Close and terminal Validate; retain run identity when draining legacy Finish paths, whose transport is run-scoped.
- **Integration points:** Audit discovery and lock-protected reloads across pending receipt, progress and effect paths. Existing sequencing, authorization and uncertain-effect recovery remain authoritative (CONC-10).
- **Watch out for:** Global recovery already accumulates faults and continues; preserve that behavior while adding originating run/attempt/receipt/phase attribution. Do not turn another run's diagnostic into the foreground result. Genuine local failures still propagate (STYLE-5, STYLE-9).
- **Not in scope:** Orphaned Plan settlement, new daemon scheduler or retries.

### Truthful harvest diagnostics

**Agent:** go-implementation-engineer

- **Pattern / placement:** Preserve runner result and wrapped failure phase through `submission_runner.go` into `harvest.go`; reuse current diagnostic retention and report rendering.
- **Key interfaces:** Available process outcome, exit code and log references remain facts; factory finalization failure is a separate cause. Do not replace a non-nil result with fabricated startup metadata.
- **Watch out for:** Failed/interrupted process outcomes must survive cleanup failure; successful process plus local finalization failure remains unsuccessful overall. Keep existing interruption recovery and sanitized outbound diagnostics (CONC-3, CONC-8, SEC-6, STYLE-10).
- **Not in scope:** Provider invocation changes or new error-reporting infrastructure.

## Verification Placement

Real disposable SQLite tests cover migration, evidence identity and recovery boundaries (TEST-3, TEST-6). Existing controlled-runner fixtures cover harvest and Plan success paths; synchronization uses channels, never sleeps (TEST-8). The orchestrator should assign separate test tasks to rostered test engineers according to real I/O versus pure logic. Run focused regressions and required `make check` during Implement. No tests ran in this architecture review; no paid nested agent or live host evidence is needed for these backend claims.

## Platform and Security Notes

Platform: existing internal packages, consumer-owned interfaces, wrapped errors, injected structured logging and stdlib tests apply. No new HTTP boundary, dependency or concurrency topology. Repository migration and single-connection conventions override generic platform suggestions.

Forward-looking review against APP_CONSTRAINTS security rules: input validation remains at current contribution boundaries; authorization must use trusted bindings and exact snapshots; no secrets/configuration changes; SQL stays parameterized in store; dependencies remain unchanged; error reporting retains sanitization. Main risks are evidence misattribution and failure leakage, addressed by the exact identity and scoped recovery boundaries above. This is plan review, not a security audit or evidence of implementation correctness.

## Non-goals and Rollout

Plan orphan recovery remains deferred. Existing successful Plan behavior is a regression requirement. Old binaries have ambiguous readers once duplicate labels exist across snapshots; operator rollout must account for this compatibility risk. Deployment and historical recovery are outside this plan.
