# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

All repository observations in this pass refer to revision f30f2d7948a58e92febe6dd782b8c59e113e658d. This was read-only source research; no product edits, test executions, live-run reproductions, merges or issue publications were performed. The supplied reports about #429, #431 and #443 are owner-reported incidents, not independently reproduced incidents.

Evidence:

- `teams/source/bug/bug.dot:1`

## f2

Current Bug has 17 activity nodes plus its stopped terminal, including analysis recording/reviews, repair reservation, a separate test child, PR review, merge, close and reporting. The child runs regression, focused and full phases separately. Both agent nodes expose success/failed rather than routing directly on application responses.

Evidence:

- `teams/source/bug/bug.dot:18`
- `teams/source/bug/bug_tests.dot:1`
- `internal/turn/research_agent.go:75`

## f3

A fixer response other than done or reshape reaches report and then stopped, with no lf_ask on that route. Failure retry uses a hard-coded node map; its completed blocked-stop branch is restricted to analyst-related stops. Exhausted continuations demand /grant-bug-attempt or /stop-bug rather than arbitrary owner guidance. These source mechanisms support the reported routing complaints.

Evidence:

- `teams/source/bug/bug.dot:83`
- `internal/store/bug_tasks.go:532`
- `internal/turn/bug_runtime_replies.go:12`

## f4

AgentActivity.acceptRetained already validates a retained accepted result. Non-Bug policies route from it without terminal parsing. Bug still calls the decoder on terminal output; nonempty invalid JSON may fall back to retained response, while valid JSON of the wrong shape or empty output can fail. This directly matches the reported receipt-shaped-final-message failure mechanism.

Evidence:

- `internal/turn/research_agent.go:240`

## f5

lf_loop currently evaluates a closed equality comparison over response, with a default before the first attempt, and returns true/false. It is not an owner-conversation loop. Shared control validation also assumes a success edge for agents, a finite positive loop limit, and durable publication boundaries for questions. Simply deleting extra DOT nodes does not supply the requested behavior.

Evidence:

- `internal/graphexec/research_control.go:49`
- `internal/graphexec/research_control.go:109`
- `internal/graphexec/research_control.go:191`

## f6

Both Bug roles already declare tier: flagship. Role resolution supports lf_model_tier overrides and inherited role tiers. Analyst instructions currently prescribe budgets, approval revisions and result rules; fixer instructions define done as authored work, explicitly not passing checks. These instructions would need to change with the simplified lifecycle.

Evidence:

- `teams/source/bug/bug-analyst.md:1`
- `teams/source/bug/bug-fixer.md:1`
- `internal/teamsource/research.go:204`

## f7

The broad-rights changes described as a separate branch are at least partly present: Codex requires workspace-write for the fixer and read-only for the analyst; shared result tools exclude self-check tools. Legacy self-check readers still survive in repair handoff and delivery digest/presentation paths, so removing the tool surface did not retire the underlying dependencies.

Evidence:

- `internal/codex/runner.go:264`
- `internal/agent/bug_permissions.go:26`
- `internal/submission/bug_tools_test.go:12`
- `internal/turn/bug_repair_handoff.go:61`
- `internal/turn/bug_pr.go:310`

## f8

Migration 0079 already consolidated 33 dedicated Bug tables into six and explicitly assumed no running Bug tasks for that destructive cutover. The 0066–0080 interval contains 13 Bug-named migration files and two unrelated files. Migration execution is ordered, skips recorded versions and applies each unseen migration transactionally. Fewer active stores and fewer historical migration files are different goals; deleting history is not an established upgrade plan.

Evidence:

- `internal/store/migrations/0079_bug_store_consolidation.sql:1`
- `internal/store/migrations/0080_bug_repair_grants.sql:1`
- `internal/store/migrate.go:32`

## f9

Measured source baseline: under internal/, select Go files whose basename starts with bug or whose path contains package directory buganalysis or bugchecks; split on _test.go suffix. This yields 57 non-test files / 15,524 lines and 59 test files / 10,009 lines. This is a repeatable naming-based baseline, not total Bug dependency size, and includes blank/comment lines.

Evidence:

- `internal/turn/bug_runtime.go:1`
- `internal/store/bug_tasks.go:1`
- `internal/bugchecks/executor.go:1`

## f10

Existing Bug admission compares its retained graph definition digest to the current graph and rejects mismatch. Role/skill changes are allowed separately. Thus changing topology needs an explicit cutover for existing tasks; this pass establishes the guard, not how many live tasks are affected.

Evidence:

- `internal/turn/bug_runtime.go:163`

## f11

Current merge and close activities already own remote effect observation and recovery: merge distinguishes confirmed/stale/blocked/failed/pending, checks PR/head/main identity, and observes integration; close requires confirmed integration and avoids blind repeat dispatch. A combined final activity touches these guarantees, not only DOT presentation.

Evidence:

- `internal/turn/bug_merge.go:24`
- `internal/turn/bug_merge.go:140`
- `internal/turn/bug_close.go:129`
