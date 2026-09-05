# Bug Fix Plan
# Source: BUG_REPORT.md
# Created: 2026-09-05
# Status: DRAFT

## Bug Tasks

| Task ID | Bug | Root Cause (summary) | Files Affected | Fix Approach | Agent | Status |
|---------|-----|----------------------|----------------|--------------|-------|--------|
| BT-001 | Process-tree harness tests cannot verify descendant discovery or timeout cleanup when the macOS runner denies process enumeration. | The sandbox blocks `ps` and `pgrep` because `sysmond` is unavailable, causing empty descendant scans and invalid fixture PGID checks. | `internal/harness/kill.go`, `internal/harness/kill_test.go`, `internal/harness/runner_test.go`, test-runner configuration as applicable | Provision a macOS test environment with permitted process inspection, or add a narrowly scoped capability check that explicitly skips only these OS-level process-tree tests when enumeration is unavailable. Preserve genuine `pgrep` errors as failures; never treat unavailable inspection as an empty descendant set. | go-implementation-engineer | TODO |
| BT-002 | Darwin child enumeration invokes `pgrep` without the mandatory pattern argument. | `children` calls `pgrep -P <parent>` although BSD `pgrep` requires a pattern. | `internal/harness/kill.go`, `internal/harness/kill_test.go` | Supply an all-process pattern compatible with supported macOS BSD `pgrep`, retaining current error logging and timeout behavior. Add a focused Darwin-compatible regression test that confirms the command finds a known direct child when process enumeration is available. | go-implementation-engineer | TODO |
