# US-009 practical way forward — run 0013

Status: diagnostic guidance only; no migration, runtime mutation, test execution or engineering completion claimed.

The work is not a bulk conversion of saved session files. The supported upgrade changes unfinished managed story content and its content/submission contract generation. Historical files, completed stories and persisted workflow versions remain intact. A successful upgrade requires a fresh published and approved Plan before implementation resumes.

## First unblocker

Produce a factory-owned, read-only inventory/export of legacy parents and outstanding effects. There is no existing CLI command that supplies the complete retirement inventory required by US-009. The per-parent upgrade preview is useful once candidate parents are known; it cannot discover or establish the absence of all legacy consumers. `task retire` concerns worktrees/branches and does not fill this gap.

The inventory must identify observation time, parent identity, content/submission/workflow versions, active or resumable consumers of each of the three legacy inputs, and pending recovery/delivery effects. Missing tooling should be implemented or a supported factory export supplied; repeatedly asking the owner for unspecified evidence does not advance the task.

## Supported per-parent migration

After an affected parent's active attempts finish, inspect its upgrade preview:

```sh
/Users/mariomh/litefactory/dist/litefactory story-contract upgrade 'github:OWNER/REPO#PARENT' --preview
```

Use an actual legacy parent identified by the inventory. Do not assume #264 needs upgrading, and do not apply this to every parent. Preview is the default when neither preview nor apply is specified.

Preview reports eligibility/conflicts for that parent's unfinished managed stories. Upgrade requires the parent to be in planning, content/submission versions to be 1/1, no active parent/story attempt, and no outstanding non-superseded StoryProgress delivery. Resolve refusal through normal supported completion/planning/recovery paths, not manual state edits.

After the operator has reviewed and authorized an eligible preview:

```sh
/Users/mariomh/litefactory/dist/litefactory story-contract upgrade 'github:OWNER/REPO#PARENT' --apply
```

For a locked legacy scope, an explicit parent `/plan ...` command or `replan` is the supported transition back to planning; this deliberately invalidates readiness. Let that run exit before applying the upgrade.

Retain the outcome. If publication is interrupted, resume the same apply instead of editing persisted state. Run Plan to publish the next complete proposal. The subsequent explicit `/implement ...` request approves/freezes it before engineering; there is no separate CLI approve command. This is a new approval of converted managed scope, not a request to manually recreate Markdown files.

## Reader removal

Refresh the inventory after upgrades/completions and pending effects drain. Only then remove each proven-unused legacy input separately: Plan v1 file handoff, Implement event-file ingress, and Validate v1 parser. Preserve v2 Plan publication, StoryProgress recovery/delivery, historical reads/migrations and workflow-v1 semantics. Execute the required compatibility and aggregate checks after the implementation changes.

If supported consumers remain, retain the relevant readers. Adoption and retirement remain separate milestones; this runbook does not waive US-009 or approve a scope split.

## Source verification

- `internal/cli/commands.go:7`: CLI surface, including absence of complete legacy-consumer inventory.
- `internal/cli/cmd_story_contract.go:24`: exact upgrade syntax and preview/apply behavior.
- `internal/store/story_contract_upgrade.go:75`: upgrade eligibility gates.
- `internal/store/story_contract_upgrade.go:195`: new generation with preserved workflow/history.
- `internal/turn/managed_scope.go:20`: explicit replan and Implement approval.
- `docs/story-contract.md:129`: operator upgrade protocol.

Verified through source inspection by the analysis-only implementation engineer. Commands above are instructions for a future authorized operator action, not actions executed in run 0013.
