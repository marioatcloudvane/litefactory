# US-5 — Report story progress and close verified work

> As the owner, I want story-level progress and verified completion so that finished work closes and failed work stays visible.

Publish story milestones and close only work with matching acceptance, validation and saved-code evidence.

## Why This Matters
The parent run alone does not show which stories succeeded. Story-level updates make partial results visible, but a successful agent exit is insufficient evidence to close work. Reporting failures must be recoverable without repeating already completed implementation.

## Architecture Notes
- **T-5.1 — Define structured story evidence and persist execution attempts:** internal/agent; internal/store; dependencies: T-3.1, T-4.1. Strict versioned outcome contract identifies selected story/revision, AC evidence and gates. Persist run/revision/attempt/evidence and milestone intents. Keep process outcome independent from story verification.
- **T-5.2 — Add completed issue state update to GitHub adapter:** internal/github; dependencies: T-1.3. Add minimal issue-close capability with completed reason via existing client; check response, errors and rate-limit classification. No reopen/lock/unlock capability is required by this spec.
- **T-5.3 — Validate evidence and emit story milestones:** internal/turn/harvest.go; internal/report; both implementation orchestrator copies; dependencies: T-5.1, T-4.2. Reject missing/unknown/duplicate/mismatched identities; require named AC/review/test evidence bound to approved revision and saved/pushed head. Feature-wide gate failure conservatively blocks all without finer evidence. Distinguish launch-admitted started notice from proven running process.
- **T-5.4 — Reconcile completion reporting and closure without reexecution:** internal/turn reporting; internal/daemon pending work scheduling; internal/store; dependencies: T-5.2, T-5.3, T-1.1. Completion comment precedes close; completed state follows both. Retry pending effects using stored evidence even after original run finishes. Handle remote close success/local-write failure by observing intended state; human closure alone never verifies.
- **T-5.5 — Test malformed evidence and successful/partial story outcomes:** internal/agent; internal/github; internal/report; internal/turn fixtures; dependencies: T-5.1, T-5.2, T-5.3. Cover process success with no evidence, bad revision IDs, duplicate entries, failed gates, missing push, already-pushed clean retry, scoped partial success and failed subprocess. Use fake runner and real git fixture.
- **T-5.6 — Test milestone/closure recovery across restarts:** internal/store; internal/daemon and turn fixtures; dependencies: T-5.4, T-3.3. Real SQLite and httptest exercise report failure, successful comment with lost acknowledgement, close failure, close success before local commit, and restart. Assert no additional agent run and no lost refusal on completed child.
- Assumption: finished implementation means validated and saved/pushed work, without requiring merge. Evidence validation is application validation of agent reports, not an independent proof of correctness.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Define structured story evidence and persist execution attempts | data | T-3.1, T-4.1 | M |
| T-5.2 | Add completed issue state update to GitHub adapter | api | T-1.3 | S |
| T-5.3 | Validate evidence and emit story milestones | logic | T-5.1, T-4.2 | L |
| T-5.4 | Reconcile completion reporting and closure without reexecution | logic | T-5.2, T-5.3, T-1.1 | L |
| T-5.5 | Test malformed evidence and successful/partial story outcomes | test | T-5.1, T-5.2, T-5.3 | M |
| T-5.6 | Test milestone/closure recovery across restarts | test | T-5.4, T-3.3 | M |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-5.1 | Each selected story has a durable attempt and frozen/started/blocked-or-failed/completed milestone containing parent/run and revision references; started explicitly means admitted for launch. |
| AC-5.2 | Structured outcomes require unique selected story/revision identities, acceptance evidence and named review/test gates; missing, unknown, duplicate or mismatched outcomes cannot authorize closure. |
| AC-5.3 | Verification requires safe worktree evidence references, passing applicable gates and binding to the harvested saved/pushed head; process success alone, failed push or human closure is insufficient. |
| AC-5.4 | A failed shared gate blocks every story without finer scoped evidence; valid partial success may close only independently verified stories and leaves other stories open with explanations. |
| AC-5.5 | Completion evidence is posted before close with completed reason; local completed state is recorded only after both succeed, and a clean retry can use already-pushed evidence. |
| AC-5.6 | Report/close failures and ambiguous responses remain durable pending work reconciled after restart without another agent run; successful remote closure before local persistence is recoverable. |
| AC-5.7 | Progress parsing, HTTP responses and recovery tests cover failed subprocesses, absent evidence, bad identities, partial results, missing push and reporting/closure faults. |
