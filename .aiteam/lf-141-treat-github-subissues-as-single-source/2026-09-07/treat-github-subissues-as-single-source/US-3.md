# US-3 — Freeze approved scope before implementation

> As the owner, I want promotion to fix and announce the approved scope so that later discussion cannot change work already underway.

Freeze all unfinished stories together and explain when further planning comments can be accepted.

## Why This Matters
Implementation needs a stable agreement even while GitHub remains editable. A visible policy must explain why later comments do not change active work. Promotion and discussion can race, so a partial notification failure must not start work on an unclear scope.

## Architecture Notes
- **T-3.1 — Persist atomic approval sets and lifecycle transitions:** internal/store; dependencies: T-1.1, T-2.1. Store whole unfinished revision set and freezing state in one transaction; enforce immutable snapshots and generation transition guards. Reuse child admission to prevent promotion overtaking accepted refinements.
- **T-3.2 — Validate promotion and deliver required freeze notices:** internal/turn preparation; internal/report; dependencies: T-3.1, T-1.4, T-2.3. Revalidate all indexed direct relationships and complete issue contracts before persisting approval. Post each notice from stable pending operation and block launch until all are delivered; preserve prepare session-state invariant.
- **T-3.3 — Enforce frozen comment policy and explicit replanning:** internal/daemon mode admission; internal/turn; internal/report; dependencies: T-3.2, T-2.2. Durable lifecycle overrides temporary labels for managed scope. Refuse authorized late comments even on closed stories; deduplicate by source comment. Quiescent explicit parent Plan opens only unfinished next-generation scope; active implementation and external reopening cannot unfreeze. US-5 invalidation may reopen an incorrectly completed issue during the run, but retains its frozen revision for repair.
- **T-3.4 — Test approval races, refusal retry and generation recovery:** internal/store; internal/daemon and turn fixtures; dependencies: T-3.1, T-3.2, T-3.3. Use real SQLite and rendezvous to cover admitted refinement vs promotion, comments discovered after freeze, partial notice success/restart, cancellation before session running and validly completed-story exclusion from replanning and retention of frozen scope after invalidation.
- Assumption: the lock is enforced by litefactory refusing work and posting notices. Native GitHub conversation locking is not part of this story.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Persist atomic approval sets and lifecycle transitions | data | T-1.1, T-2.1 | M |
| T-3.2 | Validate promotion and deliver required freeze notices | logic | T-3.1, T-1.4, T-2.3 | L |
| T-3.3 | Enforce frozen comment policy and explicit replanning | logic | T-3.2, T-2.2 | L |
| T-3.4 | Test approval races, refusal retry and generation recovery | test | T-3.1, T-3.2, T-3.3 | L |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-3.1 | Parent Implement waits for admitted refinement to finish or visibly fail, validates complete current indexed relationships and atomically records every unfinished revision and the freezing generation. |
| AC-3.2 | Every required freeze notice identifies the parent and selected revision, states that scope is locked for implementation and directs changes to parent Plan; no agent launches until all notices succeed. |
| AC-3.3 | Partial notice delivery or restart resumes the same selected set and pending notices; failed delivery cannot silently advance the workflow. |
| AC-3.4 | Authorized child comments discovered after freeze start no agent and receive a retryable policy explanation, including on completed/closed children and when their source timestamp predates freeze. |
| AC-3.5 | An explicit quiescent parent Plan action opens a new generation for unfinished stories and announces refinement availability; an active run, removable labels, child comments or external reopening cannot unfreeze scope. |
| AC-3.6 | Validly completed stories remain completed and additional scope requires a successor; US-5 may reopen invalidated completion with its approved scope still frozen. Tests cover promotion races, late comments, refusal retry and restart. |
