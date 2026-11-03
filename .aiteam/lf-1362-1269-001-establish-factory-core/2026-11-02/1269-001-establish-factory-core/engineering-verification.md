# Engineering verification

Base commit: `eb72718bc6443c082b26150797f795b613559259`. No schema migration was added.
Three subagents contributed CLI/local operations, consumer boundaries, and the
main-path/recovery tests plus documentation. The primary worker reviewed the
changes and ran every test reported here. Subagents ran compilation only.

## Complete package runs

All commands used finite timeouts and background logs. The full repository test
suite and `make check` were not run; those belong to Validate.

- `go build ./...`: passed.
- `go test -timeout=30m ./internal/cli`: passed after final lint changes.
- `go test -timeout=30m ./internal/daemon`: passed after repairing the journey
  fixture to retain its action coordinator on the daemon host.
- `go test -timeout=30m ./internal/store`: passed after final fixture edits.
- `go test -timeout=35m ./internal/turn`: passed (998.415 seconds).
- `go test -race -timeout=25m ./internal/codeevidence ./internal/overview ./internal/taskevidence ./internal/usageview`: passed.
- `go test -race -timeout=25m ./internal/factory/... ./internal/messaging/... ./internal/messagingstate ./internal/messagingstorage ./internal/connector ./internal/connectorstate ./internal/connectorstorage ./internal/identity ./internal/owneraction ./internal/durableworkflow ./internal/stagedecision ./internal/taskdiscussion ./internal/projectconnection ./internal/submission ./pkg/execution/... ./scripts/factory-pending-state`: passed.
- After lint repairs, complete `-race -timeout=25m` runs of `./internal/factory/...`,
  `./internal/connector`, `./internal/messagingstorage` and the fixture command passed.
- Final complete `-race -timeout=25m` runs of `./internal/messaging` and
  `./internal/connector` passed.

Packages without test files were compiled by the listed commands.

## Concurrency selections

The prescribed `RACE_TESTS_cli` and `RACE_TESTS_store` selections from Makefile
were run with `go test -race -timeout=30m -run PATTERN` on
each package after its complete ordinary run: both passed. The generated filters
also contain adjacent-package names from the Makefile block; a source-declaration
comparison confirmed that none match additional tests in these packages. Exact
executed commands are retained in `race-selection-commands.txt`. The corresponding
turn selection runs with `-timeout=45m`; it passed (408.607 seconds).

`go test -race -timeout=20m -run 'TestIn[Ff]light|TestDiscovery|TestOwnerAction|TestPlainWordActionJourney|TestFactoryCore|TestScheduledLinear' ./internal/daemon`: passed.

Turn prescribed race selection: passed. All listed final engineering checks passed.

## Other checks

- `gofmt -l .`: no unformatted files.
- `git diff --check`: passed.
- Pinned golangci-lint v2.13.1 reported zero issues. Executed command:

  ```sh
  GOLANGCI_LINT_CACHE=/tmp/lf1362-golangci-cache make lint PKGS='./internal/cli ./internal/connector/... ./internal/connectorstate/... ./internal/connectorstorage/... ./internal/daemon ./internal/factory/... ./internal/messaging/... ./internal/messagingstate/... ./internal/messagingstorage/... ./internal/store ./internal/codeevidence ./internal/overview ./internal/taskevidence ./internal/usageview ./internal/identity ./scripts/factory-pending-state'
  ```

  A final lint run of the connector and fixture-command packages also passed
  after the last boundary-test and fixture-script edits.
- Relative links in the two new factory documents and the pending-state fixture
  README resolve. Existing execution, command-reference and remote-connector
  documents link to those new documents.
- `python3 scripts/factory-pending-state.py rehearse`: passed. It uses a temporary
  database and `git archive` scratch checkout, opening baseline, current, then
  baseline code against unchanged synthetic state.

## Main path and evidence limits

`TestFactoryCoreTicketMainPathWithoutOptionalTransports` parameterizes GitHub and
Linear. It checks retained ticket/code routing, verified owner input,
request preflight/submission/receipt, lost-publication-reply recovery, immutable
receipt identity, superseding current connections and replay without a new run
or provider post. Neither optional transport is constructed. Providers and paid
execution are controlled, and the Plan owner-response transition is applied
through its state machine directly rather than the graph runtime.

The pinned pre-change fixture checks captures, running attempts, interrupted
runs, protected pending action payloads, consent generations, retained
connections and uncertain publication. It verifies refusal without evidence,
watermark atomicity, confirmation, revocation and replay. The reopen rehearsal
proves storage compatibility for this fixture, not a live process stop/drain,
uninterrupted upgrade, every historical state shape or live-provider delivery.
No production service or real provider was tested. Initial compilation/lint and
fixture-wiring failures were repaired. No pre-existing failure was observed in
the final completed runs.

Manual acceptance remains pending for AC-001, AC-002, AC-003, AC-005, AC-006 and
AC-007. These engineering checks do not grant acceptance or publication.
No implementation deviation was required.
