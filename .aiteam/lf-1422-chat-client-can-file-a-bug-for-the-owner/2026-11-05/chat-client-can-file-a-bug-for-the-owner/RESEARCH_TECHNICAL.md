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

Confirmed in shaping (observed): a Bug filed by the factory does not start on its own today. The Bug docs say to label the issue bug and comment as the owner; log-analysis and Validate follow-up filing only create and label the issue and start no run. Starting on its own is therefore new behaviour this work must add.

Evidence:

- `docs/workflow/bug-runtime.md:9`
- `docs/guides/workflow.md:30`
- `internal/loganalysis/trackers.go:88`
- `internal/turn/validate_remote.go:179`

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

Impact: touched are owneraction registry, factory/owneractions (catalog, new handler), connector (tool list, schema, guidance, overview), daemon/host wiring of the new handler and of the automatic Bug start (shaping addition, see f12), remote-connector and bug-runtime docs, and tests. No database migration is evident if the handler reuses existing owner-action tables; a dedicated durable row for replay safety and the start might add one (not confirmed). Depends on it: chat clients through the connector, and Discord/chat paths that list owner actions through the catalogue. Superseded: none found (looked at connector tools, owner-action catalogue, log analysis and Validate follow-up filing).

Evidence:

- `internal/factory/owneractions/catalog.go:118`
- `internal/daemon/brainstorm_launch.go:28`

## f12

Precedent for starting work without a posted owner comment (observed): after brainstorm tasks are created, the daemon starts Plan on each new issue with a private owner-authored trigger that is never posted on the forge, one launch attempt per item, and reports 'created but not started' after a bounded wait.

Evidence:

- `internal/daemon/brainstorm_launch.go:19`
- `internal/daemon/brainstorm_launch.go:24`
- `internal/daemon/brainstorm_launch.go:262`

## f13

Existing Bug filing (log analysis) is GitHub only: it looks for an issue with the marker, creates one if absent, then adds the bug label. Log-analysis filing on Linear is refused, and Validate follow-ups on Linear get no bug label. The Bug docs list Linear filing and label writing as outside the earlier Bug change. Linear is now in scope for this tool (see O-08, A-01).

Evidence:

- `internal/loganalysis/trackers.go:88`
- `internal/loganalysis/trackers.go:115`
- `internal/turn/validate_remote.go:186`
- `docs/workflow/bug-runtime.md:17`

## f14

action_confirm guidance says admission and background work are not proof of external success and the client reads recorded state (action_inspect) before describing the result. So the URL may only be known after the confirm call returns.

Evidence:

- `internal/connector/guidance/action_confirm.md:7`

## f15

Bug rules that apply unchanged: no labels are added to unlabelled Bugs (agent chosen by bug_agent or default), analysis is read-only, and repair needs the exact /approve-bug command.

Evidence:

- `docs/workflow/bug-runtime.md:15`
- `docs/workflow/bug-runtime.md:21`

## f16

Linear can create an issue idempotently (observed): the adapter refuses creation without a durable creation UUID and creates the issue with that UUID as its Linear id, in the connection's own team and project, then checks the returned id and scope. A replay with the same UUID can therefore find the same issue instead of making a second one.

Evidence:

- `internal/linear/tickets.go:457`
- `internal/linear/tickets.go:467`

## f17

Linear labels are written only through explicit mappings (observed): a Linear connection stores label IDs for the five modes (research, plan, implement, validate, spike), and the adapter adds or removes only a mapped mode label, keeping other labels. There is no bug mapping today. The stored mapping accepts other keys (up to 32), so a bug entry may fit without a schema change (inferred, not confirmed).

Evidence:

- `internal/linear/tickets.go:611`
- `internal/linear/tickets.go:684`
- `internal/store/linear_connections.go:22`
- `docs/project-connections.md:250`

## f18

Bug recognition is by label name (observed): a task with no mode label and no mode command becomes a Bug when its labels contain exactly 'bug'. For a Linear task, labels are read by name, and only mapped mode labels are renamed. So a Linear label named 'Bug' (capital B) is not recognised as a Bug today; only one named exactly 'bug' would be (inferred from the code, not checked live).

Evidence:

- `internal/daemon/workflow.go:42`
- `internal/daemon/ticket_reads.go:26`
- `internal/linear/tickets.go:110`

## f19

Impact of adding Linear: the Linear adapter (label write, label recognition), the Linear connection settings and their setup docs, the connector check of which repositories can file, and the automatic start, which must also work for a Linear task. Whether the brainstorm-style start already works for Linear tasks was not checked in this pass.

Evidence:

- `internal/projectconnection/linear.go:52`
- `internal/daemon/brainstorm_launch.go:262`
