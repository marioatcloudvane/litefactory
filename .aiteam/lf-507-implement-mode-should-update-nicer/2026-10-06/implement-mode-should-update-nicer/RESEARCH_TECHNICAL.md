# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Plan provides the requested edit-in-place precedent: one stable plan-summary delivery, saved CommentID, a SummaryDigest to skip unchanged bodies, and factory-derived story stage/status; the daemon calls it through reportPlanProgress.

Evidence:

- `internal/turn/plan_summary.go:22`
- `internal/turn/plan_summary.go:73`
- `internal/daemon/plan_reporting.go:23`

## f2

The current Implement ledger defines waiting, running, done, merged and parked with guarded transitions, a revision and worker-session identity. It limits each parent to four running stories. Ready only starts waiting stories whose dependencies are merged.

Evidence:

- `internal/store/implement_stories.go:13`
- `internal/store/implement_stories.go:112`
- `internal/turn/implement_activities.go:175`

## f3

The graph keeps the coordinator at await while stories run and reaches handoff after ready reports drained. The worker graph has one worker_agent for production code and scoped tests, not separate implementing/testing graph nodes. Worker failures use a first attempt plus two repairs; stopped answers park.

Evidence:

- `internal/teamsource/implement/implement.dot:1`
- `internal/teamsource/implement/implement.dot:66`
- `internal/teamsource/implement/story-worker.md:14`

## f4

ImplementHandoffActivity already posts once, scans for its marker, edits the same comment and skips identical content. Its sections are Merged, Parked and Still waiting; running and done are described within Still waiting. This is a reuse candidate, but its graph scheduling is too late for continuous progress.

Evidence:

- `internal/turn/implement_handoff.go:26`
- `internal/turn/implement_handoff.go:41`
- `internal/turn/implement_handoff.go:119`

## f5

activity_update accepts only sequence/message; the ActivityService has no GitHub publication dependency and keeps process-local advisory state. Its reducer bounds messages to 2048 bytes and updates to 1024, and rejects stale/inactive activity. It cannot supply durable per-story table state unchanged.

Evidence:

- `internal/submission/family_tools.go:79`
- `internal/turn/activity.go:59`
- `internal/agentresult/progress.go:12`
- `internal/agentresult/progress.go:45`

## f6

New progress tooling touches the family schema/permission catalog, runtime registration and attempt-bound handler. The existing runtime registers activity families with bound receipts. Current graph invocation restrictions mean a tool should have dedicated authorized registration, not rely on issues_comment or evidence_submit being present.

Evidence:

- `internal/submission/family_tools.go:34`
- `internal/turn/mcp_runtime.go:314`
- `internal/turn/mcp_runtime.go:332`
- `internal/turn/mcp_runtime.go:349`

## f7

storyDone commits and pushes before changing running to done. Merge moves done to merged on the feature integration branch and runs no tests. The worker explicitly runs scoped package tests; the full suite and acceptance review are deferred to Validate. Final Test cannot truthfully be inferred from the current Implement graph.

Evidence:

- `internal/turn/implement_steps.go:161`
- `internal/turn/implement_merge.go:20`
- `internal/teamsource/implement/story-worker.md:26`

## f8

Advisory integration option: retain a latest per-story phase under active worker attempt authority; aggregate it with the durable Implement ledger; schedule factory reporting during daemon progress processing and terminal transitions; use durable delivery identity, change detection and serialized parent publication. Only a running story's phase comes from the agent. New persistence belongs in internal/store. This is an inference from existing owners, not an implemented contract.

Evidence:

- `internal/turn/plan_summary.go:77`
- `internal/store/implement_stories.go:43`
- `internal/turn/activity.go:119`
- `.aiteam/APP_CONSTRAINTS.md:206`
