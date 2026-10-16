# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The generic activity and action spec already exist locally. Trusted binding supplies working branch, distinct base branch and lock. Outcomes are synced,current,conflict,retry; cancellation and git/forge outages remain errors. Conflict and contention leave the working branch unchanged; clean merges are published from a scratch tree.

Evidence:

- `internal/turn/sync_base.go:14`
- `internal/turn/sync_base.go:57`
- `internal/graphexec/sync_base.go:18`

## f2

Production RegisterSyncBase call sites found in internal/turn bind only the Implement coordinator. The binding selects integration branch, repository base and parent story-progress lock. Bug uses Workflow rather than Mode: spec registration tests explicitly set WorkflowBug, while RegisterSyncBase only accepts Mode and constructs a spec without Workflow. Extending Bug support therefore requires a workflow-aware registration/binding seam, not merely a DOT node.

Evidence:

- `internal/turn/implement_activities.go:127`
- `internal/turn/implement_activities.go:135`
- `internal/turn/sync_base.go:34`
- `internal/graphexec/implement_graph_test.go:414`
- `internal/graphexec/registry.go:59`

## f3

Canonical Implement graph has sync_start and sync_done, bounded resolver/retry loops and conflict routes. Worker path goes select_path -> budget -> worker_agent with no explicit base-sync node. Coordinator binding cannot be reused unchanged for a worker because it targets the feature branch.

Evidence:

- `internal/teamsource/implement/implement.dot:49`
- `internal/teamsource/implement/implement.dot:75`
- `internal/teamsource/implement/implement.dot:81`
- `internal/turn/implement_activities.go:141`

## f4

prepareClone fetches and ensures a worktree, then normally performs remote task reconciliation and base sync. It has a deferred Bug graph exception and a separate confirmed-resolution path. syncBase skips Validate, respects enablement and behind cap, and defers dirty work. syncTask reconciles the branch with its own remote copy; it is a different operation from merging the base.

Evidence:

- `internal/turn/prepare.go:710`
- `internal/turn/prepare.go:721`
- `internal/turn/prepare.go:734`
- `internal/turn/prepare.go:753`
- `internal/turn/prepare.go:994`
- `internal/turn/prepare.go:1016`
- `internal/turn/task_sync.go:14`

## f5

Story preparation replaces repo.BaseBranch with storyBaseBranch for workers. Plan and Implement stories use the pushed parent branch when found, otherwise fall back to repository base. This also controls starting branch selection; removing it would affect more than synchronization.

Evidence:

- `internal/turn/prepare.go:178`
- `internal/turn/prepare.go:1644`
- `internal/turn/prepare.go:1664`

## f6

Bug runtime prepares source with synchronize=true for the fixer callback. Preparation commits retained work, reconciles task remote then calls legacy syncBase. Base conflict/too-far/skip details become fixer prompt notes. Canonical Bug graph goes directly to fixer from approval and revalidation, without an lf_sync_base node.

Evidence:

- `internal/turn/bug_graph_runtime.go:219`
- `internal/turn/bug_graph_runtime.go:230`
- `internal/turn/bug_graph_prepare.go:51`
- `internal/turn/bug_graph_prepare.go:63`
- `internal/teamsource/bug/bug.dot:11`
- `internal/teamsource/bug/bug.dot:16`

## f7

Explicit activity does not consult AutoSyncBase, cap or dirty local edits: it begins at fetched remote working head, merges in scratch and publishes. Keeping legacy logic creates potential ordering and remote/local freshness interactions. Branch existence, local retained edits, refresh after publication and writer-lock coordination must be accounted for when binding other graphs.

Evidence:

- `internal/turn/sync_base.go:92`
- `internal/turn/sync_base.go:115`
- `internal/turn/implement_repo.go:13`
- `internal/turn/implement_repo.go:44`
- `internal/turn/prepare.go:1047`

## f8

Existing tests cover real-git merge/current/conflict behavior, busy/moved-head retries, unchanged base and scratch cleanup, plus generic registration for another mode and Bug spec vocabulary. Tests were inspected, not executed; there are no implementation changes in this research pass.

Evidence:

- `internal/turn/sync_base_test.go:46`
- `internal/turn/sync_base_test.go:95`
- `internal/turn/sync_base_test.go:142`
- `internal/turn/sync_base_test.go:170`
- `internal/graphexec/implement_graph_test.go:402`

## f9

Canonical sources include Go, Python, Swift and dotnet teams. Source README reserves release version selection, generation and linking to owner; historical releases and captures remain immutable. teamrelease rejects changed bytes under an existing published version. Doctor sync_branch remains an independent owner-approved typed repair.

Evidence:

- `internal/teamsource/README.md:15`
- `internal/teamrelease/release.go:81`
- `internal/teamsource/README.md:27`
- `docs/operations/doctor.md:56`
- `internal/doctor/prompt.go:13`

## f10

Bug execution already holds the task lock; Bug preparation independently acquires the clone gate, so an added activity must honor existing lock ownership rather than recursively take the task lock.

Evidence:

- `internal/turn/bug_graph_runtime.go:87`
- `internal/turn/bug_graph_prepare.go:41`
