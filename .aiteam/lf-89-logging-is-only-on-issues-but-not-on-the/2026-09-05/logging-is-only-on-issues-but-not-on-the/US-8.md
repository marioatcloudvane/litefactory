# US-8 — Stderr advisory when the requested level was never captured

> As an operator, I want to be told when I ask for a level finer than the daemon was configured to write so that I do not read an empty result as "nothing happened."

This story adds a one-line stderr advisory when `--level` asks for detail the daemon never recorded.

## Why This Matters

Asking for finer detail than the daemon was ever told to record produces a silent, empty-looking result today — indistinguishable from "nothing happened," when what actually happened is that the daemon's own setting needs to change first. This is a single sentence that closes that gap, pointing at the actual fix, without turning something that isn't an error into one.

## Architecture Notes

- The advisory goes to `env.Stderr` while log content stays on `env.Stdout`, so piping `litefactory logs` output stays clean; write directly to the `env.Stderr` writer, never via `fmt.Print*`.
- Exit code is unaffected: this is neither a usage error nor a precondition error. A user who filters to a level the daemon never wrote has done nothing wrong.
- Fires once, after the initial tail pass, reading the "levels actually seen" state that US-5's filter owns — it must not fire repeatedly inside the follow loop.
- Phrases around the actual cause (the daemon was configured at a coarser threshold) and the fix (`daemon start --level <keyword>`, delivered by US-6) — no inspection of `config.yaml` to predict the daemon's level, and no contacting the daemon.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8.1 | Emit one advisory line to `env.Stderr` after the tail pass when `--level` was requested and no record at or above it was seen | code | US-5's "levels seen" state | 1.5h |
| T-8.2 | Ensure the advisory does not repeat inside the follow loop | code | T-8.1, US-2 | 1h |
| T-8.3 | Tests: advisory text and destination, exit code unaffected, fires once rather than per poll interval | test | T-8.1, T-8.2 | 1.5h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-8.1 | `litefactory logs --level debug` with `log_level: info` in config writes one advisory line to stderr naming the configured level and `daemon start --level`, while stdout carries only log output; exit code is unaffected |
| AC-8.2 | The advisory does not repeat during a `-f` follow session |
