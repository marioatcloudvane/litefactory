# Installed workflow adapters

Workflow definitions are server-owned registrations. A remote client selects a
stable reference and exact version; it cannot install an adapter, supply commands
or select a different implementation. The definition digest freezes the contract
reviewed for a proposal. Keep old versions installed while their retained jobs
need them; a missing or changed implementation must fail closed.

A definition separates ownership from execution. Declare its binding, project
selection mode, input/continuation/result schemas, examples, required permissions,
impact classification and execution deadlines. Register only adapters whose
existing action policy has classified their effects. Discovery does not grant
execution authority.

Daemon composition supplies the implementation map and an explicit `AdapterPolicy`
for each additional adapter. The policy selects an existing analysis or author
action classification; registrations cannot override either built-in policy.
The declared required permissions must include that classification’s fixed scope.
An unclassified implementation is refused at construction, before discovery.

| Project mode | Contract |
| --- | --- |
| `required` | Caller selects a registered project explicitly. Generic log analysis uses this mode. |
| `optional` | Caller selects project context or explicitly uses the independent book-only mode. Standards authoring uses this mode. |
| `fixed` | Server registration supplies the exact project; callers supply no project, including inside inputs. |
| `none` | Independent operation; caller project input is rejected. |

For example, two installed definitions can both be displayed as “Review recent
failures” while retaining different references: `release-health` version `1`,
bound to `github:example/releases`, accepts an absolute period and dry-run choice;
`test-health` version `1`, bound to `github:example/tests`, accepts a suite name
and bounded sample count. Each fixed definition exposes its authorized binding,
omits selectable project input and rejects an override to the other project.
Their labels never determine identity, permissions or execution target. These are
registration design examples, not permission to run arbitrary custom research.

An adapter validates typed inputs, resolves exact domain targets, rechecks current
authority, executes one bounded attempt, recovers existing evidence and reads
retained results. Continuation is explicitly declared. Execution receives the
frozen definition and targets, authenticated capability, stable operation identity
and cancellation context. Recovery reads receipts; it never creates effects to
find out whether an earlier effect happened. Domain sessions and their revisions
remain authoritative; a workflow job only links and projects them.

Use the same lifecycle for all installed definitions:

1. Discover with `workflows_list` and inspect the exact `workflow_describe` version.
2. Validate and freeze inputs with `workflow_propose`; launch nothing yet.
3. Obtain separate existing `action_confirm` confirmation, preserving its timing,
   origin, expiry and impact rules.
4. Return queued admission promptly and execute through the single durable worker.
5. Save results or exact questions and session revision under the same job.
6. Propose declared continuation with `workflow_continue_propose`, then require
   fresh confirmation before the next attempt.

Authorization covers both definition binding and execution targets. Independent
standards authoring requires explicit `standards:management`; project context also
requires the current read coverage for that project. Log analysis requires
`repository:actions` and explicit finite selection of every repository involved.
Recheck at proposal, confirmation, dequeue and effects. Revocation also removes
job read access. Never infer a project from defaults or a broad read grant.

Respect advertised deadlines. Existing domain ceilings are 1,800 seconds for log
analysis plus its bounded 120-second filing phase, 120 seconds for book-only
authoring, and 15 minutes for project-context authoring, with bounded cleanup.
Cancellation and worker fencing must prevent late output from changing a settled
attempt. One attempt runs at a time; discovery, status and confirmation remain
responsive. There is no running-job cancellation or automatic retry of uncertain
effects in this release.

Before sending an external effect, retain its exact claim. A confirmed issue link
is evidence; an absent GitHub marker match is not proof of non-creation. Preserve
partial findings and links and mark unresolved effects uncertain. GitHub issue
creation has no promised idempotency key. Discord nonce deduplication is limited;
an ambiguous send must remain delivery-unconfirmed rather than being blindly
resent. Rate-limit refusals can retry according to the confirmed response.
Terminal and question notices have durable local deduplication. Notification
failure never changes the saved job result.

Keep raw logs, captured private prompts, secrets and host paths out of remote
result projections. Bound and redact before paging; bind cursors to the exact
owner, job, revision and authorization. Retain requests, confirmation evidence, attempt links and result snapshots
together so reconnects and restarts preserve identity.

An item too large for one response is retained as ordered `json_fragment` items.
Each carries its original zero-based `item_index`, one-based `part`, total `parts`
and `content`. Concatenate `content` in part order, then decode that JSON to recover
the complete sanitized item. Continue through `next_cursor` until it is absent.
Only results exceeding the total retained ceiling are explicitly truncated and
reported as uncertain; the authoritative domain record remains linked.

The same owner-scoped request key and canonical inputs return the
original operation across clients; changed inputs conflict. Only the originating
client and grant can confirm the original proposal.

Verify adapters with deterministic local barriers and receipts, rather than live
production effects. Exercise restart before dispatch, restart after dispatch,
positive and absent receipts, overlapping answers, revoked grants, deadline
expiry, late output, and unavailable Discord. The main client journey should
prove pagination, both built-ins, confirmed filing, dry-run, stable author
questions and separately confirmed continuation. Live Claude/ChatGPT and Discord
checks require a configured test installation and owner consent; local fixtures
do not establish hosted behavior.

## Configuration examples

The daemon always registers the two built-in contracts. Additional registrations
under `remote_connector.workflows` select an already installed classified adapter;
they do not replace built-ins or admit executable commands. Version `1` is
supported. An empty list adds nothing. The following illustrative registrations
show independent, project-context and two distinct fixed-project definitions:

```yaml
remote_connector:
  workflows:
    - ref: book-guidance
      version: "1"
      adapter: standards-authoring
      name: Draft book guidance
      project_mode: none
    - ref: project-guidance
      version: "1"
      adapter: standards-authoring
      name: Draft with project context
      binding_project: github:example/releases
      project_mode: required
    - ref: release-health
      version: "1"
      adapter: log-analysis
      name: Review recent failures
      binding_project: github:example/releases
      project_mode: fixed
      fixed_project: github:example/releases
    - ref: test-guidance
      version: "1"
      adapter: standards-authoring
      name: Review recent failures
      binding_project: github:example/tests
      project_mode: fixed
      fixed_project: github:example/tests
  workflow_notices: []
```

The two fixed registrations have different contracts: release-health requires
`after`, `before` and `dry_run`; test-guidance accepts authoring `intent` and
optional category, target and mode. Category filters name published categories;
edit mode requires an existing standard target and create mode omits that target. Neither accepts a selected project. Installed
Go adapters can provide additional distinct schemas; YAML registrations only
reuse a classified built-in contract. The earlier suite/sample example describes
that extension contract, rather than a YAML customization capability.

For optional Discord notices, replace the empty list with explicit associations:

```yaml
  workflow_notices:
    - project: github:example/releases
      channel_id: "123456789012345678"
    - project: ""
      channel_id: "234567890123456789"
```

An empty project names the explicit owner/factory destination for independent
jobs. The destination must also be in the factory's authorized Discord channels.
Missing Discord or an absent association records delivery as unavailable and
keeps the job usable. Configuration validation checks registration shape; normal
proposal/confirmation/effect authorization still applies to registered projects.
Validate example configuration against a local test installation before serving;
no hosted credentials are needed to check shape and safe absent defaults.

The advertised total workflow budgets are 1,980 seconds for log analysis and
960 seconds for standards authoring, including bounded persistence cleanup.
Domain calls retain their tighter 1,800+120-second and 120/900-second ceilings.

A pre-existing agent-guidance standards violation remains in the Go author prompt
builder (`internal/standards/authoring.go`, around line 731): behavioral author instructions are embedded in production Go. This story
keeps that existing behavior compatible; operator disposition is still required.
New shared workflow behavioral guidance lives in embedded Markdown resources.
