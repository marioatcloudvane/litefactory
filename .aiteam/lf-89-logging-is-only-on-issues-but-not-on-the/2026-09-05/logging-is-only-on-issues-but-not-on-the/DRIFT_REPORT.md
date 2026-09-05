# Drift Report — logging-is-only-on-issues-but-not-on-the
# Branch: lf/89-logging-is-only-on-issues-but-not-on-the | Date: 2026-09-05

## Delivered as planned

- US-1: Bare `litefactory logs` shows the app log, routed through the shared tail/render pipeline, with flag-applicability symmetry against the issue path. Pre-existing coverage confirmed sufficient; no new tests required.
- US-2: `-f` follow on the app log — synchronous poll loop, no goroutines/channels, `msg=result` does not stop follow (AC-2.2), waits for a not-yet-existing file (AC-2.3), resumes after truncation via stat-based reopen (AC-2.4). Closed this run with `cmd_logs_app_follow_test.go` and `cmd_logs_static_test.go` (AC-2.5 AST-based no-goroutine/no-channel check).
- US-3: Re-parsed JSON rendered as byte-identical `slog.TextHandler` text, order-preserving decode, `WithGroup` flattening, RFC3339-ms timestamp reformatting, `--raw` pass-through. Closed this run with the T-3.9 golden test suite (`cmd_logs_slog_test.go`), diffing real `slog.TextHandler`/`slog.JSONHandler` output.
- US-4: Bare invocation no longer prints a usage error; `Args.Min` and the `len(args)==0` branch landed atomically with US-1 as the plan required; the two pinned `router_test.go` expectations were updated as part of the same change.
- US-5: `--level` as a display-time filter on `logs`, using `logging.ParseLevel` exclusively.
- US-6: `--level` as a write-time flag on `daemon run`/`daemon start`, plist argument built independently of `internal/launchd`'s existing validation.
- US-7: `--grep`/`-i`/`--invert` carried to the app log as a pure wiring of the existing filter onto the rendered line.
- US-8: One-line stderr advisory when the requested `--level` was never captured, firing once after the tail pass, not repeating in follow.
- US-9: Tail-by-default (200 records), `-n`/`--all` flags with mutual-exclusivity and `-n >= 1` validation, backward-scan tail computation verified this run to never load the whole file (20,000-line multi-chunk fixture), computed offset handed to the follow loop without a second seek.
- Every new source file was added to `logsSourceFiles` in `cmd_logs_test.go`, keeping `TestLogsNeverImportsRedact`/`TestLogsUsesNoDaemonIPC` enforcement current.
- `internal/launchd` untouched, as ring-fenced.
- No new package, no new third-party dependency, no goroutine/channel/mutex introduced anywhere in the feature — confirmed by the module-wide `go vet ./...` pass and by `TestLogsFollowHasNoGoroutinesOrChannels`'s static AST check.

## Added scope

- `TestLogsFollowHasNoGoroutinesOrChannels` (`cmd_logs_static_test.go`): a static AST-based enforcement test for AC-2.5 was not separately called out as its own test file in US-2's task table, but is a direct, literal implementation of AC-2.5 rather than new behavior — added to make that constraint self-enforcing rather than reviewer-verified only.
- A locked-in test for the `time.Duration` JSON/Text encoding asymmetry (`TestRenderSlogLine_DurationAttr`) — this is a pre-documented, accepted limitation named in `cmd_logs_slog.go`'s own package comment, not new scope, but the plan's task table did not explicitly call for a dedicated regression test pinning that specific behavior. Added to prevent silent regression toward false byte-identity.

## Dropped or deferred

- CR-A-004 (three unchecked `f.Close()` return values in `cmd_logs_app.go`, non-hot-path): left open as a style/lint advisory from code review. No test can fail against it and no defect sits behind it; deferred to a future cleanup pass rather than blocking this run.
- CR-A-006 (optional symmetry test for malformed `--grep` on the no-issue path): explicitly marked optional/non-regression-risk by the code reviewer; not added this run.

## Architectural deviations

None. DEV-1 (US-6's `daemon run --level` early-argv-read) and DEV-2 (US-4's deliberate `router_test.go` updates) — both pre-recorded by the architect during Plan mode — were followed as specified; no further deviation was introduced during Implement.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (two additive regression tests beyond the task table, both directly implementing already-specified acceptance criteria/architecture notes rather than new behavior).
