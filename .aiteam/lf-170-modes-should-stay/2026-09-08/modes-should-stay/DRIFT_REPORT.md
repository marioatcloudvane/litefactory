# Drift Report — modes-should-stay
# Branch: lf/170-modes-should-stay | Date: 2026-09-09

## Delivered as planned
- US-001: persistent selected labels and journal retirement across successful/failed reporting, startup and legacy recovery.
- US-002: explicit owner transitions, durable effect confirmation, retry pinning, owner-edit preservation and managed-scope precedence.
- US-003: strict attempt-bound discussion/completed outcome, identity-bound working context, publication/save readiness gates and rooted transient handling.
- US-004 runtime: default asynchronous Plan questions with explicit revocation, current/previous policy parity, identity-bound continuing context across dates and both runners' cold/resumed/fallback paths. Claude sources, session rules, CLAUDE.md, AGENTS.md and persistent APP docs updated.

## Added scope
- None. A numbered journal migration and a separate strict outcome contract were anticipated architectural choices.
- CODEX_SKILLS.patch preserves the exact required protected-copy edits as a reviewable recovery artifact; it does not count as applied implementation.

## Dropped or deferred
- US-004 AC-004-4 and AC-004-5 remain incomplete: the sandbox rejects writes to .codex/skills, leaving three Codex workflow skill copies unchanged. Required changes are in CODEX_SKILLS.patch and passed read-only git apply --check. Apply only in an authorized writable environment, then rerun parity review/tests and emit new verification.

## Architectural deviations
- No intended runtime deviations. The protected-copy gap is an environment limitation, not a change to approved scope. No permission bypass attempted.

## Verdict
DRIFTED — runtime implementation and writable documentation are delivered, but required Codex workflow copies remain out of sync. No replan is needed; the remaining work requires authorized write access and revalidation. Final check results and event state are recorded in notes.md.
