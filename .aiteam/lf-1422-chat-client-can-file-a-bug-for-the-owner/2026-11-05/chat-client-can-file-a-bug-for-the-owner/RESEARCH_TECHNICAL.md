# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

All changing owner actions go through the generic action_propose tool (action name plus string arguments, client_request_key required), then action_confirm. The only dedicated per-action propose tools are stage decisions. No tool exists for creating a Bug.

Evidence:

- `internal/connector/actions.go:20`
- `internal/connector/actions.go:99`
- `internal/connector/stage_decisions.go:139`

## f2

Every owner action must be classified in a fixed allowlist (tier and scope). Conversational actions such as draft, diagnose, answer and brainstorm_create_tasks use scope repository:actions. An unclassified action is refused. A new bug-filing action therefore needs one line here to reuse repository:actions with no new scope.

Evidence:

- `internal/owneraction/registry.go:17`
- `internal/owneraction/registry.go:23`

## f3

Action schemas, availability flags and the discovery catalogue live in one place. Adding a command means a ChangeDefinition case, an Allows case, an ActionCatalog name and a domain handler with Resolve, Recheck, Execute and Recover. brainstorm_create_tasks is the closest template: it freezes exact issue title/body in a payload with a digest, checks repository consent, runs the redaction check (Protect), and Execute only admits a durable row.

Evidence:

- `internal/factory/owneractions/catalog.go:11`
- `internal/factory/owneractions/catalog.go:52`
- `internal/factory/owneractions/brainstorm_tasks.go:144`
- `internal/factory/owneractions/brainstorm_tasks.go:403`

## f4

Argument values through action_propose are capped at 8192 bytes each and 64 arguments, so a long description (expected vs actual, steps, evidence) is limited to 8 KiB unless split into several fields or a dedicated tool with its own schema.

Evidence:

- `internal/connector/actions.go:143`

## f5

A repository for an action must be a registered GitHub repository, and a repository:actions grant needs explicitly chosen repositories. Both checks exist for brainstorm actions and can be reused. Refusal text 'Action scope or explicit selected-repository consent is missing' already exists.

Evidence:

- `internal/factory/owneractions/brainstorm_tasks.go:70`
- `internal/connector/consent.go:156`
- `internal/connector/actions.go:299`

## f6

The existing feature-issue creator (draft/confirm) creates an issue through a narrow port with title and body only, redacted, with a durable claim and marker reconciliation so replay finds the existing issue. The GitHub CreateIssue deliberately sends no labels. Labelling is a separate call (AddIssueLabel, add one label without replacing others) already used by Validate follow-ups, which re-read and verify the label after an error.

Evidence:

- `internal/factory/featuredraft/create.go:14`
- `internal/github/issues.go:246`
- `internal/github/issues.go:391`
- `internal/turn/validate_remote.go:181`

## f7

Because create and label are two calls, 'exactly one issue with the bug label' needs a plan for a crash between them: reconcile by marker, then label the found issue. The Validate follow-up code does exactly this (find existing issue by marker, then label).

Evidence:

- `internal/turn/validate_remote.go:170`

## f8

Bug workflow docs say a Bug is an issue with the bug label and no Mode label, and say to 'label the issue bug and comment as the owner'. Whether a freshly labelled issue starts on its own without that owner comment is NOT confirmed by this pass. The issue says 'starting on its own'. This needs checking in the dispatcher before the design is fixed.

Evidence:

- `docs/workflow/bug-runtime.md:9`
- `docs/guides/workflow.md:31`

## f9

Client guidance is embedded markdown per tool plus overview text. The overview says changes are proposed, then the owner says yes in a separate message, and covers no 'file a bug' case. The tool list is built in mcp.go; action tools are shown only when the grant allows them. workflow_propose guidance already says to use action_confirm separately.

Evidence:

- `internal/connector/guidance/overview/factory.md:7`
- `internal/connector/mcp.go:202`
- `internal/connector/guidance/workflow_propose.md:3`

## f10

Related work: #1421 (open) reports optional empty fields refused by MCP, which made the workflow route fail; it is the companion Bug. #1297 brainstorm task drafts create Plan tasks, not Bugs. Both are separate from this request.

Evidence:

- `internal/connector/mcp.go:202`

## f11

Impact: touched are owneraction registry, factory/owneractions (catalog, new handler), connector (tool list, schema, guidance, overview), daemon/host wiring of the new handler, and docs/tests. No database migration is evident if the handler reuses existing owner-action tables; a dedicated durable claim for replay safety might add one (not confirmed). Callers: existing Discord/chat paths that list owner actions through the catalogue. None found superseded.

Evidence:

- `internal/factory/owneractions/catalog.go:118`
