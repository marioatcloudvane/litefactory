# Project connections and ticket bindings

A registered repository is the project identity. `repo add` still reads the
local clone; project connections independently select the code repository and
the ticket destination. Keep credentials in the existing keychain or environment
resolver. Connection records contain credential references, never token values.

Use the project ID reported by repository management. Inspect the saved versions
before editing:

```sh
litefactory repo connection list 1
litefactory repo connection list 1 ticket
litefactory repo binding list 1
```

Set each role separately. Supply the current revision; use zero only when that
role has no saved configuration. A stale revision fails instead of replacing a
concurrent change. The code destination must match the registered clone.
Registration seeds missing legacy defaults; the example below replaces those
initial revision-one defaults and returns revision two for each role.

```sh
litefactory repo connection set 1 code --revision 1 --origin https://api.github.com --destination acme/code --credential env:LF_CODE_TOKEN
litefactory repo connection set 1 ticket --revision 1 --origin https://api.github.com --destination acme/tasks --credential env:LF_TICKET_TOKEN
litefactory repo binding add 1 --remote-id 123456789 --number 42 --container acme/tasks
litefactory repo connection check 1 code
litefactory repo connection check 1 ticket
```

The ticket's remote ID is its immutable GitHub issue ID, not its display number.
Bindings qualify that identity with the provider and API origin. Equal issue
numbers in different repositories or instances do not transfer saved work.
An equivalent binding add is repeatable. Conflicting ownership fails. Correct
an unused binding or disable future admission with its current revision:

```sh
litefactory repo binding correct 1 --binding 1 --revision 1 --remote-id 123456789 --number 43 --container acme/tasks
litefactory repo binding disable 1 --binding 1 --revision 2
litefactory repo connection disable 1 ticket --revision 2
```

Used bindings retain their historical project ownership. Disabling retains a
tombstone and existing session links. Re-registering a clone does not overwrite
explicit connection settings. Legacy initialization seeds missing GitHub
defaults and retains conflicts for correction; ambiguous historical evidence
blocks affected dispatch rather than guessing a destination.

Health checks use bounded reads. Inspection shows the version checked, check
time, observed capabilities, status and a safe remedy. Authentication and read
access do not establish push or ticket write access. Missing or locked
credentials, rejected credentials, inaccessible destinations, rate limits and
conflicts require correction or another check; raw remote errors and secrets
are not health reports. A check that races a configuration edit cannot mark the
replacement version healthy.

The default check deadline is 30 seconds for credential resolution and provider
reads together. A code check reads the authenticated actor and repository
metadata. A ticket check also reads at most one open ticket to establish ticket
visibility. No health check creates a ticket or pushes a branch. Successful
output has this shape, with the actual UTC check time:

```text
ticket version 1 checked 2026-10-11T13:00:00Z: usable_for_checked_capabilities (read_access_verified); capabilities [authenticated_actor repository_read ticket_read write_unknown]; remedy Write access is unverified; ordinary operations still enforce permissions.
```

For example, `missing_credential (credential_unavailable)` asks you to correct
the external credential reference. `locked_credential (keychain_locked)` asks
you to unlock the login keychain. `inaccessible_or_missing_destination
(destination_unavailable)` asks you to verify the destination and the existing
actor's read access; it cannot establish whether a private repository exists.
`rate_limited` asks you to wait and check again. Inspection reports the latest
observation for the displayed version, or `unchecked` when none exists.

Edits and disables govern future admission. Saved sessions retain their ticket
binding, connection versions, code target and publishing actor across restarts.
Managed child work and pending publications use those retained destinations.
Credential rotation at the original reference must resolve to the same actor.
If the original clone, destination or actor is unavailable or has changed, saved
work blocks; replacing current configuration does not redirect it. Publication
receipts and prepared delivery contents retain their existing recovery rules.
Local doctor operations also load retained project credentials when the former
global credential is unavailable, including after current connections are
disabled. Each remote operation still selects the original session destination.

## Ticket approver

The trusted local operator appoints one verified human account per project.
The account may differ from the code owner, ticket creator and assignee. Handles
select a GitHub account for verification; Linear uses an explicit immutable user ID. The saved provider, account ID and configured API origin
establish its identity. The publishing account cannot be appointed.

```sh
litefactory repo approver inspect 1
litefactory repo approver set 1 ticket-director --connection-revision 2 --revision 0
litefactory repo approver refresh 1 --connection-revision 2 --revision 1
litefactory repo approver replace 1 new-director --connection-revision 2 --revision 1
```

Inspect reports verification provenance, activation time, current revisions and
outstanding work. Refresh looks up the same immutable account and preserves its
authority revision and replay boundary. A stale revision or failed lookup leaves
the current appointment and pending effects intact.

Replacement requires an idle project: runs, queued work, open decisions, claimed
replies and unresolved publications must settle through their existing controls.
Replacement preserves historical actors and consumed sources. The replacement
must write a new reply strictly after activation; editing an earlier reply does
not authorize it. Missing or equal creation time requires a fresh reply.

New projects require an explicit appointment. Existing projects can retain the
migration-captured authenticated GitHub approver after verification against the
known public GitHub origin. Ambiguous identity or conflicting historical actors
block affected work and remain visible for correction. Initialization never
replaces an explicit appointment or changes prepared publication bytes.

Only the appointed approver's fresh ticket instructions and workflow approvals
authorize ticket work. A third-party issue body remains context until that
approver instructs work. Assignment cannot grant or suppress authority. Code
owner delivery and exact pull request/head/target approvals retain their separate
permissions, as do trusted local operations and the publishing credential.

Remote Doctor diagnosis and stop instructions use the project approver and the
same fresh-reply boundary. Doctor repair approval keeps its separate code
authority. Queued Doctor instructions retain their original ticket connection
and publisher, including when a result must be reconciled after a restart.

When the ticket approver and code owner differ, acceptance checks and finding
choices must be settled before the code owner's combined Validate shipping
approval. The combined command cannot grant the code owner ticket authority.

## Linear tickets with GitHub code

[Getting started](getting-started/README.md) · [GitHub setup](getting-started/github-setup.md) · [Discord setup](discord.md)

One registered project selects one ticket provider. GitHub still hosts code,
branches and pull requests; Linear holds tickets. This does not migrate saved
work. Follow these steps before creating your first Linear instruction.
Uppercase values below are placeholders, not real IDs.

### 1. Register code and choose separate Linear identities

Complete [GitHub setup steps 1–4](getting-started/github-setup.md#step-1-log-gh-in-as-the-owner)
for installation, `init`, the GitHub publisher and a registered clone. If this
project will use Linear, defer the GitHub ticket-approver appointment in step 4
until step 4 below; code ownership remains on GitHub. `repo list` gives the
local numeric `PROJECT_ID`, which is different from Linear's project UUID.

Invite a separate Linear member to the workspace as the factory publisher.
Give it access to the intended team/project and permission to write issues,
comments, labels and states. For webhook setup, use a workspace admin member;
[Linear requires admin access to create/read webhooks](https://linear.app/developers/webhooks).
The human owner's Linear member will be the ticket approver. Using that human's
API key as the publisher fails with `ticket approver must differ from the
publishing account`.

As the publisher member, create an API key in Linear's Security & access
settings. In Terminal.app or an SSH shell able to unlock your login Keychain,
as the daemon's macOS user, paste it at `read` and press Return:

```sh
security unlock-keychain ~/Library/Keychains/login.keychain-db
read -rs T; security add-generic-password -s litefactory-linear -a publisher -w "$T"; unset T
```

The chosen service/account become `keychain:litefactory-linear:publisher`.
Do not enable shell tracing or put the key in configuration, a ticket or a
report. Use `add-generic-password -U` with the same reference when replacing an
existing item. Saved work requires the original publishing identity even after
credential rotation.

### 2. Create labels and discover immutable IDs

In the issue team's label settings, create five team labels named `research`,
`plan`, `implement`, `validate`, and `spike`. Use the team corresponding to the
issue key prefix (for example `ENG`), not a similarly named label on another
team. Make sure the Linear project includes that team.

Use a local GraphQL client against `https://api.linear.app/graphql`, with header
`Authorization: <publisher API key>` (no `Bearer` prefix for an API key) and
`Content-Type: application/json`. Keep the header private. Run this one read-only
discovery query; it returns the publisher, human candidates and mapping IDs:

```graphql
query SetupIDs {
  viewer { id name }
  organization { id name }
  users(first: 50) {
    nodes { id name email }
    pageInfo { hasNextPage endCursor }
  }
  teams(first: 20) {
    nodes {
      id name key
      projects(first: 20) {
        nodes { id name }
        pageInfo { hasNextPage endCursor }
      }
      states(first: 30) {
        nodes { id name type }
        pageInfo { hasNextPage endCursor }
      }
      labels(first: 30) {
        nodes { id name team { id } }
        pageInfo { hasNextPage endCursor }
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}
```

Check the response's `errors` even on HTTP 200. This is a first page, not an
unbounded export: for any connection with `hasNextPage: true`, repeat that
field with `after: "<its endCursor>"`; nested cursors belong to their individual
team/field. Narrow subsequent reads to `team(id: "<team UUID>")` for nested
pages. Reduce page sizes if query complexity is refused. The
[GraphQL guide](https://linear.app/developers/graphql) explains authentication
and the [schema](https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql)
defines these fields.

| Placeholder | Copy from the response |
| --- | --- |
| `WORKSPACE_ID` | `organization.id` for the intended workspace. |
| `TEAM_ID` | The issue team's `id`; verify its `key`. |
| `LINEAR_PROJECT_ID` | That team's intended `projects.nodes[].id`. |
| `HUMAN_USER_ID` | `users.nodes[].id`, matching the human's name/email; it must differ from `viewer.id`. |
| `RESEARCH_LABEL_ID`, `PLAN_LABEL_ID`, `IMPLEMENT_LABEL_ID`, `VALIDATE_LABEL_ID`, `SPIKE_LABEL_ID` | The five labels' `id` values; verify each `team.id` equals `TEAM_ID`. |
| `OPEN_STATE_ID` | An active team state with an open type, such as `backlog`, `unstarted` (Todo) or `started` (In Progress). |
| `COMPLETED_STATE_ID`, `CANCELED_STATE_ID` | Distinct team states with types `completed` and `canceled`, respectively. |

State **types**, not display names or the single `open` mapping, determine
openness. Backlog, Todo and In Progress can all be open; completed/canceled
states are closed. `retired` maps to the canceled state.

### 3. Save and check the ticket connection

Inspect before editing:

```sh
litefactory repo list
litefactory repo connection list PROJECT_ID ticket
```

Replace `CURRENT_REVISION` below with the current saved ticket revision (the `version` number, not the internal `id`; often
1 after registration; use 0 only if missing). Substitute the IDs from step 2
inside the JSON and destination. The API origin omits `/graphql`:

```sh
litefactory repo connection set PROJECT_ID ticket --revision CURRENT_REVISION --provider linear --origin https://api.linear.app --destination TEAM_ID/LINEAR_PROJECT_ID --workspace WORKSPACE_ID --team TEAM_ID --project LINEAR_PROJECT_ID --auth-kind api_key --credential keychain:litefactory-linear:publisher --mode-labels '{"research":"RESEARCH_LABEL_ID","plan":"PLAN_LABEL_ID","implement":"IMPLEMENT_LABEL_ID","validate":"VALIDATE_LABEL_ID","spike":"SPIKE_LABEL_ID"}' --states '{"open":"OPEN_STATE_ID","completed":"COMPLETED_STATE_ID","retired":"CANCELED_STATE_ID"}'
litefactory repo connection check PROJECT_ID ticket
litefactory repo connection list PROJECT_ID ticket
```

These are all required Linear connection settings; the optional `--remote-id`
repository hint is not needed. Copy the resulting ticket revision as
`CONNECTION_REVISION`. Expect `usable_for_checked_capabilities`; check output
also distinguishes read access, unknown write access and webhook administration.
A read check does not prove writes. Every mode used, including inferred modes,
needs its label mapping; map additional supported modes before using them.

### 4. Appoint and verify the human

```sh
litefactory repo approver inspect PROJECT_ID
litefactory repo approver set PROJECT_ID HUMAN_USER_ID --connection-revision CONNECTION_REVISION --revision 0
litefactory repo approver inspect PROJECT_ID
```

Use `set` only when there is no appointment. If the same Linear human is already
appointed, take `APPROVER_REVISION` from inspection and refresh:

```sh
litefactory repo approver refresh PROJECT_ID --connection-revision CONNECTION_REVISION --revision APPROVER_REVISION
```

An existing GitHub appointment cannot be refreshed into a Linear identity.
For an idle project switching providers, use `repo approver replace PROJECT_ID
HUMAN_USER_ID --connection-revision CONNECTION_REVISION --revision
APPROVER_REVISION`; outstanding work must settle first. Inspect again: the
provider, human UUID and connection must agree, with no authority blockers.

### 5. Start the listener and register a webhook

Configure `webhook.listen: "127.0.0.1:8787"` and
`webhook.public_url: "https://factory.example.com/litefactory/webhook"` under the
`webhook` block in `$LF_HOME/config.yaml` (default `~/.litefactory/config.yaml`).
Use your public HTTPS host. Follow the [nginx setup](getting-started/github-setup.md#reaching-the-listener)
so the original path, query, raw body and signature headers survive. Then:

```sh
litefactory daemon stop
litefactory daemon start
litefactory daemon status
litefactory repo webhook create PROJECT_ID --expected-revision 0
litefactory repo webhook status PROJECT_ID
```

`create` is for a connection version without an owned subscription. If one
already exists, inspect it and use `repair` with its current revision. Expect a
bound listener and an enabled, observed subscription for the correct team,
with a remote hook ID. Linear subscriptions carry Issue and Comment events;
a valid event still requires an authoritative read in the selected project.
Check last valid delivery after the test in step 7. Polling remains the fallback
when webhooks are unavailable; insufficient admin access does not prove the
connection itself is unusable.

### 6. Link a compatible installed team

A Linear team groups tickets; an installed litefactory team supplies workflows.
Both are needed. Choose `TEAM_NAME` from the catalogue in `settings.team_source`;
install it, and copy the exact `TEAM_VERSION` from `team list`:

```sh
litefactory team install TEAM_NAME
litefactory team list
litefactory team inspect TEAM_NAME TEAM_VERSION
litefactory team link TEAM_NAME TEAM_VERSION --repo /absolute/path/to/project
```

Use the clone path registered in step 1. Expect `Linked` or `Already linked`.
Check that its stack profile fits the repository and installed tools. The
[GitHub team instructions](getting-started/github-setup.md#step-4-register-your-repository)
explain the permitted `.litefactory/stack.overrides.json` fields; an override
cannot change host requirements or permission policy.

### 7. Send the first instruction and inspect the result

As the **human approver**, create an open issue in the connected Linear
team/project after setup. Apply the mapped `research` label, or let the factory
infer a mode from your instruction. Use the [first Research request](getting-started/README.md#request-your-first-research-run)
as the body of a new issue with no comments. The body must satisfy appointment
and enrollment boundaries. Alternatively, wait for first sight in the daemon
logs, then post it as a fresh approver comment. Historical comments are skipped;
enrolled comments at or after enrollment are retained even before first sight.
Creating the issue as the publishing member does not authorize its body.

Check the bot's run report and any Research question on the Linear issue,
`litefactory daemon status`, and `repo webhook status PROJECT_ID`. A delivery or
queued instruction alone is not a completed run. No manual ticket binding is
needed for an ordinary issue discovered in the connected project.

### 8. Maintain connection versions and quotas

Every connection edit saves a new version. Before an edit, record the old
subscription ID and revision with `repo webhook list PROJECT_ID` and
`repo webhook status PROJECT_ID OLD_SUBSCRIPTION_ID`. After editing, run the
connection check, inspect and refresh the same approver against the new
connection revision, then move the webhook:

```sh
litefactory repo webhook create PROJECT_ID --expected-revision 0
litefactory repo webhook status PROJECT_ID
litefactory repo webhook status PROJECT_ID OLD_SUBSCRIPTION_ID
litefactory repo webhook disable PROJECT_ID OLD_SUBSCRIPTION_ID --expected-revision OLD_SUBSCRIPTION_REVISION
```

Create and verify the new-version hook before disabling the old one. Take the
old subscription's current revision from its status, not the connection revision.
Saved sessions retain their original connection and publisher; keep the original
access available for owed effects. Editing configuration does not redirect them.

API-key quota exhaustion delays reads, dispatch and reporting. Although often
called a per-key limit, [Linear's current rate-limit documentation](https://linear.app/developers/rate-limiting)
says keys for the same authenticated user share quota. A second key for that
user does not provide a fresh allowance. The adapter recognizes HTTP `429` and
GraphQL `RATELIMITED`; inspection may show `rate_limited`. Respect reset/backoff,
reduce competing requests and let later polls retry retained work. A pending
report or failed check is not completion. No fixed quota size is assumed here.

### Troubleshooting

| Symptom | Action |
| --- | --- |
| `ticket approver must differ from the publishing account` | Use the separate publisher member's API key; appoint the human UUID. |
| `User interaction is not allowed` / `locked_credential (keychain_locked)` | Unlock the daemon user's login Keychain interactively and retry. |
| Mapping rejected or inferred mode blocked | Check all five label IDs, their team ownership and distinct state types. |
| Webhook administration unavailable | Give the publisher workspace admin access; polling remains available. |
| Stale revision | Inspect the current connection, approver or subscription revision; do not interchange them. |
| No run | Check open state, project/team, human author, fresh instruction, linked installed team and stack requirements. |
| `rate_limited`, `429`, `RATELIMITED` | Wait for reset/backoff and later polling; check the final report before assuming success. |

### Retained work and delivery semantics

Linear's immutable IDs remain the stored identity and mutation destination.
Durable local slots allow existing workflow ledgers and task naming to address
opaque IDs internally; a slot is never a remote GitHub issue or comment ID.
Saved sessions retain their original connection, team/project, publisher and
qualified approver evidence. Connection edits govern new admission. Original
access must remain available for owed effects; configuration replacement cannot
redirect them.

Factory metadata uses visible versioned fenced blocks on Linear. The adapter
retains provider bytes separately and preserves unrelated text. Guarded reads
and readback detect divergence but do not provide remote atomic compare-and-swap.
Detected conflicts retain owed work. Issue and comment creation retain a UUID
before dispatch and reconcile that exact object after uncertain responses;
UUID correlation does not grant permission to resend.

Subscriptions are factory-owned and team-scoped for Issue and Comment events.
Incoming hints require a valid raw-body HMAC and recent signed timestamp, then
an authoritative issue read checks workspace, team and project. A valid hint
alone grants no work authority. Independent issue and comment streams, including
thread replies, hand off bounded pages durably before coverage advances. Cursor
failure restarts from completed coverage; overlap and paced reconciliation
recover provider-retained changes. Disabling webhooks leaves polling active.

## GitHub code delivery for Linear work

Admission retains the verified GitHub code owner, publisher and original code
repository alongside the Linear session. Later configuration edits cannot replace
that saved code authority. The retained GitHub code owner requests delivery
independently of the Linear approver. Name the saved parent task and its approved generation, and provide an
external credential reference for the human GitHub owner:

```sh
litefactory repo delivery request github:acme/code#42 --generation 1 --request-id release-42-generation-1 --credential keychain:litefactory-github:code-owner
```

Use the saved parent task shown by the factory, including its durable local slot
for a Linear parent. The command verifies the human GitHub identity and original
repository, then retains the request for daemon processing. Reuse the same
request ID after interruption; a matching request is returned without another
control effect. The publishing credential cannot stand in for the human owner.

After the proposal is published, the owner posts the exact approval command on
the bound GitHub pull request:

```text
/merge <generation> <proposal-digest> <PR-number> <head-SHA> main
```

The daemon recovers fresh comments on that enrolled PR through the saved code
connection. The proposal, PR identity, repository, branch, head and main target
must still match. Changed heads require a new proposal and approval. Linear
comments and GitHub review approvals cannot authorize merging. Confirmed
integration precedes Linear parent closure; interrupted Linear reporting or
closure remains pending without another merge.
