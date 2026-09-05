# Implement mode notes

## Verified state at start of this run

- `git status --short` clean (worktree has no diff vs HEAD -- meaning any prior interrupted-run code, if it exists, is already reflected in tracked files, not sitting as uncommitted changes at run start).
- `go build ./...` -- clean, no errors.
- `go vet ./...` -- clean.
- `go test ./...` -- all packages pass, including `internal/cli` (12s) which contains `router_test.go`, `cmd_logs_test.go`, `cmd_logs_tail_test.go`, `cmd_logs_render_test.go`.
- Files present: `internal/cli/cmd_logs_app.go`, `cmd_logs_slog.go`, `cmd_logs_tail.go`, `cmd_logs_tail_test.go`, `cmd_logs_render_test.go`, alongside pre-existing `cmd_logs.go`, `cmd_logs_render.go`, `cmd_logs_test.go`.
- Because the full suite is green with these files present, the previously-flagged atomic-shipping risk (US-1's `Args.Min: 0` landing without US-4's `len(args)==0` guard, which the plan brief warned would panic) is NOT present as a live bug -- a panic would fail `go test`. This is verified fact, not assumption, established via `go build`/`go test`, per this run's explicit instruction to check rather than trust the prior research note.
- This does NOT yet confirm each story's individual ACs are fully met (e.g. flag-applicability symmetry, golden-test byte-identity, tail backward-scan efficiency, `--level`/`--grep`/stderr-advisory features which appear architecturally to require US-5/6/7/8 -- not yet confirmed present). Implementation engineers are tasked with verifying/completing their assigned stories against full AC lists, not just re-implementing blind.

## Phase 1 batching rationale (deviation from strict 1-engineer-per-story parallel dispatch)

Plan brief's own "Workflow" section groups stories by hard dependency, and several
stories are documented as sharing a single file/insertion-point:
- `logsOptions.bind` is the explicit single flag-declaration site for `--level`
  (US-5), `-n`/`--all` (US-9) -- editing it concurrently from separate agents in
  the same worktree risks lost/conflicting edits, not just a logical ordering
  issue.
- US-1 and US-4 are contractually required to ship as one atomic change
  (plan_brief.md Shared Decisions).
- US-9's computed offset is explicitly hand-off input to US-2's follow loop
  (tailLog/follow-loop coupling), and both are believed already substantively
  implemented per the prior research note.

Given this, Phase 1 ran in three waves instead of one flat parallel batch:
- Wave 1 (parallel): US-3 (renderer, standalone), US-6 (independent track), US-1+US-4 (atomic pair, standalone files)
- Wave 2 (after Wave 1): US-9+US-2 (tail default + follow, needs US-3's decoder and US-1's routing)
- Wave 3 (after Wave 2): US-5+US-7+US-8 (level filter + grep wiring + stderr advisory -- linear dependency chain, all touch the shared logsOptions.bind/rendering-pipeline file)

Each engineer was instructed to first verify what already exists for their
assigned story/stories against that story's Acceptance Criteria, and to
implement only what is missing or fix what is broken -- not to blindly
re-implement working code.

## Wave 1 results (US-3, US-6, US-1+US-4)

All three engineers confirmed prior-run production code was already correct
for their assigned ACs and made minimal additional changes:

- **US-3**: T-3.1-T-3.8 all confirmed correct as-is, no production edits needed.
  T-3.9 (golden test against real slog.TextHandler) is confirmed MISSING --
  flagged for go-unit-test-engineer in Phase 3. Engineer left a stray file
  `internal/cli/zzverify_slog_test.go` (a scratch verification file, neutralized
  to an inert `package cli` stub after being unable to delete it in-sandbox).
  This file must be deleted and replaced by a proper golden test file as part
  of Phase 3 unit testing.
- **US-6**: T-6.1-T-6.5 all confirmed correct as-is (including
  `.aiteam/AC_AMENDMENTS.md`'s DEV-1 record), no production edits needed.
  T-6.6 (tests for --level on daemon run/start) confirmed MISSING -- flagged
  for go-unit-test-engineer in Phase 3.
- **US-1+US-4**: added `internal/cli/cmd_logs_app_test.go` covering T-1.6
  (bare logs with no DB, no daemon.log, `logs daemon` rejection, flag-applicability
  symmetry for all issueOnlyFlags). router_test.go/parse_test.go (T-4.3-T-4.5)
  confirmed already correct, no edits needed. Noted AC-1.4's story text names a
  nonexistent `--level` flag on `logs` (conflated with US-6's daemon-side flag) --
  treated as a spec typo; verified against the real existing app-log-only/issue-only
  flag sets instead. Also surfaced (not fixed, out of scope for this story):
  pre-existing gosec G104 findings in cmd_logs_app.go (unchecked f.Close() at
  lines 114, 124, 132) -- flagged for code review / a later cleanup pass.

All three: go build/vet/test -race clean.

## Wave 2 results (US-9+US-2)

Engineer confirmed prior-run production code was already fully correct for
both stories; no production edits were needed.

- **US-9**: `cmd_logs_tail.go` -- AC-9.1-AC-9.4 all confirmed satisfied
  (`defaultTailLines = 200`, `fs.Visit`-based explicit-flag detection for
  mutual exclusivity + issue-path rejection, backward-scan via `f.ReadAt` in
  64 KiB chunks never loading the whole file, short-file falls through to
  offset 0). T-9.5 tests already comprehensive in `cmd_logs_tail_test.go`,
  no gap.
- **US-2**: `cmd_logs_app.go` -- AC-2.1-AC-2.5 all confirmed satisfied
  (`followAppLog`/`drainAppLog` poll via `time.Ticker` + `select` on
  `ctx.Done()`, no goroutine; `emitAppLogLines` discards `sink.write`'s
  `done` so `msg=result` never stops follow; not-yet-existent file treated
  as "wait" via `fs.ErrNotExist`; stat-based truncation detection reopens at
  offset 0 and clears `pending`). `logsSourceFiles` already lists both
  files.
  **T-2.5 (follow-path tests) confirmed MISSING** -- flagged for
  go-unit-test-engineer in Phase 3: streaming-under-cancellation,
  `msg=result` non-stop, wait-for-nonexistent-file, truncation-resume,
  ideally reusing existing `recordingWriter`/`appendToFile`/`waitForWrites`
  helpers in `cmd_logs_test.go`.

Files touched: none. `go build`/`go vet`/`go test ./internal/cli/... -race
-count=1` all clean (17.6s).

## Wave 3 results (US-5+US-7+US-8)

Engineer made real production edits this time (unlike Waves 1-2, which found
everything already correct):

- `cmd_logs.go`: added `--level` binding + doc text; wired `validateLevelFlag`.
- `cmd_logs_tail.go`: added `validateLevelFlag` (AC-5.2 vocabulary-naming usage
  error, AC-5.4 issue-path rejection naming `--level`).
- `cmd_logs_app.go`: removed `grep`/`i`/`invert` from `issueOnlyFlags` (US-7
  requires these to apply on the bare app-log path); `runAppLog` now builds
  the grep filter via `newLineFilter` (AC-7.2 exit 2 on bad regexp), wraps the
  sink in a new `levelFilterSink` when `--level` given (AC-5.1/5.3, level
  filtering composes before grep per US-7's architecture note); added
  `appLogLineLevel`/`levelFilterSink`/`newLevelFilterSink`; added the US-8
  stderr advisory emitted once after the initial tail pass, structurally
  outside `followAppLog` so it cannot repeat in the follow loop (AC-8.1/8.2).
- `cmd_logs_slog.go`: `slogTextSink`/`newSlogTextSink` gained a `grep` filter
  applied in `write`.

**Known test conflict surfaced (needs Phase 3 fix, not a bug):**
`cmd_logs_app_test.go`'s `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue` has
three subtests (`--grep`, `-i`, `--invert`) asserting these are rejected with
no issue -- this was the correct pre-US-7 contract but US-7 explicitly
requires the opposite (these flags now apply to the app-log view). The
engineer could not edit test files under its story scope; these three
subtests currently fail. go-unit-test-engineer must update this test in
Phase 3 to reflect the new, intended contract (grep/-i/--invert apply
app-log-side; only `--run/--runs/--only/-i-is-NOT-here.../--full/--repo`-style
truly-issue-only flags stay rejected -- verify against the final
`issueOnlyFlags` slice). All other `internal/cli` tests pass.

The engineer's report also mentioned a "background agent" it could not read
output from and could not coordinate with -- I (orchestrator) have not
dispatched any such agent and see no evidence of one; treating this as a
sub-agent artifact/confusion, not a real concurrent process. Verified via the
engineer's own build/test confirmation that its changes are self-consistent
and `go build`/`go vet` are clean.

Build/vet: clean. `go test ./internal/cli/... -race -count=1`: all pass
except the three known, expected `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue`
subtests above.

## Gate A -- all Phase 1 waves reported DONE (2026-09-05)

All nine stories (US-1..US-9) now have production code confirmed correct or
completed. Proceeding to Phase 1.5 Code Review. Known open items to hand the
reviewer: T-3.9 (US-3 golden test missing), T-6.6 (US-6 tests missing), T-2.5
(US-2 follow-path tests missing), the stray `zzverify_slog_test.go` inert
stub, the three now-failing `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue`
subtests (US-7-caused, expected, needs test-file update not production fix),
the AC-1.4 spec-typo (nonexistent `--level` flag on bare `logs`, conflated
with US-6), and the pre-existing gosec G104 unchecked-`f.Close()` findings in
`cmd_logs_app.go` (lines noted in Wave 1 results).

## Phase 1.5 Code Review -- cycle 1 (2026-09-05)

`go-code-reviewer` verdict: BLOCKING. Full report at `CODE_REVIEW.md`.

- **CR-B-001 (BLOCKING)**: `cmd_logs_app_test.go`'s
  `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue` subtests `--grep`/`-i`/`--invert`
  fail against the real build (`exit code = 0, want 2`) -- confirms the known
  Wave 3 test conflict. Production code is correct; only the test table is
  stale. Routing to go-implementation-engineer to update the test.
- **CR-A-001..005 (ADVISORY)**: T-3.9 golden test + stray
  `zzverify_slog_test.go` missing/stray; T-6.6 (`--level` on daemon run/start)
  tests missing; T-2.5 (follow-path, incl. `msg=result` non-stop regression)
  tests missing; three unchecked `f.Close()` in `cmd_logs_app.go` lines
  143/153/161 (newly introduced this feature, non-hot-path). All deferred to
  Phase 3 test agents / recorded here, non-blocking.
- Cross-story checks (atomic US-1+US-4 guard, `--level` dual-mechanism
  separation, sole `ParseLevel` parser, `internal/launchd` untouched, no
  goroutines/channels/mutexes/IPC, no `internal/redact` import, `daemon.log`
  access path, level-before-grep composition order, AC-8.2 fire-once) all
  confirmed CLEAN.

## Phase 1.5 Code Review -- cycle 2 (2026-09-05)

CR-B-001 fixed (test-only, no production edits): removed the three stale
`--grep`/`-i`/`--invert` subtests from
`TestLogsIssueOnlyFlagsAreRejectedWithNoIssue` (remaining table now matches
`issueOnlyFlags` exactly: `--run`/`--runs`/`--only`/`--full`/`--repo`); added
`TestLogsGrepAndInvertApplyWithNoIssue`, a real positive-case test confirming
these flags filter output correctly on the bare app-log path. Re-review
confirmed the fix, hand-verified assertions against `newLineFilter`/
`rawSink.write`, and reran `go build`/`go vet`/`go test -race` all green.

**Final verdict: ADVISORY_ONLY.** No BLOCKING findings remain. Advisories for
Phase 3 / future cleanup (from `CODE_REVIEW.md`):
- CR-A-001: T-3.9 golden test (renderSlogLine vs real slog.TextHandler) missing;
  stray inert `zzverify_slog_test.go` should be deleted.
- CR-A-002: T-6.6 tests for `--level` on `daemon run`/`daemon start` missing.
- CR-A-003 (highest priority for Phase 3): T-2.5 follow-path tests missing --
  streaming-under-cancellation, `msg=result` non-stop regression,
  wait-for-nonexistent-file, truncation-resume.
- CR-A-004: three unchecked `f.Close()` in `cmd_logs_app.go` (lines 143, 153,
  161), newly introduced this feature, non-hot-path -- suggest `defer
  f.Close()` or explicit `_ = f.Close() // best-effort`.
- CR-A-005: grouped with CR-A-001 (stray file deletion).
- CR-A-006 (new, minor): no test asserting malformed `--grep` regexp errors on
  the bare (no-issue) path specifically (only tested on issue path); not a
  regression risk since both paths share `newLineFilter`, optional symmetry
  test.

Proceeding to Phase 2 -- Environment Setup.

## Phase 2 -- Environment Setup (2026-09-05)

Determination: SKIP. Reviewed plan_brief.md's Story Index and every story's US-1..US-9
Architecture Notes. Cross-Story Constraints (Platform) are explicit: "everything needed
is stdlib (encoding/json, log/slog, os, time, regexp)... internal/logging gains no new
exported decoder... stays a leaf package except for US-6's use of its existing
Options.Level/ParseLevel." Security constraint: "No daemon IPC of any kind -- no socket,
no signal, no lock file." US-6's daemon-start half only appends an argv element consumed
by internal/launchd (ring-fenced, untouched, no new sanitisation). No story opens a
database, makes an external HTTP call, or touches a message queue -- every story reads/
writes local files only (daemon.log via config.Paths.DaemonLogPath(), and US-6's plist via
existing internal/launchd machinery). No TEST_BASE_URL / TEST_DATABASE_URL provisioning is
applicable. Proceeding directly to Phase 3 -- Testing, dispatching go-unit-test-engineer
only; go-integration-test-engineer is skipped for this feature.
