# Plan: a-duplicate-check-reference-aborts-a

- Branch: lf/259-a-duplicate-check-reference-aborts-a
- Session date: 2026-09-13; Plan iteration: 2026-09-17, run 0014
- Parent: [#259](https://github.com/marioatcloudvane/litefactory/issues/259)
- Citation base: `5ebdbd93f020b94429b07648c1f9c1984ecd5913`

## Shared Decisions

Keep three bounded repairs in the existing store/turn architecture. Reuse factory-bound identity, the communication contract, receipt replay and existing publication owners. No new packages or protocol fields.

## Workflow

US-001 and US-002 are independently deliverable. US-003 follows both and includes verification of the combined repair. Implement owns code and executed checks; later Validate assesses retained evidence. Only factory publication/save establishes readiness.

## Cross-Story Constraints

SQL stays in store with the existing single SQLite connection and embedded migrations. Preserve evidence ownership, replay, sequencing, locks, sanitization and successful Plan publication. Use ordinary Go tests with disposable SQLite and controlled runners; no new test tags, live nested agents or daemon changes. Run `make check` on the combined implementation.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Allow different story snapshots to reuse check names | [#309](https://github.com/marioatcloudvane/litefactory/issues/309) |
| US-002 | Keep broken recovery work from failing unrelated runs | [#310](https://github.com/marioatcloudvane/litefactory/issues/310) |
| US-003 | Report the real execution and finalization failure | [#311](https://github.com/marioatcloudvane/litefactory/issues/311) |

## Out of Scope

Orphaned Plan receipt recovery remains deferred by the owner. No historical receipt/data repair, snapshot-handle redesign, mandatory naming prefixes, MCP/protocol redesign, host-adoption proof, protected instruction-tree edits or deployment.

## Assumptions and Risks

No material owner questions remain. Existing backend tests are sufficient for the requested repair; live provider capability is not claimed. Deployment is operator-owned. Older binaries use ambiguous readers after duplicate labels exist across snapshots, so rollout/rollback compatibility needs operator care.

## Provenance

The latest run-0013 scope supersedes earlier four-part recommendations. Requirements and architecture use the retained research and source baseline above. Re-pin source references if implementation starts on changed code. No implementation or tests were executed in this Plan iteration.
