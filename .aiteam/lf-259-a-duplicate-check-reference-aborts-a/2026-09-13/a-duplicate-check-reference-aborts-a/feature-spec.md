# Feature Spec: a-duplicate-check-reference-aborts-a
- Status: READY FOR PLAN; no implementation or acceptance claimed
- Date: 2026-09-17
- Source: RESEARCH_BRIEF.md, especially run 0013's owner-approved scope reduction

## Goal
- Let multiple stories reuse check names safely, prevent another run's recovery error from failing healthy work, and report the actual failure phase.

## User Needs
| Area | Need |
|---|---|
| Evidence | As an operator, I want each snapshot to own its checks so two stories can both use `build`. |
| Isolation | As an operator, I want foreground processing limited to its trusted run/attempt so unrelated broken receipts do not discard successful work. |
| Diagnostics | As an operator, I want execution facts retained alongside cleanup/finalization failures so I can identify the real cause. |

## Decisions and Assumptions
- Settled: exactly three repairs above; orphaned Plan receipt recovery deferred by owner in run 0013.
- Settled: reuse factory-bound run/attempt identity, existing communication contract, receipt correlation and publication owners.
- Assumption: existing Go tests with real disposable SQLite and controlled runner results adequately prove these backend changes; live nested coding-agent observation is not part of this repair.
- Assumption: migration rollout and any historical reruns remain operator activities, outside implementation completion.
- Material unresolved decisions: none.

## Data and Interface Scope
| Surface | Required behavior |
|---|---|
| Retained check identity | `(run_id, snapshot_ref, reference)`; preserve existing rows, provenance, foreign-key relationships and retained-byte accounting through an additive migration. |
| Check writers and readers | Require exact authorized snapshot, including reference-only validation and shared-result resolution; no ambiguous run/check fallback. |
| Foreground recovery | Filter by trusted run and attempt in store-owned selection; cover submission, session Close, terminal Validate intake and foreground Finish. |
| Global maintenance | Continue across receipt failures, with diagnostics attributed to originating run/attempt/receipt and phase, plus story where present. |
| External protocol | No new agent identity fields or commands; existing authority, revision, replay and receipt guards remain effective. |
| Harvest | Preserve process result facts when available; distinguish startup, execution and subsequent factory failures. |

## Acceptance Criteria
- AC-001: Fresh and populated databases support snapshot-local check references after migration; existing evidence, provenance, ownership, foreign keys and retained-byte accounting are preserved, and repeating migration is harmless.
- AC-002: Two stories in one run using `build` under distinct snapshots both preflight and submit successfully; every production reader resolves each check to its correct snapshot. Exact same-tuple replay is idempotent; changed content under the same tuple conflicts; wrong-snapshot and relabelled aggregate evidence remain rejected. Exact request replay preserves its receipt identity.
- AC-003: Broken pending work for run A cannot fail healthy run B through foreground submission, Close, terminal Validate intake, Finish or harvest; selection respects attempt boundaries. B keeps its own correlated result, while genuine own-run failures still surface.
- AC-004: Global maintenance reports failures against their originating receipts and continues to healthy run C; existing sequencing, replay and authorization guards remain effective.
- AC-005: Genuine startup failure, successful execution followed by local cleanup/finalization failure, failed execution with cleanup failure and interruption retain truthful causes and available process outcome, exit code and logs. Post-execution errors do not say the agent never started. Sanitization remains enforced; local factory failure does not grant workflow success or publication.
- AC-006: Existing successful Plan save/publication behavior remains intact; focused store/turn/shared-result regressions and the repository-required `make check` pass before engineering completion.

## Evidence Obligations
| Criteria | Proof method | Source availability | Bounds / missing prerequisite |
|---|---|---|---|
| AC-001 | Real disposable SQLite migration tests with populated old schema, fresh schema and repeated migration | Existing store fixtures and migration helpers; new regression assertions needed | Backend schema behavior only; tests must run on implementation revision. |
| AC-002 | Store and submission/shared-result regression tests, including duplicate labels, conflict, replay and evidence-ownership cases | Existing store/turn/shared-result fixtures and retained duplicate-check research probe | Probe currently demonstrates the bug; replacement passing repair assertions required. |
| AC-003, AC-004 | Real store/turn paths with deliberately broken A, healthy B/C, separate attempts and an own-run error | Existing submission, recovery and runner fixtures; targeted regressions needed | Controlled runner results prove backend isolation, not live host capability; no external capture prerequisite. |
| AC-005 | Harvest regressions with controlled result/error combinations and retained sanitized diagnostics | Existing turn diagnostics fixtures; targeted assertions needed | No live nested runner or current provider-startup capability claim. |
| AC-006 | Existing Plan success regression coverage, focused tests and `make check` output | Repository tests and Makefile | Actual execution remains required during Implement; no tests executed during this Plan. |

## Existing Features to Reuse
- `internal/store`: migrations, immutable evidence, scoped ownership and receipt persistence.
- `internal/turn`: submission sessions, StoryProgress, shared-result resolver, harvest, diagnostic retention and existing publication gates.
- Existing factory bindings, request replay, revision checks and receipt dimensions; no protocol redesign.

## Risks and Non-goals
- Rollback risk: older binaries use ambiguous lookups once duplicate names exist across snapshots; operator rollout must account for schema compatibility.
- Excluded: orphaned Plan receipt settlement, snapshot-handle redesign, mandatory agent naming prefixes, communication/MCP redesign, host adoption proofs, protected instruction-tree edits, deployment and historical receipt/data repair.
- Deferred Plan leak remains a known issue; absence of open Plans is the owner's report, not proof of a fix.
