# US-2 — Read-only Validate coverage

> As an operator, I want validation runs constrained to non-mutating authority so that checking work cannot alter it.

This story locks the reviewed Validate security boundary into tests for both agents.

## Why This Matters

Claude's ambient permission rules are additive, so exposing its shell cannot be made safely read-only through an allow-list. Exact rejection coverage is necessary to prevent a later cleanup from reopening shell or editing access.

## Architecture Notes

- Put configuration-legality tables in `internal/config`; add adapter tests only where a runner-local guard is a second boundary.
- Platform: preserve SEC-1 and SEC-3; assert exact membership rather than only tool counts.
- Risk: do not probe live agent binaries or change their external command contracts.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Add Claude and Codex Validate-policy acceptance and rejection coverage | test | — | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | Claude Validate accepts exactly Read, Glob, Grep, and Task as exposed tools. |
| AC-2.2 | Claude Validate rejects Bash, Edit, Write, and non-read-only permission rules with mode, agent, and setting context. |
| AC-2.3 | Codex Validate accepts `read-only` and rejects `workspace-write`. |
| AC-2.4 | Existing runner-local security guards remain covered where applicable. |

