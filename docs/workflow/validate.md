# Validate

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

Validate checks finished work and helps you ship it. It tests one exact commit, may repair a failing suite a little, has three read-only reviewers assess the result, and then hands the decisions to you. Start it with `/validate <text>`, or let `validate.auto_start` start it once Implement has merged every story. For the owner's view, see [validation and delivery](../guides/validation-and-delivery.md).

The graph is `teams/source/base-team/validate/validate.dot`, and it is the only place the flow lives: the order, the repair bound, the time budget, the measured limits and the reviewers are declared there. Go supplies steps and never picks the next one.

## The suite

The suite step starts from every command in the repository recipe's `full`
phase (`.litefactory/bug-checks.yaml`, read from the tested commit; see
[Bug checks](../bug-checks.md)). Plain `/validate` chooses checks automatically;
`/validate <text>` retains owner guidance. Exact `/validate full` requests a fresh
full suite even on an unchanged commit. Two distinct full requests create distinct
generations; replay of the same source request resumes or returns its own result.
Fresh execution bypasses test-result caches without clearing shared build caches.

Compatible captured workflows can first re-check prior failures, then run all
affected and unfinished obligations. They omit an unaffected unit only with
complete retained evidence, matching execution conditions and proven ancestry
from its original execution commit. Impact considers old and current dependencies,
including deleted sources, renames and merged target-branch changes. Unknown
runner capabilities, opaque wrappers, shared inputs, changed recipes or toolchains,
incomplete history and missing compatibility proof cause complete project checks
or the unchanged full recipe. Other runners keep full execution. The recipe,
working directory, environment, race checks, fixtures and build prerequisites
remain authoritative.

The immutable command ledger keeps factual `passed`, `failed`, `blocked` and
interrupted results. A separate assessment distinguishes executed passes,
inherited passes, transient retries, proven pre-existing failures and unresolved
coverage. `covered` means every obligation is accounted for by verified evidence;
it can include failed history. An inherited pass was not executed on the current
commit. A failed identity receives one uncached retry before attribution. A retry
pass is transient with both attempts retained. A persistent failure is exempted
only by equivalent complete evidence matching both identity and diagnostics on the
configured delivery target commit pinned at comparison start. A missing, skipped,
deleted or unmatched identity stays unresolved. No target branch is merged or
updated automatically. A picked-up base fix requires containment and passing
relevant coverage on the tested branch.

The suite returns `passed` for a passing fresh execution, `covered` for satisfied
coverage using retained or comparison evidence, `failed` for attributable feature
failures, and `blocked` when required evidence or execution safety cannot be
established. A proven matching base failure removes only its suite blocker;
unmatched failures and unrelated review, approval and delivery gates remain.
Opaque full commands can pass factually without establishing inheritable metadata.
Older captures keep their existing full-suite behavior and cannot grant the new
coverage exemptions. A fresh full request still bypasses result reuse.

## Flow

```
suite -> passed | covered: review
         failed:  repair budget -> repair agent -> repair check
         blocked: review
repair check -> kept:       suite (on the new commit)
                too_big:    review
                handback:   review
                unfinished: repair budget
                blocked:    review
review -> outcome -> assessed | handed_back | blocked
owner resolve -> repair: repair budget (owner-chosen findings)
                 apply_repair: exact retained adoption -> suite | blocked: report
```

A replay returns its retained request generation. A distinct fresh-full request
runs again, including on an unchanged commit. A kept repair or verified owner
adoption tests its new commit with automatic coverage selection. A blocked suite
is not a code failure, so it is not repaired; it still gets reviews and ends blocked with the suite's reason.

## Repair

At most two repair rounds per run, declared as `lf_rounds` on the budget step and
as the limit of the repair agent. A round is counted from the moment it starts, so
a round that times out or is interrupted is used. Before anything else, the budget
step first reconciles retained capture, reset and application intents with the
actual Git state. No worker can use an unresolved worktree. Before reset, a complete
candidate must be saved and verified; failed capture leaves the source intact.

The repair agent has write access only if the operator granted the `validate-repair` permission profile under `tool_profiles.permissions` (see [permissions](../configuration/permissions.md)); without it only the repair step is refused, and reviewers stay read-only. It also has a time budget (`lf_budget`, in minutes, at
most 60; the graph declares 15). The budget bounds the agent run only, never the
tests. When it runs out the run is cancelled and the attempt is recorded as timed
out. The check step measures what the agent really changed against the commit
before the round, with the limits the graph declares (`lf_max_files` and
`lf_forbid`: package, dependency, schema, api, deleted or skipped tests, changed
acceptance criteria). A change inside the limits is committed by the factory. A
change over them, including a handback, is captured as a complete immutable tree,
binary-capable patch and path/mode manifest before reset. The short round diff
is only a preview. Full candidate bytes drive the existing risk rules; unknown
analysis never appears safe. The flow goes to review and then a handback.

### Retained candidate review and adoption

The owner uses `/validate-repair <report-revision> <candidate-id> <review-digest>
inspect|accept|discard`. The report links every ordered review part, including the
complete patch, changed paths and modes, flag reasons, diagnosis, measured risks
and uncertainty. Receipt reconciliation uses the existing multipart delivery
journal. Missing delivery or redaction leaves review incomplete and acceptance
unavailable. Reset and restart do not resolve a pending candidate.

Only the authenticated owner can resolve the current report's exact candidate and
review digest. Acceptance additionally requires existing code authority, a clean
worktree on the unchanged reviewed base and unchanged frozen scope. Frozen
acceptance-criteria and protected session-scope changes remain ineligible. A changed
candidate or branch needs fresh review; no rebase or merge is implicit.

The resolve step records authorization only. The separate
`lf_validate_repair_apply` step journals the intended commit before mutation,
installs exactly the retained tree, verifies the resulting tree and records the
result commit. Its `applied` edge returns to the suite and all reviewers; `blocked`
returns to the report. Restart reconciles base, intended result, actual branch and
tree rather than blindly replaying. Pending, discarded and authorization-only
records never select the tested commit. Discard applies no files. Historical
flagged rounds, two-round budgets and shipping gates stay unchanged.

### Repairs the owner chooses

When the suite coverage is satisfied and the reviewers raised findings, the owner
can answer
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
satisfied only by the proof its requirement needs: an executed passing check or
a verified inherited pass for a reusable execution requirement. A requirement for
fresh execution needs a pass on the tested commit. Proven base failures are not
passing criterion evidence; anything that cannot be proven is unverified. A
missing, failed or malformed reviewer result stays unresolved, never satisfied.

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

The coverage report names the tested and preceding suite commits, original
execution anchors, pinned comparison commit, selected work and omitted work with
its retained sources. It keeps unresolved reasons, exceptions and full-run
fallback causes visible. Duration measures validation work; any savings are
labelled estimates against a comparable measured full run. Without that baseline,
no numeric savings are claimed. Assessment evidence and its digest are bound to
the report revision; changed evidence needs the usual fresh owner decision.

### Owner commands

One-line commands accept the explicit report revision or the displayed short reference. Omit the target only while every unresolved display for that action has the same target and no newer target exists. Replacement requires its short reference; stale explicit references are refused.

| Command | Meaning |
|---|---|
| `/validate-check <rev> <criterion or review id> pass\|fail -- <evidence>` | evidenced manual verdict for an unverified criterion or usable unverified blocking review |
| `/validate-finding <rev> <finding id> repair\|defer` | choice for a finding |
| `/validate-finding <rev> <finding id> issue bug\|story` | create a follow-up issue |
| `/validate-push <rev>` | push the tested commit and find or create the PR into the base |
| `/validate-merge <rev> <pr> <head sha>` | merge only at that head |
| `/validate-close <rev> <issue>...` | close the parent and accepted story issues |
| `/validate-ship [reference] -- <reason>` | authorize all exact displayed blockers and ship the displayed delivery commit, retaining original factual evidence and its tested commit |
| `/return [reference] -- <requested change>` | return reviewed work directly to Implement on the same branch with the full review and explanation |

One `/validate-ship` records an immutable owner exception for all displayed
blockers, including failed suites, missing reviews, substantive findings and
operational blockers. It retains the tested commit H and displayed delivery
commit D separately and delivers D. Old tests remain evidence only for H. Later
undisplayed drift to E, changed evidence or new blockers requires a fresh
shipping decision. A failed or uncertain operation stays factual and stops the
flow. Confirmed delivery with exceptions says “shipped with owner waiver”.
The report retains the exact blockers, evidence revision, owner, source comment,
reason and time.

A non-owner, malformed or stale command changes nothing; the reply shows the
current commands. Defer is allowed only for optional (advisory) findings. A repair
or issue choice never clears a blocking finding. An unverified item can be cleared
by an evidenced owner pass; an unmet defect requires a new tested snapshot, such
as the commit a kept repair makes.
Reviewer status and evidence remain unchanged by owner verdicts. Missing reviews and unmet defects cannot be manually passed.
Push is offered only when the suite passed or its verified coverage is satisfied,
every criterion is satisfied or owner-passed, no blocking finding is open, every optional finding has a choice and
no issue creation is pending, failed or uncertain.

### Remote effects

Every effect records its intent before it acts and checks the real remote state
first on replay (remote branch head, PR by head, merged state, issue by marker). A
failed or uncertain effect keeps Validate open and is retried only when the owner
repeats the command. Verdicts, choices and confirmations are bound to the run and
the tested commit; push, merge and close re-check that the branch head is still the
tested commit.
