# Bug Escalation

## Remaining failures

- `TestDescendantsWalksMoreThanOneLevel`: descendant enumeration returns an empty result because process-table access is denied.
- `TestRunTimeoutKillsTheWholeSubtree`: process-group discovery returns empty values because `/bin/ps` cannot inspect the spawned processes.

## Root causes as understood

Both unchanged macOS harness tests depend on operating-system process-table visibility. This run's sandbox denies that visibility to both mechanisms used by the package (`/usr/bin/pgrep` and `/bin/ps`). The implementation under issue #50 does not touch these tests or the process-tree behavior, and all affected feature packages pass their race tests.

## What was attempted

- The full gate was run by the implementation audit and reproduced the same two failures with and without the race detector.
- An independent unit-test audit reproduced the failures after all new feature coverage passed.
- A dedicated analysis-only bug pass checked both process-enumeration mechanisms and classified the condition as an immutable sandbox limitation.

## Suggested next steps

Run `make check` in CI or another macOS environment that permits child-process inspection. Do not skip or weaken the process-tree tests merely to make this restricted sandbox green.
