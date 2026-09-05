## Root cause: the macOS sandbox denies access to the system process list

**Affected tests:** `internal/harness.TestDescendantsWalksMoreThanOneLevel`, `internal/harness.TestRunTimeoutKillsTheWholeSubtree`

**File(s):** `internal/harness/kill.go:171`, `internal/harness/kill_test.go:16`, `internal/harness/runner_test.go:635`

**Explanation:** Both failures are caused by the execution environment, not by the scheduling implementation. The subprocess-tree mechanism calls `/usr/bin/pgrep -P <pid>` in `children` (`kill.go:171`). In this macOS sandbox, that command fails with exit status 3 and `sysmon request failed with error: sysmond service not found` / `pgrep: Cannot get process list`. `children` deliberately treats that as an empty result (after logging), so `descendants` returns `[]`. The timeout fixture independently invokes `/bin/ps` to record the two process-group IDs; it is subject to the same denied process-list access and writes neither ID, producing the two empty strings.

The scheduling diff adds `Runner.ProposeSchedule` and its test in `internal/harness/runner.go` / `runner_test.go`, but it does not modify `kill.go`, `kill_test.go`, the timeout fixture, or the established `killSubtree` path. The failures also reproduce when the two affected tests are run alone, so they are not caused by test ordering or by the new schedule-advisor subprocess.

**Suggested fix:** No production repair is indicated. Run the process-tree tests in a macOS environment allowed to query child processes through `pgrep` and `ps` (the capability that the production kill mechanism itself requires). If restricted-sandbox runs must remain supported, add a narrowly scoped preflight in these tests that checks process-list access for a child started by the test and calls `t.Skip` only when the platform explicitly denies it; retain the tests unchanged in normal CI. Do not make `children` silently substitute a different, unverified tree-discovery mechanism, because that would weaken the SD-1 subtree-kill guarantee rather than repair it.
