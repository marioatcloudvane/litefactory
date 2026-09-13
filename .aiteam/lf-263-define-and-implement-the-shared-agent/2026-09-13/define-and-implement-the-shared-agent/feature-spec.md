# Feature Spec: define-and-implement-the-shared-agent

- Status: READY FOR PLAN — owner-approved direction; implementation approval remains factory-owned.
- Date: 2026-09-13
- Source: current handoff and run 0009 decisions in `RESEARCH_BRIEF.md`; issue #263; supplied run 0010 instruction to proceed in Plan.

## Goal

- Give litefactory and future adapters one reusable, versioned Go application contract for creating, validating and revising agent results, preserving evidence and factory authority while keeping ordinary contributions small.

## User needs

| Consumer | Required capability |
|---|---|
| Agent | Author meaningful results and targeted changes without rebuilding factory metadata or untouched content. |
| Factory | Validate the entire result against trusted mode, attempt, scope and retained evidence before recording a revision. |
| Owner | Review an evolving Plan with stable story identities and recover prior results, reports and receipts. |
| Adapter author | Reuse decoding, validation, update, snapshot and diagnostic semantics without duplicating workflow rules. |

## Existing features to reuse

| Capability | Existing owner / reference |
|---|---|
| Shared runner and contribution types, diagnostics, engineering and assessment validation | `internal/agent`; Research inventory of contribution and capture contracts. |
| Complete typed Plan content, stable IDs and dependency validation | `internal/planbrief`; preserve substantive completion requirements. |
| Attempt binding, exact-byte request correlation, preflight/submit/receipt | `internal/submission`; reuse application-facing machinery without switching live callers. |
| Frozen stories, immutable snapshots/checks, receipt and effect persistence | `internal/store`; existing decision dimensions remain separate. |
| Factory lifecycle decisions, current-worktree evidence comparison | `internal/turn`; never relocate authority into agent-authored JSON. |
| Source skills and isolated rendering | `teams/source/skills`, `internal/teamsource`, `internal/render`; use source guidance and temporary/in-memory verification. |

## Data model

| Entity | Fields / relationships | Requirements |
|---|---|---|
| Assembled snapshot | Factory `meta`; authored `result` | Agent contribution rejects `meta`; metadata is trusted context, not a bearer capability. |
| Metadata | Schema version, trusted mode, logical result ID, revision, originating run/attempt and applicable frozen scope | Factory allocates identities; managed workflow/submission versions are conditional and independent. |
| Result | `outcome`, `comment`, conditional `reason`, optional `questions`, `recommendation`, `artifacts`, typed `payload` | Outcomes completed/incomplete/blocked; nonblank comment; reason required only for incomplete/blocked; exactly one payload matching trusted mode. |
| Question | Stable ID, text, optional stable-ID options and recommended option reference | Permission-bound; nonempty questions require incomplete/blocked; answers and approval are not agent-writable. |
| Recommendation | Nonblank `text`, optional allowlisted `action` | Advisory data only; initial supported action `action.moveto.planmode`; no execution or automatic transition. |
| Artifact | Stable ID, kind, rooted relative path, optional description | Path reference alone is not immutable evidence; validate reference syntax and apply factory path checks when resolving content. |
| Research payload | Request and optional findings/evidence, decisions, assumptions | Actual claims retain appropriate evidence; omit unused narrative collections. |
| Plan payload | Optional draft context/stories; complete summary/stories on completed | Rough story requires stable key, title and `user_story.want`; complete stories retain existing full typed requirements. |
| Implement payload | Authorized story outcomes/evidence; optional aggregate checks | Reference frozen story identity; never restate or mutate requirements; preserve legacy versus separated workflow semantics. |
| Validate payload | Authorized story assessments, snapshot and complete criterion/review findings | Missing proof stays unverified; completed assessment is not acceptance; no evidence execution/capture. |
| Spike payload | Request, work done, findings, decisions, not done as applicable | Preserve partial and blocked investigation; no invented implementation obligation. |
| Update | Expected revision and ordered explicit operations targeting stable IDs/allowed fields | Atomic private-copy application followed by complete merged validation; arrays are ordered; index-based identity is forbidden. |
| Snapshot history | Logical result + successive full revisions + source attribution | Prior full snapshots, run reports, retained checks and assessments remain immutable. |
| Receipt observation | Request correlation and separate received/input, engineering, acceptance, publication/effect dimensions | Retain original submission and observations; pending is not success; existing receipt rows may project latest state without erasing history. |
| Advisory progress | Factory-bound run/attempt, sequence/deduplication identity, bounded message | Separate from authoritative existing story-progress events; cannot complete work or change result revisions/readiness/acceptance. |

## Interface scope

| Surface | Required behavior | Authorization |
|---|---|---|
| Go decode/create/validate | Strict bounded JSON parsing, semantic diagnostics, assembled snapshot creation | Trusted factory context supplied separately from authored bytes. |
| Go sparse update | Apply explicit operations, validate complete merge, atomically retain new revision | Expected revision, current attempt, authorized result/scope and allowed field edits. |
| Go scoped baseline read | Return selected current result fields/items, revision and relevant evidence references | Factory-selected authorized subset; no scope expansion through selectors. |
| Go evidence/reference helpers | Reuse deterministic capture/hash/serialization and existing retained proof validation | Capturing bytes/check claims does not manufacture executed proof. |
| Go history/receipt integration | Preserve correlation and immutable history; adapt/reuse v2 semantics | No live routing cutover, new server or public CLI required. |
| Reusable finalization skill | Early discovery/finalization pointer plus detailed per-mode guidance loaded near every submission exit | Constraints, assigned requirements and evidence obligations available before work; lifecycle decisions remain factory-owned. |

## Business rules and engineering assumptions

- Approved decisions: explicit stable-ID operations; one logical result across authorized same-mode continuations; auxiliary advisors separate; optional unused fields; common recommendation object; shared Go library and all five modes remain in scope.
- Assumption A1 — application schema starts at integer version 1; unknown versions reject explicitly. Result revision increments independently; submission contract and workflow version retain existing independent meanings. No implicit migration or upgrade.
- Assumption A2 — normal work envelope needs no auxiliary `operation` discriminator; advisory progress has a separate typed value. No classification/scheduling/conflict variants enter this v1 contract.
- Assumption A3 — keep the initially agreed recommendation action only; text-only recommendations cover other next steps without inventing a workflow vocabulary.
- Assumption A4 — omit means unspecified at create and unchanged at update; JSON null rejects; explicit remove deletes an optional editable field/item; `[]` clears an editable collection only when valid. Nested object changes preserve omitted members; provided arrays explicitly replace that editable collection. Stable-ID item operations remain preferred for small changes.
- Assumption A5 — update vocabulary must cover setting/updating allowed fields, adding/updating/removing stable-ID items and explicit item reordering. Duplicate/retired identity, missing targets, duplicate order members or incomplete reorder reject. Final schema/docs fix exact operation spellings consistently.
- Assumption A6 — continuing result identity is tied to unchanged trusted mode and authorized work context. A changed frozen scope or mode starts a new logical result; ordinary same-context continuation grants fresh attempt authority and revokes the previous writer. Reading historical results does not grant write permission.
- Assumption A7 — store durable full snapshots and receipt observations through the existing persistence owner; library APIs and offline exercised integration are delivered now, while production callers stay on current routes.
- Assumption A8 — progress limits are explicit named limits selected during architecture, tested at boundaries; timestamping/retention policy is factory-owned. Sequence gaps are advisory and never completion evidence.
- Draft Plan allows absent future sections, but every supplied reference and dependency must already be valid; no cycles or dangling criteria/tasks. Completed Plan preserves full content, role and graph obligations; draft snapshots never publish provisional stories or grant readiness.
- Allowed-edit matrix covers every mode: common narrative/optional fields within authorized scope; draft Plan editable requirements before freeze; Implement changes authored status/reference summaries but cannot patch frozen requirements or retained proof; Validate changes only a new result revision while retaining prior assessment snapshots; Research/Spike edits preserve prior history.
- Removal does not retire or rewrite frozen obligations; published/frozen requirement changes retain existing factory-mediated replan and retired-ID rules.
- Separated Implement forbids acceptance assessments; legacy verified contributions retain required engineering plus complete assessment. Common completion means selected obligations satisfied, not accepted/closed/published.
- Aggregate evidence binds the final implementation digest/snapshot, exact selected frozen story revisions and attributable executed aggregate checks. A story-scoped check alone cannot be relabelled aggregate proof. Missing, stale, failed or wrong-set proof prevents completion.
- Aggregate-only recovery records aggregate evidence without new story engineering events; immutable prior story evidence remains attributable to its original snapshot. Factory policy verifies the selected obligations before common completion.
- Preflight performs the same discoverable validation but allocates no result revision or accepted proof; submit revalidates. Exact retries reuse correlation and return the original result/receipt; corrected content uses new request correlation. Lost responses do not justify double application.
- Stale revisions return machine-readable expected/current revision and useful target diagnostics without overwriting newer content; caller rereads scoped baseline and deliberately reapplies intent.
- Complete merged-result validation includes unchanged siblings, identities, graph dependencies, mode/outcome completeness, trusted scope and retained proof. A locally plausible patch cannot bypass an invalid final result.
- Questions follow current trusted policy; ordinary Research/Plan may ask, other modes cannot gain permission from a common field. Incomplete/blocked must remain serializable without fabricated success/evidence.
- Scope/identity/capabilities, approved/accepted/published flags, owner answers and execution telemetry are not authored fields; reject unauthorized and unknown fields instead of ignoring them.

## Candidate acceptance criteria

| ID | Observable requirement |
|---|---|
| C-01 | Versioned schema/spec and matching Go decoder cover all five modes with valid completed, incomplete/questioning where permitted, and blocked examples; disallowed question modes reject. |
| C-02 | Go validation rejects malformed/unknown fields, wrong mode/types/null/version, unauthorized metadata, invalid IDs/references/graphs and outcome-dependent missing content with indexed actionable diagnostics. |
| C-03 | A rough Plan is enriched through criterion and architecture updates with stable identities and untouched siblings preserved; incomplete drafts cannot pass completed Plan checks. |
| C-04 | Explicit operations support field/item removal and ordering with documented omission/empty-array semantics; every invalid operation leaves stored state unchanged. |
| C-05 | All five modes have executable revision-checked update round trips; stale updates and patches invalid only after complete merge reject without incrementing revision. |
| C-06 | Successive authorized same-mode runs retain one result with full immutable revision/source history; revoked attempts and changed mode/scope cannot mutate it. |
| C-07 | Exact retries/lost replies recover one application and correlation; corrected content gets a new request; preflight persists no proof/revision; receipt observations retain distinct pending/accepted/published dimensions. |
| C-08 | Aggregate checks prove final bytes, exact selected frozen story revisions and executed check attribution; stale/missing/failed/wrong-scope proof rejects; aggregate-only recovery adds no story engineering events. |
| C-09 | Retained evidence, frozen requirements, historical assessments and run reports cannot be patched; separated Implement cannot grant acceptance; completed Validate with unmet/unverified findings does not grant acceptance. |
| C-10 | Progress ordering/deduplication, bounded retention/coalescing and attribution are exercised; progress cannot alter result revision, engineering, readiness, acceptance or publication. |
| C-11 | Shared finalization source skill and mode guidance match executable schema/fixtures for completed, incomplete/questioning and blocked exits; early guidance retains discovery plus necessary work constraints/baseline/evidence obligations. |
| C-12 | Reproducible comparison includes full baseline/instructions/results versus scoped reads/small updates, cold and continuation, stale retry, incomplete and blocked scenarios, counting total input/output material and declaring tokenizer/model/cache assumptions. No unmeasured billed-token or runtime savings claim. |
| C-13 | New reusable APIs and persistence integration pass focused tests plus repository `make check`; existing live routes and legacy contracts remain compatible. |

## Candidate evidence obligations

| Criteria | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| C-01–C-05 | Offline table-driven Go fixtures exercising decoder, validator and patch engine; schema/example parity assertions | Seven Research JSON documents available as design input; executable valid/invalid fixtures to implement | New application v1; Research placeholder hashes are not valid proof | Implement real deterministic fixture bytes/digests; no live agent claim. |
| C-06–C-09 | Real temporary SQLite persistence tests and direct application-boundary tests over synthetic authorized attempts/scopes/check records | Existing store/submission/agent primitives available; new integration fixtures to implement | Existing v2 submission semantics and both trusted workflow branches; isolated local state | Tests must demonstrate history, rollback/reopen and dedup, not just in-memory mutation; no daemon cutover. |
| C-10 | Deterministic bounded progress tests with supplied time/context | New synthetic progress fixtures to implement | Transport-independent v1; no provider delivery claim | Named limit/coalescing rules required in architecture before implementation. |
| C-11 | Source instruction review, executable example parity and isolated renderer/discovery tests | Shared source and existing in-memory render mechanism available | Source behavior only; no proof of actual late loading by running Claude/Codex | No authentic nested-run criterion in scope; production adoption deferred. |
| C-12 | Reproducible offline corpus comparison with raw totals and an explicit counting method | Current full instructions/examples and new scoped corpus available | Exact pinned corpus/counting version; token estimates are not billing observations | Actual billed-token savings remain unclaimed without separately supplied authentic measurements. |
| C-13 | Retained passing focused tests and `make check` output over final implementation snapshot | Normal Go/SQLite/local test facilities available | Repository toolchain; no build tags or external agent execution | Implementation must execute checks; this Plan records no passing engineering evidence. |

## Risks and scope boundaries

- New durable history and aggregate evidence mapping can accidentally duplicate workflow logic; reuse current owners and preserve separate receipt dimensions.
- Optional draft fields can weaken completion checks unless the whole merged result is validated; criteria explicitly test this boundary.
- Source-level skill discoverability does not prove live model behavior or token savings; retain bounded claims.
- Deliver reusable shared source guidance now. No installed `.claude/**` or `.codex/**` edits are required for this no-cutover delivery; if later selected, split independent stories by harness ownership, with shared source first and cross-tree parity only at aggregate gate.

## Out of scope

- MCP server/tools, MCP integration or external follow-up story edits; retain `MCP_FOLLOWUP.md` as handoff only.
- Live routing cutover, automatic contract upgrade, production adoption, legacy deletion, catalog release or team installation.
- Classification, scheduling or conflict-advice adapters; generic event normalization/framework.
- Workflow engine, fixed multi-round HITL stages, automatic transitions/acceptance/publication or new owner-approval semantics.
- Live nested agent capture, authentic dual-harness capability certification and historical evidence recovery from unrelated issues.

## Readiness

- Material owner questions: none; run 0009 settled the three choices.
- Engineering choices above are explicit bounded assumptions for architecture/story authoring, not a new request for owner confirmation.
- No code, schema runtime, tests or publication completed by this specification.
