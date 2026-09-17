go test -race ./internal/github ./internal/turn -run TestIssue(PageHTTP|QuerySocketHTTP|QueryService_)|TestClientMethodSetIsPinned -count=1
ok github.com/cloudvane/litefactory/internal/github 1.640s
ok github.com/cloudvane/litefactory/internal/turn 5.283s
Four new I/O tests plus unit and pinned-surface cases passed. Actual HTTP single-page bounds, PR filtering, bound repository, null/strict/state refusals, minimal projection, SQLite receipts no redispatch, immutable targets and revoked authority.
Initial null regression fixed in issue_query.go; all cases reexecuted after fix.
