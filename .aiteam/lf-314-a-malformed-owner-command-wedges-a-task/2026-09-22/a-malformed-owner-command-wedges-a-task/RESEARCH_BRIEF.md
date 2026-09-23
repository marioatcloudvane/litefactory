# Research brief

## Request

Complete issue #314 shaping using the owner's accepted creation-only scope and exclusion of operator recovery tooling.

## Decisions

- **d1** — This shaping result settles buildable scope without implementing, deploying or repairing live state.
- **d2** — Classify creation intake as confirmed rejection, successful intake or retryable/uncertain failure without changing the global queue retry policy.
- **d3** — The primary user story is actionable rejection feedback followed by progress of the next valid owner comment without manual queue repair.
- **d4** — Owner accepted q1 option a: cover creation approval and preparation end to end, with other command families excluded.
- **d6** — Infrastructure failures, shutdown, unknown internal failures and uncertain persistence or remote effects remain unconsumed for existing retry or reconciliation.
- **d11** — Persist one rejection outcome per source comment with task/session and comment identity, command kind, reason category, exact safe correction text, stable delivery identity, remote delivery evidence and pending/delivered/consumed status.
- **d13** — Recover a recorded successful admission before fresh eligibility checks and finish consumption without duplicating intent, refreshing expiry, granting new authority or rejecting the already admitted command.
- **d14** — Preserve existing identity filtering, bot exclusion, mode restrictions, proposal binding, freshness, edit restrictions, syntax and limits.
- **d17** — Acceptance requires malformed approval and preparation followed by a valid instruction to yield one diagnostic each, no approval effect and eventual processing of the later instruction after confirmed reporting.
- **d19** — Owner accepted q2 option a: exclude operator recovery tooling, automatic skipping and new alerts, alongside global retry changes, proposal-generation changes and deployment.
- **d20** — Reject confirmed malformed syntax or payload, unsupported version, invalid payload fields or limits, missing/wrong/ambiguous/malformed mode selection and disabled or deliberately unavailable creation capability.
- **d21** — Reject confirmed absent, expired, superseded, mismatched or ineligible proposals and invalid owner controls only when recorded effects have been reconciled.
- **d22** — Classify errors by verified origin and recorded outcome rather than message text or a broad authority sentinel that can hide storage failures.
- **d23** — Use an issue reply as the only changed public surface, identifying the rejected command, safe reason and a concrete correction without publishing raw input, secrets or hidden proposal content.
- **d24** — Syntax guidance describes the existing exact approval grammar or preparation payload requirements without generating an executable approval command.
- **d25** — Mode guidance requests one Research selection, capability guidance explains required operator configuration, and proposal guidance requests a current eligible proposal followed by a fresh owner comment.
- **d26** — Once rejection is durably retained, recovery uses its original content and identity even if the source is edited or mode, configuration or proposal eligibility changes.
- **d27** — Confirm or reconcile diagnostic delivery before consumption, and recover failed consumption without posting again.
- **d28** — Reuse existing durable report delivery and recovery records for rejection reporting, retaining any missing logical outcome information within the creation intake and reporting scope.
- **d29** — Successful preparation retains and presents the same proposal across recovery without creating duplicate proposals or converting presentation failure into input rejection.
- **d30** — Successful approval keeps its existing behavior of recording intent and consuming the comment without a new success reply or launching work.
- **d31** — Filtered bot and third-party comments keep their existing record-and-consume behavior without gaining a new diagnostic or execution path.
- **d32** — Acceptance covers each confirmed rejection category, valid preparation and approval, filtered identities and edited controls without weakening approval checks.
- **d33** — Recovery acceptance covers failure before posting, uncertain posting with lost response, restart before and after delivery, failed consumption, and changed mode or eligibility while recovery is pending.
- **d34** — Admission recovery acceptance proves no duplicate intent, false rejection or authority renewal when consumption fails and the original proposal later expires or is superseded.
- **d35** — Retry acceptance proves later comments stay ordered behind unresolved infrastructure failures and resume once the original outcome is resolved.
- **d36** — Already queued creation comments use the corrected handling when encountered, without requiring manual watermark repair or replaying previously consumed comments.


## Assumptions

- **a1** — Source inspection at revision 1be56786ec00c214d60733f6b16ac87a83a0bd5d establishes current code behavior, not the deployed daemon revision.
- **a2** — Historical task #230 timing, attempt counts and deletion-based recovery remain owner-supplied incident evidence.
- **a3** — Existing durable reply records provide the rejection recovery surface; physical storage details may be chosen during engineering provided the specified logical state and recovery guarantees hold.
- **a4** — This shaping pass used focused source reads and historical test evidence; no fresh tests, live observations or full command-family audit were performed.
- **a5** — The owner's accepted recommendations and instruction to proceed authorize the detailed recovery rules within the creation-only boundary, with no further material business decision required.
- **a6** — Deliberately unavailable loaded capability is a user-visible rejection, while missing components caused by an unexplained internal fault remain retryable because absence alone does not prove a safe rejection.
