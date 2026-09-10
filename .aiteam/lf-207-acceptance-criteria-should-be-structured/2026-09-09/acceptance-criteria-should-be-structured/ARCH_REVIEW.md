# Architect: Go Architect
# Created: 2026-09-10
# Verdict: READY FOR STORY AUTHORING; authentic dual-harness delivery evidence remains an unsatisfied acceptance prerequisite

## Reviewed baseline and scope

Base commit: `0d0bb1b5821b4322a0836c7fdb859b2294fd831b`. Reviewed APP_CONTEXT, the complete APP_CONSTRAINTS, feature-spec, workflow, platform-constraints, task-decomposer, security-scanner and docs/decisions/nested-agent-evidence.md. This is design review, not implementation verification. No production changes, forge reads, live agent invocations or test execution occurred. The owner-approved research structure and all five sections are in scope. No major deviation is required.

## System overview

Typed authoring extends existing owners: `planbrief` owns pure story content/schema validation and structural reconciliation; `agent` owns neutral stage contribution, evidence and feedback contracts; `report` owns outbound Markdown; `turn` binds trusted run identity and coordinates approval/publication/evidence; `store` owns durable versions, immutable evidence, receipts and effect intent; `daemon` owns admission, recovery and time. Both existing runner adapters consume one neutral submission capability. `cli` supplies the callable client and explicit legacy upgrade interface. No package may open SQL outside store, bypass github for forge calls, or duplicate naming/config path logic.

Introduce a small stdlib-only `internal/submission` transport component for a local request/response connection. It owns framing, bounded input/output, cancellation and the client/server protocol, without importing turn/store or obtaining forge credentials. Caller-defined handler interfaces keep orchestration in turn. Agent schema remains provider-neutral. The broker is wired by turn/daemon rather than adapters implementing competing validation.

Data flow: agent calls supplied helper with JSON on stdin -> local broker receives contribution under bound attempt -> shared pure schema and semantic validation -> turn resolves authorized selector and constructs identity envelope -> store atomically records submission, counters and decision -> existing progress/validation/effect orchestration -> broker returns durable receipt. A receipt-query call reads existing state. Preflight follows the same validator without allocating event counters or mutating accepted state. Plan publication still happens under the managed completion/save gate; a live Plan receipt acknowledges durable validated contribution, never falsely claims publication/readiness before terminal success.

Editing flow: authenticated owner comment or fetched inline issue revision -> existing parent admission/refinement -> reconcile against exact observed issue revision -> typed proposed change -> validate/render -> revision check and factory publication -> issue-derived cached revision -> next approval freezes typed requirements and issue bytes. During promoted/active scope, preserve live edits and report the existing lock/refinement route; they do not alter frozen requirements.

## Cross-story decisions

1. **Versioning and source.** Persist content/submission contract version independently of workflow version. New managed scopes use the new contract after rollout; existing ones remain legacy until explicit CLI upgrade. Select the new version at feature scope, prohibiting mixed content/event contracts within one active generation, while historical revisions remain readable. Workflow legacy/separated dispatch never changes merely because content upgrades. GitHub title/body is the sole editable source; structured projection is a revision-keyed cache and cannot win over changed issue bytes. Current Plan transport remains version 1 full-body; its replacement is a deliverable, not available for this run.
2. **Typed content and budgets.** Implement exactly the approved story/user_story/acceptance_criteria/tasks/architecture/dependencies shapes, including empty optional arrays, explicit IDs and typed references. Canonical criterion IDs are AC-001 through AC-999999 without excess leading zeros; 1–256 active criteria, never recycle removed identities. Preserve 4,096-byte criterion text and 8,192-byte explanation limits as per-item ceilings. Keep 65,536-byte rendered issue body and 8 MiB Plan limits initially. Add explicit aggregate validation: these are ceilings, not a promise every field maximum fits simultaneously. A 256-criterion story with short texts and reference-based proof must fit all consumers. New submission/assessment metadata remains bounded to 4 MiB; retained snapshots use separately bounded immutable uploads/references and existing 64 MiB per-run accounting, avoiding multiplication of snapshots per criterion. Validate escaped JSON and rendered Markdown sizes, reserving structural/footer overhead, before publish. One documented budget table is shared by all validators and fixtures. If redaction changes required content or IDs, fail publication and report correction rather than silently changing the approved contract.
3. **Factory-bound submission.** Choose a Unix-domain local broker with a supplied absolute helper executable and attempt-specific connection descriptor. Helper input is stdin; no untrusted text in argv, no new permissive environment or forge token. The descriptor is rooted, non-symlinked, owner-only, short enough for the platform socket path bound, and contains an opaque attempt capability. Selectors authorize only supplied scope. Agents cannot supply parent/run/generation/issue/revision/workflow/sequence/attempt fields; presence is a diagnostic, not an override. Factory allocation is transactional and durable. Helper generates a request identifier and retains it before sending; retry same identifier/content returns same receipt, changed content under same identifier rejects. Retry a corrected contribution under a new identifier. A receipt never constitutes merge or owner-exception authority. Actual sandbox usability remains explicitly unverified until external observation; no fallback/permission expansion is authorized by this design.
4. **Validation and receipts.** Share schema traversal between preflight and submission. Reject unknown/duplicate/missing/null keys and wrong containers; gather independent defects with indexed paths, expected/actual types and stable ordering, and report incomplete traversal/unchecked paths when deeper validation cannot run. Receipt dimensions separate input validity, durable engineering record, acceptance assessment/decision, issue effects and Plan publication/readiness. Return local decisions synchronously where possible; outstanding remote work is honestly pending and durably queryable. No waiting for GitHub under SQL transactions; parent cross-process ownership preserves ordered effects. Only daemon schedules retries/backoff. Avoid polling/sleeps in turn/helper; bounded request waits use cancellation/deadlines and notifications. Broker lifetime is bounded by its run and joined during runner cleanup; document bounded connection workers. Cancellation/end-of-run drains preserve receipt state, never infer acceptance from clean subprocess exit.
5. **Evidence and lifecycle.** New criterion evidence uses list B only: array of {check_ids, summary}. Real executed checks and complete immutable snapshot provenance remain required and referenced; summaries and digests alone are not proof. Legacy text fields remain legacy. Separated Implement records engineering and releases dependents with acceptance open; Validate sends structured retained-evidence findings without fresh checks; legacy scoped verification retains immediate closure/invalidation. Preserve honest failed/missing records and owner waivers. Plan discussion/completion also uses factory-bound submission; resulting transport still obeys readiness and success-only save. Freeze exact typed criteria together with issue revision so evidence never re-derives names from prose.
6. **Editing, upgrade and instruction ownership.** Recommend comments via the exact approved bottom footer in a dedicated non-obligation section. Inline edits remain supported and authoritative. Deterministic parsing handles recognizable edited lists/tables/sections; ambiguous semantic updates use an ordinary-language refinement proposal under existing Plan machinery, never invented hidden truth. Stable IDs survive reorder/clarification; semantic replacement retires the old ID and allocates a new one; uncertainty stays pending. Recheck revisions immediately before writes and confirm resulting bytes; preserve unrelated changes and report races. Use existing forge capability, and do not claim an atomic GitHub compare-and-swap unless its implementation actually provides it. Upgrade preview lists all unfinished selected-feature revisions/conflicts; explicit apply refuses active/frozen unsafe transitions and records recoverable publication intent, preserves legacy history and requests next approval. Instruction work separates shared/root sources, Codex-owned .claude, and Claude-owned .codex; independent story readiness does not require sibling-tree parity.

## Decomposition and per-task hints

All implementation tasks go to go-implementation-engineer, with explicit test work subsequently assigned to go-unit-test-engineer by the orchestrator as applicable. All tests below are offline stdlib tests, never integration tags or live paid agents. Estimates are task-sized XS/S/M/L with no task greater than one day. PPO may renumber local task IDs but must preserve scope, dependencies and evidence limitations.

### US-001 — Versioned typed story contract and deterministic rendering
Dependencies: none. Covers full approved shape, ID/graph validation, compatible budgets and deterministic rendering, including footer. Packages: planbrief and report; no I/O in pure validators/renderers.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-001.1 Define new content types and published budget contract | data | M | — | Keep all five sections typed; version separate from workflow; enumerate JSON key strictness and graph identity rules. |
| T-001.2 Implement aggregate validator and deterministic renderer | logic | L | T-001.1 | Narrative cannot declare ACs; delimit multiline/table data safely; render bottom guidance outside obligations; validate total escaped/rendered bytes. |
| T-001.3 Add schema/render/limits regression suite | test | M | T-001.2 | Include accidental Evidence contract paragraph, nulls/duplicates, cycles, canonical IDs and 256 short criteria with enough evidence capacity; test invalid aggregate maxima rejection. |

### US-002 — Strict stage contributions, evidence list B and diagnostic preflight
Dependencies: US-001. Packages: agent plus planbrief consumer seam, avoiding cycles.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-002.1 Define neutral contribution and diagnostic contracts | data | M | — | Include Plan discussion/completion, story engineering/legacy outcomes and Validate findings; omit trusted envelope fields. Stage-specific unions must reject unsupported combinations. |
| T-002.2 Implement shared complete-as-possible validation | logic | L | T-002.1 | Multi-error paths and expected/actual types; list B only in new version; exact frozen AC and review coverage; no mutation from preflight. |
| T-002.3 Test type defects and proof-reference semantics | test | M | T-002.2 | Multiple errors in one request, malformed syntax incompleteness, no false validity, original parent-number/evidence-array incidents as synthetic mechanisms; missing checks remain unverified. |

### US-003 — Durable contract bindings, evidence references and submission receipts
Dependencies: US-002. Packages: store only for SQL and migrations; agent values adapted at turn boundary.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-003.1 Persist contract versions, projection/ID history and receipt identity | data | L | — | New embedded migration; legacy defaults; one SQLite connection; immutable generation history; no workflow recoding. |
| T-003.2 Add durable sequence/idempotency/evidence-reference operations | logic | L | T-003.1 | Atomically bind submission hash, counters and receipt; immutable snapshots retained once with size accounting; content-changing replay refused. |
| T-003.3 Add real SQLite migration/concurrency/replay tests | test | M | T-003.2 | Counter uniqueness, same-content retry, stale binding, rollback and independent pending effects; never hold transaction across I/O. |

### US-004 — Agent-callable local preflight, submit and receipt interface
Dependencies: US-003. Packages: submission, cli, config path methods and turn wiring; both adapters consume neutral capability.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-004.1 Implement bounded local broker and helper commands | api | L | — | stdin JSON, owner-only rooted descriptor/socket, fresh opaque capability, method allowlist, request-id persistence, bounded framing. No forge/env leakage. |
| T-004.2 Bind lifecycle and receipt notifications | logic | L | T-004.1 | Limit worker count; context cancellation and join; preflight pure; store supplies authoritative correlation; no retries/sleeps below daemon. |
| T-004.3 Test containment, authorization and lifecycle | test | M | T-004.2 | Symlink/replacement, foreign capability, cross-story/stale selectors, oversized streams, disconnect/timeout and no mutation on invalid request. Offline clients only. |

### US-005 — Connect submissions to engineering, Validate and recovery
Dependencies: US-004. Packages: turn, daemon, agent, harness, codex.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-005.1 Construct factory envelope and dispatch stage-specific contributions | logic | L | — | Reuse approved bundles, StoryProgress and validation orchestration; no agent-supplied counters or identities; retain both workflows. |
| T-005.2 Return live durable decisions and recover effect debt | logic | L | T-005.1 | local recorded vs remote pending states; daemon backoff; corrections within same run; final drain/cold/resume/fallback carry current binding. |
| T-005.3 Test same-run rejection/correction and failure recovery | test | L | T-005.2 | Held fake runner observes numeric-parent and wrong evidence format diagnostics then corrected recorded receipt; concurrent stories, lost acknowledgment, crash, failed forge effects, Validate no fresh checks. |

### US-006 — Publish typed Plans and freeze issue-derived typed requirements
Dependencies: US-004, US-001. Packages: turn plan attempt/publication/approval, store, report.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-006.1 Consume bound Plan contribution and render publishable stories | logic | L | — | Factory supplies existing identities/base; preflight validates complete proposal; preserve success-only publish and link-only index readiness; discussion no proposal. |
| T-006.2 Freeze structured revision and consume criterion IDs throughout lifecycle | logic | L | T-006.1 | Frozen typed record derived from current GitHub bytes; semantic source and rendering provenance; no Markdown name round-trip for new evidence contract. |
| T-006.3 Test Plan/outcome/revision and acceptance projection compatibility | test | L | T-006.2 | All sections preserved, no narrative obligations, stale bases fail before mutation, incomplete save never ready, approvals and checkboxes reference exact IDs. |

### US-007 — Reconcile comment and inline story edits without losing authority
Dependencies: US-006. Packages: planbrief, daemon/story_refinements, turn publication, store refinement/projection.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-007.1 Reconcile recognizable inline edits across all sections | logic | L | — | Multiline criteria belong to item; assign IDs only to clear new items; preserve unrelated bytes and historical IDs; ambiguous semantics produce pending findings. |
| T-007.2 Route comment-inferred updates through existing managed gates | logic | L | T-007.1 | Numeric owner, durable dedup, ordinary comment does not change mode; inference uses existing Plan run instead of nested agent service; fresh issue base before publish. |
| T-007.3 Test edit/comment conflicts and approval locks | test | L | T-007.2 | x-to-y obligation replacement, competing edits, stale projection, missing sections, reordered tasks, dependency contradiction and locked active generation; footer never a criterion. |

### US-008 — Explicit preview/apply legacy contract upgrade
Dependencies: US-005, US-007. Packages: cli, turn/store/github consumer seams.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-008.1 Add owner CLI preview with exact feature/revision scope | api | M | — | Suggested syntax: story-contract upgrade <parent-ref> --preview; default preview, explicit --apply. Reuse CLI identity/config resolution, never gh outside init. |
| T-008.2 Implement revision-bound recoverable apply | logic | L | T-008.1 | Upgrade feature's unfinished contract cohort only; refuse active frozen scope and ambiguous conversion; unchanged completed legacy stories remain historical; no automatic replan/reapproval. |
| T-008.3 Test preview/apply/replay and mixed-history dispatch | test | L | T-008.2 | No mutation preview/refusal, stale remote revision, partial publication recovery, legacy pending effects, workflow unchanged, resumed/fallback version selection. |

### US-009 — Shared protocol instructions and operator documentation
Dependencies: US-005, US-006, US-007, US-008. Shared/root sources only; no installed protected-tree writes.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-009.1 Update shared workflow/agents and root/docs contracts | logic | M | — | Describe helper, schema, receipts, list B, edit footer, upgrade and pending external proof; update APP_CONTEXT/CONSTRAINTS narrowly if new version path supersedes v1-only wording. Preserve literal mode header contract. |
| T-009.2 Verify shared sources via temporary/in-memory rendering | test | S | T-009.1 | No generator writes both protected trees. Document precise source-to-target manifest for manual opposite-harness handoff. |

### US-010 — Claude instruction artifacts maintained by Codex
Dependencies: US-009. Execution harness: Codex. Scope exclusively .claude/**.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-010.1 Update affected installed .claude agents/modes/skills | logic | M | — | Render/check against shared source target manifest; never write .codex. Scoped completion independent of sibling story. |
| T-010.2 Retain scoped .claude consistency checks | test | S | T-010.1 | Temporary comparison and source review, report temporary parity drift and opposite-harness handoff. |

### US-011 — Codex instruction artifacts maintained by Claude
Dependencies: US-009. Execution harness: Claude. Scope exclusively .codex/**.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-011.1 Update affected installed .codex skills | logic | M | — | Exact shared-source manifest; never write .claude. Codex must leave this ready for manual Claude switch. |
| T-011.2 Retain scoped .codex consistency checks | test | S | T-011.1 | Temporary comparison only; no sibling-tree prerequisite. |

### US-012 — Aggregate compatibility and retained external delivery evidence
Dependencies: US-008, US-010, US-011. This story must preserve the authentic external obligation, not claim fixture equivalence.
| Task | Type | Est | Depends | Hints |
|---|---|---|---|---|
| T-012.1 Add offline full-lifecycle matrix and aggregate parity checks | test | L | — | Both workflow versions, both fake adapter paths, all new contract stages, 256-criterion capacity, correction and restart recovery; make check before engineering completion. |
| T-012.2 Document operator external capture procedure and evidence inventory | logic | M | T-012.1 | No paid provider launch in automated tests/managed runs; sanitized provenance includes actual versions, host/sandbox, run/attempt, contract/workflow, request/receipt correlation and final observation. |
| T-012.3 Assess supplied authentic evidence availability for handoff | logic | S | T-012.2 | Capture unavailable now; leave actual two-harness invocation/correction/decision AC factually unverified. Operator supplies separately authorized external observations; Validate reviews retained artifacts, never runs or commissions new checks. |

## Acceptance evidence inventory requirements

Every story body must carry an Evidence Inventory H2 outside its Acceptance Criteria section with one row for EACH story-local criterion: criterion ID, proof method, exact source availability, version/context bounds, missing prerequisite/disposition. Offline fixtures prove only the tested source behavior. Historical reports #170/#204 and runs 12–14 are owner-reported incident evidence; no independent raw payload verification occurred here.

US-001–008: offline schema/renderer/store/CLI/fake-forge tests plus scoped source review, available source and synthetic fixtures, new versus legacy contract/workflow explicitly named; new retained checks unavailable until implementation. US-009–011: source and temporary render comparisons, available source catalog, scoped harness/version and path ownership bounds; opposite-harness work pending where unavailable. US-012: synthetic aggregate matrix does NOT establish actual agent capability. A separate criterion retains authentic external end-to-end evidence for BOTH actual harnesses invoking helper, receiving diagnostics, correcting within the run and observing durable decision before exit; source unavailable, versions/host unknown until capture, disposition UNSATISFIED. No in-run nested Codex or sandbox widening. A reasoned numeric-owner exception may waive this blocker only while retaining unverified facts.

Inventory completeness verdict: feature-spec candidate AC-001 through AC-010 map to stories above, including all user-approved structure/edit/upgrade/evidence choices; authentic evidence prerequisite is unresolved and must survive PPO renumbering. Nothing requires another product question before story authoring.

## Security and platform findings

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern addressed by plan | New strict shared validators aggregate independent faults and bound allocation; pending implementation evidence. |
| Authentication/authorization | Concern addressed by plan | Broker capability binds exact run/stage/scope; numeric owner remains forge authority; no envelope overrides. |
| Secrets/configuration | Compliant design | No new forge credentials/environment allowance; config owns paths, helper descriptor owner-only; do not log capability values. |
| Data handling | Concern addressed by plan | Rooted stable reads, immutable snapshot references, complete provenance, bounded payloads and redacted outbound bodies. |
| Dependencies | Compliant design | stdlib-only additions, existing SQLite migrations; no router/ORM/module addition. No fresh vulnerability audit claimed. |
| Error handling | Compliant design | Typed result diagnostics, not swallowed decode errors; no extra recover; stable order and pending facts. |

Platform: SQL exclusively store with one connection and transactions ending before I/O; context first, cloned mutable boundary data; interface at consumer; no detached background context; bounded broker lifecycle workers and daemon-owned time/retries. Use rules SEC-2/4/6/8/20/21, CFG-7/8, TEST-2/3/5/6/7/8/10, CONC-1/2/4/6/9–12/21–23, STYLE-5/9 and EXT-4/5. Generic platform guidance never overrides these project-specific rules. No new panic recovery, assertion library, migration library or live-provider CI check.

## Current proposal validation handoff

For THIS iteration use existing `planbrief.ParseProposal`, which invokes `PlanProposal.Validate`, `ValidateStoryBody` and `AcceptanceCriteria`; separately use `ParseOutcome`, `ParseWorkingContext` if saved, and `ParseStoryIndex`. Current envelope v1, parent github:marioatcloudvane/litefactory#207, generation 1. Create stories have zero issue IDs/numbers and empty expected_base; no existing identities were supplied. Outcome uses exact current run 301/attempt binding. Story Index parser accepts `| ID | Title | Size | Depends On | Sub-issue |`; use an em dash for unpublished sub-issue cells, never fake GitHub links. Factory replaces the whole index with confirmed link-only output before readiness. Index contains no story content/task tables. Rooted temp Go helper can invoke these pure exported parsers, report AC names/counts and exact set correspondence with evidence inventory, then be removed; no broad Go tests required for Plan-only artifacts. Current limits are 100 stories, 8 MiB proposal and 65,536 bytes/full body. All H2 required sections must be nonempty and the Acceptance Criteria section contains only actual AC-ID checklist lines, never evidence prose or inventory.
