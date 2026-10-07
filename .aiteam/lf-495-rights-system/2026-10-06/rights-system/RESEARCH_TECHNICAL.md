# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The identity store defines owner and bot roles, keyed by forge and role, with numeric user IDs as durable identity. The comment filter allows only configured-owner comments to trigger; other human comments are recorded as third_party and trigger nothing. No repository role is consulted by this gate.

Evidence:

- `internal/store/identities.go:15`
- `internal/turn/classify.go:245`

## f2

An issue body may bootstrap a run when assigned to the installation owner, nonempty, and without existing comments. Issue authorship can differ from the owner. Assignment is therefore an admission mechanism for this path, not delegated approval. Owner comments use a separate numeric-author gate; reassignment alone should not be treated as revoking owner-comment authority.

Evidence:

- `internal/daemon/tick.go:1537`
- `internal/daemon/tick.go:1582`
- `internal/turn/classify.go:245`

## f3

Graph owner replies are validated again in store admission: author must equal configured owner, reply must match the published active task/mode/question, and pre-question or replayed replies are refused. Broadening only the comment filter cannot establish delegated approval.

Evidence:

- `internal/store/graph_owner_waits.go:61`
- `internal/store/graph_owner_waits.go:72`
- `internal/daemon/research_owner_resume.go:69`

## f4

Graph Plan has distinct story-intent, technical-shaping and acceptance-criteria/assembled-review stages. Story replies carry story, stage, candidate and wait identities, and are currently owner-only. These provide concrete scope and freshness references for future grants.

Evidence:

- `docs/workflow/plan-graph.md:3`
- `internal/daemon/plan_story_task.go:223`
- `internal/daemon/plan_story_task.go:281`

## f5

Current constraints separate implementation engineering completion, read-only Validate acceptance, owner exceptions and explicit PR/current-head delivery approval. A human acceptance-signoff capability must not be confused with the agent's retained-evidence validation result. Waivers and merge authority are independent actions.

Evidence:

- `.aiteam/APP_CONSTRAINTS.md:102`
- `.aiteam/APP_CONSTRAINTS.md:207`
- `internal/daemon/delivery.go:20`
- `internal/daemon/owner_exceptions.go:22`

## f6

Session and approval state resides in the installation's local store. Issue reassignment changes GitHub routing data but the inspected admission paths do not transfer that local state. Cross-installation continuation, coordinated execution and transfer of approval/effect history are unresolved, rather than proven existing capabilities.

Evidence:

- `internal/store/migrations/0001_init.sql:51`
- `internal/store/graph_owner_waits.go:56`
- `internal/store/plan_workflow.go:111`

## f7

Code admits graph Plan by default while preserving already-selected legacy paths. Constraint CONC-24 still states new sessions select legacy until an owner tryout/lift, so repository documentation and code disagree. This research establishes available code surfaces, not hosted rollout status.

Evidence:

- `internal/store/plan_workflow.go:21`
- `internal/turn/workflow_admission_gap_test.go:70`
- `.aiteam/APP_CONSTRAINTS.md:224`

## f8

Agent tool permission profiles control subprocess capabilities and are separate from human comment authorization. Human grants should not implicitly expand tool profiles or expose forge credentials to agents.

Evidence:

- `internal/config/policy.go:350`
- `.aiteam/APP_CONSTRAINTS.md:100`
