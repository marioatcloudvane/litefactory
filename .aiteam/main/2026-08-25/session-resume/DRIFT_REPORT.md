# Drift Report — session-resume
# Branch: main | Date: 2026-08-26

Plan written against `e30257c`. Implemented against `4d9a843` (HEAD moved twice during the gap: the
sibling `fix-comments-pickup` work landed as `3c8b570`/`104eca5`, merged at `90e0fe8`, and a
research-only commit merged at `4d9a843`).

## Delivered as planned

**Story 1 — the contract is pinned and the session id has one source**
- T-SR-1.1 / T-SR-1.3 — the probe was re-run against the operator's real binary and recorded in
  `CLAUDE_CLI_CONTRACT.md`, including the tool-boundary evidence on a resumed run.
- T-SR-1.2 — `harness.Request.ClaudeSessionID` added; the derivation at `runner.go:230` deleted;
  `prepare.go` populates it from `segment.ClaudeSessionID`. `naming.ClaudeSessionID` now has exactly
  one production call site (`internal/turn/prepare.go:122`), verified by grep.
- T-SR-1.4 — `Request.validate` rejects an empty or non-canonical UUID with `ErrInvalidRequest`.
- T-SR-1.5 — validation table plus the stored-vs-derived test, mutation-checked red.

**Story 2 — a run continues the previous conversation**
- T-SR-2.1 — `Runner.args` swaps `--session-id` for `--resume`; the flag rationale block rewritten.
- T-SR-2.2 — the resume decision comes from prior terminal-status runs on the segment via
  `store.ListRuns`, filtered in Go. No migration, no new column, no new accessor.
- T-SR-2.3 — `PromptInput.Resume` + the delta prompt, branched inside `BuildPrompt`.
- T-SR-2.4 — `ErrSessionNotResumable` + exactly one cold rerun reusing the same run row and ordinal.
- T-SR-2.5 — `Meta.Resumed` + the `path` slog field.
- T-SR-2.6/2.7/2.8 — the regression test, the prompt-shape and decision tables, the fallback tests.

**Story 3 — a cold start gets the thread, compacted, with prior owner comments defused**
- T-SR-3.1 — `harness.HistoryEntry` + `old_owner` / `old_third_party` / `litefactory`.
- T-SR-3.2 — the thread rendered as one `<note>`, reusing `capText` / `note` / `neutraliseNoteTags`
  unchanged; the `Source:` line states the record is condensed and may be incomplete.
- T-SR-3.3 — `MaxHistoryBytes` newest-first fill, chronological emit, one omission marker.
- T-SR-3.4 — `prepare.go` maps `store.Classification` → role, excludes the triggering comment,
  renders on the cold path only.
- T-SR-3.5/3.6/3.7 — role table, the historic-`/spike` test, `</note>` neutralisation, budget
  eviction, resumed-carries-nothing, plus the fallback-carries-history seam test.

Both owner decisions were applied as settled: OD-1 accept-as-is with the exposure recorded in a code
comment on `historyNote`; OD-2 `MaxHistoryBytes = 32 << 10`.

## Added scope

- **`CLAUDE_CLI_CONTRACT.md` is larger than the plan implied.** It documents five probes rather than
  three and adds a "re-verifying after a claude upgrade" checklist. Added because the contract is
  with an external binary and the whole point of S1 is that a version bump is caught by a human
  reading a document.
- **`naming.ValidClaudeSessionID`** — a one-line exported predicate over the existing `uuidPattern`,
  added so the validator in `harness` reuses `naming`'s own definition of the format rather than
  hand-rolling a second regex. Cheaper than the alternative and keeps one source of truth.
- **`harness.RunPath` is a three-valued type, not the `Resume bool` the plan described.** The plan
  also asks (T-SR-2.5) for a `path` log field taking `resume` | `cold` | `cold_after_resume_refused`
  — three values a bool cannot express. `args` still branches on one boolean (`Path.resuming()`), the
  zero value is `cold`, and `validate` rejects an unrecognised path. This is the plan's two
  requirements reconciled, not new scope.
- **`PromptInput.PreviousMode`** — needed to satisfy AC-SR-2.5 (re-emit mode guidance when this run's
  mode differs from the previous run's). The plan states the requirement without naming the field.
- **`internal/harness/doc.go` updated.** Its "# The invocation" block asserted `--session-id` was
  *the* invocation, which this work made false. Rewritten rather than left stale.
- **`internal/turn/execute_test.go`'s `"seeded-session-id"` fixture** replaced with a canonical UUID.
  It does not currently reach `Request.validate`, so nothing forced it; changed because the trap is
  now one edit away. The validator was not weakened.
- **One doc-comment typo fixed** (`missing---verbose`), the sole advisory from code review that was
  worth acting on.

## Dropped or deferred

Nothing. Every task in the plan's three task tables was completed.

## Architectural deviations

**One, and it is a correction to the plan rather than a departure from it.**

The plan's AC-SR-2.7 and its Story 2 Architecture Notes specify detecting a resume refusal with
`RunResult.Model == "" && Events == 0 && ExitCode != 0`. **That rule would never have fired.** The
probe against the real binary shows `--resume` on a missing session emits **one** stdout event before
exiting — a terminal `result` event with `subtype: "error_during_execution"` — so `Events == 1` and
`HasResult == true`.

The implemented rule keeps the plan's stated *intent* ("no init event, so claude never began the
conversation") and drops its incorrect *mechanism*:

```
resuming && Outcome == OutcomeFailed && Model == "" && ExitCode > 0
```

`> 0` rather than `!= 0`, because `SignalExitCode` is `-1` and a killed process is not a refusal.
`TestResumeRefusedClauses` pins every clause and `TestRunReportsARefusedResume` asserts the refusal
arrives with `Events == 1 && HasResult`, so the plan's original rule is provably dead code had it
been written. Recorded in `CLAUDE_CLI_CONTRACT.md` and `notes.md`.

**Version discrepancy, not a deviation:** the plan was probed against `claude 2.1.245`; the
operator's installed binary is `2.1.246`, which is what the contract was re-verified against. Every
behaviour and every stderr string is identical.

## Stale citations corrected

The plan's claims were verified; several of its coordinates had moved (mostly because the sibling
session's work landed after the plan was written, and then because Stories 1 and 2 shifted their own
files).

| Plan cites | Actual |
|---|---|
| `daemon/tick.go:225-251` (`bootstrap`) | `internal/daemon/tick.go:323` (doc from :315) — moved ~+98 lines by the sibling session. Read-only; untouched. |
| `result.go:69-72` (`ErrStartFailed`) | `internal/harness/errors.go:21-23`. That range in `result.go` is only prose about it. |
| `report/run.go:502-517` | `resultSection` at `internal/report/run.go:510`. |
| `prompt.go:57` (`truncationMarker`) | `:63` before this work, `:80` after. |
| `prompt.go:264-299` (note framing) | `note` `:521`, `capText` `:545`, `neutraliseNoteTags` `:556`. |
| `prepare.go:108` (session id derivation) | `internal/turn/prepare.go:122` after Stories 1–2. |
| `runner.go:277-301` / `302-318` (rationale / `args`) | `:350-386` / `:388-412` after Stories 1–2. |
| `runner.go:265-268` (run logger) | `:337-342` after Stories 1–2. |

Verified accurate and unchanged: `runner.go:150-172`, `runner.go:176-199`, `runner.go:230`,
`runner.go:452-459`, `segments.go:14`, `segments.go:41-43`, `naming/paths.go:12`, `store/runs.go:379`,
`store/comments.go:151`, `0001_init.sql:87`, `meta.go:35`, `meta.go:50-51`, `meta.go:59-60`,
`prompt.go:11-16`, `turn/classify.go:258-260`, `turn/fixture_test.go:244`.

## Constraints held

- `internal/daemon/tick.go` byte-identical — SHA-256
  `9a308a98bbe613d40c0f00fdd4af046102f13403d86ca8d7f5e0f9aec51e3ef7`, empty `git diff`.
- No migration, no new table, no new column, no new store accessor.
  `store.FirstSegmentOrdinal` untouched at `1`.
- `--continue` is never constructed anywhere in the tree.
- `--resume` and `--session-id` are structurally incapable of appearing together: `args` assigns one
  `sessionFlag` variable.
- No prompt-injection defence added, strengthened or renamed.
- Nothing committed, nothing pushed. The daemon was never started; no live-store writes, no GitHub
  calls, no comments posted.

## Verdict

**CLEAN** — delivered matches the plan. The one substantive divergence is a correction the plan
itself could not have made: its resume-refusal detection rule was wrong about the external binary,
and the empirical contract that S1 exists to pin is what caught it.
