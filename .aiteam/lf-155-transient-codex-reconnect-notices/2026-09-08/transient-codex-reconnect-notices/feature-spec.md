# Feature Spec: transient-codex-reconnect-notices
# Status: APPROVED
# Date: 2026-09-08

## Goal
- Let owners retain normal successful-run publication when Codex recovers from stream errors, with accurate failure reasons for genuinely unsuccessful runs.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As a repository owner, I want recovered Codex runs classified from their terminal and process evidence so that completed work follows normal commit/push handling and genuine failures retain their safeguards. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Codex stream evidence | Completion and explicit terminal failure | Existing runtime state | Yes | Generic error events do not establish terminal failure; explicit failure remains sticky. |
| Codex stream evidence | Agent final message | Existing text | When emitted | Diagnostic notices must not replace final response content. |
| Codex terminal failure | Failure detail | Optional provider text | No | Support the documented nested error.message shape; missing or unusable text has a meaningful fallback. |
| Agent result | Outcome, exit code, final text, error | Existing contract | Existing requirements | No public result fields, persisted schema, or outcome enumeration additions. |

### Relationships
- Existing Codex JSONL evidence and process result → existing agent result → existing turn persistence and harvest behavior.
- Existing generic error events → existing raw run logs; no additional warning delivery surface.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Existing runner | Run | Unchanged | Correct normalization of completion, diagnostic errors, explicit failure and exit status. |
| Existing structured consumers | Classify, ProposeSchedule, ProposeConflictResolution | Unchanged | Recoverable notices must not invalidate otherwise valid terminal evidence or replace parseable final payloads. |
| Existing run reporting | Final text and failure reason | Unchanged | Preserve final summary independently from provider diagnostics and retain existing outbound redaction. |

## Business Rules
- A run succeeds only with process exit zero, completion evidence, and no explicit terminal failure; generic error events alone do not prevent success.
- Event type semantics determine diagnostic handling; reconnect-message spelling, retry count, and certificate wording are irrelevant to classification.
- Explicit turn.failed is unsuccessful even with exit zero or a completion event in either order.
- Explicit terminal failure with exit zero produces failed rather than missing-result classification; terminal detail is used when available and an accurate fallback otherwise.
- Zero exit without completion or explicit terminal failure remains no_result; never describe a zero-exit abnormal outcome as a nonzero process failure.
- Preserve existing timeout/cancellation precedence and outcomes, including existing completed-success precedence in a context race.
- Nonzero exit remains unsuccessful even after completion; generic diagnostics cannot override process or terminal failure evidence.
- Preserve final agent response text when diagnostics occur before or after it; retain raw diagnostic records in existing logs.
- Keep existing unsuccessful-run commit/push prevention, local worktree retention, and successful-run publication behavior.

## Edge Cases & Error Handling
- One or multiple generic errors followed by completion and exit zero → success when no explicit failure occurred.
- Generic errors with wording other than reconnect notices → same diagnostic treatment.
- Diagnostic after final text or a structured JSON payload → original final content stays usable.
- Generic errors followed by exit zero without completion → no_result with a missing-completion explanation.
- Explicit terminal failure with nested, top-level, missing, or malformed optional detail → unsuccessful with a meaningful explanation and no panic.
- Explicit failure and completion in either order → unsuccessful; do not clear genuine failure on completion.
- Completion followed by nonzero process exit → failed with a truthful nonzero-exit explanation.
- Timeout or caller cancellation without successful terminal/process evidence → existing timed_out or cancelled outcome.
- Malformed JSONL or unrelated event types → existing parser behavior remains compatible.

## Assumptions and Provenance
- From RESEARCH_BRIEF.md: generic error is used for retry diagnostics; terminal failure must be distinguished; final-message overwrite and contradictory zero-exit reporting share the affected branches.
- From APP_CONTEXT.md: internal/codex owns this normalization; internal/agent and internal/turn already provide the required result and publication mechanisms.
- Assumed for this first fix: keep diagnostics in existing logs only; no warning API/UI is needed.
- Assumed from the research recommendation: explicit terminal failure remains sticky, including contradictory terminal streams; no supported legitimate contrary sequence was established.
- Assumed from the research recommendation: include meaningful explicit-failure details and generic fallback without exposing a new outbound channel.
- Historical incident binary version is unknown; versioned upstream evidence and synthetic fixtures support this change without requiring access to historical runtime state. Record supported version evidence available during implementation and do not claim incident-version verification.

## Out of Scope
- TLS trust, certificate, proxy, VPN, or environment-policy changes.
- Additional retries, reconnect loops, retry-budget changes, or process lifecycle redesign.
- Recovery, modification, committing, or pushing of historical task worktrees.
- Relaxing harvest success gates or changing persisted success predicates.
- New warning fields, reporting UI, CLI options, packages, database migrations, or public contracts.
- Multi-turn terminal-state redesign or clearing explicit failure based on later completion.

## Acceptance Criteria
- [ ] AC-1: Synthetic streams containing one or multiple generic error notices, an agent final message, turn.completed, and exit zero succeed; cover both reconnect wording and unrelated diagnostic wording without message-prefix classification.
- [ ] AC-2: Generic diagnostics before and after the final agent message preserve that message exactly and remain present in the existing raw run log.
- [ ] AC-3: Exit zero with diagnostics but no completion and no explicit terminal failure yields no_result with a missing-completion explanation; zero-exit abnormal cases never report coding agent exited with status 0.
- [ ] AC-4: Explicit turn.failed produces failure with exit zero and nonzero; nested provider error detail is handled when present, missing or malformed optional detail yields a meaningful fallback, and completion before or after explicit failure does not restore success.
- [ ] AC-5: Completion with nonzero process exit remains failed; timeout, cancellation, and established completed-success/context-race precedence retain their existing outcomes under deterministic checks.
- [ ] AC-6: Classify, ProposeSchedule, and ProposeConflictResolution accept otherwise valid final payloads after recoverable diagnostics, preserve payloads when diagnostics follow them, and continue rejecting explicit failure, absent completion, and nonzero exit under their existing contracts.
- [ ] AC-7: Existing harvest verification continues to prove successful publication eligibility and unsuccessful-run commit/push prevention with retained local work; no success gate or shared result contract is relaxed.
- [ ] AC-8: Focused deterministic Codex regression tests and required aggregate make check pass; implementation evidence identifies the versioned protocol fixture source and does not claim an unverified historical Codex version.
