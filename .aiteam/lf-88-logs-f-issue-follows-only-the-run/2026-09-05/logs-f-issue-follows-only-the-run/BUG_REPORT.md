## Root cause: The current macOS execution sandbox forbids process-tree inspection
**Affected tests:** `internal/harness.TestDescendantsWalksMoreThanOneLevel`, `internal/harness.TestRunTimeoutKillsTheWholeSubtree`

**File(s):** `internal/harness/kill.go:171-199`, `internal/harness/kill_test.go:19-35`, `internal/harness/runner_test.go:606-611`

**Explanation:** Both failures reproduce in isolation with `go test ./internal/harness -run 'TestDescendantsWalksMoreThanOneLevel|TestRunTimeoutKillsTheWholeSubtree' -count=1 -v` (21.995 s), so they are not caused by the uncommitted `internal/cli` logs-follow changes.

The process-inspection primitives the harness and fixtures require are unavailable in this execution environment. Direct `/bin/ps` invocation is denied with `operation not permitted`; `/usr/bin/pgrep` returns exit status 3 and writes `sysmon request failed with error: sysmond service not found` and `pgrep: Cannot get process list`. Consequently `children` treats every scan as an empty result, `descendants` remains empty for ten seconds, and the timeout fixture's `/bin/ps -o pgid=` pipelines write empty PGID files. The latter therefore fails its fixture-sanity assertion before it can evaluate whether the sleeper was killed.

This is an environment/platform prerequisite failure for process-tree tests and also means a real subtree sweep cannot enumerate escaped descendants while run under the same restrictions. The changed files are limited to `internal/cli/cmd_logs.go` and CLI tests; they neither invoke nor import `internal/harness`.

**Suggested fix:** Run these harness tests where macOS process enumeration is permitted and `sysmond` is available (or provision the test runner accordingly). If such a runner cannot be guaranteed, isolate/gate this OS-level subprocess test suite with an explicit, narrowly-scoped capability check; do not mask the failure by treating unavailable process inspection as a successful descendant lookup.

## Root cause: `children` invokes BSD `pgrep` without its required pattern argument
**Affected tests:** `internal/harness.TestDescendantsWalksMoreThanOneLevel`, `internal/harness.TestRunTimeoutKillsTheWholeSubtree` (once process inspection is available)

**File(s):** `internal/harness/kill.go:176`

**Explanation:** The Darwin 25.5 `/usr/bin/pgrep` usage is `pgrep ... pattern ...`; `children` executes only `pgrep -P <parent>`. Thus the subprocess command does not conform to the documented BSD command syntax. The present sandbox fails earlier while trying to obtain the process list, so this defect is masked by the first root cause, but it is a separate portability/correctness risk: a healthy macOS process service may reject the invocation for a missing pattern rather than list all children.

**Suggested fix:** Make the child-enumeration invocation valid on supported macOS releases by supplying an all-process matching pattern and add a focused test or platform check that verifies the exact command can list a known child. Retain existing timeout/error logging behavior for genuine enumeration failures.
