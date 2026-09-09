# Feature Spec: a-run-cannot-launch-a-coding-agent
# Status: APPROVED
# Date: 2026-09-09

## Goal
- Make the run sandbox's live-agent evidence limitation visible during planning, provide an honest fixture-based acceptance route, and explain the reported initialization failure without weakening execution permissions.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As a planner, I want the supported evidence boundary recorded in persistent planning inputs so approved stories do not require a nested live Codex process inside a run. |
| US-2 | As an implementer, I want a documented external capture and sanitized fixture provenance contract so I can verify supported behavior offline and distinguish unavailable evidence from satisfied acceptance criteria. |
| US-3 | As an owner reading a failed run report, I want a narrowly matched Codex initialization-permission diagnostic so the reported signature has a useful explanation and supported next step. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Captured evidence | Origin | Documented metadata | Yes | Identify external capture context, date, coding-agent version, relevant flags, and source/collector; no credentials or private runtime paths. |
| Captured evidence | Sanitized stream | Existing fixture format | For authentic claims | Preserve events and relationships used by assertions; distinguish authentic captures from synthetic examples. |
| Captured evidence | Sanitization | Documented transformations | Yes | Describe removals and stable substitutions; retain identity distinctions required by the proof. |
| Captured evidence | Verification scope | Named assertions and limitations | Yes | Name what the stream proves and what it cannot establish about current runtime delivery or current sandbox behavior. |
| Related issue reference | Identity and source | Supplied parent narrative | Yes | Reference #73 and #125 as reported history only; no invented child issue numbers, base hashes, status, or current verification. |

### Relationships
- Authentic fixture stream → accompanying provenance document → explicit offline assertions.
- Planning evidence decision → each affected acceptance obligation → offline verification or explicit unavailable-evidence prerequisite.
- Known initialization signature → contextual explanation in existing failure reporting.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Documentation | Persistent constraints and planning guidance | Existing repository workflow | Keep Claude sources and Codex skill copies equivalent; constraints must be encountered before acceptance criteria are frozen. |
| Documentation | External capture/provenance procedure | Operator outside managed run | Describe safe acquisition and sanitization; this feature does not execute a live capture. |
| Existing reporting | Failed Codex initialization explanation | Existing run reporting | Preserve failure outcome and existing redaction; no new CLI command, network endpoint, database entity, or permission switch. |

## Business Rules
- Choose the fixture route recommended by issue #183; managed runs must not loosen the sandbox to collect nested live-agent evidence.
- Distinguish the known nested Codex limitation from ordinary top-level agent execution and from Claude behavior; do not assert every coding agent cannot launch.
- Planning must enumerate evidence needs before freezing stories and must reject a requirement for managed-run nested Codex execution as an unsupported acceptance method under the current boundary.
- Prefer already available authentic captured fixtures when they prove the criterion; never substitute a synthetic fixture for an authentic capability claim.
- Missing authentic evidence remains an explicit prerequisite or unsatisfied criterion; it must not silently remove promised scope or become a success assertion.
- A captured stream may prove parser handling or relationships represented in that capture; it alone cannot prove present-day delivery, sandbox compatibility, conversation lifecycle, or end-to-end runtime behavior.
- Cite supplied #73 and #125 history as motivation and candidate follow-up scope; neither issue is reopened, updated, or declared recovered by this plan.
- Match the specific in-process app-server initialization permission failure, with or without the preceding PATH-alias warning; the PATH warning alone or arbitrary EPERM is insufficient.
- Where nesting is not independently known, diagnostic wording must identify a possible sandbox restriction rather than assert a proven root cause.
- Do not trigger retries with broader permissions, switch coding agents automatically, or turn a failed process into success.

## Edge Cases & Error Handling
- PATH-alias warning followed by successful agent events → retain successful outcome; no initialization-failure classification.
- Generic permission error, authentication failure, quota refusal, or unrelated startup failure → retain existing classification.
- Specific initialization error without evidence that the process was nested → explain the known limitation conditionally and preserve the raw sanitized error.
- Authentic capture unavailable in repository → document safe external acquisition as a prerequisite; do not launch a probe in the managed run or invent provenance.
- Sanitization removes the relationship required by an assertion → evidence is insufficient for that assertion.
- Existing captured version differs from deployed version → retain version-specific proof limits; do not claim current compatibility.

## Out of Scope
- Weakening or bypassing the sandbox; new privileged brokers, socket exceptions, execution backdoors, or agent fallback behavior.
- Launching nested coding agents or running external capture during managed implementation or CI.
- Revalidating upstream Codex behavior or diagnosing the user's machine using live probes.
- Reading GitHub directly, modifying #73/#125, rewriting their frozen acceptance criteria, or completing their deferred stories.
- Building a general fixture registry, automatic acquisition service, or generalized error taxonomy.

## Acceptance Criteria
- [ ] AC-1 Persistent planning inputs record the nested Codex evidence limitation, the unchanged security boundary, and the supported external-capture/offline-fixture decision; corresponding Claude and Codex planning guidance agree.
- [ ] AC-2 Planning guidance requires each live-agent evidence obligation to declare its supported proof method before freezing scope and explicitly records missing evidence without dropping acceptance obligations.
- [ ] AC-3 A capture/provenance procedure identifies capture environment, date, version, relevant invocation details, sanitization transformations, asserted relationships, verification scope, and limitations; it clearly separates authentic and synthetic evidence.
- [ ] AC-4 The procedure uses the existing quota fixture/provenance pattern as a concrete repository reference and explains why a fixture cannot by itself prove current runtime delivery or lifecycle behavior.
- [ ] AC-5 A persistent decision links #73 and #125 using only the supplied narrative, states that their reported blocks were not revalidated, and explains the evidence review needed before either can claim recovery.
- [ ] AC-6 The specific app-server initialization permission failure receives an actionable, sanitized explanation of the known nesting limitation and fixture route, preserving failure semantics and expressing uncertainty when nesting is not known.
- [ ] AC-7 Offline tests cover the exact initialization signature with and without the PATH warning, warning-only success, generic EPERM, unrelated initialization failures, and existing authentication/quota classifications without spawning a real coding agent.
- [ ] AC-8 Required aggregate repository checks pass without changing agent permissions or requiring live-agent observation.

## Assumptions and Evidence
- Source: factory-supplied issue #183 and owner comment; no RESEARCH_BRIEF.md was supplied.
- Repository lookup: `.aiteam/APP_CONTEXT.md` identifies existing runner adapters, neutral result contracts, report rendering, and managed planning/evidence parsing as reusable owners.
- Assumption: the recommended fixture route is the intended decision because the issue explicitly prioritizes it and rejects boundary changes by default.
- Assumption: document the authentic-capture contract and reuse available examples now; new authentic captures cannot be promised from this sandboxed run.
- Assumption: narrowly scoped failure reporting is included because the issue explicitly asks to make the observed failure legible; architecture review determines the reachable diagnostic surface without broadening classification.
- Existing GitHub story context: none supplied; this feature proposes new scope only and does not infer existing story identities or revisions.
- Questions for owner: none; headless planning proceeds under the recorded assumptions.
