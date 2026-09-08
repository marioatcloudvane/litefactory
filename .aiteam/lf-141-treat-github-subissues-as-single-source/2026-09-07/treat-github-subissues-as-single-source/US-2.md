# US-2 — Route story discussion through the parent plan

> As the owner, I want child comments to refine the parent plan so that discussion retains the feature context.

Route authorized planning comments through the existing parent session and return an attributable result on the child.

## Why This Matters
A comment on a story can currently launch separate work that lacks the parent plan context. Routing it through the parent keeps decisions together. Concurrent comments and interrupted reporting must not cause conflicting runs or silently lost discussion.

## Architecture Notes
- **T-2.1 — Persist child comment provenance, admission and scan progress:** internal/store; dependencies: T-1.1. Store child/comment/actor/parent generation and consumed state distinctly from parent run trigger and discovery cursor. Admission is not consumption; advance only after report delivery per CONC-4.
- **T-2.2 — Discover managed child activity and route to parent dispatch:** internal/daemon/tick.go; internal/daemon dispatch; dependencies: T-2.1, T-1.3. Scan tracked children including closed, paginate deterministically and use parent inflight key. Numeric owner checks precede instruction admission. Distinguish pending refinement from ordinary parent discovery; no child worktree or competing session.
- **T-2.3 — Execute Plan refinements with child attribution:** internal/turn/prepare.go; internal/report; internal/turn reporting; dependencies: T-2.2, T-1.4. Provide parent plan and targeted current child context while preserving source comment URL/ID. Acknowledge/report on child with parent run reference; publication uses current-base proposal contract. Refusals and failed reports leave source retryable.
- **T-2.4 — Test parent routing, authorization and ordered admission:** internal/daemon/fixture_test.go; internal/turn/fixture_test.go; dependencies: T-2.1, T-2.2, T-2.3. Prove unrelated/marker-spoofed issues do not inherit authority, renamed owner numeric identity works, parent/child simultaneous activity serializes and undelivered child report does not advance cursor. Real SQLite plus channels/Quiesce, no sleep.
- Risk: scan progress and consumed-comment state are different; undelivered replies must remain retryable.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Persist child comment provenance, admission and scan progress | data | T-1.1 | M |
| T-2.2 | Discover managed child activity and route to parent dispatch | logic | T-2.1, T-1.3 | L |
| T-2.3 | Execute Plan refinements with child attribution | logic | T-2.2, T-1.4 | M |
| T-2.4 | Test parent routing, authorization and ordered admission | test | T-2.1, T-2.2, T-2.3 | L |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-2.1 | An authorized comment on a managed child in Planning is admitted through the parent task, session and worktree, with source child/comment/actor provenance and current story context. |
| AC-2.2 | Managed-child activity is discovered independently of parent updates, including paginated feeds and closed tracked children; bounded scan rotation does not permanently starve a child. |
| AC-2.3 | Parent commands and child refinements share the canonical parent inflight and cross-process lock; simultaneous events never create competing child sessions or worktrees. |
| AC-2.4 | Numeric owner authorization and verified indexed direct relationship precede routing; renamed owners remain authorized, while spoofed markers and unauthorized actors gain no authority. |
| AC-2.5 | Child acknowledgements/results link the parent run and original source; delivery failure remains retryable and does not consume the triggering comment. |
| AC-2.6 | Unindexed ordinary issues retain their existing behavior; stored child admission remains distinct from parent trigger and discovery progress. |
