# Sync and conflicts

The factory can update a task worktree from its configured base branch before work. The registered clone is preserved. Synchronization depends on [configured sync policy and limits](../configuration/config-yaml.md), branch state and clean applicable work.

Request normal work with an owner comment:

```text
/implement Implement the approved story using the current base branch.
```

The report describes an applied synchronization, refusal or deferral. Dirty retained edits can cause synchronization to be deferred while the agent works with the existing checkout; a deferral is not a successful base merge. Inspect the divergence and finish or preserve those edits before retrying.

When enabled, conflict advice produces a stored resolution proposal for review. Inspect the baseline, raw size/line metrics, resolved regions and any omitted preview content. The displayed diff is bounded and sanitized; it is not the raw data that will be applied. Go parsing checks syntax only, not overall correctness; other files do not gain a Go syntax guarantee.

After reviewing the concrete proposal, use its reported ID:

```text
/confirm 17 apply this resolution
```

Only that exact phrase with a positive proposal ID authorizes apply. “Yes”, reactions and approval without the ID do not. The factory rechecks current revisions, ownership, reconstructed content and the actual merge before writes. Stale or changed inputs require a new proposal.

Dirty confirmation stays pending: follow its report to preserve/finish the existing work, then retry the confirmation when the worktree is ready. Do not discard edits to force a merge. A push failure after a local commit does not mean the commit was undone; inspect the recorded state before recovery.

See [command reference](issue-command-reference.md), [recovery](../operations/recovery.md) and [validation and delivery](validation-and-delivery.md).

[Using the factory](README.md)
