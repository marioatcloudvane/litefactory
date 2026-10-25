# Story 002 implementation notes

Only the approved worker timing and timeout recovery story is in scope. Three subagents built configuration/tests, ledger/recovery storage, and owner reply/scheduling plus the main end-to-end test. The main worker implemented runtime/provider timing and reviewed the combined changes. Team changes remain in canonical sources; generated releases and catalogue links are unchanged.

## Existing agent guidance violations for operator disposition

The agent-guidance standard requires behavioral instructions in agent Markdown or skills. Existing instructions encountered in Go remain unchanged, as the standard and approved scope require:

- `internal/turn/research_agent.go:21` and `:32`: `researchNodeInstructions` and `researchNodeHardRules` contain result-route and agent behavior instructions.
- `internal/turn/research_agent.go:551`, `:584` and `:586`: `implementDelegationRequest` and the existing behavioral sentences in `implementAttemptGuidance` instruct delegation and stopping behavior. This story changes only the supplied timing facts to exact seconds and the persisted deadline.
- `internal/turn/implement_policy.go:17` and `:19`: `implementNodeInstructions` and `implementNodeHardRules` contain agent behavior and result submission instructions.
- `internal/turn/submission_runner.go`: existing submission/result instructions remain in Go. This story bounds local helper cleanup and preserves the existing accepted-publication recovery path; it does not migrate that guidance.

No instruction migration or team release is included. The operator decides whether and how to remedy these existing violations.

## Decisions at implementation boundaries

- Raw-presence migration resolves YAML scalar aliases and mapping merge keys. These are valid existing configuration syntax; ignoring them would silently replace explicit legacy seconds with constructor defaults. The resolver still reads only raw presence and never infers explicit values from filled defaults (`internal/config/config.go:1114`).
- An expired parent cannot perform graph bookkeeping with its original context. A separate five-second context is limited to durable local timeout settlement, including restoring a missing stop receipt from the immutable timing snapshot. It invokes no provider, collects no new process evidence and sends no remote notice. Independent evidence collection keeps its one shared allowance of at most 30 seconds (`internal/agent/deadline.go:51`).

## Verification

Complete package tests passed for agent, configuration, graph execution, canonical team sources, both provider runners, reports, store, turn, daemon and CLI. All used finite timeouts; race detection was enabled except for store, turn, daemon and CLI, as required. The final runtime run passed in 520.074 seconds (turn), 120.984 seconds (daemon) and 30.761 seconds (CLI). Earlier failures found during implementation were fixed; no pre-existing failures remain to report.

The two-story end-to-end regression passed three consecutive runs. Focused race checks passed for submission/expiry receipt boundaries, retained Research results under parent expiry, redaction before ledger clipping, and restoration of missing timeout settlement evidence. Build, pinned scoped lint (zero findings), formatting and diff whitespace checks passed.

Acceptance remains with Validate. No live service or production tests were used; network fixtures bind local test servers. The retained logs below record the final relevant runs; provider and graph/team package passes are extracted from their complete grouped run, whose configuration and report failures were subsequently fixed and rerun separately.

## Submission

Request `61e650162cefc8cd911bd1a85bbddefb` passed preflight and was submitted as completed. The matching receipt acknowledged accepted input at revision 1. Engineering, acceptance and publication remain pending factory processing; acknowledgment grants none of them.
