# Code Review — US-002 and US-001 revalidation

Feature: modes-should-stay
Reviewed by: go-code-reviewer
Date: 2026-09-09
Status: CLEAN (re-review cycle 1)

## Scope and authority

Reviewed the supplied frozen US-002 (#189, issue ID 5399419763, revision 1d5a6e09bd9771c74a542aa97912c3bfc903df7cf618f608c87587f5b1c6b8a3; dependency US-001) and affected US-001 (#188, issue ID 5399419269, revision 3959846ddf1ba22335787fb9d1b6053d73e6724c2230cdc3cc93c86d70be955e; no dependencies). Scope: daemon/mode.go, daemon/tick.go, daemon/daemon.go, store/modes.go, migration 0019, report/reply.go, plus immediate managed admission callers needed to inspect source identity. Shared architectural decisions and relevant CONC-4/9/12 were checked. No production files edited; test implementation and execution belong to the concurrent validation specialist.

## BLOCKING findings

### CR-B-001: RESOLVED — Parent watermark prematurely retires child transition recovery

**File:** internal/store/modes.go:84 and :105
**Story:** US-002 / T-002.2; affected US-001 retirement revalidation
**Category:** Cross-story / recovery correctness
**Finding:** US-002 newly journals child refinement modes under the parent session (`admitMode` now includes `t.refining`). Both retirement queries still treat every positive trigger ID less than the parent watermark as completed. Child refinement consumption is independent of the parent's comment watermark (CONC-9). A child comment can be discovered late with ID 100 while the parent watermark is 200. If its recorded Research-to-Plan transition removes Research and then addition fails, startup deletes that still-pending transition because 200 >= 100. A subsequently changed owner label can then be treated as a fresh previous selection and deleted by the newly reconstructed transition instead of producing the required visible conflict. Confirmed child selections can likewise be forgotten while their report remains undelivered.
**Required fix:** Exclude admitted child-refinement triggers from parent-watermark inference in both per-session and startup retirement. Retire them using their own durable consumed/report state, or explicit cleanup state. Preserve legacy parent watermark behavior. Add a deterministic real-SQLite restart case with a child comment below the parent watermark, unfinished transition/report, and owner edit; verify the original journal survives until actual child completion and the conflicting owner edit is retained/refused.

**Resolution (cycle 1):** Both queries now exclude all admitted child-refinement identities from parent-watermark inference and independently recognize consumed child records, delivered run reports, or explicit cleanup. The source-specific report transaction updates child consumption without touching the parent watermark. Pending transitions and undelivered child reports therefore retain their journal even below the parent watermark, while legacy parent-only cleanup behavior is preserved. Static re-review found no new issue. The implementation engineer reports passing focused store Mode/Retire tests; the validation specialist owns the deterministic restart regression and final acceptance test gate.

## ADVISORY findings

None.

## Cross-story summary

The fresh command precedence, ambiguity validation, old/new intent columns, effect verification, no-transaction-over-forge discipline, and confirmed-selection no-replay branch are consistent with the intended architecture. The canonical label target remains the admitted parent task for child refinements. Managed scope authorization still passes through the existing turn readiness/freeze/replan gates. Report completion and ordinary legacy retirement no longer delete or restore labels. The newly shared journal use for child refinements now uses source-aware retirement, resolving the cross-story recovery issue.

## Verdict

US-002: CLEAN — CR-B-001 resolved; scoped review passes. Acceptance publication remains subject to the independent test gate.
US-001 revalidation: CLEAN — source-aware retirement preserves the retained-label and report-recovery semantics, including legacy parent watermark inference. No unresolved review findings.
