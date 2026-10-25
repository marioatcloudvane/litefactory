# Executed checks

Engineering evidence only; no acceptance or exhaustive Research coverage claim.

## Report complete race run; finite 8m timeout

```text
ok  	github.com/cloudvane/litefactory/internal/report	11.242s
```

## Doctor complete race run; finite 15m timeout

```text
ok  	github.com/cloudvane/litefactory/internal/doctor	50.585s
```

## Messaging and Discord complete race run; finite 25m timeout

```text
ok  	github.com/cloudvane/litefactory/internal/messaging	234.566s
ok  	github.com/cloudvane/litefactory/internal/messaging/discord	(cached)
```

## Turn complete run without race; finite 40m timeout

```text
ok  	github.com/cloudvane/litefactory/internal/turn	496.351s
```

## Daemon complete run without race; finite 40m timeout

```text
ok  	github.com/cloudvane/litefactory/internal/daemon	117.847s
```

## CLI complete run without race; finite 30m timeout

```text
ok  	github.com/cloudvane/litefactory/internal/cli	32.321s
```

## Pinned scoped lint of six changed package trees

```text
golangci-lint v2.13.1 (installed)
0 issues.
```

## Other caller package results

These unchanged caller packages passed in the earlier scoped race command. That grouped command initially failed on obsolete report golden output; the report package was corrected and its latest complete run above passed. No grouped-command success is inferred from individual package results.

```text
ok  	github.com/cloudvane/litefactory/internal/agentresult	3.683s
ok  	github.com/cloudvane/litefactory/internal/meterview	3.225s
ok  	github.com/cloudvane/litefactory/cmd/litefactory	1.568s
```

`go build ./...` passed with exit 0. The pinned lint command used `GOLANGCI_LINT_CACHE` under the permitted temporary directory and the six changed package trees. `gofmt -l` for those trees and `git diff --check` returned no output. No repository-wide test suite or full check ran. No pre-existing failures were identified.

Three subagents contributed. The end-to-end main path and semantic assembled recovery, failed-read, lost-response, unavailable-apply, failure-phase, uncertainty, navigation and saved-log destination checks are included in the complete runs above.

The frozen story still requires the original classified Research inventory for exhaustive branch reconciliation. It was unavailable in the supplied context and published issue/comments. The branch report records the accessible mapping and evidence limits; this is an access blocker, not an approved deferral or acceptance.
