# Executed engineering checks

All commands below completed successfully against this worktree. No whole-repository test suite or full check was run. Acceptance and authentic application execution are not claimed.

- Complete race-enabled packages: `go test -race -timeout=0 ./internal/teamrelease ./internal/teamsource ./internal/bugchecks ./internal/teampackage ./internal/rights ./internal/harness ./internal/codex ./cmd/teamrelease ./cmd/litefactory`. All tested packages passed; the release command has no test files. Output: race-package-tests.log.
- Complete consumers: `go test -timeout=0 ./internal/turn ./internal/daemon ./internal/cli`. All passed. Output: consumer-package-tests.log.
- Scoped pinned lint: `make lint PKGS='./internal/teamrelease ./internal/bugchecks ./internal/harness ./internal/codex'`. Exit 0, zero issues. Output: scoped-lint.log.gz. The sandbox prevented persistent linter cache writes; these were cache warnings, not lint findings.
- Formatting: gofmt on all four added Go test files; gofmt -l returned no paths. git diff --check returned no findings. Output: format-check.log.
- Builds: `go build ./cmd/teamrelease ./cmd/litefactory`. Exit 0. No daemon distribution binary was overwritten. Output: command-builds.log.
- Release drift: `go run ./cmd/teamrelease -check -source internal/teamsource -source internal/teamsource/python-fastapi`. Exit 0. Output: release-drift-check.log.
- Exact dependency compatibility: uv pip compile with Python 3.14 and macOS arm64/amd64 targets, no Python downloads; npm package-lock-only resolution with ignore-scripts and engine-strict. Both succeeded. Exact inputs, metadata constraints and lock outputs are retained here; these are dependency/package checks, not an example application.
- Historical comparison: all previous catalogue entries identical, no historical Go release changes, and all 201 new release inventory file hashes match. Output: release-inventory-check.json.

Focused tests cover deterministic generation, all six shared workflow graphs, resource closure, exact installed-release selection, native delivery to both providers and cold replay, recording provider environments and check recipes, manifest/script/test/version diagnostics, confined overrides, permissions and version/documentation consistency. Full package runs include those tests and the existing structure/regression tests.

No subagents were started because the run's delegation restriction applies. Factory save/publication and read-only Validate retain their separate authority.
