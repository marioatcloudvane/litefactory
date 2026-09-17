# MCP tool families — shared plan context

- Feature: `mcp-tool-families`
- Branch: `lf/273-mcp-tool-families`
- Session date: 2026-09-14
- Parent: [#273](https://github.com/marioatcloudvane/litefactory/issues/273)
- Citation base: research inspected `dda6f72c704cc6e84cb2c09649853de2d85d9b44`; run 0002 confirmed unchanged internal/docs sources at `14674782dce0a9e15b3ba27e759776d7b4390cd4`. Requirements and architecture rechecked current interfaces in this Plan run; re-pin implementation citations to the actual implementation base.
- State: completed typed proposal prepared; factory publication and saved readiness are separate pending outcomes.

## Shared Decisions

The five tool families use the landed MCP groundwork and existing business owners. `internal/submission` owns transport, catalog and client correlation; `internal/turn` owns bound orchestration; `internal/store` owns SQL and durable transitions; `internal/github` owns forge I/O. Existing validators, checkpoint operations, progress reduction and creation authorization remain authoritative. New responsibility names describe in-process components, not separately deployed services.

The added families serve Research, Implement and Validate according to the explicit permission matrix. Existing Plan/Spike communication behavior remains compatible. Discovery and direct-call enforcement must agree. Factory-bound immutable permissions include repository, source, explicit comment targets, approved historical receipt scope and catalog/gate visibility; mutable operation/grant decisions are checked by their existing owner at call time.

New requests receive stable, family- and scope-specific correlation before transmission. Existing communication/helper identities remain compatible. Generalized receipts retain bounded dispositions and references to authoritative workflow/publication records. Input acceptance, engineering, assessment and remote effects remain separate. A receipt lookup neither recursively records itself nor redispatches effects; read/preflight receipts do not retain a full issue catalog.

Creation reuses #272's server-held grant and private single-use permit, as agreed with the owner. Agent arguments, public content and comment volume cannot supply approval. Exact final redacted publication bytes, correlation markers and relationships precede approval hashing. Ordinary comments use explicitly authorized existing targets independently of creation policy. Durable ownership precedes outward publication; uncertain outcomes remain uncertain and permit bounded observation only.

Preflight checks proposed input before submission; activity describes the agent’s current reported progress; receipts recover what the factory knows happened to a specific request. Passing preflight grants no approval, reported activity supplies no proof, and receipt lookup does not retry publication.

Canonical content hashes bind reviewed creation bytes to approval. The existing creation primitive retains operation identity through its grant-scoped reservation, while stable journal request identity provides transport recovery. Receipt correlation references the authoritative operation; it does not replace that identity with a content-hash-only dedup key.

## Workflow

Build the shared correlation/receipt foundation first. Family handlers then extend the same guarded dispatcher in independently testable steps. Evidence operations and feedback must share checkpoint validation rather than duplicate it. Publication families share durable outcome and recovery infrastructure while preserving separate ordinary-comment and creation-class authorization. The final feature gate checks the complete catalog, cross-family boundaries, cancellation and recovery, and records the production handoff.

Story-specific tasks, acceptance criteria and architecture live exclusively in the factory-rendered story issues. The Story Index below contains navigation only; the typed proposal is publication input, not an independently editable story source. Engineering completion does not grant acceptance or automatically select another mode.

## Cross-Story Constraints

- Keep all SQL in `internal/store`, use an embedded migration for schema changes, retain modernc SQLite and its one-connection correctness constraint, and end transactions before remote I/O.
- Use context-first operations and existing cancellation. `internal/daemon` owns time and scheduling; `internal/turn` receives time/lifecycle inputs and performs bounded work without sleeping retry loops or new permanent workers.
- Strictly validate fields and scope. Discovery never grants write targets; a request ID alone never grants receipt access. Refused calls have no domain effect and expose no secrets, private permits or unrelated payloads.
- Preserve the existing separated and legacy workflow rules, frozen scope, immutable snapshots and historical acceptance. Advisory activity cannot become evidence, approve scope or extend authority.
- Reuse current dependency versions, package boundaries, forge access and redaction. No protected instruction-tree changes or new dependency stack are planned.
- Tests derive from each story's criteria. Use handwritten fakes for unit checks and ordinary Go tests with real temporary SQLite, broker/socket/pipe and HTTP fixtures for local I/O guarantees. Count actual fixture POSTs and use channel rendezvous for concurrency; do not introduce integration tags or special integration naming.
- Run `make check` at the feature completion gate. Do not run `make build`, write `dist/`, launch nested providers, or mutate live daemon state.
- Every story carries criterion-specific evidence method/source/bounds/prerequisite notes. Controlled fixtures establish local implementation behavior only; no claim of authentic host discovery or delivery follows from them.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Recover scoped family request outcomes | [#281](https://github.com/marioatcloudvane/litefactory/issues/281) |
| US-002 | Validate proposed input through bound tool profiles | [#282](https://github.com/marioatcloudvane/litefactory/issues/282) |
| US-003 | Discover bounded repository-local issue summaries | [#283](https://github.com/marioatcloudvane/litefactory/issues/283) |
| US-004 | Publish authorized comments with durable recovery | [#284](https://github.com/marioatcloudvane/litefactory/issues/284) |
| US-005 | Publish exactly reviewed creation batches using existing grants | [#285](https://github.com/marioatcloudvane/litefactory/issues/285) |
| US-006 | Record engineering and assessment through shared checkpoints | [#286](https://github.com/marioatcloudvane/litefactory/issues/286) |
| US-007 | Report bounded advisory activity | [#287](https://github.com/marioatcloudvane/litefactory/issues/287) |
| US-008 | Verify every family contract and document the host handoff | [#288](https://github.com/marioatcloudvane/litefactory/issues/288) |

## Out of Scope

Native Claude/Codex registration and permission configuration, actual provider launches, startup/teardown/resume/fallback, production construction of scopes, trusted owner approval intake and next-attempt grant binding, periodic recovery/lifecycle scheduling, and authentic cross-host verification belong to #274. Residual groundwork documentation mentioning #264 must be reconciled in the handoff without claiming those production connections already work. #274 is not a dependency of this feature.

No new authorization engine, caller-visible approval token, mode auto-chaining, general issue-body browsing, arbitrary repository access, durable activity history, or live GitHub publication is included.

## Assumptions and Risks

Bounded read/preflight receipt dispositions satisfy cross-family recovery without retaining full read payloads. The active factory supplies explicit existing comment targets and separately authorized historical receipt visibility; #273 supplies testable binding APIs while #274 supplies production construction. Activity receipts may survive a restart while the process-local current display is unavailable. Prepared batches are testable with trusted fixture approval; actual owner-intake integration remains unverified until #274.

Remote APIs cannot establish exactly-once publication from every uncertain response. The plan guarantees durable single dispatch ownership and no blind reposting, retaining uncertainty or partial identities until bounded observation can establish facts. Changed prepared bytes require fresh approval. These are deliberate observable outcomes, not successful publication claims.

## Provenance

The session research brief records the original issue and owner replies; the feature specification separates settled scope, assumptions, candidate criteria and evidence sources. The architecture review confirms current ownership seams and highlights immutable scope, exact approved bytes, cross-process dispatch and authoritative-record references. The issue's original caller-carried token wording is superseded by the owner's reuse direction and the landed private-permit contract.

Run 0006 incorporates the verified run-0005 assessment into eight existing stories with 24 criteria. Existing criterion text and identities remain unchanged; additional regression coverage uses a new criterion identity with assigned tasks and evidence inventory. No implementation, test execution or authentic host observation occurred in this Plan run. Only a terminal successful factory publication and save establish readiness; a locally completed proposal or durable input acknowledgment does not.
