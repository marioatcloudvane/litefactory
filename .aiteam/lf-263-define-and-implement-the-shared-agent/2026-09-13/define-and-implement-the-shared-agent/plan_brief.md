# Plan: define-and-implement-the-shared-agent

- Feature: define-and-implement-the-shared-agent
- Branch: lf/263-define-and-implement-the-shared-agent
- Session date: 2026-09-13
- Parent: https://github.com/marioatcloudvane/litefactory/issues/263
- Citation base: `04f2898c83796d07c3041a063bc2f28f5971c5f3`
- State: run 0011 traceability correction passed complete factory preflight and is durably retained under request `60fa836aeb44ea13bdb2eb7305285fc9`; receipt input is valid and revised Plan publication is pending. Factory publication and successful save determine readiness.

## Shared Decisions

- Deliver one reusable application contract in `internal/agentresult`, composing existing neutral `agent` and `planbrief` semantics. SQL and transaction ownership remain in `internal/store`; a directly testable `internal/turn` service supplies trusted context and evidence. Current runners and submission routes are not switched.
- Application schema version starts at integer `1`. Logical result revision, persisted submission contract version and workflow version remain separate. Unknown application versions reject; nothing implicitly upgrades existing contracts or workflow.
- Snapshots contain factory-owned `meta` and authored `result`; create contributions contain only `result`. Updates contain `expected_revision` and ordered operations. Factory context binds identities, mode, authority and scope; neither a snapshot nor an agent-supplied reference grants authority.
- Common outcomes are `completed`, `incomplete`, `blocked`. Comment is nonblank; reason is required only for incomplete/blocked. Optional unused fields are omitted. Exactly one payload matches trusted Research, Plan, Implement, Validate or Spike mode. Questions obey trusted permissions. Common recommendation is `{text, action?}`, initially allowing only `action.moveto.planmode`, advisory only.
- Explicit operations are `set`, `remove`, `add_item`, `update_item`, `remove_item`, `reorder`. Typed field/stable-ID selectors identify targets; numeric indexes do not. Omitted object members remain unchanged, supplied arrays explicitly replace editable collections, null rejects, and removal/retirement is explicit. Whole merged results must satisfy domain rules before an atomic revision is retained.
- Draft Plan may accumulate story details; completion retains existing full typed requirements and graph validation. Frozen scope and evidence facts are immutable. Implement engineering and Validate assessment are distinct from authoritative acceptance. A completed assessment may contain unmet/unverified findings.
- Authorized same-mode/work-context continuations share one result identity and preserve full prior snapshots. Each run obtains fresh writer authority. A changed mode or frozen scope starts a new logical result; mutable draft Plan detail alone does not change frozen scope identity.
- Request correlation, receipt observations and independent decision dimensions remain explicit. Exact retries do not apply twice; corrected content uses fresh correlation. Preflight grants neither proof nor a revision. Input acceptance never implies engineering, acceptance, publication or terminal save success.
- All documentation/examples must match executable contract fixtures. Evidence inventories distinguish offline behavior from authentic agent delivery; no nested paid agent runs or measured billing claims are required or established here.

## Workflow

Six independently testable capabilities are planned. Contract validation precedes patching and progress. Durable history composes validated creation and patching; aggregate evidence builds on persistence. Finalization guidance and the final aggregate check follow all shared behavior.

Dependency graph: `US-001 -> US-002 -> US-003 -> US-004`; `US-001 -> US-005`; `US-001, US-002, US-003, US-004, US-005 -> US-006`.

The selected mode remains Plan. Factory publication replaces the Story Index links and retains authoritative child identities. Only explicit owner selection starts Implement; engineering and later Validate keep their existing independent gates.

## Cross-Story Constraints

- Reuse existing ownership, deterministic manifest hashing, retained evidence semantics and contribution diagnostics. No duplicated workflow engine, new module, server or background clock is introduced.
- Every storage accessor takes context first; migrations use the next available embedded SQL number. Keep the SQLite pool at one connection and never call a connection-taking accessor from inside a transaction. No transaction spans filesystem or forge I/O.
- Strict bounded decode rejects duplicate keys, null, unknown fields and unauthorized identities before semantic validation. Diagnostics identify locations/corrections without exposing capabilities. Rooted artifact syntax is separate from secure content resolution.
- Frozen requirements, original run reports, snapshots/checks, prior assessments and request bytes remain immutable. Mutable latest receipt projections cannot erase append-only observations.
- Shared skill source and isolated rendering suffice for this delivery. Existing installed protected trees, current role dependencies and immutable catalog releases are outside scope. Future adoption must honor independent harness ownership.
- Use meaningful pure tests and real temporary SQLite/local-file tests without integration tags or invented integration suffixes. Retain actual focused check output and run `make check` on the final implementation. Never build into `dist/`, modify daemon state, commit or push.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Create and validate a shared agent result | [#266](https://github.com/marioatcloudvane/litefactory/issues/266) |
| US-002 | Revise selected result content safely | [#267](https://github.com/marioatcloudvane/litefactory/issues/267) |
| US-003 | Retain result history and recover submissions | [#268](https://github.com/marioatcloudvane/litefactory/issues/268) |
| US-004 | Bind completion to retained aggregate evidence | [#269](https://github.com/marioatcloudvane/litefactory/issues/269) |
| US-005 | Report bounded advisory progress | [#270](https://github.com/marioatcloudvane/litefactory/issues/270) |
| US-006 | Finalize results with reusable guidance and measured examples | [#271](https://github.com/marioatcloudvane/litefactory/issues/271) |

## Out of Scope

MCP server/tools or external follow-up edits; live routing cutover; production adoption; legacy removal; automatic version upgrades; auxiliary classification/scheduling/conflict adapters; generic event framework; workflow engine or fixed HITL stages; automatic transitions or acceptance; installed-tree updates, catalog release or team installation; authentic dual-harness certification, paid nested-agent observation and historical evidence recovery from unrelated issues.

## Provenance

The [approved Research handoff](https://github.com/marioatcloudvane/litefactory/blob/04f2898c83796d07c3041a063bc2f28f5971c5f3/.aiteam/lf-263-define-and-implement-the-shared-agent/2026-09-13/define-and-implement-the-shared-agent/RESEARCH_BRIEF.md#current-handoff--approved-in-run-0009) and run 0009 approvals are primary scope evidence; historical incomplete notes and run 0006 deferrals are superseded. `feature-spec.md` records requirement assumptions and `ARCH_REVIEW.md` records architectural decisions against the citation base above. Research JSON files remain illustrative inputs, not executable proof; implementation will produce normative schema and verified fixtures.

Engineering must recheck changed package interfaces, next migration number and retained evidence helpers against its current base. This Plan records no executed engineering tests, live agent behavior, billing savings, remote publication or readiness. No material owner questions remain; numeric limits and source-only delivery are explicit engineering decisions within approved scope.

Run 0011 adds explicit committed Research references and agreed-schema lineage to Story 1. This is a provenance correction: story keys, criterion IDs, acceptance obligations, dependencies and other story content are retained. The historical draft marker and auxiliary examples remain illustrative; Plan records integer schema version 1 and a work-only envelope as engineering refinements.
