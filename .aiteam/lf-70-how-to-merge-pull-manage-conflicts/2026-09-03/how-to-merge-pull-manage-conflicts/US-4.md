# US-4 — Refuse the run and name the conflicting files

> As the owner of a task, I want litefactory to stop and tell me exactly which
> files conflict, rather than run my agent against a stale branch or guess at
> a resolution, so that I know precisely what needs my attention.

This story is the safety valve for the case US-2 cannot handle automatically:
when the base branch cannot be merged in cleanly, the run stops before the
agent starts and says so, by name.

## Why This Matters

Auto-merging is only trustworthy if it has a hard stop for the cases it
cannot safely handle. When the base and the task branch have both changed the
same lines, there is no safe automatic choice — guessing risks silently
discarding someone's work, and running the agent against a stale branch just
defers the problem. This story is that hard stop: the agent never starts, the
branch is never touched, and the owner is told exactly which files collided so
they know precisely what to look at instead of being handed a generic
"something went wrong."

## Architecture Notes

- Pattern: follow `ReplyAgentSwitchRefused` end to end — this is a refusal
  reply, not a command reply, and needs its own path through
  `internal/turn/report.go`'s `replyKind()`.
- Package placement: `report.ReplyKind` gains `ReplyRefusesSync` in
  `internal/report`; the mapping case is added in `internal/turn/report.go`.
- Key interfaces: `ReplyRefusesSync` needs a `String()` case, inclusion in the
  enum-exhaustiveness validity check, a `RefusesSync()`-style predicate, and a
  `body()` case producing the refusal text. Note for the engineer: `replyKind()`
  currently maps `CommandKind` values; a sync refusal is not a command
  refusal, so this is a new branch in that function, not a reuse of an
  existing one.
- Integration point: `syncBase`'s `blocked` outcome takes the existing
  `*failure` path (CONC-3: failures become reportable results, not raw
  errors) straight through to `Execute` → `report.Reply{Kind:
  ReplyRefusesSync}` → `PostComment`. When `BlockRunOnConflict` is true (the
  default — see US-8, and the hard coupling with US-7 in `plan_brief.md`),
  the agent is never started: no `claude` exec, no run-artifact directory of
  the usual shape, no commit. Downstream code that assumes "every trigger
  produces a run with a transcript" must tolerate this the same way it already
  tolerates `ReplyAgentSwitchRefused`.
- The refusal sentence lives in a package-level constant alongside the
  existing `onlyCommandSentence` / `toolsRefusalSentence` /
  `agentRefusalSentence` constants — this is now a fourth refusal wording, and
  it must not drift into a different account of the same rule than its
  siblings.
- The conflicting-file names themselves come from US-2's `MergePreview` via
  US-5's sanitizer — this story renders the refusal shell (the fact of
  refusal, the reason, the sentence); it does not itself decide what is safe
  to print.
- Depends on US-2 (the `blocked` outcome this story reacts to).
- NOT in scope: sanitizing the path list (US-5); the normal-run sync section
  for non-blocked outcomes (US-3); deciding whether to block at all versus
  merely disclose (that's the `BlockRunOnConflict` config flag, US-8, and its
  override, US-7).

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Add `ReplyRefusesSync` const, `String()`, validity, `body()` case | Code | US-2 T-2.5 | M |
| T-4.2 | Add refusal sentence as a package-level constant | Code | T-4.1 | XS |
| T-4.3 | Add `replyKind()` case in `internal/turn/report.go` | Code | T-4.1 | S |
| T-4.4 | Wire `blocked` outcome to the `*failure` path | Code | US-2 T-2.5, T-4.3 | M |
| T-4.5 | Tests: agent never starts on `blocked`, no run artifact directory created, refusal comment posted | Test | T-4.4 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | Given a same-line conflict, the agent never starts, `ReplyRefusesSync` is the reply kind used, the conflicting paths are listed, and the branch SHA is unchanged from before the run. |
| AC-4.2 | `ReplyRefusesSync` is a valid `ReplyKind` with a working `String()` and a mapped `replyKind()` case; the enum-exhaustiveness test passes with it included. |
