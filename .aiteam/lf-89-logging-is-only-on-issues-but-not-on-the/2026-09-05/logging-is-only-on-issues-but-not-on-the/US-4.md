# US-4 — Bare invocation stops printing a usage block; help stays at `logs --help`

> As an operator, I want bare `litefactory logs` to stop printing a usage block, with the command's own help still reachable at `litefactory logs --help`, so that discoverability is not lost.

This story updates the `logs` command's help text and the tests that pin its old behavior, so the new bare-invocation contract is documented and its change is a recorded decision, not a silent edit.

## Why This Matters

Taking away the usage block on bare `litefactory logs` must not mean taking away the ability to find out what the command does — `logs --help` still needs to describe the new app-log mode alongside the existing issue mode. And because this changes what a very common invocation does, the change itself needs to be visible in the tests that describe today's behavior, not just made in the code and left for someone else to notice by accident later.

## Architecture Notes

- Pattern to follow: the router's help routing is already correct — `errHelpRequested` causes the command's usage to print to stdout with exit 0. Nothing about *how* help is served changes here; only the *no-argument* case's behavior does (delivered in US-1).
- Platform: `writeCommandUsage` rebuilds the flag set purely to enumerate flags, so `Command.New` must have no effect beyond binding them — this story's `Usage`/`Long` changes must not add any file access, path resolution, or `config.Locate()` call inside `New`.
- Update `Usage` to `logs [<issue>] ...` — the well-formed-commands check requires the literal `<issue>` substring, which `[<issue>]` still satisfies — and add a `Long` paragraph describing the app-log mode.
- This story carries the deliberate, recorded test change: the two `router_test.go` expectations pinning today's "bare logs is a usage error, and top-level `--help` lists `logs <issue>`" behavior are edited to state the new contract explicitly, not deleted and not silently loosened. `parse_test.go`'s local arity fixture is unrelated generic-parser testing and is not touched.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Update `Usage` string to `logs [<issue>] ...` | code | US-1's `ArgSpec` change | 0.5h |
| T-4.2 | Add a `Long` help paragraph describing the app-log mode | code | T-4.1 | 1h |
| T-4.3 | Update `router_test.go`'s "missing argument" case to state the new contract (bare `logs` exits 0) | test | US-1 | 1h |
| T-4.4 | Update `router_test.go`'s top-level `--help` case to the new `logs [<issue>] ...` usage line | test | T-4.1 | 0.5h |
| T-4.5 | Confirm `parse_test.go`'s local arity fixture is left unchanged (`Min: 1`, tests generic parser behavior, not this command) | test | — | 0.5h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | `litefactory logs --help` and `litefactory logs -h` print the `logs` command's description, usage and flags to stdout, exit 0 |
| AC-4.2 | `litefactory help` and bare `litefactory` still print the global command table, byte-identically to today |
| AC-4.3 | `internal/cli/router_test.go`'s two pinned cases are updated to state the new bare-`logs` contract explicitly |
| AC-4.4 | `internal/cli/parse_test.go`'s arity test continues to pass unchanged |
