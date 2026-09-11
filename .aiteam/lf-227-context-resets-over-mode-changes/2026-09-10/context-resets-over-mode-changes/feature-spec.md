# Feature Spec: context-resets-over-mode-changes
# Status: APPROVED
# Date: 2026-09-11

## Goal
- Give the owner fresh native coding-agent conversations after admitted mode changes while preserving durable task knowledge and existing workflow authority.

## Decisions and Assumptions
| Kind | Statement | Basis |
|---|---|---|
| Owner decision | Reset on every admitted change among Research, Plan, Implement, Validate and Spike, including returning to an earlier mode, for both agents and all supported delivery configurations. | Run 0003 “Ok looks good, plan it out!” adopts research recommendations D1–D3. |
| Owner decision | Retain bounded recorded issue comments alongside the current instruction and handoff. | D2 adoption. |
| Owner decision | Do not require outgoing completion for a permitted transition; continue from available reliable inputs, explicitly report omissions, and stop only work requiring missing or ambiguous authoritative inputs. | D3 adoption. |
| Assumption | Same-mode continuation and interrupted-run recovery preserve their existing rules and independent reset reasons. | D1 and existing recovery contracts. |
| Assumption | Reset changes native conversation identity; worktree, task identity, artifact session, frozen scope and retained evidence survive. | Research engineering defaults and existing session separation. |
| Assumption | Explicit handoff delivery and bounded missing-input diagnostics are sufficient; automatic summarization of an unrecorded outgoing transcript is not required. | Research identifies saved artifacts as the handoff and cannot recover unsaved reasoning. |
| Assumption | Existing supported workflow/content versions remain independent and unchanged. | Existing managed workflow and submission contracts. |

## User Stories
| ID | Story |
|---|---|
| US-1 | As the owner, I want every admitted mode change to begin a fresh conversation so previous role instructions and native history do not carry over. |
| US-2 | As the receiving agent, I want the same explicitly identified artifact session and appropriate saved inputs so I can continue across modes, dates and process restarts. |
| US-3 | As the owner, I want missing handoff information reported without unnecessary transition restrictions so incomplete research and detours remain useful. |
| US-4 | As the owner, I want demonstrable cross-agent behavior and documented evidence limits so offline checks are not mistaken for live lifecycle proof. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Admitted run | Selected mode | Existing mode value | Yes | Factory authority; rejected requests do not establish a new active conversation. |
| Conversation | Native identity and resume handle | Existing adapter session values | Yes where supported | New identity after mode change; cannot select an old conversation when returning to its mode. |
| Task handoff | Artifact session binding | Validated task-bound path/reference | Yes when a session exists | Same task/worktree; survive dates and fresh starts; ambiguity is explicit. |
| Task handoff | Saved content and source references | Existing artifacts and retained records | By receiving mode | Clearly distinguish instructions, authoritative frozen inputs, evidence, decisions, assumptions and untrusted background. |
| Task handoff | Missing-input diagnostics | Bounded structured or rendered findings | On omissions | Identify unavailable or ambiguous input and affected work; never imply successful reconstruction. |

### Relationships
- Admitted task run → selected conversation segment and retained artifact session.
- Receiving mode → appropriate existing saved artifacts and factory-authoritative scope/evidence.
- Corrective Implement → relevant retained blocking Validate findings and their affected scope/evidence references.

## API / Interface Changes
| Method | Action | Auth | Notes |
|---|---|---|---|
| Existing owner commands / canonical labels | Select next mode | Existing owner and admission checks | No new command, permission or automatic mode chain. |
| Shared runner input | Supply reset decision, bound session and explicit handoff | Factory-owned input | Both adapters, cold/resumed/fallback preparation; preserve current comment bounds. |
| Agent report | Disclose missing handoff inputs | Existing reporting path | Identify affected work without inventing lost context. |

## Business Rules
- Retain existing bounded comment history policy; comments are background and cannot replace frozen requirements or select tools/permissions.
- Research → Plan explicitly makes the research brief available for consumption; reuse saved Plan working context when applicable.
- Plan → Implement explicitly supplies shared planning decisions/non-goals alongside complete factory-frozen stories, identities, revisions and dependencies.
- Implement → Validate continues to use retained immutable engineering evidence and checks; Validate remains read-only and runs no fresh checks.
- Validate → Implement explicitly carries relevant actionable blocking findings and retained evidence references; history alone is insufficient.
- Research, Plan and Spike transitions preserve available findings, decisions, assumptions, unresolved work and artifact provenance without requiring an outgoing completion ceremony.
- Reset behavior is independent of external instruction delivery; delivery changes retain their existing independent reset behavior.
- Persisted task session discovery remains backward compatible; validate legacy candidates and refuse ambiguity rather than silently selecting another task/session.
- Receiving agents must be explicitly instructed to consume the required available handoff; path discovery alone is insufficient.
- Shared instructions and installed copies must express the same handoff contract; protected installed-tree maintenance is independently scoped by harness, with aggregate parity checked afterward.

## Edge Cases & Error Handling
- Return to an earlier mode → allocate another fresh conversation, never resurrect the earlier native history.
- Repeated same-mode replies → preserve resume eligibility, subject to existing interruption and delivery identity rules.
- Transition denied by existing gates → retain those gates and do not treat denial as a successful mode execution/reset.
- Partial or absent research → Plan may reconstruct from authoritative inputs and report gaps.
- Missing approved implementation scope → affected implementation does not proceed.
- Missing/stale/misattributed validation evidence → criterion remains unverified; failed evidence remains failed.
- Multiple plausible artifact sessions, cross-task binding or unsafe artifact path → explicit diagnostic; no silent guess or unauthorized read.
- Restart/fallback after admitting a new mode → preserve the new mode boundary and existing attempt recovery rules without reusing the outgoing transcript.
- Handoff exceeding existing input limits → retain authoritative requirements and explicit omissions/references; never silently truncate into a claim of completeness.

## Out of Scope
- Deleting comments, files, worktrees or retained evidence; resetting task identity; guaranteeing zero loss of unsaved reasoning.
- New automatic phase chaining, weaker readiness/approval gates, configuration-dependent reset opt-out, or changes to managed contract versions.
- Live nested coding-agent launches, sandbox expansion, fallback agent introduction, or requiring live paid-agent capture in CI.
- Recovery of unrelated historical evidence obligations or publication of a team catalog release.

## Candidate Acceptance Criteria
- [ ] AC-001: Every admitted mode change selects a fresh native conversation across both adapters and supported delivery paths, including reverse transitions; same-mode continuation and existing independent resets/recovery retain their behavior.
- [ ] AC-002: The receiving run retains an explicit validated task artifact-session binding across dates, restarts and fallback; unsafe or ambiguous candidates are diagnosed without a silent substitution.
- [ ] AC-003: Receiving-mode input explicitly delivers the applicable Research/Plan/Spike knowledge, frozen implementation scope, retained validation evidence and corrective blocking findings, with bounded recorded comments and proper authority distinctions.
- [ ] AC-004: Incomplete handoffs do not add a blanket admission gate; missing authoritative inputs prevent only dependent work and produce actionable omissions without weakening approval or acceptance.
- [ ] AC-005: Shared and installed instructions consistently require handoff consumption and preservation; independently scoped installed-tree checks and final aggregate parity establish their documented contract.
- [ ] AC-006: Separately authorized external observations demonstrate actual fresh conversations and usable handoffs for both actual adapters at representative forward and reverse transitions; absent authentic evidence remains explicitly unverified.

## Candidate Evidence Inventory
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-001 | Offline turn/store/adapter command-construction and recovery regression checks; actual runtime component tracked separately in AC-006. | Existing delivery, prepare, adapter and continuation code/test sources identified in RESEARCH_BRIEF.md; new checks required. | Repository behavior and constructed invocations only; no native runtime claim. | Implement and retain executed checks; no checks executed by requirements. |
| AC-002 | Offline binding/discovery fixtures covering dates, restart, ambiguity and unsafe paths. | Existing sessiondir and plan_context sources; additional fixtures required. | Controlled filesystem/task fixtures only. | Implement and retain checks. |
| AC-003 | Offline prompt/input assertions across modes and cold/resume/fallback, including exact frozen data and findings attribution. | Existing approved_context, validation and prompt tests; additional transition fixtures required. | Delivery contract only; cannot prove arbitrary model comprehension or completeness of unsaved knowledge. | Implement and retain checks; authentic receiving behavior remains AC-006. |
| AC-004 | Offline admission/input-failure assertions and source review. | Existing gate and retained-evidence behavior; new omission cases required. | Repository policy enforcement only. | Implement and retain checks. |
| AC-005 | Scoped documentation review plus offline rendering/contract checks, then aggregate parity. | Shared sources and installed trees available. | Documented instructions and rendered bytes only, not agent compliance. | Independent opposite-harness installed-tree execution if needed; retain checks and scoped review. |
| AC-006 | Separately authorized operator observation outside managed nested-agent execution, with sanitized provenance, native identities, mode sequence and supplied/consumed handoff evidence. | Authentic feature-specific capture unavailable. | Record exact agent versions, host/sandbox, feature revision and transition/configuration represented; historical capture proves only its recorded context. | External observation remains an unsatisfied acceptance obligation; synthetic fixtures cannot satisfy it and CI must not launch live agents. |

## Readiness
- Owner questions: none; D1–D3 are adopted and ordinary engineering choices remain for architecture.
- Ready for architecture and typed story authoring; preserve candidate evidence inventories when assigning final story-local criterion IDs.
