# Executed engineering checks — run 0007

## US-001 scoped snapshot

Frozen story #232 revision ad2a5af7b7121f5ad770073abc6d22e46c499f0516d3c9998f25565a0a7a4df1. Snapshot us001-run7-scoped, implementation digest ac086ebcfb5fd0147ec54684485218280b193c063d88b0794a5bb567aa92047d. Factory request 836d505bdf15f79256b14c72e836fe30 records passing checks with immutable source bytes.

- us001-race: `go test -race ./internal/github` passed (2.336s).
- us001-build: `go build ./...` passed (exit 0).

Specialists additionally executed isolated unit race/coverage cases and real-client HTTP cold/warm/changed cases. HTTP fixture coverage includes issue/direct-parent changes, stale-error invalidation, independent cached pagination with changed tail, ID filtering, and later-page failure/cancellation without partial results. Unit coverage includes representation/auth isolation, finite limits, oversized complete passthrough, eviction/restart and concurrent cloned responses.

Initial aggregate make check stopped at test lint diagnostics; corrected by test specialists. Pinned lint then passed with zero issues. Aggregate race suite is in progress and will be rerun against final implementation if later source changes occur. No pending check is recorded as passing. Offline fixtures do not prove live endpoint validator support or production request savings.

## US-002 scoped snapshot

Frozen story #233 revision 9c1be5ccfdc83bb4e2fa29b7ab59ccee742892744c704e8616c88c630aa8957e. Snapshot us002-run7-scoped, digest 2c6afd43f0217ab9aa96639eb487f1e1487ac2d52c2408415225f756d0f0afb4. Factory request 0b8136e8c99ec455a78d723b3cf291aa records specialist-reported executed checks with immutable source bytes:

- us002-scoped-race: `GOFLAGS=-p=1 go test -race ./internal/daemon -run 'TestDiscovery(Budget|HTTP)_'` passed 19.130s after final style correction.
- us002-lint: pinned v2.13.1 `make lint` with writable cache passed, 0 issues.

Fifteen injected-tick policy functions passed, covering exact reserve/spend, outstanding reservations, primary 304 refunds versus traffic, refill/burst, stale/reset/invalid metadata and fair history priority. Eight real HTTP/migrated SQLite scenarios passed; retained focused output is evidence/us002-final-http-race.log. These cover 100-page completion, same-parent rotation and cross-repository deferred priority, retained failed cursors, queued active delivery at floor, concurrent real-request settlement, cancellation and genuine backoff.

Initial aggregate result: FAIL at internal/turn 20-minute package timeout (other packages passed). No passing aggregate evidence claimed. Stale standalone daemon run explicitly cancelled after fixture correction. Final serialized full make check is running; all tests, race instrumentation and configured timeout remain unchanged.

Final serialized aggregate attempt found daemon failures (494.553s) in backlog probe spies, advanced-clock quota fixtures and immediate-concurrency assumptions. The check process was cancelled after demonstrated failure; no success is claimed. Full available output is evidence/aggregate-cycle2-daemon-failures.log. US-002 invalidation request8726a7b8df518875c89b6d8fa24e29ee records valid input and rejected engineering state pending replacement. Second fix/retest cycle is active.

Second-cycle targeted retests passed: backlog/cross-repository/refill 19.112s; confirmation/recovery 20.317s; HTTP/store/delivery/backoff 15.967s. Corrected US-002 capture `us002-run7-cycle2` (digest `d3bdb9a1ccfa05a46041c99534fd274541286d9589b858770f84863bde560417`) recorded with request `6ec04ea2aa6819f87064a00a1af15ee5`. Final serialized make check has passed format/vet/lint and is rerunning all race tests.

## Final result — incomplete aggregate gate

Final build passed (`evidence/final-build.log`). Final format/vet/pinned lint passed, but the complete race gate timed out after 20 minutes in `TestABackoffArmedMidTickStopsFurtherDispatch` (blocked 17m37s in gatedForge.GetIssue during fixture.settle). Full output is `evidence/final-aggregate-timeout.log`; no successful aggregate result is recorded. The test process was cancelled after the known failure.

US-002 final invalidation request `8f8ee053ce704f340bd422b6ba142b60` returned valid input, engineering rejected and remote effects pending. Scope snapshots and passing focused checks remain historical engineering evidence only. Final aggregate evidence and save are missing. No Validate work was performed.
