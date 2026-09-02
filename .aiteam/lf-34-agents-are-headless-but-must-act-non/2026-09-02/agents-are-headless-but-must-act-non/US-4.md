# US-4 — Implement Orchestrator is autonomous and environment-driven

> As a feature owner, I want implementation to proceed autonomously while surfacing only severe blockers so that headless runs remain productive and safe.

This updates both Implement Orchestrators, including environment setup, as one owned unit.

## Why This Matters
Implementation cannot conduct live dialogue, yet current instructions imply it can. The challenge is separating ordinary uncertainty from severe blockers while retaining test-environment safety.

## Architecture Notes
- Severe means material plan change, unsafe/destructive continuation, or an immovable gate.
- Final escalation reports location, trigger, completed work, needed decision, and options; the worktree stays intact.
- Phase 2 consumes `TEST_BASE_URL` and `TEST_DATABASE_URL`; missing values skip, safe complete values proceed, production-shaped targets are refused.
- Risk: split ownership causes conflicts or weakens safeguards.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-4.1 | Add assumption and severe-escalation semantics to both definitions | logic | — | S |
| T-4.2 | Replace Phase 2 dialogue with environment-driven setup | infra | T-4.1 | S |
| T-4.3 | Validate triggers, checkpoints, reports, outcomes, and wrappers | test | T-4.2 | S |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-4.1 | Both definitions share severe triggers, checkpoints, escalation fields, and fallback. |
| AC-4.2 | Ordinary ambiguity is resolved and reported; severe blockers end without waiting or altering git state. |
| AC-4.3 | No interactive prompt or acknowledgement-dependent continuation remains. |
| AC-4.4 | Missing configuration is reported without guessed or secret values. |
| AC-4.5 | Production targets remain refused and no-dependency work still skips. |
| AC-4.6 | Tool-specific wrappers remain intact. |
