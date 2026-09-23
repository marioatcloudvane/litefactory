# Feature Spec: share-one-implementation-for-loop-if
# Status: APPROVED
# Date: 2026-09-20

## Goal
- Let workflow authors reuse one implementation of agent execution, branching, loops, reporting and owner questions, while Research document generation remains an explicit scoped activity.

## User Stories
| ID | Story |
|---|---|
| US-1 | As a workflow author, I want generic activities and a small explicit mode contract so another mode does not require copied handlers or lifecycle code. |
| US-2 | As a Research owner, I want both Research documents generated from the reviewed candidate before reporting or handoff so findings remain coherent. |
| US-3 | As a maintainer, I want Research fully converted and reuse demonstrated with a second-mode fixture so future roles need only Markdown and DOT when existing contracts suffice. |

## Decisions and provenance
| Decision | Source |
|---|---|
| Use lf_agent, lf_if, lf_loop, lf_comment and lf_ask for reusable mechanics. | Owner corrections; research brief d2, d16. |
| Add lf_research_brief for both Research documents; retain internal scoped validation and presentation. | Latest accepted sketch; brief d35–d42. |
| Convert Research completely without old action aliases or compatibility decoders. | Owner direction; brief d21, d33. |
| Defer Plan DOT, Plan approval policy and lf_plan_publish implementation. | Latest owner scope; brief d22–d24. |
| Keep this a bounded refactor of existing machinery and records. | Current request to keep it simple; brief a1, a8. |

## Existing features to reuse
| Capability | Existing owner |
|---|---|
| Graph profiles, registries, validation and source controls | internal/graphexec; internal/turn/research_control.go |
| Captured workflow packages and Markdown roles | internal/teamsource/research.go; internal/teamsource/research/ |
| Attempt/run binding, runner dispatch, resume fallback and metering | internal/turn/research_agent.go and graph lifecycle; internal/agent; internal/harness; internal/codex |
| Versioned private Research result | internal/agentresult/research_node.go |
| Brief writer and safe artifact validation | internal/turn/research_briefs.go; internal/turn/research_artifacts.go |
| Retained comments, delivery and owner replies | internal/turn/research_comments.go; internal/store/graph_*.go |

## Data Model
| Entity | Required relationship | Constraints |
|---|---|---|
| Workflow definition and captured package | Explicit mode, digest, Markdown roles, skills and resources | Preserve admitted permissions and captured identity; generic action spelling grants no mode authority. |
| Activity registration | Supported mode(s), input/result rules, outcomes | Permit generic mechanisms and explicitly scoped activities together; unknown or unauthorized actions fail admission. |
| Mode contract | Result decoder/validator, response vocabulary, presentation and owner-gate policy | Research retains ask/done/approved semantics; no dynamic plugin or general schema. |
| Node result | Existing instance, node, attempt and candidate identity | Controls and effects resolve the named source's current eligible result; malformed or mismatched results fail. |
| Brief result | Source candidate and two artifact references | Success only after both documents and artifact checks succeed. |
| Comment, owner wait, reply and dispatch | Existing candidate, stage, revision and delivery identities | Reuse records and authenticated reply checks; no cosmetic database renaming. |

## API / Interface Changes
| Surface | Input | Output / authority |
|---|---|---|
| lf_agent | Captured Markdown role, skills and admitted mode contract | Retained validated stage result; success/failed; never implicit publication or approval. |
| lf_if / lf_loop | Named source, response comparison and existing declared initial default | true/false; default applies only before any source attempt exists; durable limits remain. |
| lf_comment | Mode-rendered final or failure report and applicable source | Existing retained delivery and published outcome; no owner wait. |
| lf_ask | Named candidate and question/review kind | Retained delivery, published outcome and durable owner wait; ends invocation until an authenticated reply. |
| lf_research_brief | Named validated Research source bound to current instance/candidate | Both Research documents plus source-bound references, or failed with diagnostic; no remote publication. |
| Shared lifecycle | Explicit admitted mode and registered policy | Shared loading, admission, role delivery, execution, continuation and recovery; Research behavior preserved. |

## Business Rules
- Keep reusable mechanics in one implementation; mode-specific validation, vocabulary, templates and meaningful domain effects remain explicitly scoped.
- Preserve Research investigation → shaping and its question/review boundaries; Markdown roles remain roles, not new activities.
- Validate payloads, reference declarations and pre-existing data artifacts before retaining success; defer existence checks only for the two documents generated by lf_research_brief.
- Every successful Research report, question/review and stage handoff requires successful brief generation for that same candidate; failure diagnostics may bypass generation.
- The brief activity writes only RESEARCH_BRIEF.md and RESEARCH_TECHNICAL.md in the admitted session, reusing containment, regular-file and symlink checks.
- Worker done/approved vocabulary cannot create owner authority; approval requires the matching authenticated owner reply for the reviewed candidate and stage.
- Markdown/DOT cannot expand permissions, switch the admitted mode or execute arbitrary expressions.
- Preserve valid source selection, bounded cycles, unambiguous outcomes, owner-wait path rules, durable counters and existing failure policy.
- Preserve non-graph execution and activation defaults; no automatic Plan selection or production activation.

## Edge Cases & Error Handling
- Invalid output, invalid source, missing contract or unauthorized scoped action → actionable rejection without silent advance.
- Exhausted durable limits or failed activity → preserved failure reason; no new automatic retry policy.
- Stale, duplicate or unrelated owner reply → cannot approve or resume another candidate; valid replies retain existing once-only consumption.
- Lost publication acknowledgment or restart → reconcile the retained effect and identity before further delivery; do not knowingly publish twice.
- Repeated brief generation for the same candidate → same document content; partial writes or failed artifact checks never release success or downstream publication.
- Pre-cutover Research graph instances/captures → explicitly unsupported; reject without migration, alias fallback or automatic deletion of stored data.

## Acceptance Criteria
- [ ] AC-001: The registry accepts the five generic activities and Research-only lf_research_brief with explicit mode authorization; rejects unknown actions, old Research mechanism names and unauthorized scoped activities.
- [ ] AC-002: Research and a second-mode test fixture use the same generic activity and lifecycle implementations, including loading, role delivery, source controls, reporting, owner waits and continuation/recovery; the fixture requires no copied handler and generates no Research documents.
- [ ] AC-003: Controls preserve first-pass defaults only before a source attempt, true/false outcomes, identity-checked source decoding, durable bounds and rejection of malformed output or invalid/bypassed owner-wait paths.
- [ ] AC-004: lf_research_brief generates both documents from the same retained candidate, validates safe paths and references, reproduces content on replay, and blocks success after either partial write or artifact failure; successful reporting and handoff cannot bypass it, while failure diagnostics can.
- [ ] AC-005: The converted Research graph preserves investigation and shaping, questions, review, authenticated candidate-bound approval and final reporting; stage completion, stale replies and duplicate replies cannot grant approval or trigger duplicate continuation.
- [ ] AC-006: Retained delivery and restart tests preserve candidate/delivery identity, reconcile acknowledgment loss and preserve actual failures; runner request/fallback tests retain admitted roles, permissions and attempt/meter identities through the shared lifecycle.
- [ ] AC-007: A fixture adds a Markdown role and DOT node using an existing contract without adding a handler or role-specific runtime branch; the shipped Research graph and role instructions use the new activities and document boundary.
- [ ] AC-008: No compatibility aliases, migration or automatic data deletion are introduced; unsupported old graph captures fail clearly; non-graph Research and default activation behavior remain unchanged; the implementation passes make check and leaves gofmt -l . empty.

## Evidence inventory
| Criteria | Proof method and available source | Bound / prerequisite |
|---|---|---|
| AC-001–003 | Offline registry/package tests, source-control tests and second-mode fixtures using existing internal/graphexec, internal/teamsource, internal/turn and internal/store test patterns. | Execute against implementation snapshot; synthetic fixture proves reuse, not a production second-mode workflow. |
| AC-004 | Temporary-session writer/artifact tests with write-failure and unsafe-path cases; graph admission and execution tests for required source-bound generation. | Local filesystem fixtures; no remote or live provider required. |
| AC-005–006 | Existing assembled Research graph, owner-wait, delivery-recovery and runner-adapter test patterns with local durable store and injected runners/transport. | Proves application behavior under tested failures; cannot certify authentic Claude/Codex host operation. |
| AC-007–008 | Captured-package fixture, old-capture rejection and non-graph regression tests; source review; make check; gofmt -l . | Exact implementation snapshot; actual execution evidence must be retained later. |
- Referenced docs/decisions/nested-agent-evidence.md and docs/story-contract-instruction-manifest.md are absent in this checkout; current evidence boundaries are supplied by workflow/SKILL.md, teams/source/skills/test-evidence/SKILL.md and docs/workflow/research-graph.md.
- No tests are executed by this Plan contribution; evidence rows are obligations for implementation, not passing results.

## Assumptions
- Use one small static mode contract/registration path and existing typed result envelopes; exact Go interface shape belongs to architectural design.
- Preserve existing persisted comment kinds and result vocabulary because the owner requested shared mechanics rather than new semantics.
- Limit role/instruction changes to the shipped captured Research package under internal/teamsource/research and relevant shared documentation; no protected .claude/** or .codex/** edits are currently identified. If required, split those edits into independent opposite-harness stories.
- Old graph compatibility is waived; this does not retire unrelated legacy/non-graph paths or existing runtime evidence obligations.

## Risks
- Moving document generation after result retention changes validation ordering; generated-document declarations must not be mistaken for proof that files already exist.
- Renaming actions changes digests; old unsupported captures need a clear diagnostic rather than accidental execution with new semantics.
- Generic names remove the naming-based mode fence; explicit authorization and mode-owned contracts must retain it.
- Shared lifecycle extraction can accidentally lose request context, resume fallback, metering or effect identity; focused regression coverage is required.

## Out of Scope
- Plan DOT or workflow design, approval policy, Plan integration, lf_plan_publish implementation and unrelated mode conversion.
- Backward compatibility, migration/resumption of pre-cutover Research graphs and deletion of existing stored data.
- General document frameworks, dynamic plugins, new workflow schemas, arbitrary uploaded workflows and executable graph expressions.
- Default production activation, new live-provider capability claims, paid nested-agent observations and unrelated #274/#296 adoption/retirement obligations.
- Implementation, remote publication or readiness claims during this planning step.

## Open Decisions
- None; the latest owner instruction authorizes planning the recorded scope with the bounded assumptions above.
