# Spike

Status: `fix_unverified`

## Request

Apply the proposed fix for issue 435 and add one regression test.

## What was done

- Confirmed observeLocked dropped observations with an earlier reset.
- Changed observeLocked so an earlier future reset takes the smaller limit and remaining, adopts the earlier reset and refreshes observedAt.
- Added TestDiscoveryBudget_EarlierFutureResetStaysFresh and read it back.

## What was found

_Nothing was recorded._

## Decisions worth remembering

_None._

## Not done

- Info-level logging of deferred ticks was not done; it is a separate follow-up.
- GitHub reporting differing resets inside one window is still inferred, not verified.

## Change made

- Defect: observeLocked discarded valid core observations whose reset was earlier than the stored reset but still in the future, so freshness and remaining never resynced.
- Files claimed: internal/daemon/discovery_budget.go, internal/daemon/discovery_budget_internal_test.go
- Regression test: internal/daemon/discovery_budget_internal_test.go
- Summary: An equal or earlier future reset now refreshes observedAt and takes the smaller limit and remaining, with the earlier reset kept. A later reset still starts a new window and expired resets are still rejected.

## Measured change and checks

- Result: `failed` (a full check reported failure)
- Files changed (2): internal/daemon/discovery_budget.go, internal/daemon/discovery_budget_internal_test.go
- Check phases run: focused, full
- The change is in the working branch and is **not ready**; nothing was discarded.

## Recommendation

_None._
