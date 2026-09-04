# US-6 — Attachment routing coverage

> As an operator, I want attachment access proven across every mode and agent so ingestion matches configuration.

This story verifies daemon mode/profile resolution into turn requests.

## Why This Matters

Turn tests prove the boolean gate but not that the daemon chose the correct profile. Both agents and Spike need coverage without real downloads.

## Architecture Notes

- Extend daemon fixtures and capture resolved requests; do not duplicate ingestion tests.
- Platform: use quiescence/rendezvous under TEST-8/9 and preserve copied payloads under CONC-1.
- Risk: assert run mode, profile, attachment value, and an explicit override.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Cover attachment routing for every mode and both agents | test | T-1.1, T-2.1, T-3.1 | M |
| T-6.2 | Preserve permission, Validate, and legacy-label coverage | test | T-5.1, T-6.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | Five modes are covered for Claude and Codex. |
| AC-6.2 | Research/Plan enable; Implement/Validate disable; Spike stays Spike and disables. |
| AC-6.3 | An explicit override reaches the request unchanged. |
| AC-6.4 | Payload and legacy-label dispatch absence are asserted without real downloads/runners. |

