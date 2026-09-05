## Root cause: The execution sandbox denies macOS process-table inspection

**Affected tests:** `TestDescendantsWalksMoreThanOneLevel`, `TestRunTimeoutKillsTheWholeSubtree`

**File(s):** `internal/harness/kill.go:171`, `internal/harness/kill_test.go:16`, `internal/harness/runner_test.go:599`

**Classification:** Immutable sandbox inability, not test fragility and not evidence of a product defect.

**Explanation:** Both failures require reading the host process table, an operation this managed execution sandbox denies. A direct probe in the same environment produced `sysmon request failed with error: sysmond service not found` and `pgrep: Cannot get process list` from `/usr/bin/pgrep`, while `/bin/ps` failed with `operation not permitted`. The first test consequently receives no children from `children`, whose production implementation invokes `/usr/bin/pgrep -P`, and times out with an empty descendant list. The second fixture invokes `/bin/ps` twice to prove that its sleeper entered a process group distinct from the parent shell. Because `ps` is denied and its output is piped through `tr`, both output files are created empty and the fixture's sanity assertion fails before it can assess subtree killing. Reproduction is deterministic with the focused non-race run, and the implicated source and tests have no feature-work diff. The failures therefore do not establish that the process-tree walk or kill behavior is incorrect on the supported unsandboxed macOS daemon host.

**Suggested fix:** Do not change the product kill sweep or weaken these platform tests. Run the harness process tests, and the final `make check`, on the normal macOS host/CI executor where `/usr/bin/pgrep` and `/bin/ps` are permitted to inspect child processes. Treat this restricted agent sandbox as unsuitable for certifying these two process-lifecycle assertions. If future infrastructure needs an automated preflight, put it in the test runner outside the Go suite and route the suite to a capable macOS executor; do not skip the assertions within the suite, because that would reduce the required process-kill and platform coverage.
