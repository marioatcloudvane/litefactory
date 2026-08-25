# Spike — mode-closure

2026-08-25. Three changes asked for in one sitting: stop losing owner comments,
make Spike produce a real artifact that knows when to hand back, and make every
mode closed so an agent can never walk itself from Research into Implement.

## What was done

**The listing window now overlaps.** Every tick after the first asks for
`cursor − 5 minutes`. The per-task watermark is a monotonic `MAX()` over comment
ids, so a comment listed twice is compared and dropped — re-asking is free, and
losing a comment is not. The first-ever tick is exempt, or AC-2.9 would be
undone.

**Discovery asks twice.** `github.Client` gained `ListCommentedIssues`, over
`GET /repos/{o}/{r}/issues/comments?since=`. The tick unions its result with the
issue listing and fills in detail with `GetIssue` for anything only the comment
listing found. Two listings per repository per tick, and still nothing per
issue.

**`CLAUDE.md` gained *One Run, One Mode* and a *Spike* section.** Spike now
always writes `$session/SPIKE.md` against a fixed skeleton, and carries an
explicit hand-back rule. Mode transitions are human-only.

## What was found

The seven-hour delay on issue #3 was not a stuck daemon, and it was litefactory's
bug rather than GitHub's.

**The first diagnosis was wrong.** It read: GitHub does not move an issue's
`updated_at` when a comment is written to it. It does — issue #4's comment
landed at 19:14:47Z and the issue read 19:14:48Z a second later. That was
committed in 3c8b570 and is corrected here.

What actually happened is that the listing is eventually consistent while the
poll cursor is monotonic, and nothing overlapped the two:

    11:40:37Z  owner comments
    11:40:56Z  tick since=11:40:26Z -> issues=0   the index has not caught up
    11:41:26Z  tick since=11:40:56Z -> issues=0   the cursor is already past it
               … sixty ticks, thirty minutes, nothing …
    18:40:23Z  the issue is closed by hand; updated_at moves a second time
    18:40:49Z  tick -> issues=3, and the comments drain seven hours late

One window of lag and the comment is gone for good, because `since` has moved
beyond it by the time it is visible. Issue #4 survived only because the daemon
was down for 75 minutes: its cursor was so far behind that the index had long
since settled.

So the run on #3 was triggered by the issue being closed, not by the comment
asking for it.

## Decisions worth remembering

- **The fake forge models what GitHub does, not what the bug looked like.**
  `addComment` touches `UpdatedAt`, because GitHub does. A test that cares about
  the lag says so itself — by backdating the comment, or the issue — rather than
  having the fixture pretend the bump never happens. The first pass here had the
  fixture lie in the opposite direction, which is how the wrong diagnosis
  survived a green suite.
- **A seventh endpoint was a decision.** `TestClientMethodSetIs…` failed on the
  addition, exactly as intended, and was updated with the reason rather than
  bypassed. Seven still does not pay for an SDK.
- **Pull request comments arrive in that listing too** — every pull request is
  an issue and the comment payload cannot tell them apart. They are found out at
  `GetIssue`, which returns `ErrInvalidArgument`, and skipped rather than
  failing the tick.
- **A malformed `issue_url` drops the comment** instead of guessing a number.
  Losing one comment is recoverable; attributing it to the wrong issue is not.
- **Mode closure lives in `CLAUDE.md`, not in the harness.** litefactory does not
  know what a mode is, and should not learn: the repository being worked on is
  the authority on its own modes. Same reasoning that kept the classifier out of
  Go in the first place.

## Not done

- **The cursor hold-back still keys on `issue.UpdatedAt`.** When a comment queue
  defers, the poll cursor is pulled back to that issue's update time, which can
  be much older than the comment that deferred. Harmless — it re-lists more than
  needed and self-corrects — but it is keyed on the wrong field.
- **Five minutes is a guess**, chosen as far beyond any lag observed rather than
  derived from one. If a comment is ever lost again, that number is the first
  thing to raise, and the second is to make it configurable.
- **No lag was ever reproduced against the real API** — the evidence is the
  daemon's own log, after the fact. The tests model the lag rather than
  provoking it.
- **`/research`, `/plan`, `/implement` still reply "unsupported" and start
  nothing**, while the same words without the slash now start a run that picks
  its own mode. The command form is stricter than plain English. Left alone
  deliberately: it needs its own change and is not a spike.
- Nothing was merged. `lf/1`, `lf/2` and `lf/3` are all still unmerged branches.

## Recommendation

Ship this, then decide on the `/research` gap — that is a Plan-sized question
about what the command vocabulary should be now that free text works, not
another spike.
