# Code Review — US-5

Feature: autoresume-after-session-limit-reached
Reviewed by: go-code-reviewer
Date: 2026-09-08
Status: CLEAN

Frozen story: #165, issue_id 5390504842, revision `648e41fb2a5d88d459d29ac442ca2be66a5333574c47373ef930e64e3da1d627`; dependencies: US-3 and US-4.

## BLOCKING findings

None.

## ADVISORY findings

None.

## Scope and acceptance assessment

Reviewed US-5 production changes in `internal/store/continuation_settlement.go`, `internal/store/runs.go`, `internal/store/continuations.go`, `internal/store/continuation_dispatch.go`, and `internal/daemon/schedules.go`, against the frozen story and ARCH_REVIEW.md task hints T-5.1/T-5.2. The reviewer authored the preceding US-4 dispatch integration, but did not author US-5 lifecycle or settlement implementation. This verdict covers the US-5 additions and their integration; it does not independently re-certify US-4 implementation.

- AC-5.1: Orphan reconciliation first terminally records abandoned allocated runs, then reconciles automatic queue state. Reserved actions with no allocated run return to pending. Running actions settle from their allocated run's durable evidence; they never return to pending merely because a worker disappeared. Already-persisted pending continuation and announcement debt survive this transaction.
- AC-5.2: Automatic run allocation checks the root's allocated-row count transactionally, while reservation and report operations do not consume attempts. Each new intent requires a new source run, future reset bound, and due time at least 60 elapsed seconds later. Subsequent attempts inherit root identity and increment their bounded ordinal. The existing turn eligibility contract suppresses a fourth intent and records manual recovery on exhaustion.
- AC-5.3: A fresh eligible interruption records its continuation and the original schedule's explicit suspended state atomically. Intermediate automatic outcomes become interrupted after their successor is durable; they leave the original prerequisite suspended. Reconciliation searches ordinary running schedules separately, so it cannot mistake a suspended prerequisite for a lost reservation.
- AC-5.4: Terminal automatic outcome and prerequisite settlement share the completion transaction. Success uses the persisted semantic predicate and final run completion timestamp; failure or cancellation blocks descendants. Settlement updates only the matching suspended prerequisite/root and invokes the existing dependency-resolution helper, retaining the original dependency identity and multiple-prerequisite semantics. RunReported and exit-zero quota evidence are not treated as success merely because of exit status. Manual scheduler completion now uses the same stored success predicate and actual completion timestamp.
- AC-5.5: The transaction boundaries support the required duplicate/restart/fault scenarios: interruption replay is immutable, source-run identity is unique, allocation and queue ownership commit together, and settlement is conditional on running/suspended state. The separate test specialist owns executable fault-matrix evidence; no test result is asserted by this static review.
- AC-5.6: Lifecycle work changes neither provider transport nor permissions, worktree save policy, or frozen managed-scope selection. Both provider adapters feed the same neutral outcome contract. Runner-to-turn-to-scheduler tests, required coverage, build, and aggregate repository checks remain mandatory and are not represented as complete by this review.

## Cross-story summary

US-3 atomic outcome/intent persistence now finishes the preceding automatic action only after any successor is durable. US-4 conditional allocation and cancellation integrate with US-5 root/prerequisite settlement without splitting SQL ownership or adding clocks, workers, dependencies, or forge credentials. The original schedule remains the sole dependency anchor. Cancellation keeps its separate owner-notice acknowledgement; run/report replay continues to use durable semantic outcomes. All new result sets are closed before subsequent transaction queries, preserving the one-connection store requirement. No blocking integration, error-handling, resource, concurrency, or security issue was found in the reviewed US-5 production changes.

## Verdict

CLEAN — no findings. Story-specific tests and final aggregate gates remain required before verification.
