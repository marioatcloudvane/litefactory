# Validate: repair and review

This page covers the part of the Validate graph that follows the suite step. The
suite step itself (running the full recipe on one exact commit) is described with
the suite activity. The graph is `internal/teamsource/validate/validate.dot`, and it
is the only place the flow lives: the order, the repair bound, the time budget, the
measured limits and the reviewers are declared there. Go supplies steps and never
picks the next one.

## Flow

```
suite -> passed:  review
         failed:  repair budget -> repair agent -> repair check
         blocked: review
repair check -> kept:       suite (on the new commit)
                too_big:    review
                handback:   review
                unfinished: repair budget
                blocked:    review
review -> outcome -> assessed | handed_back | blocked
```

A finished suite result for the commit is reused as it is. The suite runs again
only on the commit a kept repair made. A blocked suite is not a code failure, so it
is not repaired; it still gets reviews and ends blocked with the suite's reason.

## Repair

At most two repair rounds per run, declared as `lf_rounds` on the budget step and
as the limit of the repair agent. A round is counted from the moment it starts, so
a round that times out or is interrupted is used. Before anything else, the budget
step settles a round a restart left unfinished: the worktree goes back to the
commit before the round and the round is recorded as interrupted.

The repair agent has write access and a time budget (`lf_budget`, in minutes, at
most 60; the graph declares 15). The budget bounds the agent run only, never the
tests. When it runs out the run is cancelled and the attempt is recorded as timed
out. The check step measures what the agent really changed against the commit
before the round, with the limits the graph declares (`lf_max_files` and
`lf_forbid`: package, dependency, schema, api, deleted or skipped tests, changed
acceptance criteria). A change inside the limits is committed by the factory. A
change over them is retained as a diff, the worktree is reset and the flow goes to
review and then a handback.

## Review

One step starts the three read-only reviewers (acceptance, code, architecture)
side by side on a scratch checkout detached at the final tested commit. Each takes
one run slot. A reviewer without a slot makes the step return `retry`; recorded
results stay and only the missing reviewers start on the next dispatch. A reviewer
is never started twice for one commit, and a lost reply is resolved through the
retained result before anything is started again.

Findings use the existing Validate assessment shape, tied to the commit's
snapshot. Acceptance covers every criterion and the plan review, code covers the
code review, architecture covers the architecture review. A criterion is
satisfied only by citing a passed suite command of the same commit; anything that
cannot be proven is unverified. A missing, failed or malformed reviewer result
stays unresolved, never satisfied.

## Ends

The outcome step joins the three parts into one assessment per story and records
one outcome per run and final commit: `assessed`, `handed_back` or `blocked`. It
never says accepted or approved. A handback is built from retained facts only: the
failing command and its argv, the output tail, the expected behavior, the files and
lines of the repair diagnosis, links to the suite and repair records, one objective
per failure (a diagnostic step when the cause is unknown), a copyable instruction
for the owner and the reviewers' findings.
