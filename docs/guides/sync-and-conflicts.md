# Sync and conflicts

[Documentation](../README.md) · [Using the factory](README.md)

Before a task runs, the factory can bring its worktree up to date with the base branch (usually `main`). Your registered clone is never touched; only the task's own worktree changes. The settings are in the [configuration reference](../configuration/config-yaml.md):

| Setting | Default | Effect |
| --- | --- | --- |
| `settings.auto_sync_base` | `true` | Sync before a run. Turn it off and runs go ahead on whatever the branch has. |
| `settings.auto_sync_max_behind` | `100` | Refuse a run if the branch is more than this many commits behind (`0` means no limit). |
| `settings.block_run_on_conflict` | `true` | Stop the run when the merge conflicts instead of working on a stale base. |
| `settings.suggest_conflict_resolution` | `false` | Ask a model for a conflict-resolution proposal you can review (see below). |

You can also turn sync on or off for one issue with the `Sync: On` or `Sync: Off` label ([owner commands](owner-commands.md)).

Request normal work as usual:

```text
/implement Implement the approved story using the current base branch.
```

The run report says whether the sync was applied, refused, skipped or deferred. If the worktree still holds unfinished edits from an earlier run, the sync is deferred (`deferred_dirty`) and the agent works with the files as they are. A deferral is not a successful merge. Finish or keep those edits first, then try again. Never clean the worktree just to make a sync pass.

## Implement and the base branch

When Implement runs a feature, the coordinator merges the base branch into the feature branch before it starts any story, and once more before it reports the feature done. If that merge conflicts, a resolver agent merges it in the feature worktree and resolves the conflict, and the factory saves the result and syncs again. If the resolver is blocked, the run stops and asks you. See [Plan graph](../workflow/plan-graph.md#after-the-plan).

## Reviewing a conflict proposal

With `suggest_conflict_resolution` on, a run blocked by a conflict also stores a resolution proposal and posts it for you to review. The posted diff is shortened and cleaned, so it is not the exact data that will be applied. The proposal size is bounded by `conflict_content_max_bytes` (16 KiB by default), and a pending proposal goes stale after `conflict_proposal_ttl_hours` (24 hours by default). For Go files the factory checks that the result parses; that is a syntax check only, not a test of correctness.

This setting is off by default because it sends repository file content to a model.

To apply a proposal, use its ID:

```text
/confirm 17 apply this resolution
```

Only that phrase with a positive proposal ID counts. "Yes", a reaction, or approval without the ID do not. Before it writes anything the factory checks again that the revisions, ownership, and the actual merge still match. If something changed, you need a new proposal.

If the worktree is dirty when you confirm, the confirmation stays pending. Follow the report to finish or keep the existing work, then confirm again. A push failure after a local commit does not undo the commit; look at the recorded state before you recover.

See [owner commands](owner-commands.md), [recovery](../operations/recovery.md) and [validation and delivery](validation-and-delivery.md).
