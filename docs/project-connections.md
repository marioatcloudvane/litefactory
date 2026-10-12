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

One project selects one ticket provider. Different GitHub and Linear projects
can run together. This does not move or synchronize tickets or saved work.
GitHub continues to host the registered clone, branches and pull requests.

Configure the Linear connection through the existing operations. Use immutable
workspace, team, project, label, state and human user IDs. The destination is
`team-id/project-id`; display identifiers such as `ENG-42` are descriptive.
The API root is `https://api.linear.app`. An API key uses `api_key`; OAuth uses
`oauth`. Keep the credential in the existing external resolver. Background
LaunchAgent connections require keychain references.

```sh
litefactory repo connection set 1 ticket --revision 1 --provider linear --origin https://api.linear.app --destination TEAM_ID/PROJECT_ID --workspace WORKSPACE_ID --team TEAM_ID --project PROJECT_ID --auth-kind api_key --credential keychain:litefactory-linear:publisher --mode-labels '{"plan":"PLAN_LABEL_ID","implement":"IMPLEMENT_LABEL_ID","validate":"VALIDATE_LABEL_ID"}' --states '{"open":"OPEN_STATE_ID","completed":"COMPLETED_STATE_ID","retired":"CANCELED_STATE_ID"}'
litefactory repo connection check 1 ticket
litefactory repo approver set 1 HUMAN_USER_ID --connection-revision 2 --revision 0
litefactory repo binding add 1 --remote-id ISSUE_UUID --key ISSUE_UUID
```

Setup reads the publisher, selected workspace/team/project and mappings before
saving. Each configured mode requires its explicit label mapping; unrelated
labels survive mode changes. Completion and retirement require distinct states
with completed and canceled semantics. Unsupported modes or stale mappings
stop the affected operation. Inspection distinguishes ticket read access,
unverified write access and webhook administration availability. Insufficient
webhook administration leaves polling available.

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

Linear's [GraphQL schema](https://github.com/linear/linear/blob/master/packages/sdk/src/schema.graphql)
and [webhook documentation](https://linear.app/developers/webhooks) define the
provider operations and required webhook privilege.

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
