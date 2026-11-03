# Remote owner-access separation

Engineering evidence for story-001. Acceptance of AC-002–AC-006 remains pending with Validate/owner review.

## Delivered ownership

- Core neutral target values: `internal/factory/contracts/read_targets.go`.
- Core target service: `internal/factory/readtargets/service.go`; authoritative SQL helpers are defined once in `internal/store/read_targets.go` and reused inside the existing compound consent and admission transactions.
- Connector read authorization: `internal/connectoraccess/access.go`; private `connectorstorage.New(one DB)` composes it with credential persistence and the same DB's core reader. Only the private binding attests backend identity to the remote server.
- Connector lifecycle: `internal/connector/lifecycle.go`; daemon wiring supplies bounded services and cancellation/join coordination. Direct daemon authorization reconciliation and mixed store protected-read methods have been retired.
- Scope remains one module/process and shared unchanged SQLite state. No new scopes, tools, authority, provider, schema or serialized authorization format.

The main path test `TestRemoteRetainedTargetReadAndRevocation` exercises public OAuth/browser consent, finite repository coverage, code exchange and MCP retained evidence acquisition/release. It refuses anonymous, missing and uncovered targets before acquisition, and withholds evidence when revocation commits during acquisition.

Three subagents built the characterization/end-to-end checks, lifecycle extraction and real-build rollback rehearsal. The lead implemented target/authorization separation, reviewed every subagent change, updated documentation and ran final checks. No unplanned outcome ruling was needed.

## Evidence index

- [Safety baseline and Research/invariant coverage](SAFETY_BASELINE.md).
- [Lifecycle and shared transport evidence](LIFECYCLE_EVIDENCE.md).
- [Retirement and binding audit](RETIREMENT_AUDIT.md).
- [Pinned real-build old/current/old authorization rehearsal](rehearsal/README.md).
- Final scoped command outputs are retained in [checks](checks/). The full repository suite and full check were not run by this story worker.

## Documentation review

Reviewed `docs/factory-core.md`, `docs/guides/remote-connector.md` and `docs/cli/connector-grants.md` against final ownership and observed rehearsal. Nineteen local references resolve. The operator route stops/drains the process, selects compatible previous code and restarts against latest saved state without restoring an obsolete authorization snapshot. Provider certification, retraction of returned bytes and newest-commit survival after power loss remain outside the proof. No wording or hash tests were added.

## Existing standards issue retained

The approved plan identified pre-existing client response-guidance prose in `internal/connector/stage_decisions.go:191–199`. It remains unchanged for operator disposition under the agent-guidance standard; this refactor adds no instruction prose or agent/skill changes.

## Final checks

Final runs are recorded in the check logs. A first lint run found staticcheck S1016 in the new core usage target conversion. The direct type conversion fixed it; the final scoped lint passed with zero issues. The directly changed readtargets package was run again after that correction. All final scoped package runs passed; no pre-existing failing test or lint finding was encountered in those runs. The initial S1016 finding was caused by this story and was fixed.


Final complete runs used `GOFLAGS=-trimpath`, `-count=1`, and finite Go/outer timeouts. They started together as independent background package processes, with one process per package. No full repository test suite or `make check` was run.

| Scope | Executed final command/options | Result |
|---|---|---|
| store | `go test -count=1 -timeout=35m ./internal/store`, then Makefile `RACE_TESTS_store` selection with `-race -timeout=20m` | Passed, 148.606s complete; 163.035s selected race |
| daemon | `go test -count=1 -timeout=35m ./internal/daemon` | Passed, 198.634s; full run without race per story-worker repository rule |
| cli | `go test -count=1 -timeout=35m ./internal/cli`, then Makefile `RACE_TESTS_cli` selection with `-race -timeout=20m` | Passed, 48.207s complete; 90.960s selected race |
| connector, connectorstate, connectorstorage, connectoraccess, factory, factory/contracts, factory/readtargets, taskdiscussion, stagedecision, codeevidence, overview, taskevidence, usageview | Each package completely: `go test -count=1 -race -timeout=10m ./internal/<package>` | All passed; packages with no test files compiled successfully |
| readtargets after style correction | `go test -count=1 -race -timeout=10m ./internal/factory/readtargets` | Passed (no test files) |
| Build | `go build ./...` after final Go edits | Passed |
| Scoped lint | pinned golangci-lint v2.13.1 via `make lint PKGS='<the 16 scoped packages above>'` | Passed, zero issues; see `checks/lint-final.log` |
| Structural | `git diff --check`, `gofmt -l .`, local documentation references | Passed, formatting output empty and all 19 references resolve |
| Authorization rehearsal | Final eight-process pinned old/current/old run in isolated scratch | Passed; all rehearsed Go/module bytes match final code. The remote guide introduction was clarified afterward without changing code. |

Exact race selectors and full output are retained in individual logs. Earlier baseline/focused runs remain distinct evidence. Acceptance and live-client certification were not performed. Owner check pending: AC-002, AC-003, AC-004, AC-005, AC-006.
