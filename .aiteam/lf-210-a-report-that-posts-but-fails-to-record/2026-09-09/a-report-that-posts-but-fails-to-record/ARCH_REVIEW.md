# Architecture Review — report delivery and refused runs

Date: 2026-09-10. Mode: Plan. Verdict: APPROVED WITH EXPLICIT IMPLEMENTATION DEFAULTS.
Source revision: ea675a8b42aca7b30f2390d0e3a6c558a5bc6d71.
Input: feature-spec.md and six owner-approved Research directions. No implementation or tests performed.

## System overview

Keep this feature inside existing packages. `daemon` admits and schedules work; `turn` prepares/executes and coordinates report delivery; `report` renders and redacts; `store` owns durable transitions and transactions; `github` owns authenticated create/list transport. Add no messaging framework, service, new dependency, timer goroutine, configuration knob or resend command.

Current preparation allocates before managed approval (`internal/turn/prepare.go`), replay treats any unreported allocation as a report (`execute.go:replayed`), and `report.go` posts before `CompleteRunReport` discovers an unfinished row. `harvest.go` also has post-allocation returns before terminal persistence, including story registration and resume-token persistence. Fix the complete allocation-to-owner-exit boundary, not just the reported plan gate.

Normal flow: existing admission/ownership -> deterministic preparation gates -> retained prelaunch allocation if identity-dependent preparation needs it -> final launch boundary -> durable terminal outcome -> retained redacted delivery intent -> possibly-sent boundary -> GitHub POST/verified recovery -> confirmed receipt -> atomic completion/source acknowledgment. Only daemon schedules another attempt.

Significant new components are a narrow prelaunch lifecycle distinction in the existing run store and a delivery record shared by Reporter.Report and Reporter.ReportReply. Delivery state must not be confused with run status. A comment can be confirmed while local run/source completion still owes a transaction.

## Shared decisions and boundaries

1. **Keep one allocation per trigger.** Existing `(session_id, trigger_comment_id)` uniqueness and ordinal reservation stay intact. Introduce an explicit durable prelaunch state for identity-dependent preparation. A transient prelaunch failure leaves the same allocation eligible to resume preparation, with its diagnostic retained; it is neither `running` nor a reportable finished invocation. Retry reuses that row/ordinal, does not reset a terminal run, and does not create another run for the same trigger. Deterministic refusal finalizes that allocation as never launched when one exists; pre-allocation refusal uses a runless reply. Once crossing the launch boundary, no automatic preparation retry is allowed: ambiguous crash there is abandoned through existing exclusive startup recovery. This is the smallest design compatible with audit identity and trigger uniqueness. Check and adjust existing queries that assume all allocations are execution attempts, especially predecessor selection, startup schedules and continuations.
2. **Preserve lifecycle ownership.** Keep `SetSessionState(...Running)` last in successful preparation with its original cancellable context (CONC-7). No compensating session-state writes inside failed preparation. The allocation's launch state must be persisted before this final write and external launch; a failed final session write is known prelaunch failure and remains owned by the executor for repair. Active daemon inflight membership/task ownership defers silently. Never infer death from age, PID, missing logs, or an unlocked task alone. When terminal persistence fails after the owner observes completion, return and retain that exact stopped-outcome repair debt under existing daemon task ownership, retry persistence on later ticks, block reporting and new execution, and release worker capacity. If the database cannot write anything, an in-process retained result is the available positive evidence; do not promise impossible durable writes during total storage failure. Restart then uses exclusive `ReconcileOrphans` conservatively, preserving abandonment semantics. A new independent orphan scanner or session-state compensator is unnecessary.
3. **A stable delivery record is the retry authority.** Its identity includes canonical execution task, actual destination, trigger namespace/id, purpose and optional run identity. Persist exact final redacted bytes and a versioned marker before dispatch. State distinguishes not dispatched, possibly sent, confirmed and locally completed. Fail closed if the possibly-sent boundary cannot be saved. Confirmed receipts survive remote deletion. Completion updates the proper parent/child/synthetic source accounting and run report state in the same store transaction as delivery completion. SQL transactions never span HTTP or subprocess calls; keep the single SQLite connection.
4. **Recovery is conservative and bounded in external effects.** Normal new delivery does not list comments. Uncertain recovery verifies numeric bot author, actual destination, complete identity and exact retained bytes; adopt the lowest numeric ID among equivalent matches. Conflicting or unverifiable candidates block. Partial/error/empty list is not proof that an uncertain POST failed. Proven local failure before HTTP dispatch may return to dispatch eligibility; other transport failures remain uncertain unless positive evidence proves receipt. Preserve existing duplicate comments. Legacy allocated-run matching requires unique persisted task/run/trigger provenance and reconstructible exact content; legacy runless collisions or unavailable content block visibly. No editing, deleting or recreating confirmed comments.
5. **Retry policy stays in daemon.** Persist delivery attempt count, next eligible time, bounded sanitized reason and escalation state in store; daemon supplies the time. First recovery after poll interval; double per failed eligible recovery, cap at `daemon.MaxBackoff` (one hour), honor later global rate-limit hold. First failure logs once, third failed eligible attempt escalates once, then only changed-state/resolution logs; include task/delivery/run and due time. No posting or source consumption merely because a budget expired. At cap continue safe lookup/local-completion attempts; unrelated tasks continue. Failed storage writes retain in-process scheduling debt until saving succeeds, then resume from durable state after restart where available.
6. **Evidence is offline and scoped.** Use existing fake runner/clock, httptest GitHub and real migrated temporary SQLite; retain named checks and immutable implementation snapshots during Implement. Validate reads them without executing new checks. No authentic nested-provider or deployed-incident proof is claimed. Historical row 283 investigation remains separate and does not block this repair.

The prelaunch state is an implementation default resolving the spec's phrase “never-launched/failed outcome”: a retryable preparation is an accountable pending request, not a finished invocation. It is explicitly not a stranded running row. If implementation cannot maintain these boundaries, return the specific conflict instead of silently consuming a transient error.

## Minimal proposed story split

Three sequential stories avoid overlapping edits and keep each user-visible behavior reviewable. Final story IDs/criterion IDs belong to the typed proposal. No local per-story files.

### Story A — Refuse safely and preserve run lifecycle

Scope: preparation classification and early gates, accountable prelaunch allocation, launch/terminal ownership repair, terminal reportability guard. Candidate spec AC-002/AC-003 and AC-001's refusal behavior. Depends on no other story. Delivery idempotence of its explanation is completed by Story B, not falsely claimed as independently delivered by A.

Suggested tasks (all production tasks owner `go-implementation-engineer`):
- A1 data, M: extend existing run lifecycle persistence and migrations for prelaunch eligibility while preserving trigger uniqueness and legacy running interpretation. Hint: `internal/store`; audit predecessor, schedule/continuation and startup selectors. No general attempt subsystem.
- A2 logic, L, depends A1: deterministic gate/refusal classification and reentrant preparation with same allocation. Hint: `internal/turn/prepare.go`, managed scope, `execute.go`; known domain refusals are result values, infrastructure errors retry. Keep actual approval atomic and retain current mode/tools. Failure diagnostics must distinguish no launch.
- A3 logic, L, depends A2: terminal owner-result repair and reportability guard. Hint: `harvest.go`, daemon worker/inflight handoff, Reporter entry; catch registration/launch/finalization paths, gate before render, retain repair debt without worker-slot starvation or timer loops. Startup remains exclusive.
- A4 test, M, owner `go-unit-test-engineer`, depends A2/A3: classification/reportability tests and fault-path coverage using existing fixtures.
- A5 test, L, owner `go-integration-test-engineer`, depends A3: temporary SQLite restart and transactional eligibility checks; fake runner proves zero launches on refusal and exactly one after transient preparation repair. No integration tags or specially suffixed integration test names.

Criterion/evidence guidance: declare separate criteria for known readiness/refinement refusal, retryable prelaunch recovery with same run identity, all allocated exit paths and failed finalization repair, live-worker deferral, and unchanged exclusive startup abandonment. Count POSTs, runner calls, run identities/statuses, watermarks, and unrelated-task progress. Do not require no allocation for a race caught by the final approval gate; require truthful no-launch handling.

### Story B — Persist and recover report/reply delivery

Scope: durable logical delivery and exact redacted content for allocated reports, runless reports and replies including redirected child/synthetic sources. Depends A. Candidate spec AC-004–AC-007 and complete AC-001 once-only explanation requirement.

Suggested tasks:
- B1 data, L, owner `go-implementation-engineer`: delivery migration/accessors with identity uniqueness, state transitions, receipt and atomic source/run completion. Hint: store only; typed data crossing into turn, no SQL across HTTP, preserve existing independent source consumption semantics.
- B2 logic, L, owner `go-implementation-engineer`, depends B1: connect Reporter.Report/ReportReply to a shared delivery coordinator. Hint: renderer remains pure, exact redacted output retained; resume persisted intent before mutable re-render; terminal guard from A remains before allocated report rendering.
- B3 logic, L, owner `go-implementation-engineer`, depends B2: reconcile trusted current/legacy markers through existing numeric identity and complete list transport. Hint: consumer-side interface only if needed for existing fake; no added edit/delete endpoint. Empty uncertain search blocks and confirmed receipt skips search.
- B4 test, M, owner `go-unit-test-engineer`, depends B2/B3: identity separation, marker/content/provenance matching, deterministic equivalent-candidate selection and legacy conflicts.
- B5 test, L, owner `go-integration-test-engineer`, depends B3: SQLite + fake HTTP fault windows and reconstructed-instance replay. Assert saved intent failure has zero POSTs, post-success/record-failure has one, unknown response cannot repost, receipt completion failure only retries local state, redirected source is acknowledged correctly, deleted confirmed comment is not recreated.

Criterion/evidence guidance: split durable identity/content, effect-boundary fault recovery, strong/legacy matching, unknown lookup safety, and one durable readiness explanation. Include two distinct runless triggers/purposes, synthetic identity and actual child destination. Equivalent matches must have identical retained body; wrong author/destination never qualifies. Test startup does not guess whether an old unreported report was already sent. List-B acceptance evidence later names retained checks; no fabricated passing evidence during Plan.

### Story C — Schedule safe recovery and expose persistent failures

Scope: persisted retry eligibility and structured first/escalation/change diagnostics, existing backlog/ordering integration, documentation and aggregate engineering gate. Depends A and B. Candidate spec AC-008.

Suggested tasks:
- C1 logic, L, owner `go-implementation-engineer`: daemon scheduling and persisted delivery recovery metadata using existing clock/backoff ceiling and backlog paths. Hint: per-delivery delay must not become global rate hold; a global hold still delays delivery. Startup restores due state. No retries/sleeps below daemon.
- C2 logic, M, owner `go-implementation-engineer`, depends C1: bounded redacted diagnostics and concise operator documentation of pending/uncertain state, automatic safe repair, retained history and no force resend. Hint: injected slog, retain unresolved state even after third failure; no metrics framework or CLI.
- C3 test, L, owner `go-unit-test-engineer`, depends C1/C2: fake-time progression, exact third-failure escalation, changed-state logging, no repeated action on ineligible ticks, unrelated-task progress. Rendezvous on channels and Quiesce; no sleeps.
- C4 test, M, owner `go-integration-test-engineer`, depends C1/C2: persisted retry/receipt restart tests and aggregate `make check` evidence for completed change. Include local store outage recovery and unresolved queue order. Record actual executed output; engineering completion is separate from acceptance.

## Security findings

Scope: planned run lifecycle and report/reply delivery; rulebook `.aiteam/APP_CONSTRAINTS.md` Security section.

| Area | Finding | Required implication |
| --- | --- | --- |
| Input validation | Concern: marker text alone is forge-controlled evidence | Verify complete identity/content/destination and numeric authorship; legacy ambiguity blocks. |
| Authentication/authorization | Compliant proposed boundary | Reuse factory GitHub client and identity, preserve managed readiness/frozen scope; SEC-8/20. |
| Secrets/configuration | Compliant proposed boundary | No new credentials/config; no agent forge access; SEC-4/6, CFG-4. |
| Data handling | Concern: retained delivery body and diagnostics | Retain only final redacted outbound bytes and bounded sanitized reasons; continue typed redact.Body on mutations; SEC-6. |
| Dependencies | Compliant proposed scope | No dependency changes; no vulnerability status asserted without scanning. |
| Error handling | Concern: post/record uncertainty is observable | Typed dispositions, durable safe deferral, no raw outbound database/network errors; CONC-3/4/10. |

## Platform and constraint checklist

- Platform: project layout — existing internal packages only; cmd remains wiring.
- Platform: errors — typed domain classification and wrapped storage/transport errors; no string matching, new panic/recover or swallowed cleanup errors.
- Platform: concurrency — existing bounded workers, task ownership and daemon clock; retain repair debt but release capacity. CONC-1/2/5/6/7.
- Platform: interfaces — consumer-owned narrow fakeable seams, TEST-5; retain concrete store for transaction evidence.
- Platform: dependencies — no new modules, TEST-2.
- Platform: HTTP/API server — not applicable (client-only); existing create/list transport sufficient.
- Platform: database — new embedded migration as needed, SQL only in store, parameterized ctx-first accessors, one connection, no transaction across side effects.
- Platform: testing — stdlib and existing fixtures, actual temporary SQLite for atomicity (TEST-3/6), fake clock/channels not sleeps (TEST-8), meaningful race/check evidence.
- Platform: observability — existing injected slog, bounded/redacted fields, no infrastructure.
- Platform: lint/format — engineering must run `make check`; intentional surface/clock/gate changes require design review, TEST-10. No tests run for this Plan artifact.
- CONC-4 is preserved: intent/uncertainty are not acknowledgment; only confirmed delivery can precede source consumption.
- CONC-13/14 recovery and scheduled/continuation reservation semantics must be regression-tested when prelaunch rows become distinguishable; no new auto-resume route for actually launched work.
- EXT-4 evidence inventory: each final criterion maps to a retained offline check on the implemented snapshot; no live nested agent prerequisite. Unavailable historical incident evidence remains unavailable.

## Deviations

None from normative standards. Explicit semantic clarification: distinguish retryable prelaunch allocation from terminal never-launched refusal; preserve session-state-last and one-trigger/one-run uniqueness. Unknown HTTP delivery may remain blocked indefinitely rather than risking duplicate posting, as approved. Retry constants and retention-without-pruning are Plan defaults, not owner-selected numerical policies.
