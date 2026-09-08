# US-4 — Supply approved story revisions on every execution

> As an implementation agent, I want the selected GitHub story context on execution and resume so that work follows the approved scope.

Deliver the exact approved story bodies and source references to both agent workflows on every execution path.

## Why This Matters
A correct plan is ineffective if a resumed agent sees stale files or a newly edited issue instead. Every execution needs the same approved revision and enough parent context to apply it. Changes in relationships must be detected before a later attempt starts.

## Architecture Notes
- **T-4.1 — Assemble selected issue revisions into execution context:** internal/turn/prepare.go; internal/harness/prompt.go; internal/agent; dependencies: T-3.2. Load immutable approved story bundle, dependency context and parent identity. Revalidate live relationships before dispatch without substituting edited bodies. Both runners consume the same stdin prompt and no runner gains GitHub access.
- **T-4.2 — Refresh revision context across resume and fallback paths:** internal/turn/harvest.go; internal/harness/prompt.go; dependencies: T-4.1. Explicitly repeat selected revisions for resume and cold-after-resume-refusal; retain generation on retries. Report body drift, block detached/deleted relationships, and never consult legacy story files as fallback.
- **T-4.3 — Update both implementation and specialist input workflows:** .claude/agents; .codex/skills; .claude/modes; AGENTS.md and CLAUDE.md relevant contracts; .aiteam/APP_CONTEXT.md and relevant README/docs; dependencies: T-4.1, T-4.2. Orchestrator assigns each specialist its selected GitHub-derived story/revision. Add per-story review/test checkpoints as each story finishes, producing incremental evidence before sibling completion; the current aggregate Gate A/B ordering cannot be its only validation path. Coordinate overlapping work so checks use stable snapshots and revise affected completion if later shared changes invalidate it. Preserve final feature-wide checks, mode-header coordination (STYLE-8) and tool boundaries. Align the execution/review/test documentation and current context with issue-derived inputs, freeze/readiness rules and scoped validation; cover only the new workflow, with no legacy conversion or cleanup.
- **T-4.4 — Test cold, resumed and fallback revision parity:** internal/turn/resume_test.go; internal/harness prompt tests; internal/codex runner fixtures; dependencies: T-4.1, T-4.2, T-4.3. Prove changed live bodies and stale conversation history cannot replace approved data on either runner. Exercise relationships changing between attempts, hostile source text and context collection isolation; no real coding-agent subprocess.
- Risk: stale conversation state is not authoritative; explicitly supply selected revision context even when resuming.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Assemble selected issue revisions into execution context | logic | T-3.2 | M |
| T-4.2 | Refresh revision context across resume and fallback paths | logic | T-4.1 | M |
| T-4.3 | Update both implementation and specialist input workflows | logic | T-4.1, T-4.2 | M |
| T-4.4 | Test cold, resumed and fallback revision parity | test | T-4.1, T-4.2, T-4.3 | M |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-4.1 | Cold execution receives parent/run/generation identity and complete selected story bodies, source links, revision identities and dependency context through the shared trusted prompt. |
| AC-4.2 | Resume and cold fallback after resume refusal explicitly receive that same approved bundle; changed live bodies and stale conversation history cannot replace it. |
| AC-4.3 | Before each later dispatch/resume, missing, detached, moved or invalid story relationships block launch with an actionable report; a running attempt retains its snapshot and reports detected drift. |
| AC-4.4 | Neither runner fetches its own alternative specification, gains forge credentials, nor falls back to legacy story files; source content is delimited as untrusted data. |
| AC-4.5 | Both implementation orchestrators and implementation/review/test specialists consume the selected issue-derived revisions; their mirrored instructions schedule story-scoped review/tests and incremental verification before sibling completion, retain final aggregate validation, and remain aligned. Current context and relevant user documentation describe the same new workflow. |
| AC-4.6 | Fake-runner tests cover Claude and Codex cold/resume/fallback paths, hostile source data and copied context collections without launching live agents. |
