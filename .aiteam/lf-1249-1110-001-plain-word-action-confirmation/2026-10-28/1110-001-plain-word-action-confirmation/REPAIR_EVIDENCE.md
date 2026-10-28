# Validate hand-back repair evidence

Checked snapshot: `bc96d970df666bda8d5ed0ab8a69650d5c4168d5`.
The factory prepared this branch with main already merged. `origin/main` at
`7001374160be29fc2bbd920fa5724a77f8cf8fc3` is an ancestor of HEAD. No merge,
commit, push, product-code edit or assertion change was performed by this worker.

The only code difference from Validate's reviewed `1967a4a00b842452f9919befd15542f9c2135d4c`
is upstream fix `abb828c6eccc50913193f300dee76c8ef82adf5e` from PR #1253:
`internal/turn/validate_coverage_freshness_test.go` now appends `GOFLAGS=` to its
warm-control subprocess environment. Inherited `-count=1` had disabled nested
result-cache reuse and made the two warm control executions count four bodies.
The upstream change retains the assertions and fresh-request behavior while
removing that environment contamination from the warm control. This repair did
not reproduce the original failing snapshot; that cause is supported by the
upstream diff and owner-supplied diagnosis. Current-code checks follow.

| Check | Command | Result |
|---|---|---|
| Requested diagnostic regression | `GODEBUG=gocachetest=1 go test ./internal/turn -run '^TestValidateFreshFullRequestsExecuteWarmGoBodies$' -count=1 -timeout=5m` | exit 0, 6.866s |
| Inherited environment regression | `GOFLAGS='-trimpath -count=1' GODEBUG=gocachetest=1 go test ./internal/turn -run '^TestValidateFreshFullRequestsExecuteWarmGoBodies$' -count=1 -timeout=5m -v` | exit 0, 7.199s |
| Story Auto main path | `go test -race -count=1 -timeout=5m -run '^TestPlainWordActionJourney$' ./internal/daemon` | exit 0, 3.605s |

The effective outer GOFLAGS was unset for the requested diagnostic regression,
and explicitly `-trimpath -count=1` for the inherited-environment check. The
fixture resets nested warm-control GOFLAGS to empty in both cases. Retained logs
show outer result caching disabled by the requested `-count=1`; successful nested
warm-control output is captured inside the test, so it is not emitted here.
The warm-control cache-count and fresh-request assertions passed. The verbose
run reports one skipped `isolated-executor` subcheck because this host refuses
process identity observation. That subcheck is not claimed as executed proof.
No runner permission was bypassed.

The passed main-path command exercises ordinary owner words through actual MCP
JSON-RPC and Discord adapters, the shared service and real domain handlers,
both confirmation tiers, exact revisions, separate five-second confirmation,
the configured owner's reaction, and recorded outcomes/history. Controlled
providers establish wiring and policy; no live connector approval is claimed.

Two subagents reviewed upstream scope and main-path coverage in parallel with
parent-owned test runs. Neither edited code or ran duplicate tests. No product
package changed in this repair, so previous complete scoped package runs and lint
remain historical evidence. The full repository suite and acceptance review were
not rerun by this Implement worker; those remain with Validate. No failing check
is being waived. Manual owner criteria AC-002 through AC-007 remain pending.
The previously recorded bounded-execution-context choice is unchanged.
