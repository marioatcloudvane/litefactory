# Validate

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

Validate checks finished work and helps you ship it. It tests one exact commit, may repair a failing suite a little, has three read-only reviewers assess the result, and then hands the decisions to you. Start it with `/validate <text>`, or let `validate.auto_start` start it once Implement has merged every story. For the owner's view, see [validation and delivery](../guides/validation-and-delivery.md).

The graph is `teams/source/base-team/validate/validate.dot`, and it is the only place the flow lives: the order, the repair bound, the time budget, the measured limits and the reviewers are declared there. Go supplies steps and never picks the next one.

## The suite

The suite step runs the `full` phase of the repository's check recipe (`.litefactory/bug-checks.yaml`, read from the tested commit; see [Bug checks](../bug-checks.md)) on one exact commit, in the check sandbox. It is:

- **passed** when every command exits 0 on an unchanged snapshot,
- **failed** when a command exits 1 (later commands do not run),
- **blocked** when there is no recipe, the full phase is empty, a command exits with another code, the snapshot changed, or an earlier run could not be confirmed stopped.

A finished result for the commit is reused as it is. Validate does not use the stack profile's own `checks` for this.

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
owner resolve -> repair: repair budget (owner-chosen findings)
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

The repair agent has write access only if the operator granted the `validate-repair` permission profile under `tool_profiles.permissions` (see [permissions](../configuration/permissions.md)); without it only the repair step is refused, and reviewers stay read-only. It also has a time budget (`lf_budget`, in minutes, at
most 60; the graph declares 15). The budget bounds the agent run only, never the
tests. When it runs out the run is cancelled and the attempt is recorded as timed
out. The check step measures what the agent really changed against the commit
before the round, with the limits the graph declares (`lf_max_files` and
`lf_forbid`: package, dependency, schema, api, deleted or skipped tests, changed
acceptance criteria). A change inside the limits is committed by the factory. A
change over them is retained as a diff, the worktree is reset and the flow goes to
review and then a handback.

### Repairs the owner chooses

When the suite passed and the reviewers raised findings, the owner can answer
`/validate-finding <rev> <finding id> repair`. The choice is recorded; once every
blocking finding of the tested commit has a choice, that reply makes the resolve
step return `repair`, and one round starts for all findings chosen for repair.
Pending owner checks do not hold the round back: a kept repair is a new commit, and
the checks are asked again on it. The round uses the same budget, agent and check
steps and the same two-round bound as a failed suite. The agent's task lists each
chosen finding: its id, story, criterion or review name, the reviewer's text and
check ids. A kept change is a new commit, so the suite and the reviewers run again
on it and each finding clears or comes back. A change that is too big, a handback,
a blocked or unfinished round and a spent budget go back through review and
outcome (reused on the unchanged commit) to the owner report, which lists each
round with its reason. Graphs released without the `repair` outcome keep the old
flow: the choice is recorded and the run waits.

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

## The owner's turn

After the outcome, the graph hands the run to the owner. The flow is declared in
`validate.dot` like the rest:

```
outcome -> owner_resolve
owner_resolve -> wait | stale:  owner_report
                 create_issue:  owner_issue -> owner_report
                 push:          owner_push  -> owner_resolve
                 merge:         owner_merge -> owner_resolve
                 close:         owner_close -> closed: delivered | open: owner_report
owner_report -> owner_ask -> waiting
```

Push and merge return to `owner_resolve`. Without a new owner comment it returns
`wait` (and the run reports and waits) after any individual command, so one
confirmation never implies the next; only the explicit `/validate-ship` carries on
to the next delivery step (push, then merge, then close) until a step fails or
delivery ends.

Validate succeeds (the `delivered` terminal) only after the merge and the close
both succeeded. The loop is bounded by the `lf_limit` of `owner_resolve`; when it
runs out the run stops and nothing has been approved, because every remote effect
needs a command inside the loop.

### The report

One report comment per run, posted once and then edited in place (found by its
marker; an unchanged digest skips the edit; a failed post or edit never fails a
step, the next refresh catches up). It has one row per work item (suite, repair
rounds, each reviewer, manual checks, findings, delivery), the findings with stable
ids (`story-key:criterion-or-review`), evidence, the handback text, what is open and
the exact next commands. Its revision is a short digest of the tested commit, the
findings, verdicts, choices and effect states.

### Owner commands

Exact grammar, one line, each naming the current report revision:

| Command | Meaning |
|---|---|
| `/validate-check <rev> <criterion or review id> pass\|fail -- <evidence>` | evidenced manual verdict for an unverified criterion or usable unverified blocking review |
| `/validate-finding <rev> <finding id> repair\|defer` | choice for a finding |
| `/validate-finding <rev> <finding id> issue bug\|story` | create a follow-up issue |
| `/validate-push <rev>` | push the tested commit and find or create the PR into the base |
| `/validate-merge <rev> <pr> <head sha>` | merge only at that head |
| `/validate-close <rev> <issue>...` | close the parent and accepted story issues |
| `/validate-ship <rev> -- <evidence>` | the normal case in one command: record every eligible criterion or review awaiting an owner check as passed with the evidence, defer every open optional finding, then push and open the PR, merge it at the recorded PR number and head, and close the parent and story issues |

`/validate-ship` is refused with one notice naming what blocks, and records
nothing, when the suite did not pass, a reviewer has no usable result, the run is
not assessed, a failed criterion or blocking finding is open, or an issue the owner
chose is not created yet. A failed or uncertain step stops the ship at that step;
repeating `/validate-ship` (with the current revision) continues at the first undone
step and never repeats a finished one. When nothing blocks and delivery is
unfinished, the report's next step is `/validate-ship`; the individual commands stay
for exceptions.

A non-owner, malformed or stale command changes nothing; the reply shows the
current commands. Defer is allowed only for optional (advisory) findings. A repair
or issue choice never clears a blocking finding. An unverified item can be cleared
by an evidenced owner pass; an unmet defect requires a new tested snapshot, such
as the commit a kept repair makes.
Reviewer status and evidence remain unchanged by owner verdicts. Missing reviews and unmet defects cannot be manually passed.
Push is offered only when the suite passed, every criterion is satisfied or
owner-passed, no blocking finding is open, every optional finding has a choice and
no issue creation is pending, failed or uncertain.

### Remote effects

Every effect records its intent before it acts and checks the real remote state
first on replay (remote branch head, PR by head, merged state, issue by marker). A
failed or uncertain effect keeps Validate open and is retried only when the owner
repeats the command. Verdicts, choices and confirmations are bound to the run and
the tested commit; push, merge and close re-check that the branch head is still the
tested commit.
