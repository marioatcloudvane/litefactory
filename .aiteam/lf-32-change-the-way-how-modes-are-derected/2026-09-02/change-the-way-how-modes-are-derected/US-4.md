# US-4 — Persist and mutate one-run mode labels safely

> As an operator, I want mode labels to reflect the selected run without becoming stale so that the issue remains understandable and future comments route correctly.

This story makes selection and cleanup durable and adds targeted GitHub label operations.

## Why This Matters

A label write and a run report cannot share one transaction, and the process can
stop between them. Without durable state, retrying can classify twice or a stale
label can silently control an unrelated later comment. Targeted mutations also
matter because replacing all labels could erase a maintainer's concurrent edits.

## Architecture Notes

- Add migration `0004` (or the next available number at implementation time)
  for a mode-selection record keyed by session and trigger, carrying mode,
  canonical label, source, apply state, and cleanup state.
- Keep every query and transaction in `internal/store`; accessors take context
  first and transactional promises use real SQLite tests.
- Add targeted add/remove methods to `internal/github` and only those methods to
  the daemon's consumption interface. Never edit the complete label collection.
- Gate later triggers for a session while cleanup is pending. Retry removing the
  exact recorded canonical label without rerunning the consumed instruction;
  removal of an already-absent label is success.
- Platform: retain the single SQLite connection and one migration transaction
  per embedded file.
- Risk: GitHub cannot distinguish a stale label from a human reapplying the same
  spelling. Cleanup removes the recorded label; a human wanting the same next
  mode may reapply it after cleanup or leave the next instruction unlabelled for
  classification.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T4.1 | Add the mode-selection migration, types, and transactional accessors | Code | — | M |
| T4.2 | Add targeted GitHub mode-label add and idempotent remove operations | Code | — | M |
| T4.3 | Add durable apply/cleanup transitions and pending-cleanup discovery support | Code | T4.1, T4.2 | M |
| T4.4 | Test migration, idempotency, restart recovery, API errors, and preservation of unrelated labels | Test | T4.3 | M |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC4.1 | One session/trigger pair has at most one durable concrete mode selection and repeated reads never require reclassification. |
| AC4.2 | Adding or removing a mode label changes no unrelated issue label. |
| AC4.3 | A label-add failure leaves no ordinary run and preserves enough state for retry. |
| AC4.4 | After a successful report and watermark commit, cleanup is retried independently and never reruns the consumed trigger. |
| AC4.5 | A session with pending cleanup cannot process its next trigger under the stale label. |
| AC4.6 | Restart at every apply/report/cleanup boundary converges without duplicate classification, duplicate work, or permanent stale state. |

