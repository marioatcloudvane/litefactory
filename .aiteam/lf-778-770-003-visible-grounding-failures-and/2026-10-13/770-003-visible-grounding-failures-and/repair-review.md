# Validate return repair

The concrete conflict fixture replaced its MCP runtime without supplying paths.
Consequently, `implementScratchDir` resolved to a relative `implement-scratch`
inside the checkout. The repair supplies the fixture's existing canonical
temporary paths to that runtime, covering all eight conflict variants.

Production symlink resolution and sandbox permissions are unchanged. In
particular, permission errors still abort canonicalization and factory state
remains denied to sandboxed checks. No production service was exercised.

The supplied review retained only a summarized suite failure. Its original full
log and generated sandbox rules were not available through the supplied result
descriptor. The diagnosis above follows the concrete fixture and scratch-path
code; it does not claim inspection of that unavailable log.

Complete scoped package checks are run by this repair worker. The repository
wide suite is reserved for Validate by the worker instructions. Manual and live
acceptance remain unclaimed.

Executed checks on the repaired worktree all passed:

- `go test -count=1 -timeout=0 ./internal/turn ./internal/store ./internal/daemon ./internal/cli`
- `go test -race -count=1 -timeout=0 ./internal/doctor ./internal/messaging ./internal/messaging/ask ./internal/messaging/discord ./internal/messaging/feature ./internal/messaging/presentation ./internal/messaging/response ./internal/meterview ./internal/projectconnection ./internal/repos ./internal/resultservice ./internal/rights ./internal/store/storetest ./internal/webhookreg`
- `go test -race -count=1 -timeout=0 ./internal/teamrelease`
- `make lint PKGS='./internal/turn'` with a dedicated temporary lint cache: pinned v2.13.1, zero issues.
- `gofmt -l internal/turn/bug_graph_runtime_test.go` and `git diff --check`: clean.

The complete turn run includes every concrete conflict variant. The complete
messaging run includes the both-provider main owner journey. Logs are retained
beside this note. Three subagents assisted diagnosis, the fixture repair and
read-only renderer review; the primary worker ran the checks.
