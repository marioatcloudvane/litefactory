# Architect: Go Architect
# Created: 2026-09-12

## Verdict

The feature is practical at three deliverable stories: preview/evaluation, guarded retirement/recovery, daily scheduling/operator documentation. Keep one evaluator, one daemon mutation entry point, one small persistence addition, and existing structured logs. No queue framework, archival service, dashboard, or separate completion model.

One explicit normative boundary needs resolution in the Plan: SEC-7's exact Clone allowlist cannot perform local or remote branch deletion without a worktree. Do not silently add methods or evade the restriction using a package-level function. See Deviation below.

## System Overview

### How This Feature Fits

`internal/cli` presents preview; `internal/daemon` owns scheduling, coordination and actual retirement. Put shared retirement orchestration/evaluation in focused daemon files rather than a new package: CLI already imports daemon. `internal/store` owns inventory, short database operations and minimal durable scheduling/attempt records. `internal/github` owns closure observation and uncached refresh. `internal/git` owns exact repository inspection and guarded mutations; `internal/turn` only needs existing recreation integration verified.

### New Packages / Components

No new package is necessary. Add a small retirement evaluator/service within daemon, a store retirement entity/accessor file with one embedded migration, and narrow Git retirement operations subject to the boundary decision below. The evaluator returns resource decisions and evidence; it does not read the clock, sleep, schedule work or delete resources.

### Key Boundaries

- CLI invokes the shared evaluator with preview mode and prints sorted results to its output writer. It must not instantiate/start another polling daemon.
- The daemon supplies time, registered ownership and narrow consumer interfaces for Git/forge probes; test doubles belong at these consumption sites (TEST-5).
- GitHub requests use the existing client and `WithoutConditionalCache`; closure reason is diagnostic context, not authority to discard commits.
- Storage accesses contain SQL exclusively in store and never hold a transaction across GitHub, Git, logging or filesystem work. Preserve its one-connection pool.
- Structured decision values cross evaluator-to-CLI/logger boundaries. Expected skips and attempted failures remain outcomes, not invisible errors (CONC-3).

### Data Flow

Preview: enumerate registered canonical owners → read live issue → fetch/probe exact refs and owned worktree → evaluate → print/log would-remove or reason. No scheduler or successful-retirement writes.

Automatic pass: due marker and cursor → bounded owner admission → existing task/parent ownership → clone gate → reread run/pending-effect state → Git/issue evidence → final uncached closure check and expected-head checks → durable intent → narrow mutation → confirm actual result → store result/clear only confirmed worktree location → log → advance cursor.

Recovery: read unfinished resource intent → reacquire ownership → inspect exact resource → record absence or retry with fresh eligibility. Never convert an old intent into authority to delete a recreated checkout or newly advanced branch.

### Concurrency & Context

Keep cleanup on the existing daemon lifecycle; process a small bounded batch per Tick and preserve progress between ticks. Do not add another long-lived goroutine or consume all registered tasks synchronously on startup. Give each batch a bounded context; respect rate-limit backoff, normal polling, shutdown and existing capacity. Tick remains the domain-clock owner. Persist a stable owner cursor and pass boundary so one repeatedly failing owner does not starve later owners. Complete a pass before setting its next elapsed-24-hour due time.

Use existing session and managed-parent ownership before the per-clone gate, matching the run path; never reverse that order. Recheck prelaunch/running work and checkout-dependent recovery/effects after acquiring ownership. Do not keep the clone gate during an agent run. No new lock service is warranted. Preview may observe a stale snapshot and must label it advisory; its fetch uses existing Git locking/error behavior and cannot authorize a later deletion.

### Shared State and Side Effects

Retain task/session identity, branch identity, run evidence, acceptance and delivery debt. Store only a sweep progress row and resource intent/outcome records needed for safe replay; logs remain the user-facing history. Keep the original owned path in the intent until stale registration has been reconciled even if the session path has cleared. No generic event sourcing.

Delete worktree before its local branch when both qualify; remote branch retirement is separately eligible before retention. Local and remote heads require separate exact-ancestry proof. Remote deletion must compare the expected remote head at the server, and local deletion must compare the observed local head. Do not substitute PR merge flags, ahead/behind counts or patch equivalence. Normal unforced worktree removal cannot substitute for the explicit ignored-content check.

Reopening uses `git.EnsureWorktree`: it already prefers a surviving local branch, then the remote branch, then the remote base. Avoid creating a second recreation routine. Add lifecycle regression evidence for fully and partly retired states.

## Per-task structural hints

The task labels below are architectural groupings for the orchestrator's typed tasks, not additional requirements or story artifacts.

### A-1: Shared evaluation and preview
**Agent:** go-implementation-engineer
- **Pattern:** Existing CLI constructor plus daemon consumer-oriented service; deterministic resource decisions.
- **Package placement:** `internal/cli/cmd_task.go`, focused `internal/daemon/retirement*.go`, `internal/github` issue observation and `internal/git` inspection.
- **Key interfaces:** Expected resource identity, observation timestamps, explicit issue-state outcome, observed local/remote/base SHAs, retention evidence and action/reason.
- **Integration points:** Store registered sessions/repositories and completed runs; naming ownership; existing structured logger.
- **Watch out for:** Shared parent ownership, session UpdatedAt is lock-mutated, malformed or missing issue state, ignored files, remote-only resources and strict ownership.
- **Not in scope:** Writes to retirement state, manual apply, archival tags, new API credentials.

### A-2: Guarded execution and minimal recovery persistence
**Agent:** go-implementation-engineer
- **Pattern:** Existing durable-intent-before-effect conventions (CONC-4/10), short store transactions, exact-head guarded Git operations.
- **Package placement:** `internal/store/retirement.go` and next numbered embedded migration; daemon executor; narrowly authorized Git surface.
- **Key interfaces:** Sweep position; per-resource intended action, original identity and expected SHAs; confirmed/unknown/failed outcome.
- **Concurrency/context:** Canonical task/parent lock then clone gate; cancellation at each external boundary, no transaction over I/O.
- **Integration points:** Reuse existing pending effects/state to conservatively defer checkout-dependent work. Preserve normal reopening setup in `internal/turn` and `git.EnsureWorktree`.
- **Watch out for:** Intent-write failure prohibits deletion; outcome-write failure leaves durable uncertainty. An absent owned checkout differs from an unrelated path. Never global-prune arbitrary registrations or recursive-delete guessed paths. Never clear run/acceptance/delivery data.
- **Not in scope:** Generic recovery engine, task-clean behavioral changes, workflow acceptance decisions.

### A-3: Daily bounded sweep and operator contract
**Agent:** go-implementation-engineer
- **Pattern:** Existing Tick admission/backoff and injected structured logger (CONC-2/6, STYLE-2).
- **Package placement:** `internal/daemon/tick.go` and focused retirement scheduling file; configuration template/help/operator documentation.
- **Key interfaces:** Due/pass cursor state supplied by store, evaluator/executor resource outcomes, existing daemon lifecycle.
- **Concurrency/context:** No extra scheduler goroutine; bounded continuation across ticks, stable ordering, no full rescan on restart.
- **Integration points:** Startup orphan reconciliation, request backoff, Quiesce, zero-candidate and interrupted summaries; existing `logs --all --grep retirement`.
- **Watch out for:** Storage failure must not announce completed sweep. Deferred owner advances fairly but remains eligible next pass. GitHub state and final recheck both appear at normal log level; no file contents or credentials.
- **Not in scope:** New settings, cron expressions, history dashboard, recurring issue comments.

### A-4: Decision and orchestration tests
**Agent:** go-unit-test-engineer
- **Pattern:** Table-driven decision cases with narrow hand-written fakes (TEST-1/2/5).
- **Package placement:** Alongside daemon evaluator and CLI rendering.
- **Integration points:** All closure states, retention boundary, parent identity, preview no mutation, independent branch proofs, log levels/outcomes.
- **Watch out for:** Unit fakes do not prove Git deletion atomicity or SQLite transaction guarantees. Channel rendezvous only for concurrency; no sleeps/polling (TEST-8).
- **Not in scope:** A new test framework or acceptance claims before execution.

### A-5: Real dependency and lifecycle evidence
**Agent:** go-integration-test-engineer
- **Pattern:** Ordinary Go tests using existing temporary real Git/bare origin, migrated SQLite and httptest forge fixtures (TEST-3/6).
- **Package placement:** Co-located git/store/daemon test files; no integration tags or `_Integration` naming.
- **Integration points:** Local-only/ignored content preservation, independent ref races, absent upstream, stale registration, crash windows, restart/fairness, active task exclusion and reopen recreation.
- **Watch out for:** Prove actual remote expected-head rejection and local compare-and-delete against real Git. Keep registered clone worktree/index/HEAD unchanged. Capture Git binary version and repeatable scratch probe for new external behavior (EXT-1/2). Run `make check` in Implement, never build into dist.
- **Not in scope:** Production deletion or paid/nested-agent observations. Synthetic forge tests prove handling, not hosted service availability.

## Architecture Notes

- Platform: layout — existing internal packages suffice; no external dependency required.
- Platform: errors — typed skip/result categories; wrap operational causes and preserve rate-limit metadata.
- Platform: concurrency — bounded Tick work, existing locks, contexts; no added unbounded worker.
- Platform: interfaces — narrow consumer-owned seams only where needed by evaluator/test doubles.
- Platform: database — existing embedded migrations, ctx-first accessors and one SQLite connection.
- Platform: HTTP — reuse existing outbound client; no new listener.
- Platform: logging — injected slog and existing daemon log viewer; no metrics/tracing platform.
- Platform: tests/lint — repository standards override generic platform advice; no assertion-library addition, integration tags or unpinned linter.

## Security Findings

**Scope:** Planned deletion and observation boundaries only; not an audit of existing code.
**Rulebook:** `.aiteam/APP_CONSTRAINTS.md` SEC/CFG/CONC plus package contracts.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern | Exact registered ownership, canonical paths and refs must be revalidated before destructive operations; prefix matching is insufficient. |
| Authentication/authorization | Concern | Existing forge identity is adequate; Git branch mutation needs the explicit SEC-7 resolution below. |
| Secrets/configuration | Compliant by design | Reuse process credentials and validated retention config; no secret-bearing new settings. |
| Data handling | Concern | Ignored files and local-only commits must stop retirement; logs carry metadata, never contents. |
| Dependencies | Compliant by design | No new package dependency is required; no vulnerability certification claimed. |
| Error handling | Compliant by design | Fail closed on verification uncertainty and distinguish intent from confirmed effects. |

## Deviation: SEC-7 exact Git capability surface

**Conflict:** SEC-7 fixes Clone's method set to `Fetch WorktreeAdd WorktreeList WorktreeRemove RevParse RemoteURL ConfigGet ShowRef` plus Path. `internal/git/surface_test.go` explicitly requires amendment before a new Clone operation. Worktree mutators cannot serve registered remote-only resources or local branches after worktree removal. Adding an unrelated public capability/function merely to evade this guard would violate its purpose.

**Impact:** Branch retirement in A-2 needs a narrow intentional policy amendment. Evaluation and worktree retirement can remain within existing capabilities plus narrow read-only observations, but complete branch retirement cannot honestly be frozen as compliant with the current exact allowlist.

**Recommended option:** Explicitly authorize a small task-branch maintenance capability in internal/git, guarded by exact resource identity and expected SHA, with no ability to checkout/commit/reset/pull the registered clone. Amend SEC-7/S-6, package comments and surface tests deliberately in that same implementation scope; preserve the registered checkout/index/HEAD guarantee and base/default protections. This is the minimum needed for the requested feature, not permission for general clone mutation.

**Alternative:** Retain local and remote branches automatically and implement only worktree retirement, leaving branch deletion to the operator; smaller but materially reduces the approved feature.

Until that narrow exception is approved or confirmed already authorized by the orchestrator's owner-context review, the current rule stands. Do not silently loosen surface tests.
