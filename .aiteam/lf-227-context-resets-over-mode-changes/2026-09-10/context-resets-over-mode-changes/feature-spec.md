# Feature Spec: context-resets-over-mode-changes
# Status: APPROVED
# Date: 2026-09-12

## Goal
- Start each admitted new mode with a fresh coding-agent conversation and direction to reuse the task's existing written work.

## Decisions and Assumptions
| Kind | Statement | Basis |
|---|---|---|
| Owner scope | Replace the oversized seven-story direction with existing conversation rotation plus an artifact-directory pointer. | Run 0004 simplicity request and run 0005 “Ok, re-plan this!” |
| Retained decision | Reset on admitted mode changes, including returns to earlier modes, for Claude and Codex with and without external team delivery. | Current RESEARCH_BRIEF.md and previously accepted recommendations. |
| Retained decision | Preserve bounded issue-comment history and existing admission/approval/evidence gates; incomplete research alone does not prohibit Plan. | Current research's retained decisions. |
| Assumption | Same-mode replies remain resumable under existing eligibility rules; existing independent reset reasons and interrupted recovery remain valid. | Existing segments and recovery contracts summarized by research. |
| Assumption | Reset native conversation history only; retain the task, worktree, existing artifact files, frozen stories and retained evidence. | Original goal and narrowed research recommendation. |
| Assumption | A supplied directory and read-brief instruction are sufficient scope; arbitrary model comprehension and recovery of unwritten reasoning are not guaranteed. | Research explicitly rejects generalized handoff machinery. |

## User Stories
| ID | Story |
|---|---|
| US-1 | As the owner, I want an admitted mode change to start a fresh conversation that can read existing task briefs, so stale conversation context does not accumulate across phases. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Existing run | Mode and launch/outcome evidence | Existing stored fields | Existing rules | Compare actual conversation use, not merely a rejected or prelaunch attempt. |
| Existing conversation segment | Ordinal and session identity | Existing stored fields | Existing rules | Allocate a new segment for a changed mode; never resurrect an earlier mode's conversation. |
| Prompt context | Task artifact directory | Optional path | When safely resolved | Reuse existing rooted discovery and known Plan session; no persisted binding or new schema. |

### Relationships
- Existing runs and segments remain attached to the existing task.
- Existing artifacts remain in the existing session directory across date changes and conversation resets.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|---|---|---|---|
| Existing runner preparation | Select conversation segment | Existing admission | Apply mode-change reset consistently across delivery configurations. |
| Existing common prompt | Supply artifact path and read instruction | Existing worktree read access | Read relevant existing brief/context; frozen factory inputs remain authoritative. |
- No new command, configuration setting, permission, endpoint or database migration.

## Business Rules
- Compare the admitted mode against the previous actually used conversation mode using existing recorded evidence.
- Research → Plan → Research produces successive new conversations; ordinary replies in a mode can resume the latest eligible conversation.
- Preserve external-delivery identity changes as existing reset reasons.
- Render a known task-session directory and short instruction to read relevant existing work in the shared prompt used by both adapters.
- Prefer an established identity-bound Plan session; otherwise reuse existing rooted discovery for this task's MANIFEST/phase artifacts, including earlier dates.
- Missing artifacts do not invent a handoff or force a completed outgoing phase; preserve ordinary setup and existing prerequisite rules.

## Edge Cases & Error Handling
- First run → retain existing cold-start behavior.
- Refused or prelaunch attempt → does not change the last actually used mode or consume a populated conversation as a fresh one.
- Interrupted automatic recovery → preserve saved-segment rules; do not rotate solely because recovery occurs.
- Multiple conflicting session candidates without an authoritative selection → disclose ambiguity; do not claim continuity or arbitrarily select another task's artifacts.
- Path outside the task worktree or unsafe resolution → reject the candidate using existing rooted checks.
- Missing brief → use available inputs and ordinary setup; missing required approved scope/evidence still follows existing gates.

## Out of Scope
- New persisted artifact binding, schema, epochs, migration or mode-state subsystem.
- Generalized handoff bundles, automatic transcript summaries or complete validation-feedback transport.
- Shared/installed instruction-tree rollout, cross-tree parity project or new authentic-agent observation gate.
- Guaranteed model comprehension, recovery of unwritten reasoning, token-saving measurements or live nested-agent launches.
- Implementation during Plan or local retirement of factory-managed stories outside the supplied submission protocol.

## Acceptance Criteria
- [ ] AC-101: Preparation of a newly admitted different-mode run selects a distinct conversation identity and cold invocation for both adapters, with and without external team delivery, including Research → Plan → Research.
- [ ] AC-102: Same-mode eligible replies retain resume behavior; first-run, independent delivery resets, refused/prelaunch attempts and saved-segment interrupted recovery retain established behavior without accidental reuse of a populated old conversation as fresh.
- [ ] AC-103: When an existing task-session directory is safely resolved, the common prompt for both adapters includes that directory and a short instruction to read relevant existing brief/context, including reuse across dates and fresh conversations.
- [ ] AC-104: Missing, conflicting and unsafe artifact candidates follow the documented edge-case behavior; neither incorrect continuity nor a new mandatory completed-phase gate is introduced.
- [ ] AC-105: Existing bounded comments, authoritative frozen implementation inputs and retained validation inputs continue to reach their established prompt paths; reset does not replace the task/worktree or alter admission/approval rules.

## Candidate Evidence Obligations
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-101 | Offline segment/request and adapter command-argument assertions. | Existing delivery/mode-transition tests and harness/Codex runner tests; focused extensions required. | Repository-selected identity and invocation contract at implemented revision; no live-provider or comprehension claim. | Implement and retain passing regressions. |
| AC-102 | Offline run-history and saved-recovery fixtures. | Existing delivery, prepare and continuation tests. | Controlled recorded states, retaining actual-launch distinctions. | Add focused regressions and run applicable existing checks. |
| AC-103 | Offline filesystem and shared-prompt assertions. | Existing sessiondir, plan-context and prompt test infrastructure. | Fixture paths, dates and prompt bytes only. | Add cases proving both adapter request paths carry the pointer. |
| AC-104 | Offline missing/conflicting/unsafe path fixtures and source review. | Existing rooted discovery helpers and tests. | Controlled filesystem cases; no new persistence contract. | Add changed selection/error cases. |
| AC-105 | Existing gate/input regression tests and focused prompt review. | Approved-context, validation, mode-transition and prompt sources/tests. | Repository input/admission behavior; bounded comments do not promise all historical feedback. | Run relevant regressions and required make check during implementation. |

## Readiness
- Owner questions: none; narrowed outcome, scope and success criteria are settled.
- Ready for architecture and typed authoring as one bounded slice; factory reconciliation of superseded unfinished published scope remains necessary before implementation.
- This replaces the earlier broad local spec; it does not claim previous factory obligations are retired or accepted.
