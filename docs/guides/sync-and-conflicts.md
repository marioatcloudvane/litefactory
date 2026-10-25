# Sync and conflicts

[Documentation](../README.md) · [Using the factory](README.md)

Before a task runs, the factory can bring its worktree up to date with the base branch (usually `main`). Your registered clone is never touched; only the task's own worktree changes. The settings are in the [configuration reference](../configuration/config-yaml.md):

| Setting | Default | Effect |
| --- | --- | --- |
| `settings.auto_sync_base` | `true` | Sync before a run. Turn it off and runs go ahead on whatever the branch has. |
| `settings.auto_sync_max_behind` | `100` | Limit automatic task/base preparation merges; beyond this distance, skip synchronization and continue with a caveat (`0` means unlimited). |
| `settings.block_run_on_conflict` | `true` | Deprecated compatibility setting. Either value permits preparation continuation through conflicts, with a caveat. |
| `settings.suggest_conflict_resolution` | `false` | Ask a model for a conflict-resolution proposal you can review (see below). |

You can also turn sync on or off for one issue with the `Sync: On` or `Sync: Off` label ([owner commands](owner-commands.md)).

Request normal work as usual:

```text
/implement Implement the approved story using the current base branch.
```

Preparation observes both the remote task branch and the base branch. Conflicts, retained edits and synchronization skipped beyond the distance cap leave the checkout, index and branch intact. The original request continues into permitted work without another command or acknowledgement. The normal result contains one combined preparation caveat for unresolved conditions. Ahead-only branches, absent remote task branches and successful synchronization add no freshness warning.

A deferral is not a successful merge. Existing unmerged files remain visible; work may continue on unaffected authorized files or be limited to reading. Final save and integration checks still apply, and operational or checkout identity failures still stop the run. Never clean, reset or stash existing work just to make synchronization pass.

Pending refinements remain pending. Active approved managed scope may continue; a managed request without active approved scope is limited to preparation and reading. Standalone Implement and approved Bug fixers retain their existing write authority. Unusable optional planning context stays preserved and excluded; it supplies no scope or authority.

## Implement and the base branch

When Implement runs a feature, coordinator and worker preparation may defer synchronization and continue permitted work with a caveat. Before the feature is reported done, final synchronization and integration must still succeed. Final-sync conflicts retain the existing resolver flow: a resolver agent works in the feature worktree, and the factory saves the result and syncs again. If that resolver is blocked, the run stops and asks you. See [Plan graph](../workflow/plan-graph.md#after-the-plan).

## Reviewing a conflict proposal

With `suggest_conflict_resolution` on, the existing conflict-proposal flow can store a resolution proposal and post it for you to review. A preparation caveat does not apply a proposal or grant permission to resolve conflicts. The posted diff is shortened and cleaned, so it is not the exact data that will be applied. The proposal size is bounded by `conflict_content_max_bytes` (16 KiB by default), and a pending proposal goes stale after `conflict_proposal_ttl_hours` (24 hours by default). For Go files the factory checks that the result parses; that is a syntax check only, not a test of correctness.

This setting is off by default because it sends repository file content to a model.

To apply a proposal, use its ID:

```text
/confirm 17 apply this resolution
```

Only that phrase with a positive proposal ID counts. "Yes", a reaction, or approval without the ID do not. Before it writes anything the factory checks again that the revisions, ownership, and the actual merge still match. If something changed, you need a new proposal.

If the worktree is dirty when you confirm, the confirmation stays pending. Follow the report to finish or keep the existing work, then confirm again. A push failure after a local commit does not undo the commit; look at the recorded state before you recover.

See [owner commands](owner-commands.md), [recovery](../operations/recovery.md) and [validation and delivery](validation-and-delivery.md).
