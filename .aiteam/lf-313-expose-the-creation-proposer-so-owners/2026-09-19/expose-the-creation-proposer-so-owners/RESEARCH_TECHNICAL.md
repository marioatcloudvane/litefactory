# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Only PrepareCreationProposal ever retains a proposal. It accepts only an owner comment starting with '/mcp-create prepare '.

Evidence:

- `internal/turn/publication_recovery.go:115`
- `internal/daemon/mcp_creation.go:46`

## f2

CreationPublicationService.Prepare does the same preparation from a Go value. It still has no non-test callers on the current base (origin/main).

Evidence:

- `internal/turn/creation_publication.go:97`

## f3

The agent has no tool that originates a proposal. issues_create_batch is registered only when an owner-approved intent already exists.

Evidence:

- `internal/turn/mcp_runtime.go:296`
- `internal/turn/mcp_guidance.go:68`

## f4

The proposal reply template already renders the full envelope plus the approval command. It reads as written for a factory-produced proposal.

Evidence:

- `internal/report/reply.go:737`

## f5

The owner path and Prepare differ only in nonce (deterministic vs random) and the 48000-byte cap. Retention, intent, TTL, grant binding and dispatch are shared.

Evidence:

- `internal/turn/publication_recovery.go:152`
- `internal/turn/creation_publication.go:97`

## f6

CreationArtifact has title, body, parent and links only. There is no mode field.

Evidence:

- `internal/submission/creation.go:26`

## f7

No prior work toward an agent-facing prepare tool exists in the repository.

Evidence:

- `internal/turn/creation_publication.go:97`

## f8

The proposal reply renderer refuses any envelope over 48000 bytes and re-checks its hash. A proposal retained above that size could never be shown to the owner, so it could never be approved.

Evidence:

- `internal/report/reporter.go:122`

## f9

Approval handling records intent only and never launches a run. The grant binds on the next Research attempt, and only if that attempt bound after the approval, within 30 minutes of the approval comment.

Evidence:

- `internal/daemon/mcp_creation.go:58`
- `internal/store/mcp_creation.go:237`

## f10

Retaining a new proposal makes it the current one and bumps the revision. An approval of an older hash is then refused.

Evidence:

- `internal/store/mcp_creation.go:118`

## f11

The tool list is fixed at launch and checked against the provider catalog. A new tool must be registered before the guard is bound.

Evidence:

- `internal/turn/mcp_runtime.go:314`
- `internal/turn/mcp_runtime.go:358`

## f12

Reply posting is triggered by an owner comment and skips when that trigger is already consumed. A post-run presentation therefore needs its own delivery identity.

Evidence:

- `internal/turn/report.go:624`

## f13

Mode is chosen per issue when its first comment is admitted, by command or classifier. A created issue without a label is therefore already handled.

Evidence:

- `internal/daemon/mode.go:132`

## f14

NEW since the last pass: on origin/main (38 commits ahead of this branch) the creation wiring in prepareMCP is skipped for graph Research-node invocations (Invocation == InvocationResearchNode). Both the issue-comment service and the creation grant/issues_create_batch registration are gated on that. Graph-run Research is now the normal Research path, so the new tool must be registered for that invocation kind, and the post-run presentation must fit the graph run's result flow. This branch does not yet contain that change; the rebase will touch the same function.

Evidence:

- `internal/turn/mcp_runtime.go:292`
- `internal/turn/mcp_runtime.go:301`
