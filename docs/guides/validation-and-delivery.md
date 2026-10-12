# Validation and delivery

[Documentation](../README.md) · [Using the factory](README.md)

Validate checks the finished work and helps you ship it. Building, checking, pushing, merging and closing are separate results, and each remote step needs your own command.

```text
/validate Check the finished stories against their acceptance criteria.
```

Validate also starts by itself after Implement has merged every story if `validate.auto_start` is on (it is off by default; see the [configuration reference](../configuration/config-yaml.md)).

## What Validate does

1. **Suite.** It runs the repository's full test phase on one exact commit. The commands come from `.litefactory/bug-checks.yaml` in that commit (see [Bug checks](../bug-checks.md)). With no valid recipe the suite is blocked, not passed.
2. **Repair.** If the suite fails, one agent may try a small fix, at most two rounds. The factory measures what it really changed. A small change is kept as a new commit and the suite runs again. A change that is too big, or one that touches packages, dependencies, schema, public API, tests or acceptance criteria, is not kept.
3. **Review.** Three read-only reviewers look at the final commit: acceptance criteria, code, and architecture. A criterion counts as satisfied only when a passed suite command of that commit backs it. Anything that cannot be proven stays unverified.
4. **Report.** You get one report comment, updated in place, with a short revision code. It shows the suite, repair rounds, reviews, manual checks, findings and the exact next commands.

The run ends as assessed, handed back to Implement with what must change, or blocked with the reason. It never says "approved" by itself. Details: [Validate](../workflow/validate.md).

## Your commands

Every command names the current report revision and must be the whole comment. The report shows the exact text. In short:

- `/validate-check` records your manual verdict on a criterion nobody could verify.
- `/validate-finding` chooses to repair a finding, defer it, or turn it into a new bug or story issue.
- `/validate-push`, `/validate-merge` and `/validate-close` push the tested commit and open the pull request, merge it at the exact head, and close the parent and story issues.
- `/validate-ship` does the whole normal case in one command: it records your evidence for the open manual checks, defers the optional findings, pushes, merges and closes. It is refused, and records nothing, if the suite did not pass, a review has no usable result, a failed criterion or blocking finding is open, or an issue you chose is not created yet.

Exact syntax: [owner commands](owner-commands.md#validate-controls).

A stale, malformed or non-owner command changes nothing, and the reply shows the current commands. A repair or issue choice does not clear a blocking finding; only a new tested commit does. If a push, merge or close fails or is uncertain, nothing is retried by itself: repeat the command with the current revision and it continues at the first undone step. The factory checks the real state of the branch and pull request first, so nothing is done twice. Validate counts as delivered only after the merge and the close both succeeded.

## Older managed features

Features admitted before the graph workflows keep their own controls: `/deliver <generation>` prepares a pull request proposal, `/merge <generation> <digest> <PR> <head-SHA> main` approves that exact PR and head, `/followup <generation> <sha256> linked|attach` approves a follow-up issue and `/waive <generation> <selectors> -- <reason>` records a reasoned exception. A waiver keeps the failed or unverified finding visible; it does not fake a passing test or approve a merge. Their syntax is in [owner commands](owner-commands.md#scheduling-conflicts-and-delivery).

## If you are blocked

Work out which part is missing: the suite, a review, a manual check, an open finding, the push, the pull request or the remote confirmation. The report names it. Then see [workflow](workflow.md), [Validate](../workflow/validate.md) and [recovery](../operations/recovery.md).
