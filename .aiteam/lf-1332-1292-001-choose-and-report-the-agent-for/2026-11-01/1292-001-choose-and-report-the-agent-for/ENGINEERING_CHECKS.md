# Engineering check evidence

Starting commit: `5aed96075867c2c9dda6d88f1aacab2be107326d`.

See T01-characterization.md for passing pre-replacement evidence and T05-main-path.md for the automated main path.

## Complete package checks

The SQL-heavy packages used finite 30-minute timeouts without race instrumentation; configuration, GitHub, report and project connections used race detection with finite 15-minute timeouts. Daemon was rerun after preserving non-Bug judge defaults and updating the resolver comment. The final configuration run followed the lint correction.

## Final configuration race run

ok  	github.com/cloudvane/litefactory/internal/config	9.872s

## Complete GitHub, report and project-connection race runs

ok  	github.com/cloudvane/litefactory/internal/github	4.625s
ok  	github.com/cloudvane/litefactory/internal/report	87.814s
ok  	github.com/cloudvane/litefactory/internal/projectconnection	11.653s

## Complete turn, store and CLI runs

ok  	github.com/cloudvane/litefactory/internal/turn	809.269s
ok  	github.com/cloudvane/litefactory/internal/store	185.948s
ok  	github.com/cloudvane/litefactory/internal/cli	60.415s

## Final complete daemon run

ok  	github.com/cloudvane/litefactory/internal/daemon	163.314s

## Repository-selected race checks

The -run pattern was generated from the union of Makefile RACE_TESTS_turn, RACE_TESTS_store and RACE_TESTS_cli (461 test names), anchored to complete test names. Executed go test -race -timeout=30m -run <that pattern> ./internal/turn ./internal/store ./internal/cli.

ok  	github.com/cloudvane/litefactory/internal/turn	483.717s
ok  	github.com/cloudvane/litefactory/internal/store	196.279s
ok  	github.com/cloudvane/litefactory/internal/cli	98.504s

## New Bug path concurrency checks

go test -race -timeout=5m ./internal/daemon -run 'TestBugExecutingRequestKeepsSelectedAgent|TestCodexBugAgentMainPath'

ok  	github.com/cloudvane/litefactory/internal/daemon	9.773s

## Actual filing paths

go test -timeout=5m ./internal/daemon -run 'TestValidateFiledBugUsesConfiguredAgent|TestUnlabelledBugFilingSources'

ok  	github.com/cloudvane/litefactory/internal/daemon	0.782s

## Judge scope regression

go test -timeout=5m ./internal/daemon -run 'TestJudgeRecordedBugProvider|TestJudgeChallengesBug|TestCodexBugAgentMainPath'

ok  	github.com/cloudvane/litefactory/internal/daemon	5.035s

## Lint

GOLANGCI_LINT_CACHE under /tmp; make lint with PKGS limited to internal/config, github, report, store, turn and daemon. After the final judge change, daemon lint was repeated.

golangci-lint v2.13.1 (installed)
0 issues.
golangci-lint v2.13.1 (installed)
0 issues.

## Build, format and structural checks

go build ./... exited 0. git diff --check exited 0. gofmt -l . returned no files. Relative links in the three changed guidance files resolve.

## Evidence limits

All reported checks passed; there are no remaining or established pre-existing failures. Three subagents contributed characterization/end-to-end proof, provider/selection verification, and reporting/guidance. No deviations were needed. No production service or paid coding-agent invocation was used. Engineering is complete; acceptance remains with Validate and the owner. Manual criteria AC-001 and AC-003 through AC-007 remain pending, including the real Codex delivery check. No commit or push was performed by the worker.
