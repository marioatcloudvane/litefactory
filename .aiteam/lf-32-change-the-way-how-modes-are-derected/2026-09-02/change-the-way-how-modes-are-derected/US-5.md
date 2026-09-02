# US-5 — Integrate routing and retire agent-side inference

> As an issue owner, I want labels, commands, and plain-language requests to enter one predictable routing workflow so that the selected mode is visible and enforced.

This story wires the complete decision ladder into daemon execution and removes the old inference prompt.

## Why This Matters

The individual primitives only become useful when every trigger passes through
one ordered decision. Compatibility commands, manually selected labels, and
automatic classification must agree on one result and one failure policy. The
final integration also removes the extra routing responsibility from the agent
that performs the work.

## Architecture Notes

- Resolve mode after authorization and command parsing but before tools,
  worktree preparation, or ordinary run allocation. Keep existing agent-label
  refusal precedence explicit and document the new combined ladder.
- A valid label bypasses classification. An absent declaration reuses a durable
  selection or invokes the default classifier once, persists it, and applies
  the canonical label before dispatch.
- Matching slash command and label proceed; disagreement draws a sanitized
  refusal and consumes according to existing reply ordering without starting a
  run.
- After report and watermark commit, schedule/attempt durable label cleanup;
  cleanup failures defer subsequent triggers for that session rather than
  changing the completed run outcome.
- Remove `harness.InferMode`, infer guidance, the infer ask-policy seam, and
  free-text mode derivation only after all concrete paths are wired.
- Platform: worker slots, per-session in-flight protection, rate-limit backoff,
  report-before-watermark ordering, and deterministic repository fairness remain
  the daemon's only timing model.
- Risk: multiple eligible comments currently share one tick's issue-label
  snapshot. Stop after one mode-bearing trigger per session and refresh the
  issue after cleanup before deciding the next, so a one-shot label is never
  applied to a batch.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T5.1 | Add the pure mode-label resolver and sanitized refusal replies | Code | — | M |
| T5.2 | Integrate command, label, durable-selection, and classifier precedence in the worker | Code | T5.1 | L |
| T5.3 | Wire report completion to cleanup and enforce one refreshed trigger per selection | Code | T5.2 | M |
| T5.4 | Remove infer-mode prompt and request behavior from ordinary agents | Code | T5.3 | S |
| T5.5 | Add end-to-end daemon fixtures for manual, inferred, conflicting, failed, restarted, and transitioned flows | Test | T5.4 | L |
| T5.6 | Update current package and configuration documentation to describe litefactory-owned routing | Docs | T5.5 | S |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC5.1 | One valid canonical mode label starts a run in that mode without invoking a classifier. |
| AC5.2 | With no mode declaration, the configured default agent classifies once, the canonical label is applied, and the work run starts with the concrete result. |
| AC5.3 | Ambiguous, malformed, unknown, or conflicting declarations produce a sanitized reply and no worktree or run row. |
| AC5.4 | A completed run's label is removed durably, and the next unlabelled owner comment is classified afresh. |
| AC5.5 | A work-agent prompt never contains `Mode: infer`, inference guidance, or permission for the work agent to change mode. |
| AC5.6 | Multiple comments discovered together cannot inherit one one-shot selection; each later trigger is decided from a refreshed issue snapshot. |
| AC5.7 | Rate limits and failures at classification, label apply, reporting, and cleanup leave observable retryable state without duplicate work or advanced watermarks. |
| AC5.8 | `go build ./...`, `gofmt -l .`, and `make check` pass with no new dependency or integration-test convention. |
