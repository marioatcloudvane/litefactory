# MCP tool families — executed engineering checks

Workflow 2, frozen scope US-001 through US-008. Acceptance review was not performed. All specialist evidence is local Go execution with fakes or actual temporary SQLite, sockets, MCP pipes and controlled HTTP servers; no live forge or authentic nested host claim.

| Story | Executed targeted result |
|---|---|
| US-001 receipts | 19 unit cases and five journal/SQLite/socket tests passed, including race checks. |
| US-006 checkpoints | Unit stage/binding checks and four real checkpoint tests (seven nested cases) passed; existing helper regressions passed. |
| US-002 profiles/preflight | Six unit tests and six broker/SQLite tests passed; strict raw UTF-8 fix retested. |
| US-004 comments | 24 unit cases and seven actual SQLite/socket/HTTP tests passed after UTF-8 fix; one dispatch owner/POST under retry and reopen. |
| US-007 activity | Ten unit lifecycle cases, existing reducer tests and five broker/SQLite tests passed. |
| US-003 issue reads | Unit bounds/shape tests and four HTTP/broker tests passed after top-level null fix; pinned forge API passed. |
| US-005 creation | 27 new unit subtests, targeted existing primitive tests and eight actual publication/store tests passed. |
| US-008 composition | 63 unit catalog/construction cases and ten complete MCP pipe/socket/SQLite/HTTP matrix cases passed. |

Detailed commands, outcomes and limitations appear in per-story reports and check JSON. Counts refer to each specialist selection; overlapping regressions are not summed as unique tests.

Aggregate go build ./... passed. Initial make check passed format/vet and failed lint: 41 exported comments, six unchecked type assertions and seven HTTP constant findings. All 54 were corrected; affected targeted race tests passed. Initial linter cache writes were refused outside writable roots; subsequent check uses GOLANGCI_LINT_CACHE under /private/tmp without changing tool version or rules. Aggregate rerun remains in progress.

Captured snapshots affected by lint fixes were explicitly invalidated before refresh. No prior acceptance was reopened; none was performed in this run.

## Final aggregate result

PASS: make check exited 0 with pinned golangci-lint v2.13.1 reporting zero issues; full repository race tests passed (33 passing package lines). Final turn package completed in 1456.091s. gofmt -l . returned no files. go build ./... passed before lint-only documentation/test changes. Full passing output: make-check-run7-r2.log.

All eight engineering contributions were preflighted and recorded against final exact snapshots. US-008 captures the 50-file aggregate snapshot; a final hash comparison found zero mismatches. Each receipt reports input valid, engineering recorded, acceptance not applicable and remote effect pending. Pending remote effects, factory save and acceptance are not claimed complete. Final references: final-engineering-receipts.json.
