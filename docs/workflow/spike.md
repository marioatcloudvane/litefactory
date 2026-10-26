# Spike

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

Spike answers one narrow question with a small, time-boxed experiment. It writes
`SPIKE.md` in the session directory, reports once, and never creates a Plan or
opens a pull request by itself. If the question needs more than a small change, it
hands back with a recommendation.

## Activation

Spike runs only on the graph. Start it with `/spike <question>` or `Mode: Spike` (see [owner commands](../guides/owner-commands.md)). Every new eligible Spike session is admitted to
the graph once, before anything is dispatched, and stays on it: a session that
already has graph work resumes through its retained admission. There is no
repository setting for the route. A leftover `.litefactory/spike.yaml` from the
earlier temporary selection has no effect and is never read; delete it.

The earlier single-agent Spike has been retired. A session whose recorded history
belongs to that route, or whose provenance cannot be told apart from it, is
refused before any agent is dispatched, with a message to start a fresh graph
Spike task using a new `/spike` request. Nothing is migrated or rewritten, and
the recorded history is kept. An explicit switch to another mode cancels a
running Spike graph.

## Permissions

Spike borrows no other mode's tool profile. The operator grants the two graph
roles independently under `tool_profiles.permissions`: `spike-investigator`
(read-only) and `spike-fixer` (edits, no shell). The fixer profile is the mode
ceiling each node is narrowed from, the investigator profile may not exceed it, and
every tool must sit inside `claude.tool_ceiling`. If either binding is missing or
inconsistent, Spike is refused for that agent instead of falling back to another
profile. `litefactory init` writes both.

## The graph

The whole flow lives in one file,
[`teams/source/base-team/spike/spike.dot`](../../teams/source/base-team/spike/spike.dot).
Order, limits, access levels, size thresholds and the status written for each
path are declared there; Go supplies steps and never picks the next one. Roles are
`spike-investigator` and `spike-fixer`, both using the
[`spike-result`](../../teams/source/base-team/spike/skills/spike-result/SKILL.md)
skill, which defines the result each response must carry. Read those files rather
than a copy here.

## Investigate (read-only)

The investigator has read access only. It runs at most **two times**: once at the
start and once more after an owner reply. It may ask **one question round**. If it
asks, the factory writes `SPIKE.md` with status `asking` first, then publishes the
question and waits. The note exists before the wait begins. The reply re-runs the
investigator directly. Every limit persists across replies, so a second question is
refused by the graph limits, not by a counter in Go.

## Small-fix rule and the size gate

After investigation the size gate measures the investigator's *proposed* change
against limits declared in the DOT: at most three files, and none of the
forbidden kinds `package`, `dependency`, `schema`, `api`. It returns one outcome:

- `none`: no change proposed; status `answered`.
- `small`: go to the fixer.
- `too_big`: more than one defect, too many files or a forbidden kind; status
  `hand_back`. This is the **pre-fix oversized** path: it only recommends, files
  nothing, and the owner decides what follows.
- `blocked`: a change is proposed but too incomplete to measure; status `blocked`.

For `small`, the gate also saves the worktree HEAD as the baseline against which
the real change is later measured.

## The fixer (write, gated)

The fixer has write access but no shell and no way to ask. It makes exactly the
one proposed fix plus one regression test. There is **no owner approval step**,
no question, and no retry: nothing in the graph leads back to the fixer or the
check, so each runs at most once. A fixer that asks anyway is recorded as
`blocked`, not as an owner wait.

The check then measures the change that actually happened, not the fixer's claim.
It is blocked if nothing changed or the claimed regression test is not among the
changed files. It is `too_big` if the real change exceeds the file limit or
touches a forbidden kind; no command runs in that case. Otherwise it runs the
`focused` and then the `full` phase of the repository's check recipe in the
sandbox, stopping on the first nonpassing command.

## Prerequisites: pinned bug-checks recipe

Post-fix checks reuse the Bug check recipe, read from the recorded baseline commit
and not from the working tree, so the fixer cannot change what is run. See
[`docs/bug-checks.md`](../bug-checks.md) for the format, the exit-code contract and
the sandbox. Spike needs a valid `.litefactory/bug-checks.yaml` with `focused` and
`full` phases (the recipe must validate as a whole) and a configured check
sandbox. Without them the check is `blocked`, and the fix is reported unverified
by the check or recorded as blocked, never as fixed.

## Outcomes

`SPIKE.md` carries one declared status, written before the report:

| Result | Meaning |
|---|---|
| answered | Question answered; no change proposed. |
| hand_back | Pre-fix: the proposed change was too big; recommendation only. |
| fixed (`fixed`) | Small change made and every named phase passed. |
| unverified (`fix_unverified`) | Change made, but a check reported failure. It is not repaired or retried. |
| too-big (`fix_too_big`) | Post-fix: the real change turned out oversized. |
| blocked (`blocked`) | Could not be measured or checked, the fixer was stuck or asked, or the sandbox or recipe was unavailable. |
| asking | Waiting on the one owner question. |

`fixed` cannot be written unless the retained check record says `passed`.

**Post-fix oversized** differs from pre-fix: because work already exists, the
factory automatically files **one plain repository issue** carrying the request,
findings, fixer report and the forbidden kinds hit. It is not a Plan child and
carries a hidden marker so a restart finds the existing issue instead of filing a
second one. Filing failure does not stop the report; the report says whether it
was filed.

## Retained changes

The fixer's edits are left in the worktree; litefactory inspects and commits the
worktree itself. Spike never reverts a change that turned out unverified or too
big, so the owner can inspect it. The check record (files, kinds, commands with
redacted, capped output tails, recipe digest) is kept with the session.

## Limits of post-fix evidence

`fixed` means the named phases passed on this worktree, using the pinned recipe,
in the sandbox. It does not mean the fix is reviewed, complete or safe to merge.
Output text is never interpreted as success. Failure is judged by exit codes, and
the recipe's own coverage limits what "passed" proves. An `unverified` result is
not proof that the fix is wrong, only that a check failed. Owner review of the
change is still required.

## Failure report

A failed investigator, note or fixer step publishes one failure report
(`report_failure`) explaining why and stops. Nothing retries on its own, and
failed work stays in the worktree and the durable graph record.
