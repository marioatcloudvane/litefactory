# Spike — test-coverage

2026-08-30. Asked: where does litefactory stand on test coverage, unit *and*
integration? A look, not a change.

## What was done

- Inventoried every test file, package, and build tag in the module.
- Ran `go test ./... -covermode=atomic -coverprofile=...` and broke the profile
  down by package, file, and function.
- Read the test-support code (`internal/gittest`) and grepped for the markers
  that separate a unit test from an integration test here — real subprocesses,
  real sqlite files, `httptest`, network hosts, `t.Skip` gates.

## What was found

### Headline

**87.0% of statements**, module-wide, all tests green. 79 test files, ~683 test
functions, 18 packages — **every package has tests**. Full suite ≈ 110s of
package time (parallel; wall clock well under that).

### By package

| Package | Coverage |
|---|---|
| internal/naming | 98.9% |
| internal/secrets | 95.7% |
| internal/gittest | 95.3% |
| internal/github | 94.9% |
| internal/config | 94.8% |
| internal/logging | 93.6% |
| internal/harness | 93.1% |
| internal/identity | 92.9% |
| internal/redact | 91.5% |
| internal/report | 90.0% |
| internal/launchd | 89.8% |
| internal/daemon | 87.9% |
| internal/repos | 87.1% |
| internal/turn | 86.1% |
| internal/git | 83.6% |
| internal/cli | 81.1% |
| internal/store | 78.9% |
| cmd/litefactory | 34.8% |

### Unit vs. integration — the split is real but unlabelled

There is **no `//go:build integration` tag anywhere in the repo**, no
`testing.Short()` gate, and no separate make target. `make test` runs
`go test -race ./...` and that is the whole suite. So "unit" and "integration"
are not a distinction the build makes — but the tests themselves fall into
three tiers:

1. **Pure unit** — table-driven, no I/O. Most of `config`, `naming`, `redact`,
   `report`, `turn`, `identity`.
2. **Integration-in-all-but-name, hermetic** — real dependencies, temp dirs, no
   network:
   - `internal/git` + `internal/gittest`: every fixture is a **real repository
     built by real git**, deliberately (the package doc says a mock "would only
     ever confirm what we already believed"). Hermetic: HOME redirected, global
     and system git config off, fixed identity, dates and branch.
   - `internal/store`: real sqlite files under `t.TempDir()`, including a
     reopen-and-migrate test — not `:memory:`.
   - `internal/harness`, `internal/launchd`: real subprocesses, signal and kill
     paths.
   - `internal/github`, `internal/cli`, `internal/daemon`: `httptest` servers
     for the GitHub API surface.
3. **Host-dependent, skipped when absent** — 7 `t.Skip` sites: the real
   `/usr/bin/security` keychain shell-out, `claude` on the launchd PATH, home
   directory presence. These silently do not run on machines lacking the
   dependency (and in most CI images).

**No test touches the real network.** `internal/repos/network_test.go` goes
further and enforces that mechanically: it parses the AST of `repo add` and
fails if it can reach `git ls-remote` or `Clone.Fetch`, so the offline
guarantee cannot regress unnoticed.

### Where the gaps are

Worst files by average function coverage:

| File | Avg | Note |
|---|---|---|
| `cmd/litefactory/main.go` | 33% | `main`, `run`, `openLogSink` all 0% — process entrypoint |
| `internal/daemon/executor.go` | 50% | `Execute` 0% |
| `internal/store/repos.go` | 53% | `GetRepoByPath`, `GetRepoBySlug`, `Slug` all 0% |
| `internal/store/segments.go` | 58% | `GetSegment` 0% |
| `internal/store/runs.go` | 69% | `SetRunCommit`, `GetRunByOrdinal` 0% |
| `internal/cli/cmd_daemon.go` | 72% | `writeDaemonRunReport`, `daemonRunError`, `daemonClaudeOrigin` 0% |

32 functions sit at exactly 0%. Roughly half are noise —
`Error()`/`Unwrap()`/accessor one-liners on error types (`redact/errors.go` has
three, `git`, `launchd`, `store` one each). The ones that actually matter:

- **`internal/store` getters** — `GetComment`, `GetRepoByPath`, `GetRepoBySlug`,
  `GetSegment`, `GetRunByOrdinal`, `SetRunCommit`. Real query paths against a
  real schema, entirely unexercised. Cheapest genuine win in the repo, and the
  reason `store` is the lowest non-`cmd` package at 78.9%.
- **`internal/daemon/executor.go:Execute`** — the daemon's execution seam.
- **`cmd/litefactory` at 34.8%** — expected for a `main`, less interesting.

Sub-60% but non-zero, worth a glance: `daemon/setup.go:validate` (42.9%),
`turn/execute.go:validate` (37.5%), `cli/cmd_task.go:taskLookupError` (36.4%),
`github/issues.go:labels` (25.0%), `report/reply.go:String` (22.2%).

## Decisions worth remembering

- The absence of an `integration` build tag looks like a deliberate choice, not
  an oversight: the project's position (stated in `internal/gittest`'s package
  doc) is that real dependencies *are* the unit under test, so there is nothing
  to segregate. Anyone proposing a tag split should argue against that doc, not
  around it.
- Coverage numbers here are honest in a way tag-split projects often are not —
  87% is measured with the real git, sqlite, and subprocess paths included.
- The 7 `t.Skip` gates are the one place the number can quietly overstate
  reality: on a Linux CI box, the keychain and launchd-PATH tests vanish and
  nobody is told.

## Not done

- Did not run with `-race` (the Makefile's `make test` does; coverage run here
  did not, to keep it fast). Numbers are unaffected.
- Did not measure cross-package coverage (`-coverpkg=./...`), which would show
  how much of, say, `internal/store` is reached incidentally by `internal/cli`
  tests. Per-package numbers above are each package's *own* tests only.
- Did not assess test *quality* — assertion strength, mutation survival. This is
  line coverage, and line coverage only.
- Wrote no tests and changed no code.

## Recommendation

Nothing here needs Research or Plan. The coverage story is healthy; the gap list
is short, concrete, and mechanical.

If you want to act on it, the highest-value single move is a **Spike or small
Implement pass on the `internal/store` getters** — six unexercised query
functions against a schema that is already fixtured in `store_test.go`. That
alone lifts the weakest real package out of the 70s.

Consider separately whether the 7 `t.Skip` gates should fail loudly in CI rather
than skip, so coverage does not silently shrink on machines without a keychain.
