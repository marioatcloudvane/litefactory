# US-1 — Say out loud why an issue body did not run

> As the owner of a litefactory installation, I want to see in litefactory's own log
> why it declined to act on an issue I assigned to it, so that I can tell the
> difference between "it decided not to" and "it is not running".

Promotes the issue-body refusal log lines from debug level to info level, so that a
refusal on an issue the owner assigned to himself leaves a trace he can actually find.

## Why This Matters

When the owner assigned issue #10 to litefactory and nothing happened, he had no way to
find out why. That is not an accident of this one issue — today, when litefactory looks
at an issue and decides not to act on it, it says so at a detail level that is switched
off by default. Nothing appears in the log file, nothing appears on the issue, and
nothing appears in the command that shows what a run did, because no run was ever
started. From where the owner is sitting, a deliberate, correct refusal is
indistinguishable from the whole system being down.

The hard part is not making it speak. It is deciding which refusals are worth hearing.
One of them — "this issue is not assigned to you" — is true of almost every issue in
the repository almost all of the time, and the log file this writes to is never rotated
or trimmed. Turning that one up would bury the four messages that actually matter under
thousands that do not, which would leave the owner no better off than the silence he
has now. So this story deliberately keeps that one quiet and turns up only the
refusals that follow somebody having deliberately pointed litefactory at the issue.

This is the smallest of the three pieces and the only one that is useful entirely on
its own. It would have explained issue #10 without any of the rest of this work
existing.

## Architecture Notes

- **The three gate refusals share one log call site**, at `internal/daemon/tick.go:1085-1086`
  (`DebugContext`), reached from the switch at `:1076-1083`. Splitting that call by which
  branch fired is the only real design decision in this story.
- **Recommended shape**: keep the single switch and the single call site; add a parallel
  `atInfo bool` set only in the loud arms, then select the level once —
  `lvl := slog.LevelDebug; if atInfo { lvl = slog.LevelInfo }` followed by one
  `t.log.Log(ctx, lvl, …)`. This preserves the one message string and the one attribute
  set (`slog.Int("existing_comments", len(comments))`) rather than duplicating them.
- **Which branch goes where:**

  | Branch | Site | Today | After |
  |---|---|---|---|
  | not assigned to the owner | `tick.go:1077` | Debug | **stays Debug** |
  | body empty or whitespace | `tick.go:1079` | Debug | **Info** |
  | already has comments | `tick.go:1081` | Debug | **Info** |
  | body parses to nothing (`KindNone`) | `tick.go:1097`, log `:1104` | Debug | **Info** |
  | command spawns nothing | `tick.go:1105`, log `:1113` | Debug | **Info** |
  | `lf:tools=` label unresolvable | `tick.go:1510-1515` | **already Info** | **no change** |

- **`KindNone` and `!Spawns()` are separate `DebugContext` calls with their own
  attributes** — promote each in place to `InfoContext`. No structural change to those.
- **The "not assigned" branch stays at Debug** because `Daemon.sight`
  (`internal/daemon/tick.go:394`) creates a session for every open issue in every
  registered repository, so this branch fires for most issues most of the time. Under
  US-2 it will fire on *every* tick rather than once, which reinforces the decision
  rather than reopening it. It is also not a refusal in the owner's sense: nobody asked
  for anything.
- **This is the house rule, not a deviation from it.** The same trade is already
  legislated for the one other place litefactory silently discards something —
  `Filter.selfReason` logs at Info (`internal/turn/classify.go:352-356`) precisely
  because "a line only visible under debug is not a line the owner can find". Info, not
  Warn: these are refusals by design, not malfunctions.
- **`internal/daemon/doc.go` does not mention the issue-body trigger at all** — verified
  against the current file. In a codebase whose package docs are the specification, a
  shipped trigger the spec does not name is a trigger nobody knows about. Bringing it up
  to date belongs here because this is the first story to touch the file.
- **Nothing is posted to the forge**, so the two "posts nothing" tests keep passing
  unchanged.
- Risk: **low.** No behaviour changes — only the visibility of decisions already being
  made. The only way to get this wrong is to promote the not-assigned branch by
  accident, which the tests below are written to catch.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Split the shared refusal log call (`tick.go:1085-1086`) so the level is selected per branch, keeping one message string and one attribute set | code | — | S |
| T-1.2 | Promote the `KindNone` log (`tick.go:1104`) and the `!command.Spawns()` log (`tick.go:1113`) from `DebugContext` to `InfoContext` in place | code | T-1.1 | XS |
| T-1.3 | Confirm the `lf:tools=` body-path branch (`tick.go:1510-1515`) is already Info and leave it untouched | code | — | XS |
| T-1.4 | Add log-level assertions per branch, modelled on `TestSelfDropIsLogged` (`internal/turn/classify_test.go:528`) — assert the line's text **and** `"level":"INFO"` against a handler at `slog.LevelInfo` | test | T-1.1, T-1.2 | S |
| T-1.5 | Add a test pinning that the not-assigned branch does **not** emit at Info | test | T-1.1 | S |
| T-1.6 | Bring `internal/daemon/doc.go` up to date: the package spec must name the issue-body trigger, which it currently does not mention at all | docs | T-1.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | With `log_level` unset (default `info`), an issue assigned to the owner whose body is empty produces exactly one log line at level `INFO` naming the empty body as the reason. |
| AC-1.2 | With `log_level` unset, an issue assigned to the owner that is refused because it already has comments produces exactly one log line at level `INFO` naming that reason. |
| AC-1.3 | With `log_level` unset, an issue assigned to the owner whose body parses to nothing to act on produces a log line at level `INFO`. |
| AC-1.4 | With `log_level` unset, an issue assigned to the owner whose body names a command that spawns nothing produces a log line at level `INFO`. |
| AC-1.5 | An issue **not** assigned to the owner produces **no** log line at level `INFO` from the issue-body gate. The existing debug line is unchanged. |
| AC-1.6 | Every promoted line names which condition fired; the level is asserted in the test, not merely the text. |
| AC-1.7 | No comment, reaction or any other write is made to the forge on any refusal path. `TestABodyThatCommandsNothingStartsNothingAndPostsNothing` (`internal/daemon/tick_test.go:696`) and `TestIssueBodyToolsRefusalPostsNothing` (`internal/daemon/tools_test.go:339`) pass unchanged. |
| AC-1.8 | `internal/daemon/doc.go` describes the issue-body trigger — the conditions under which an issue's body runs as the first instruction, and that it is gated on assignment rather than authorship. |
| AC-1.9 | No change to any migration, to `internal/report`, to `internal/github`, or to the daemon's decisions — only to the level at which existing decisions are reported. |
