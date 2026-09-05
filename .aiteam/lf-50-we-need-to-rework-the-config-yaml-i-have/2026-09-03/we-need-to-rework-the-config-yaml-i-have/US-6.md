# US-6 — Mode-selected permissions

> As an operator, I want permissions selected from the run mode so that issue labels cannot independently widen an agent's authority.

This story replaces tool-profile label selection with deterministic automatic profile selection.

## Why This Matters

Permissions should follow the requested workflow step, not a separately applied label. Removing that second control makes authority easier to reason about, but stale labels must fail visibly rather than being ignored or retaining old behavior.

## Architecture Notes

- The daemon selects the canonical profile only after agent and mode resolution, then passes copied agent-specific permissions into the turn request.
- Platform: preserve SEC-5 sanitization, immutable/copy boundaries under CONC-1, and daemon ownership of orchestration; no runtime missing-profile fallback is allowed.
- Risk: old grant paths or raw label text must not survive in refusal, logging, metadata, or inferred-mode flows.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Replace label-resolved profiles with deterministic mode-to-profile selection | logic | T-4.2, T-7.1 | L |
| T-6.2 | Refuse every detected legacy tools-profile label with safe guidance | logic | T-6.1 | M |
| T-6.3 | Cover explicit/inferred selection, Spike mapping, refusals, logs, and metadata | test | T-6.1, T-6.2 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | Permissions are selected automatically only after both mode and agent are resolved. |
| AC-6.2 | Research, Plan, Implement, and Validate select their same-named profiles; Spike selects Implement while remaining recorded as Spike. |
| AC-6.3 | Every case-insensitive `lf:tools=` label refuses before dispatch with actionable sanitized guidance. |
| AC-6.4 | Unknown classification or missing selection never falls back to a broader profile. |
| AC-6.5 | Selected copied permissions are reflected consistently in runner input, logs, and metadata. |

