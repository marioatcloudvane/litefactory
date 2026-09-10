# Plan Brief — a-report-that-posts-but-fails-to-record
# Branch: lf/210-a-report-that-posts-but-fails-to-record
# Date: 2026-09-10
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/210
# Citation base: ea675a8b42aca7b30f2390d0e3a6c558a5bc6d71
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions
- Implement the six directions explicitly approved in Research run 0003. Keep the repair within existing packages and one SQLite store; add no general messaging framework or dependencies.
- A pending prelaunch allocation is neither running nor reportable. Reuse its trigger-bound row/ordinal for transient preparation; terminal executions are never reset. Known refusal is final and needs a fresh command once its explanation is confirmed and recorded.
- Run state and delivery state are separate. Nonterminal allocated runs never render/post results; positive lifecycle-owner evidence and existing exclusive startup reconciliation determine recovery.
- Reports and replies share a stable delivery identity including execution task, actual destination, trigger namespace/id, purpose and optional run. Persist exact final redacted intent before dispatch and retain it with history, without pruning in this feature.
- Possible delivery never authorizes a blind repost. A trusted receipt or strict matching comment permits atomic delivery/run/source completion. A confirmed receipt remains proof after remote deletion. Preserve historical duplicates.
- The daemon owns time and safe retry scheduling; store owns durable transitions. No storage transaction spans HTTP or subprocess execution. Source ordering remains intact until confirmed delivery and local acknowledgment.
- Numeric retry/escalation values are implementation defaults, not additional owner decisions: existing poll interval with exponential delay capped at existing one-hour MaxBackoff; escalation on the third failed eligible recovery. No new settings or force-resend command.

## Workflow
- US-001 -> US-002 -> US-003. Establish truthful run eligibility first, use it in shared durable delivery, then schedule and expose recovery. Deliver the feature together: US-001 alone does not provide once-only refusal delivery.
- Use the factory-persisted workflow version. Under separated workflow, retain each story's immutable implementation snapshot and executed checks during Implement; Validate assesses those retained checks read-only. Engineering progress is not acceptance or publication.
- Only the factory publishes story identities, fills this index, saves work and establishes readiness. This submitted Plan leaves those effects pending until terminal processing succeeds.

## Cross-Story Constraints
- SQL and ctx-first database access stay in internal/store; use embedded migrations and the existing single connection. internal/turn owns one-turn orchestration; internal/daemon owns ownership, ticks and recovery scheduling; report stays independent of turn.
- Preserve managed approval/frozen-scope gates, numeric bot identity, typed redacted outbound bodies, bounded sanitized diagnostics, existing worker limits, cancellable final session-state write and exclusive startup recovery.
- Use existing fake runners/clocks, fake HTTP and actual migrated temporary SQLite. No new test framework, integration tags, sleeps or live nested agents. Implement runs scoped checks and final make check, retaining actual output; Plan runs no checks.
- No credentials, runtime-state edits, dist writes, daemon restart, protected instruction-tree changes, commits or pushes by agents.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Refuse safely and keep every run recoverable | [#237](https://github.com/marioatcloudvane/litefactory/issues/237) |
| US-002 | Deliver each report or refusal without blind reposting | [#238](https://github.com/marioatcloudvane/litefactory/issues/238) |
| US-003 | Retry safely and make persistent delivery failures visible | [#239](https://github.com/marioatcloudvane/litefactory/issues/239) |

## Out of Scope
- Reconstructing historical row 283 without deployed revision/restart evidence; no claim that incident is explained. Its investigation remains separate.
- Automatic duplicate cleanup, comment editing/deletion/recreation, forced resend/skip controls, deployment, and general rate-limit redesign.
- Universal exactly-once guarantees, new generic infrastructure, factory protocol changes or installed instruction changes.

## Provenance
- Current architecture citation: ea675a8b42aca7b30f2390d0e3a6c558a5bc6d71; see ARCH_REVIEW.md and feature-spec.md for planning rationale and assumptions.
- Research preserves incident attribution and unknown historical facts; controlled offline checks cannot establish live GitHub or nested-agent behavior.
- Before implementation, consume only factory-supplied frozen story revisions and verify cited ownership/selector seams against that checkout. This shared brief is context, not a replacement story source.
