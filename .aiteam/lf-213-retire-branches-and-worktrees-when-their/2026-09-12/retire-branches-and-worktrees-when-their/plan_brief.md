# Plan: retire-branches-and-worktrees-when-their

- Branch: lf/213-retire-branches-and-worktrees-when-their
- Date: 2026-09-12
- Parent issue: https://github.com/marioatcloudvane/litefactory/issues/213
- Citation base: 2846e83138b9663b4495618fc20d645d1a0952e9

## Shared Decisions

Reuse existing CLI, daemon, GitHub, Git, store and logging packages. One evaluator serves advisory preview and automation; the daemon owns new retirement mutations and supplies time. Only fresh explicit GitHub closure plus independent content-preservation proof permits deletion. Exact ancestry is required; issue closure reason never proves merge. Keep registered identity and workflow evidence after resources retire.

Existing task/parent ownership precedes the clone gate. Storage transactions never span external I/O. Use minimal durable intent/outcome and pass progress; logs provide operator history. Ordinary retirement decisions are Info, unknown verification Warn and failed mutations/storage Error. Do not log sensitive contents.

## Workflow

US-001 → US-002 → US-003. Preview/evaluation establishes common decisions; guarded execution supplies safe effects and recovery; scheduling connects them to daily operation. No new mode is selected by this plan.

## Cross-Story Constraints

All Git execution stays in internal/git and SQL in internal/store. Preserve the one-connection pool, registered clone checkout/index/HEAD, configured base/default branches, local-only content and existing acceptance/delivery semantics. No new dependencies or instruction-tree edits. Use offline real Git/store tests for destructive and recovery semantics, unit fakes for decisions, no production deletion or nested-agent evidence. Implement executes checks; Validate assesses retained evidence.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Preview safe retirement of registered task resources | [#255](https://github.com/marioatcloudvane/litefactory/issues/255) |
| US-002 | Retire eligible resources with guarded deletion and recovery | [#256](https://github.com/marioatcloudvane/litefactory/issues/256) |
| US-003 | Run daily retirement and explain its outcomes | [#257](https://github.com/marioatcloudvane/litefactory/issues/257) |

## Out of Scope

Automatic archive tags, deleting unmerged history, patch-equivalence heuristics, history rewriting, base changes, GitHub delete-on-merge settings, manual apply/history UI, configurable schedules, policy matrices, new job/event frameworks, issue comments or acceptance changes, and behavioral expansion of manual task clean.

## Provenance

Owner runs 0001–0003 and this session's research, feature spec and architectural review. Daily cadence, direct fresh GitHub closure verification and visible logging are agreed. Sizing assumptions: elapsed 24-hour cadence, retention from last completed run with valid no-run creation fallback, conservative ignored-file skips, exact ancestry and existing logs. No owner questions remain. Citations describe the pinned checkout; recheck touched source during Implement if the base changes. Helper acceptance records input only; publication/readiness and save remain pending until factory completion.
