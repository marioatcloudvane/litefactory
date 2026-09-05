# Code Review
# Feature: logging-is-only-on-issues-but-not-on-the (app-level log view, `litefactory logs`)
# Reviewed by: go-code-reviewer
# Date: 2026-09-05 (cycle 1), 2026-09-05 (cycle 2 re-review)
# Status: ADVISORY_ONLY

---

## Cycle 2 re-review (this run)

**Scope:** `internal/cli/cmd_logs_app_test.go` only, per orchestrator instruction — a
test-only fix responding to CR-B-001 below. No production code was touched by the
engineer between cycle 1 and cycle 2, so nothing outside this file was re-read.

### CR-B-001 — RESOLVED, confirmed

Verified against the current file:

1. **Stale subtests gone, table matches production.** `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue`'s
   table (`cmd_logs_app_test.go:102-111`) now lists exactly `--run`, `--runs`, `--only`,
   `--full`, `--repo` — a 1:1 match against `issueOnlyFlags = []string{"run", "runs",
   "only", "full", "repo"}` in `internal/cli/cmd_logs_app.go:32`. The three stale
   `--grep`/`-i`/`--invert` subtests are no longer present anywhere in the file.
2. **New positive test is real and correct.** `TestLogsGrepAndInvertApplyWithNoIssue`
   (`cmd_logs_app_test.go:134-188`) writes three JSONL lines to a real `daemon.log`
   under a temp `LF_HOME`, then dispatches three subtests through the actual router
   (`runBareLogs` → `NewRouter(...).Run(...)`, no shortcuts, no mocking of the filter):
   - `grep matches`: `--raw --grep "tick failed"` → expects only `errorLine` on stdout.
   - `grep is case-insensitive with -i`: `--raw --grep "TICK FAILED" -i` → expects
     `errorLine`. This exercises the real case-folding path — confirmed by reading
     `newLineFilter` (`cmd_logs_render.go:152-164`), which prepends `(?i)` to the
     pattern only when `ignoreCase` is set, and `lineFilter.match` (`:168-173`), which
     does `f.re.Match(...) != f.invert`.
   - `invert keeps everything but the match`: `--raw --grep "tick failed" --invert` →
     expects `startedLine + stoppedLine`. This matches `match`'s `!= f.invert` logic
     exactly (a matching line is excluded, all others pass).
   All three assertions were checked by hand against `rawSink.write` and
   `lineFilter.match`'s actual logic (`cmd_logs_render.go:189-201`, `:166-173`) and are
   correct. This is not a stub — it exercises the real `--grep`/`-i`/`--invert`
   composition end-to-end via `--raw`, which sidesteps slog-text re-rendering so the
   assertions are exact-byte comparisons, not fuzzy `strings.Contains` checks.
3. **Suite is green.** Ran, from this worktree:
   - `go build ./...` — clean, no output.
   - `go vet ./...` — clean, no output.
   - `go test ./internal/cli/... -race -count=1` — `ok  github.com/cloudvane/litefactory/internal/cli  17.562s`.
   - `go test ./internal/cli/... -run 'TestLogsIssueOnlyFlagsAreRejectedWithNoIssue|TestLogsGrepAndInvertApplyWithNoIssue' -v` —
     all 5 + 3 subtests PASS, no failures, no skips.

**Verdict on CR-B-001: RESOLVED.** The fix is mechanical, test-only, and correctly
scoped — the table now reflects the true `issueOnlyFlags` contract and the removed
negative coverage was replaced with real positive coverage, not merely deleted.

### CR-A-006 (new, minor): no bad-`--grep`-regexp coverage on the bare (no-issue) path

**File:** `internal/cli/cmd_logs_app_test.go`
**Story:** US-7
**Category:** Tests
**Finding:** `cmd_logs_render_test.go:319-321` already asserts that a malformed
`--grep` pattern (`"(unclosed"`) exits `ExitUsage` on the **issue** path (`logs 123
--grep (unclosed`). No equivalent assertion exists for the **bare** no-issue path
(`logs --grep (unclosed`). In practice this is very low risk: `runAppLog` and
`runLogs` both call the same shared `newLineFilter` (`cmd_logs_render.go:152`) and
both wrap its error identically via `Usagef(cmd, "%s", err)` (compare
`cmd_logs_app.go:92-95` against the issue-path equivalent), so the two call sites
share one code path for regexp compilation and cannot plausibly diverge in behavior.
Not a regression risk; not blocking.
**Suggestion:** Optionally add one more subtest to `TestLogsGrepAndInvertApplyWithNoIssue`
(or a standalone test) asserting `logs --grep (unclosed` exits `ExitUsage` on the bare
path too, for symmetry and to guard against the two call sites ever being
de-duplicated apart in the future.

---

## Cycle 1 findings (preserved)

### BLOCKING findings

#### CR-B-001: Stale test asserts flags rejected that production code deliberately made valid — RESOLVED IN CYCLE 2, see above.
**File:** `internal/cli/cmd_logs_app_test.go:106-113` (subtests `--grep`, `-i`, `--invert` of `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue`)
**Story:** US-7 (test not updated alongside the production change)
**Category:** Error handling / Tests — failing suite blocks Gate B
**Finding:** `issueOnlyFlags` in `internal/cli/cmd_logs_app.go:32` is `[]string{"run", "runs", "only", "full", "repo"}` — `--grep`, `-i`, and `--invert` were deliberately removed from this list by US-7 (the file's own doc comment at lines 29-31 explains why: "--grep, -i and --invert are deliberately not listed either: they filter the app log's rendered ... line just as they filter an issue's run log (US-7)"). `cmd_logs_app_test.go`'s `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue` still contains subtests for exactly those three flags expecting `ExitUsage`. Verified by running the suite:
```
go test ./internal/cli/... -run TestLogsIssueOnlyFlagsAreRejectedWithNoIssue -v
--- FAIL: TestLogsIssueOnlyFlagsAreRejectedWithNoIssue/--grep
--- FAIL: TestLogsIssueOnlyFlagsAreRejectedWithNoIssue/-i
--- FAIL: TestLogsIssueOnlyFlagsAreRejectedWithNoIssue/--invert
```
This is the only failure in the full module (`go test ./...` and `go vet ./...` are otherwise clean). This is a test-file bug, not a production bug: the production behavior (letting `--grep`/`-i`/`--invert` apply to the bare app-log view) is correct per US-7 and per the shared decision that level-filter/grep composition applies uniformly.
**Required fix:** Remove the `--grep`, `-i`, and `--invert` cases from `TestLogsIssueOnlyFlagsAreRejectedWithNoIssue`'s table (they no longer belong to the issue-only-flags contract), and add/confirm positive coverage elsewhere (e.g. in a grep/level composition test) that these three flags are accepted and function correctly on the bare `logs` invocation. This must be fixed before Phase 3 test agents run — a failing suite blocks Gate B.

---

### ADVISORY findings (still open, deferred to Phase 3 — unchanged from cycle 1)

#### CR-A-001: Missing golden test for slog-text re-render byte-identity (T-3.9)
**File:** `internal/cli/cmd_logs_render_test.go`, `internal/cli/cmd_logs_slog.go`
**Story:** US-3 / T-3.9
**Category:** Tests
**Finding:** No test anywhere in the package diffs `renderSlogLine`'s re-rendered output against real `slog.NewTextHandler` output for byte-identity (confirmed via `grep -rn "NewTextHandler|golden" internal/cli/*_test.go` — no matches). T-3.9 called for exactly this golden test. `internal/cli/zzverify_slog_test.go` is a genuinely inert stub: `package cli` with no test functions, containing only a comment stating it is "a leftover scratch artifact from a prior implementation run" that "could not be deleted ... due to sandbox restrictions on rm/mv" and should be replaced by a real test engineer's golden test.
**Suggestion:** Delete `zzverify_slog_test.go` (or empty it further/remove the file) once a real golden test lands, and add the T-3.9 golden test comparing `renderSlogLine`'s output against `slog.NewTextHandler`-produced output for a representative record set (multiple field types, `WithGroup`, non-ASCII, need-for-quoting cases).

#### CR-A-002: No test coverage for `--level` on `daemon run`/`daemon start`
**File:** `internal/cli/cmd_daemon.go` (`LogLevelOverride`), `internal/cli/cmd_daemon_agent.go` (`daemonProgram`)
**Story:** US-6 / T-6.6
**Category:** Tests
**Finding:** `internal/cli/cmd_daemon_run_test.go` and `internal/cli/cmd_daemon_agent_test.go` contain no references to `LogLevelOverride` or `daemonProgram` (confirmed via grep on both files — zero matches). The dual-binding mechanism (hand-rolled argv scan in `main.go`'s `run()` for `daemon run`, plist-argument construction in `daemonProgram` for `daemon start`) is entirely untested: no test exercises `--level`/`-level`/`--level=X`/`-level=X` parsing, the "flag needs an argument" error path, an invalid level value's exit-2 usage error, or that `daemon start --level warn` produces a `ProgramArguments` slice ending in `--level warn`.
**Suggestion:** Add unit tests for `LogLevelOverride` covering all four accepted spellings, the missing-argument case, and an invalid-level case (expect wrapped `logging.ParseLevel` error); add a test for `daemonProgram(level string)` confirming the appended argument shape and confirming an empty `level` appends nothing.

#### CR-A-003: No test coverage for the app-log follow path
**File:** `internal/cli/cmd_logs_app.go` (`followAppLog`, `drainAppLog`)
**Story:** US-2 / T-2.5
**Category:** Tests
**Finding:** No test in `cmd_logs_app_test.go` or elsewhere calls or exercises `followAppLog`/`drainAppLog` (confirmed via grep — zero matches for either identifier in any `_test.go` file). `TestLogsRawAndFollowStillApplyWithNoIssue` despite its name only asserts `--raw` is accepted with no issue; it never passes `-f`. Untested scenarios include: streaming under context cancellation, the "a record whose text reads like `msg=result` must never stop the follow loop" invariant (the single most safety-critical behavioral constraint of this shared decision), waiting for a not-yet-existent `daemon.log` to appear, and resuming correctly after truncation/rotation (reopen-at-offset-0).
**Suggestion:** Add tests for each of the four scenarios above, in particular a regression test asserting that a synthesized line containing `msg=result` does not stop `followAppLog`'s loop — this is explicitly called out in the plan brief as the sink contract's most important behavior and currently has no test proving it holds.

#### CR-A-004: Unchecked `f.Close()` on early-return error paths
**File:** `internal/cli/cmd_logs_app.go:143, 153, 161`
**Story:** US-1/US-2 (new file, no prior history — confirmed via `git log --oneline -- internal/cli/cmd_logs_app.go`, single commit, and `git show main:internal/cli/cmd_logs_app.go` fails with "exists on disk, but not in main")
**Category:** Resource management
**Finding:** Three call sites in `runAppLog` call `f.Close()` without checking or explicitly discarding its return value, immediately before returning a different, already-relevant error (seek failure, sink write failure, read failure). This is newly-introduced code, not inherited from `main`. The success path a few lines later (line 170, `closeErr := f.Close()`) does check the close error, showing the pattern was known but not applied uniformly. This function runs at most once per `logs` invocation (not a loop), so this is not a hot-path fd leak; gosec G104 would still flag these three lines.
**Suggestion:** Either use `defer f.Close()` immediately after the successful `os.Open` (eliminating the three duplicated manual closes) with the deferred error handled/logged as best-effort, or make the discard explicit: `_ = f.Close() // best-effort; the seek/read/write error above is the one that matters`.

#### CR-A-005: Stray inert test-stub file left in the tree
**File:** `internal/cli/zzverify_slog_test.go`
**Story:** US-3
**Category:** Idiom / cleanliness
**Finding:** Same file as CR-A-001 — flagged separately here because independent of whether the golden test lands, this file itself (a `zz`-prefixed, no-op `package cli` file whose own comment says it "is safe to delete") should not remain in the committed tree past this review cycle.
**Suggestion:** Delete the file. (Grouped with CR-A-001's fix since a real golden test file is the natural replacement.)

---

## Confirmation of the 8 flagged open items (cycle 1)

1. **T-3.9 golden test / `zzverify_slog_test.go`** — CONFIRMED on both counts. The byte-identity golden test against `slog.NewTextHandler` is missing (CR-A-001). `zzverify_slog_test.go` is genuinely inert — no test functions, just an explanatory comment — and should be deleted (CR-A-005).
2. **T-6.6 gap: no tests for `--level` on `daemon run`/`daemon start`** — CONFIRMED. Neither `cmd_daemon_run_test.go` nor `cmd_daemon_agent_test.go` references `LogLevelOverride` or `daemonProgram` (CR-A-002).
3. **T-2.5 gap: no tests for the app-log follow path** — CONFIRMED. No test invokes `followAppLog`/`drainAppLog`; the four named scenarios (cancellation, no-stop-on-`msg=result`, wait-for-nonexistent-file, truncation-resume) are all untested (CR-A-003).
4. **Known-failing tests from US-7 side effect** — CONFIRMED REAL via `go test` in cycle 1; RESOLVED in cycle 2 (see above).
5. **AC-1.4 spec-typo reading** — CONFIRMED reasonable. The engineer's own doc comment on `issueOnlyFlags` (`cmd_logs_app.go:20-32`) lays out the full symmetry explicitly: `-n`/`--all`/`--level` are app-log-only and rejected on the issue path by `validateTailFlags`/`validateLevelFlag`; `--run`/`--runs`/`--only`/`--full`/`--repo` are issue-only and rejected on the bare path by `validateFlagApplicability`; `--raw`/`-f`/`--grep`/`-i`/`--invert` apply to both paths. This is a coherent, correctly-implemented two-way symmetry, and the spec's naming of a nonexistent `logs --level` flag under AC-1.4 is reasonably treated as a typo/conflation with US-6's `--level`.
6. **Pre-existing gosec G104 unchecked `f.Close()`** — PARTIALLY CONFIRMED, provenance clarified: these are NOT pre-existing — `cmd_logs_app.go` does not exist on `main` at all (confirmed via `git show main:internal/cli/cmd_logs_app.go` → fatal: path does not exist in main), so all three unchecked closes were introduced by this feature. Classified ADVISORY (not BLOCKING) per this review's own standard, since the calls sit on single-shot early-return paths, not hot/looped paths (CR-A-004).
7. **Level-filter/grep composition order** — CONFIRMED CORRECT. `levelFilterSink` wraps a grep-aware base sink, so level filtering runs before the base sink's grep check on the write path, achieving "level before grep" as required.
8. **AC-8.2 advisory fires once** — CONFIRMED CORRECT. The stderr advisory in `runAppLog` (lines 178-189) is emitted structurally after the initial tail pass completes and strictly before `followAppLog` is ever called (line 194), so it cannot re-fire per poll tick.

---

## Cross-story consistency

- **Ring-fence**: `internal/launchd` is completely untouched (`git diff main -- internal/launchd/` is empty) — `Manager.NewAgent`, `Agent.Validate`, `Agent.Render` unmodified. US-6's `--level` plumbing lives entirely in `cmd_daemon_agent.go`'s `daemonProgram`, as required.
- **Sole parser constraint**: `logging.ParseLevel` is the only level-parsing function used anywhere across this feature — confirmed at every call site touched (`main.go`'s `configuredLevel`/`LogLevelOverride` caller, `cmd_daemon.go`'s `LogLevelOverride` and `daemonStartCommand`, `cmd_logs_tail.go`'s `validateLevelFlag`, `cmd_logs_app.go`'s level handling). No second switch statement or direct `slog.Level.UnmarshalText` use found.
- **Two unrelated `--level` mechanisms kept separate**: `logs --level` (display-time filter, `logsOptions.level` + `validateLevelFlag`) and `daemon run`/`daemon start --level` (write-time threshold, `LogLevelOverride`/`daemonProgram`) do not share an option type and `logs --level` never touches `config.yaml` — confirmed by reading both code paths.
- **US-1 + US-4 atomic guard**: confirmed present and correctly ordered — `hasIssue := len(args) > 0` is computed before any validation or dispatch, and the `!hasIssue` branch (`runAppLog`) is checked before `args[0]` is ever read in `runLogs(..., args[0])`. No panic risk on bare `litefactory logs`.
- **Platform constraint**: grep sweep across all touched files for `go func`, `chan `, `sync.`, `os/signal`, `net/rpc`, `fsnotify` found zero real occurrences (`cmd_logs.go:30`'s single "fsnotify" hit is a comment explaining why it was *not* used, not an import). Every new `os.Open` against `daemon.log` (`cmd_logs_app.go`, `cmd_logs_tail.go`) carries a `#nosec G304` justification matching the existing pattern in `cmd_logs.go`.
- **Security**: no source file added by this feature imports `internal/redact` (`cmd_logs.go:194`'s own comment states the invariant explicitly, and the `TestLogsNeverImportsRedact` machinery in `cmd_logs_test.go` is driven by a `logsSourceFiles` slice that correctly lists all five feature files: `cmd_logs.go`, `cmd_logs_app.go`, `cmd_logs_render.go`, `cmd_logs_slog.go`, `cmd_logs_tail.go`). `daemon.log`'s path is obtained exclusively via `config.Paths.DaemonLogPath()` in `cmd_logs_app.go` — no raw `filepath.Join` against a home/`$LF_HOME` value found in any touched file.
- **Tests**: `router_test.go`'s deliberate US-4 contract change (moving the "missing positional" case to `task clean`) is clearly commented in place. `parse_test.go`'s local fixture retains `Min: 1` unchanged and is unaffected by the real `logs` command's `Min: 0`. The cycle 1 gap (CR-B-001) is resolved as of cycle 2; the one remaining minor gap is CR-A-006 above.
- **Risk not rising to a finding**: the app-log follow path (`followAppLog`) is new, moderately complex (its own truncation/rotation detection, distinct from `tailLog`), and currently has zero direct test coverage (CR-A-003). Nothing in a static read suggests a bug, but this is the single riskiest untested surface in the feature and worth calling out to the orchestrator as a priority for Phase 3's test agents even though it isn't itself a BLOCKING code issue.

---

## Verdict

ADVISORY_ONLY — CR-B-001 is resolved and confirmed (build, vet, and `-race` suite all green). 6 ADVISORY findings remain open (CR-A-001 through CR-A-006), none blocking. Proceeding to environment setup / Phase 2 is clear from a code-review standpoint; CR-A-001 through CR-A-006 are recorded for `notes.md` and are priorities for Phase 3's test engineers (in particular CR-A-003, the untested `followAppLog`/`drainAppLog` path, and CR-A-001/CR-A-005, the missing golden test and stray stub file).
