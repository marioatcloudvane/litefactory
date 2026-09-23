# Plan: a-malformed-owner-command-wedges-a-task
- Branch: `lf/314-a-malformed-owner-command-wedges-a-task`
- Session: 2026-09-22
- Parent: [#314](https://github.com/marioatcloudvane/litefactory/issues/314)
- Citation base: `bef0007a8fc77aefb93e57d93b79d9c2411d79a8`
- Status: Plan content complete; factory submission, publication, save and readiness pending.

## Shared Decisions
- Approved scope covers creation approval and preparation. Existing grammar, limits, identity and authority checks remain intact.
- Distinguish confirmed refusal from operational failure at its origin; unknown, wrapped infrastructure and uncertain-effect failures remain pending.
- Retain immutable outcomes by trusted source identity. Resolve retained outcomes before fresh parsing, mode/configuration or proposal eligibility checks.
- Reuse existing durable reporting and consumption boundaries. Reporting must be confirmed before consuming inputs requiring a diagnostic or proposal presentation.
- Keep persistence in store, one-turn creation orchestration in turn, intake/retry scheduling in daemon and safe output in report/redact. No new package or dependency is planned.

## Workflow
Deliver creation-rejection-queue-progress first, then creation-successful-intake-recovery. The second story extends the first story's durable source recovery instead of requiring a separate infrastructure-only story. Implement owns code and executed evidence; Validate later assesses the retained final snapshot. Plan completion and helper acknowledgment alone do not establish publication or readiness.

## Cross-Story Constraints
- Preserve numeric identity filtering and bot exclusion before recovery; no rejected input grants authority or starts work.
- SQL and transactions remain in store with context-first accessors, numbered embedded migration if needed and the existing single-connection pool. Never hold a transaction during remote I/O.
- Keep exact safe report bytes, stable identity and conservative possibly-sent reconciliation; a marker alone is not proof of delivery.
- Use ordinary offline Go tests with migrated temporary SQLite and httptest where durable I/O matters, fake unit dependencies and deterministic synchronization. No integration build tags or special integration test naming.
- Run repository-required `make check` in Implement against the final snapshot and retain criterion-mapped results. No tests were run by story synthesis.
- Each criterion's proof method, source availability, version bound and missing execution prerequisite is carried in its typed story notes. No live host or nested-agent observation is required or claimed.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| creation-rejection-queue-progress | Explain rejected creation commands and release the queue after confirmed delivery | [#368](https://github.com/marioatcloudvane/litefactory/issues/368) |
| creation-successful-intake-recovery | Recover successful creation intake without duplicating or renewing authority | [#369](https://github.com/marioatcloudvane/litefactory/issues/369) |

## Out of Scope
Other command families, global retry-policy changes, automatic skip thresholds, new alerts, operator recovery commands, proposal-generation changes, syntax/limit relaxation, live repair, restart, deployment and protected instruction-tree edits remain excluded.

## Provenance
The owner's acceptance of both recommendations and subsequent instruction to plan settle scope. feature-spec.md is the requirement source; ARCH_REVIEW.md supplies structural guidance. Saved research used revision `1be56786ec00c214d60733f6b16ac87a83a0bd5d`; current architectural inspection used the citation base above. Neither historical tests nor this source review prove the future implementation or deployed daemon behavior. Re-pin inspection/evidence to the implementation snapshot before acceptance.

The repository references docs/story-contract-instruction-manifest.md and docs/decisions/nested-agent-evidence.md, but neither file is present. Their contents were not inferred; explicit factory contracts and the per-criterion evidence inventory govern this contribution. There are no unresolved material decisions. The factory supplies real story issue links after successful publication.
