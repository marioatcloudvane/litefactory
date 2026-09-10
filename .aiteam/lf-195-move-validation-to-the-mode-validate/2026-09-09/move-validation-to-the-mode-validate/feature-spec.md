# Feature Spec: move-validation-to-the-mode-validate
# Status: APPROVED FOR PLANNING
# Date: 2026-09-09

## Goal
- Separate implemented work from acceptance and final delivery: Implement finishes coding and tests without closing stories; owner-triggered Validate assesses acceptance and reviews, the factory records sign-off and closes accepted stories, and an approved pull-request merge precedes parent closure.

## Sources and Decisions

| Source | Binding interpretation |
|---|---|
| Research brief, runs 0001–0002 | Existing managed lifecycle, evidence, protected execution, forge ownership, and instruction inconsistencies inform scope; research recommendations are superseded by run 0003 answers. |
| Owner answer 1 | Never dispatch Validate or a return to Implement automatically; suggest a transition only on a firm signal such as complete implementation/test evidence or a concrete finding requiring a fix. |
| Owner answer 2 | Validate owns acceptance assessment, code review, architecture consistency, and comparison against the approved plan; advisory improvements do not block. |
| Owner answer 3 | Validate expects checks already executed by Implement; it never runs or commissions fresh tests. Missing required evidence is reported as unmet/unverified and can be explicitly waived. |
| Owner answer 4 | Later changes are new issues; no automatic reopening or expiry of accepted stories because code or main changes later. Evidence still identifies the implementation actually assessed. |
| Owner answer 5 | Explicit owner authority may waive any acceptance/review/test/readiness obligation individually or in bulk; retain reason, scope, actor, and original evidence outcome. |
| Owner answer 6 | Manual checkbox edits or manual issue closure alone do not authorize acceptance or waivers. |
| Owner answer 7 | Every attached child must be closed before normal parent readiness, including attached follow-ups; broader owner override remains available and documented. |
| Owner answer 8 | Propose follow-up issue contents; create only after explicit owner approval; creation alone does not waive the source obligation. |
| Owner answer 9 | Preserve normal task-branch persistence; propose a pull request into main and execute its merge only after explicit owner approval. |
| Owner answer 10 | Close parent only after confirmed successful integration into main. |
| Owner answer 11 | Apply new workflow only to newly approved features; existing active generations retain existing semantics, and previously closed stories remain closed. |
| Owner harness note | Independent implementation stories for `.claude/**` and `.codex/**`; each uses the other harness and never requires edits to both protected trees in one story. Manual switching is supported and must not deadlock dependencies. |

## User Stories

| ID | Story |
|---|---|
| CAP-1 | As the owner, I want implemented stories and their dependencies to progress without acceptance or GitHub closure so engineering can finish before independent review. |
| CAP-2 | As the owner, I want to invoke Validate on frozen approved stories and retained implementation/test evidence so sign-off reflects the agreed scope. |
| CAP-3 | As the owner, I want criterion-specific results, review findings, visible checkmarks, and automatic closure after acceptance so I can see what was accepted and why. |
| CAP-4 | As the owner, I want explicit exception commands and approved linked follow-ups so irrelevant requirements do not block delivery and deferred work stays documented. |
| CAP-5 | As the owner, I want readiness across all children followed by an approved PR merge and parent closure only after success so acceptance and delivery remain distinct. |
| CAP-6 | As an operator, I want existing features to keep their workflow and interrupted/retried new flows to recover without duplicate effects. |
| CAP-7 | As an operator switching harnesses, I want independently deliverable shared, Claude, and Codex instruction changes with bounded file ownership so protected files do not deadlock one story. |

## Data Model

| Entity | Fields | Constraints |
|---|---|---|
| Feature workflow | Parent numeric identity, approved generation, workflow version, implementation completion, acceptance readiness, delivery status | Version selected at feature approval; legacy active features keep their version. |
| Story implementation | Numeric issue identity, frozen title/body revision, dependency keys, implementation snapshot, test outcomes and retained evidence, engineering status | Engineering completion is distinct from acceptance and GitHub closure; dependencies can unblock at engineering completion. |
| Acceptance decision | Story/revision/snapshot, criterion identifier, satisfied/unmet/unverified/waived outcome, proof reference, review findings | Complete criterion accounting; waived never means a test passed; previously accepted decisions do not expire automatically. |
| Owner exception | Owner comment identity, actor, reason, selected scope, expanded criterion/story/readiness decisions | Explicit trusted owner action only; idempotent application; retain original unmet evidence and visible notes. |
| Follow-up proposal | Source story/criterion/finding, full proposed title/body, approval comment, created issue identity/link | Approval precedes creation; stable retry identity; link separately by default, attach only if explicitly requested. |
| Delivery proposal | Parent/generation, readiness report, task branch/head, target main, PR identity/head, approval comment, merge/closure state | Approved proposal identifies concrete reviewable content; parent closure follows verified merge success; changed delivery head needs fresh delivery approval, not automatic reopening of story acceptance. |
| Remote effect | Stable action identity, target numeric identity, payload/revision guards, status and recovery marker | Record intent before forge changes; recover lost responses and retries without duplicate comments/issues/PRs. |

### Relationships
- Feature → immutable approved story revisions → implementation evidence → criterion decisions.
- Owner exception → explicit accepted scope and original findings; follow-up → source obligation and approval.
- Parent → all attached children for readiness; linked-only follow-ups are outside this attachment relationship.
- Delivery approval → concrete PR/head; successful integration → eligible parent-close effect.

## API / Interface Changes

| Action | Authority | Behavior |
|---|---|---|
| Existing Implement dispatch | Owner; factory dispatch | Supply frozen approved scope; record implementation and executed checks; stop without closing children or marking acceptance. |
| Existing `/validate <instruction>` dispatch | Owner; factory dispatch | Admit approved managed scope; provide immutable approved stories plus retained implementation/check evidence on cold, resumed, and fallback runs. |
| Structured Validate result | Read-only agent; factory ingestion | Return complete criterion outcomes and review findings through a protected output channel without repository writes or test execution. |
| Explicit exception control | Owner comment | Select criteria, stories, or all remaining scope with a reason; factory applies documented waiver decisions and corresponding projections. |
| Follow-up approval | Owner comment | Approve concrete proposed content and optional attachment; factory creates and links exactly once. |
| Delivery approval | Owner comment | Approve concrete PR integration into main and subsequent closure; factory performs and verifies effects. |
| Status comments | Factory | Distinguish implemented, accepted, waived, unresolved, awaiting approval, delivery failure, and integrated/closed states. |

## Business Rules
- Implement retains coding, environment setup necessary for checks, test writing/execution, fixes, and aggregate engineering gates; Validate owns review and acceptance, and never changes code.
- New engineering evidence cannot automatically tick acceptance boxes, close stories, or claim feature delivery.
- Validate must account for every frozen criterion and required review area using retained evidence; missing tests remain unverified until an explicit owner waiver or a later Implement run supplies evidence.
- Factory ticks satisfied or explicitly waived criteria, labels waived criteria visibly, preserves unrelated body content, and closes each fully accepted story without waiting for sibling acceptance.
- Body projection distinguishes factory checkbox changes from changes to approved requirements; conflicting live edits are reported without overwriting them or substituting them for frozen scope.
- All attached children participate in normal readiness, including children not in the managed approved story set; closed state alone never fabricates criterion evidence or owner exceptions for managed stories.
- Explicit owner waivers can resolve any substantive acceptance/readiness blocker; they do not grant agents forge credentials, bypass identity checks, pretend operations succeeded, or authorize a merge implicitly.
- Missing authentic evidence remains explicitly unverified even when its obligation is waived; a waiver records accepted risk rather than proven capability.
- Final integration uses a PR into main; retain branch persistence and require concrete owner approval before merging. Never close the parent merely because an agent reports success.
- Validation and final delivery actions remain separate from an automatic mode chain; suggestions require concrete supporting evidence.
- Follow-ups default to linked issues outside the parent attachment tree, preserving deferred work without silently changing the all-attached-children rule.
- Each protected instruction story completes its own changes and verification with the permitted other harness; no cross-protected-tree edit or mutually dependent parity requirement inside either story. Final overall consistency is an aggregate gate after both independent stories.

## Edge Cases & Error Handling
- Duplicate delivery, response loss, crash, or restart → replay journaled intent and reconcile numeric remote identity before another effect; no duplicate sign-off, follow-up, PR, or closure.
- Missing/stale/incorrectly attributed implementation evidence at initial validation → unverified finding, no fresh checks launched by Validate, no fabricated pass.
- Later implementation changes after acceptance → new work tracked in new issues; retain historical signed snapshot and do not reopen automatically.
- Task-branch head changes after delivery proposal → require approval of the current PR/head without revoking historical story acceptance.
- Unrelated user body edits during acceptance projection → preserve edits; refuse ambiguous projection and report recoverable conflict.
- Third-party comments, manual ticks, or manual closure → no waiver authority; report inconsistent managed acceptance when relevant.
- Required child remains open → report exact child and block normal readiness unless explicit documented owner override covers it.
- PR conflict, check restriction, denied merge, or transient API failure → parent remains open with diagnostic and resumable pending delivery state; owner waivers cannot fabricate integration success.
- Legacy pending close effects at rollout → use original generation workflow semantics; do not reinterpret or reopen historical work.
- Harness cannot edit assigned protected tree → report exact harness requirement and keep other independently ready stories selectable for a manual switch.

## Out of Scope
- Automatic Implement/Validate cycling, test execution in Validate, and automatic expiry or reopening of accepted work after later changes.
- Retroactive migration/revalidation of active legacy features or already closed stories.
- Direct push into main, automatic approval to merge, and automatic follow-up creation without owner approval.
- Broadening agent filesystem permissions, granting forge credentials to agents, or unsupported live nested Codex observation.
- A new maintenance mode or mutating context-document update stage inside read-only Validate.

## Acceptance Criteria
- [ ] AC-SPEC-1: A newly approved feature can finish all implementation stories and required engineering checks while child issues remain open and dependent implementation work progresses.
- [ ] AC-SPEC-2: Owner-triggered managed Validate receives frozen approved revisions and retained check evidence on every runner path, performs no test execution/code edits, and reports every criterion plus code, architecture, and plan review outcomes.
- [ ] AC-SPEC-3: The factory visibly marks accepted criteria, distinguishes waived from proven outcomes, preserves unrelated body text, and closes each fully accepted story with recoverable ordered effects.
- [ ] AC-SPEC-4: Explicit owner comments can waive any criterion, story, or remaining acceptance/readiness scope with a reason; manual ticks/closures and untrusted comments cannot confer this authority.
- [ ] AC-SPEC-5: Approved follow-ups are created once with source links; creation does not waive requirements and attached follow-ups participate in normal all-child readiness.
- [ ] AC-SPEC-6: Normal parent readiness requires all attached children closed; concrete owner-approved PR integration into main is verified before parent closure, and failures keep the parent open.
- [ ] AC-SPEC-7: Later changes do not automatically reopen accepted stories; delivery approval remains tied to the concrete PR/head being merged.
- [ ] AC-SPEC-8: Legacy active generations and pending effects retain prior behavior while newly approved features use the separated workflow; retries and restarts do not duplicate effects.
- [ ] AC-SPEC-9: Shared instructions and independent `.claude`/`.codex` stories express the same lifecycle without a story requiring writes to both protected trees or depending cyclically on the other harness story.

## Candidate Evidence Inventory

| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-SPEC-1 | Store/turn state transition and dependency tests with retained engineering evidence | Existing `internal/store`, `internal/turn`, and progress tests; synthetic fixtures | New lifecycle behavior; offline state-machine claims only | Implementation and executed regression evidence required. |
| AC-SPEC-2 | Runner prompt/result contract tests, read-only profile assertions, and scoped instruction review | Existing `internal/harness`, `internal/codex`, config profiles, frozen bundle contracts | Both supported adapters; synthetic output verifies parsing/authorization, not live model behavior | New contract tests and review required; no authentic live-agent behavior claim planned. |
| AC-SPEC-3 | Parser/projection, remote-effect, identity and recovery tests | Existing `internal/planbrief`, GitHub fixtures and story progress recovery tests | Offline deterministic body and effect semantics | Implement new cases and retain their executed results. |
| AC-SPEC-4 | Owner authorization/command tests and report snapshots | Existing command/owner admission and report patterns | Synthetic comments prove control handling, not authentic GitHub identity infrastructure | New waiver contracts and test results required. |
| AC-SPEC-5 | Fake forge response-loss/idempotency/link tests | Existing GitHub client and issue creation fixtures | Offline API adapter/state-machine behavior | Add follow-up creation/approval coverage; live operational success must be observed at runtime. |
| AC-SPEC-6 | Offline git fixtures, HTTP contract tests, and delivery recovery tests | `internal/gittest`, existing GitHub client fixtures; PR/merge functionality not yet present | Proves controlled integration behavior; does not claim a real hosted merge occurred during implementation | Implement merge API and success verification; actual feature delivery requires future owner approval and confirmed remote success. |
| AC-SPEC-7 | Acceptance persistence and stale-delivery-approval tests | Store and git fixture infrastructure | Historical acceptance versus concrete current delivery target | New behavior and executed tests required. |
| AC-SPEC-8 | Migration/replay compatibility and fault-injection regression tests | Existing SQL migrations and managed lifecycle tests | Legacy and new workflow versions in same upgraded database | New migration fixtures and recovery coverage required. |
| AC-SPEC-9 | Scoped file review and rendering/profile parity assertions | Checked-in shared team sources and `.claude`/`.codex` instructions | Proves published contract consistency, not live nested execution success | Separate permitted-harness edits and final consistency review required; no nested live capture prerequisite. |

## Assumptions
- Answer 6 rejects manual edits as acceptance authority and retains explicit owner comments with reasons, consistent with the recommendation's underlying control model.
- Answer 7's emphasis on all children selects every attached child; separately linked follow-ups remain possible, and the broad answer 5 still permits an explicit readiness exception.
- No new owner-facing command spelling is mandated; architecture may select bounded explicit comment controls consistent with existing parsing and authorization.
- Engineering completion still requires executed checks normally; unrestricted owner exceptions can accept unavailable or failing obligations with explicit documentation, without rewriting their factual outcomes.
- The proposed story capabilities above describe requirements, not final issue decomposition; the architect/product owner choose independent implementation boundaries and stable story identifiers.
