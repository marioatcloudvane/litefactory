# US-2 — Providers hierarchy

> As an operator, I want GitHub grouped under providers so that future source-control services have an obvious home.

This story relocates GitHub configuration while retaining its current endpoint and credential behavior.

## Why This Matters

GitHub is currently treated as a unique root concern even though other providers may arrive later. The new hierarchy creates room for them without prematurely building abstractions or weakening credential safeguards.

## Architecture Notes

- Retain the concrete GitHub configuration and client boundary; do not create a generic provider interface or future-provider placeholders.
- Platform: credential references and recursive secret scanning remain governed by CFG-4, with existing URL and mutual-exclusion validation retained.
- Risk: fixtures and diagnostics must never expose actual credentials, and unsupported provider keys must remain strict failures.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Define `providers.github` and migrate GitHub consumers | data | T-1.1 | M |
| T-2.2 | Cover provider strictness, credentials, secret scanning, and runtime wiring | test | T-2.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | GitHub configuration is accepted only beneath `providers.github`. |
| AC-2.2 | Legacy top-level GitHub and unsupported provider keys fail with actionable errors. |
| AC-2.3 | API endpoint validation, credential-source mutual exclusion, and credential resolution remain unchanged. |
| AC-2.4 | Literal-secret detection covers all relocated provider strings without echoing secret content. |
| AC-2.5 | No abstraction or implementation for GitLab, Bitbucket, or another provider is introduced. |

