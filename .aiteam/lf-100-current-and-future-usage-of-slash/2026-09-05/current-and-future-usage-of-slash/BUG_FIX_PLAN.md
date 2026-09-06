# Bug Fix Plan
# Source: BUG_REPORT.md
# Created: 2026-09-05
# Status: DRAFT

## Bug Tasks

| Task ID | Bug | Root Cause (summary) | Files Affected | Fix Approach | Agent | Status |
|---------|-----|----------------------|----------------|--------------|-------|--------|
| BT-001 | `TestDescendantsWalksMoreThanOneLevel` and `TestRunTimeoutKillsTheWholeSubtree` fail in restricted macOS sandboxes despite the scheduling change being unrelated. | The sandbox denies the process-list access used by `/usr/bin/pgrep -P` and the timeout fixture's `/bin/ps`, so the fixture cannot observe descendants or process-group IDs. | `internal/harness/kill_test.go`, `internal/harness/runner_test.go` | Add a narrowly scoped test preflight that starts a child process and verifies the test can query that child's process information. Skip only when macOS explicitly denies process-list access; preserve the existing subtree-kill assertions and production `children`/`killSubtree` behavior in capable environments. | go-implementation-engineer | TODO |
