# Feature Spec: acceptance-criteria-should-be-structured
# Status: APPROVED
# Date: 2026-09-10

## Goal
- Eliminate serial, whole-run failures caused by agents reconstructing schemas: agents submit typed contributions; litefactory supplies identities, renders stories, and returns actionable validation plus durable decisions before the agent exits.

## Approved scope and provenance
- Source: session RESEARCH_BRIEF.md, particularly run 0005 decisions; owner runs 0004–0005 approve all five structured sections, evidence list B, criterion rules, feedback, explicit CLI upgrades, comment-preferred editing and GitHub authority.
- Existing mechanisms to extend: APP_CONTEXT.md identifies planbrief proposal parsing, report rendering, turn publication/freeze/progress/validation, store durable evidence/effects, daemon child refinement routing and both runner adapters.
- Existing proposals remain version 1 for this planning iteration; the future protocol must be explicitly versioned and dispatched without changing persisted workflow version.
- No owner questions remain; architecture may resolve transport and exact shared aggregate limits without reopening approved product scope.

## User Stories
| Theme | Story |
|---|---|
| Typed authoring | As an agent, I supply structured story content so narrative formatting cannot create undeclared obligations. |
| Editable authority | As an owner, I describe changes in issue comments or edit inline so GitHub remains the natural authoritative interface. |
| Identity ownership | As an agent, I submit outcomes for an authorized story without copying factory identities or allocating counters. |
| Correction | As an agent, I see all discoverable defects and a durable decision while active so I can correct a submission in the same run. |
| Compatibility | As an owner, I explicitly preview and upgrade legacy contracts through the CLI without changing historical evidence or active scope. |

## Data Model
| Entity | Required fields and types | Constraints |
|---|---|---|
| Plan contribution | summary: string; stories: array of story objects | Factory supplies contract, parent, generation, run/attempt and existing issue/base identities. |
| Story | key: string; intent: create/update/unchanged; title: string; user_story: object; acceptance_criteria: object array; tasks: object array; architecture: object; dependencies: object | Existing intent and selected-story rules preserved; no free-form full body on the new agent contribution path. |
| User story | actor, want, benefit: nonblank strings; notes: string array | Narrative and multiline strings never produce criteria. |
| Criterion | id: string; text: nonblank string | Canonical AC- plus positive 3–6 decimal digits with minimum-three-digit padding; AC-000/AC-0001 invalid; 1–256 active criteria; 4,096 UTF-8 bytes/text. |
| Task | id, description, owner_role: strings; criterion_ids, depends_on: string arrays | At least one task; unique task IDs; role from supplied roster; references resolve; within-story task dependencies acyclic. |
| Architecture | summary: nonblank string; components: array of name/responsibility objects; interfaces: array of name/input/output objects; constraints: string array; decisions: array of decision/reason objects | Optional collections may be empty; all object fields typed; required narrative fields nonblank. |
| Dependencies | story_keys, issue_refs: string arrays | Cross-story keys resolve; external refs use task-reference strings resolved and authorized by factory; cycles rejected; no implicit attachment or follow-up approval. |
| Issue projection | exact title/body revision; typed story; version; stable ID history | Derived cache only; issue is authority; no local/hidden copy overrides owner changes. |
| Story contribution | authorized story selector; stage-specific outcome and evidence/findings | Unknown, stale, unauthorized selectors and supplied envelope overrides rejected. |
| Evidence explanation | check_ids: string array; summary: string | Evidence is consistently an array of 1–16 explanation objects, each at most 8,192 UTF-8 bytes; no string-or-array union in new contract. |
| Executed check/snapshot | retained check identity/result/provenance; exact file bytes, safe path, digest/deletion state; implementation attribution | Real execution and captured state required; summary/claimed pass/digest alone insufficient; shared retained references avoid duplicate snapshots. |
| Finding | criterion ID or required review name; status; explanation array | Criteria cover exact frozen IDs; reviews retain code/architecture/plan and blocking/advisory semantics; missing evidence explicitly unverified. |
| Factory envelope | version, parent, run, generation, issue, revision, workflow, sequence, attempt, submission/receipt IDs | Constructed from bound factory state; concurrency-safe durable allocation; content cannot override any field. |
| Diagnostic | path, code, expected, actual, message; complete flag; unchecked paths | Indexed paths; safely aggregate independent defects; distinguish incomplete checks from successful input validation. |
| Receipt | durable correlation, stage, pending/rejected/recorded decision, diagnostics, acceptance state, remote-effect state | Input valid, evidence recorded, acceptance, issue closure and parent delivery are distinct facts. |

## API / Interface Changes
| Action | Actor/authorization | Required behavior |
|---|---|---|
| Preflight contribution | Current bound agent capability | Same schema/semantic rules as submission; no sequence allocation or accepted-state mutation; Plan and stage outputs supported. |
| Submit and observe decision | Current bound agent capability | Factory envelope; bounded feedback while caller active; durable receipt and safe retry; both harnesses, cold/resumed/fallback attempts. |
| Propose story refinement | Authenticated numeric owner through GitHub comment | Infer typed change against observed revision; understandable summary; durable comment deduplication; preserve mode/approval gates. |
| Edit issue inline | Owner's ordinary GitHub editing | Preserve bytes; reconcile all five sections; report ambiguity in ordinary language; no mandatory JSON or local artifact editing. |
| Preview/apply contract upgrade | Authorized owner through new CLI capability | Preview affected scope and validation conflicts; explicit apply; never implicit through republication/comment; active frozen revisions protected. |
| Publish/render | Factory only | Deterministic headings, task tables, checklists, dependencies and final guidance; sanitize/redact and check total rendered limits before publication. |

## Business Rules
- All typed collections exist, including empty optional arrays; reject unknown/duplicate keys, null substitutions, wrong types, unsupported outcomes, invalid references and cycles with actionable diagnostics.
- Preserve accepted structure from Research; all five sections typed, not acceptance-only; Plan discussion/completion outcomes and Validate output follow the same factory-envelope principle.
- Keep criterion identity on reorder/clarification; material replacement receives a new ID; never reuse a removed ID for another obligation; uncertain meaning remains pending for resolution.
- Assign a new clearly added checklist item the next unused criterion ID; multiline continuation belongs to its item; explanatory paragraphs never become obligations solely because nonempty.
- Reconcile tasks, narrative, architecture and dependencies as well as criteria; semantic changes require next approved scope; presentation-only changes need no requirement reapproval when equivalence is established.
- Prefer comments but support inline edits; render bottom guidance in its own non-obligation section: “Want to change this story? Comment with the requested change and litefactory will update it. Direct edits are supported, but comments help avoid conflicting revisions.”
- Recheck exact issue revision immediately before any update/freeze; preserve unrelated edits; conflicting comments/inline edits produce understandable pending conflict, never guessed approval or stale overwrite.
- Comment routing must reuse existing child-refinement and parent admission behavior; ordinary comments do not select a mode, bypass active-run/replan gates, or authorize new follow-ups.
- New contracts apply to newly created contract scope; existing stories remain legacy until explicit CLI upgrade. Mixed-version behavior must be deterministic or unsupported transitions explicitly rejected before mutation; workflow and content/event contract versions remain distinct.
- Legacy workflow retains its existing scoped verification/closure/invalidation semantics. Separated Implement records engineering/executed tests and releases dependencies while acceptance stays open; Validate reviews retained evidence without fresh checks; factory projects acceptance and closure.
- Historical separated acceptance survives later code/main changes; changed requirements belong to new approved scope/issues, not retroactive erasure.
- Unknown criterion coverage is rejected; supplemental checks have check IDs, not fake AC IDs. Empty check references permitted for honest missing-evidence explanations or source-based reviews, never substitute proof of required execution.
- Jointly validate proposal, rendered body, evidence and retained-reference budgets so 256 valid criterion records remain representable; reject over-limit inputs before publication with actual limits and locations.
- Keep GitHub sole editable story source, factory sole forge client, shared plan_brief.md link-only index, transient transport uncommitted, and no automatic phase chaining.
- Instruction delivery requires independent protected-tree stories: Codex writes .claude/** only; Claude writes .codex/** only; shared/root source work separately scoped; aggregate parity follows both independently ready stories.

## Edge Cases & Error Handling
- Numeric parent or legacy text-evidence array → new contribution rejects forbidden envelope field or wrong version/type before submission; legacy rules remain accurately diagnosed when bound.
- Multiple unrelated invalid fields → one response lists all safely discoverable errors; invalid JSON/container bounds dependent checks and reports incompleteness explicitly.
- Crash, duplicated call, concurrent story publishers, lost acknowledgment, daemon backoff, cancellation or end-of-run drain → durable correlation/recovery, no duplicate accepted effect; bounded unknown result remains pending.
- Valid input but missing retained evidence, wrong snapshot, stale revision or forbidden stage → precise rejection/unverified finding; no successful acceptance receipt.
- Remote publication/projection/closure fails after local record → receipt preserves recorded fact and pending/failed remote effect; replay recovers without inventing completion.
- Duplicate/removed IDs, ambiguous free prose or external dependencies → preserve owner edit and explain conflict; no silent requirement dropping, renumbering or stale hidden edge.
- Upgrade with active scope/concurrent issue edit → refuse unsafe transition or defer with precise reason; preserve old frozen bundle and history.

## Candidate Acceptance Criteria and Evidence Inventory
| ID | Candidate obligation | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|---|
| AC-001 | Every story section is typed and deterministic rendering cannot manufacture undeclared obligations. | Offline schema, renderer and round-trip/projection assertions. | Current source and synthetic fixtures available; new tests not yet written. | New content contract; legacy explicitly separate. | Implement and retain scoped test output. |
| AC-002 | IDs, references and aggregate budgets reject invalid/unrepresentable contracts before publication. | Boundary and cross-consumer compatibility tests including 256 criteria. | Existing parsers/source available; new boundary fixtures needed. | New version and documented byte/count limits. | Implement validators and retained-reference capacity tests. |
| AC-003 | Comment and inline edits preserve GitHub authority, stable identity and frozen approval across conflicts. | Offline fake-forge lifecycle/revision-race tests and scoped source review. | Existing refinement/publication code; synthetic issue revisions available. | Supported edit grammar/semantic uncertainty behavior; no claim of perfect free-prose interpretation. | Add reconciliation/conflict fixtures and retained results. |
| AC-004 | Factory supplies envelope/counters and enforces selector authorization for all covered stages. | Unit and orchestration tests for override/stale/unauthorized/concurrent submissions. | Existing approved bundles/event types available. | Bound Plan/Implement/Validate contracts and both workflow versions. | New protocol and allocator tests. |
| AC-005 | Evidence list B preserves multiple attributable results and missing-evidence honesty. | Schema/provenance/coverage tests and retained-evidence review. | Existing snapshot/check source available; synthetic assertions possible. | New contract only; legacy text preserved; Validate runs no checks. | Implement list/reference format and retain results. |
| AC-006 | Plan and story preflight return comprehensive indexed diagnostics without mutating accepted state. | Multiple-error, malformed-container and side-effect assertions. | Owner incidents are narrative only; synthetic equivalents available. | Tests prove implemented handling, not independently reproduced historical runs. | New validator tests; historical raw payloads not required for regression mechanism. |
| AC-007 | Durable receipt/correction works within one run and preserves pending/effect distinctions through recovery. | Offline held-run/fake-adapter lifecycle and fault-injection tests. | Existing progress correction tests and synthetic adapters available. | Factory lifecycle under controlled fixtures; no authentic runtime capability claim. | Add receipt/final-drain/backoff/retry fault cases and retain checks. |
| AC-008 | Both actual supported coding agents can invoke feedback, correct and observe the authoritative decision before finishing. | Separately authorized operator-controlled external end-to-end observation with sanitized provenance. | Authentic observation for new protocol unavailable. | Must record agent versions, host/sandbox, contract/workflow, run identities and lifecycle; historical captures prove only documented historical context. | UNSATISFIED until suitable authentic external evidence supplied; never require nested Codex in managed sandbox or relabel fake-adapter tests as this proof. |
| AC-009 | Explicit CLI upgrade preserves legacy dispatch, active frozen scope and historical acceptance. | Offline CLI/store/forge tests including preview, concurrent edits, replay/cold/resume/fallback. | Existing registration/versioned evidence source available; command absent per Research. | Workflow version independent of content/event version. | Implement upgrade and compatibility fixtures. |
| AC-010 | Both harness instructions and shared sources describe correct submission/edit/receipt behavior with independent ownership. | Scoped generated-output/source review, temporary comparison and aggregate parity review. | Current source/renderers available. | Each protected tree changed only by authorized opposite harness; no runtime proof inferred. | Separate ready stories and eventual aggregate parity evidence. |

## Assumptions
- This is a new feature with no supplied existing child identities; create stories with zero numeric identities and empty bases in the current version-1 proposal.
- Requirements may be split or assigned new story-local AC identifiers by story authoring; carry the complete evidence-inventory fields and honest missing prerequisites into each affected full story body.
- CLI upgrade syntax, author-callable transport details and precise aggregate retention budgets are engineering design decisions within the approved behavior; they require no new owner round trip.
- Authentic live-agent receipt capability remains an explicit acceptance obligation whose external evidence is currently unavailable; implementation planning can proceed without falsely certifying it.

## Out of Scope
- Repairing historical #170/#204 runs, modifying runtime state, automatic legacy upgrades, replacing GitHub with a local artifact, changing sandbox permissions, adding a fallback agent, or requiring live nested agent execution in CI/managed runs.
- Inferring current real-agent capability from synthetic fixtures or historical captures; bypassing approved linked-follow-up, frozen-scope, exception, delivery or closure rules.
