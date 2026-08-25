# Spike — mode-closure

2026-08-25. Three changes asked for in one sitting: stop losing owner comments,
make Spike produce a real artifact that knows when to hand back, and make every
mode closed so an agent can never walk itself from Research into Implement.

## What was done

**Comment discovery no longer depends on `updated_at`.** `github.Client` gained
`ListCommentedIssues`, over `GET /repos/{o}/{r}/issues/comments?since=`. The
tick unions its result with the issue listing and fills in detail with
`GetIssue` for anything only the comment listing found. Two listings per
repository per tick, and still nothing per issue.

**`CLAUDE.md` gained *One Run, One Mode* and a *Spike* section.** Spike now
always writes `$session/SPIKE.md` against a fixed skeleton, and carries an
explicit hand-back rule. Mode transitions are human-only.

## What was found

The seven-hour delay on issue #3 was not a stuck daemon. GitHub does not move an
issue's `updated_at` when a comment is written to it, so a `since` walk over
issues cannot see an owner comment on an otherwise untouched issue:

    13:39:26  daemon: first sight of #3, no comments yet
    13:40:37  owner comments /research
    13:42:25  owner comments /spike
    13:42:26  tick since=11:41:56Z -> issues=0   (both comments sit inside it)
       … 60 further ticks, every one issues=0 …
    18:40:23  the issue is closed by hand; updated_at finally moves
    18:40:49  tick -> issues=3, and the comments drain seven hours late

The single-issue endpoint *does* reflect comments in `updated_at` — #3 reads
`18:57:15Z`, the moment the bot's report landed. It is the listing index that
does not. So the run that eventually happened was triggered by the issue being
closed, not by the comment asking for it.

The fixture is why no test caught this: `addComment` bumped the issue's
`UpdatedAt`, modelling the assumption rather than the API.

## Decisions worth remembering

- **The fake forge now models the real behaviour** — `addComment` no longer
  touches `UpdatedAt`. Every pre-existing daemon test passed unchanged against
  the stricter fixture, which is the evidence that the new path carries all of
  them and not just the new case.
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
  now be much older than the comment that deferred. Harmless — it re-lists more
  than needed and self-corrects — but it is keyed on the wrong field. Keying it
  on the deferred comment's timestamp is the right fix.
- **`/research`, `/plan`, `/implement` still reply "unsupported" and start
  nothing**, while the same words without the slash now start a run that picks
  its own mode. The command form is stricter than plain English. Left alone
  deliberately: it needs its own change and is not a spike.
- Nothing was merged. `lf/1`, `lf/2` and `lf/3` are all still unmerged branches.

## Recommendation

Ship this, then decide on the `/research` gap — that is a Plan-sized question
about what the command vocabulary should be now that free text works, not
another spike.
