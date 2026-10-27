# Connector grants

Inspect and stop connector access from the machine that owns the factory:

```sh
litefactory connector grants list
litefactory connector grants inspect <grant-id> --json
litefactory connector grants revoke <grant-id>
litefactory connector grants revoke-all --json
```

Use the exact grant ID from the list. Client names and ID prefixes are not targets.
`revoke` affects every access and refresh token family under that grant and leaves
other grants unchanged. `revoke-all` takes no target and revokes all existing
grants, including grants from previous owner generations, in one transaction.
These explicit commands need no interactive confirmation.

All four commands accept `--json` and select the existing factory through
`LF_HOME` (default `~/.litefactory`). Output identifies the resolved factory home.
They open its database directly: the daemon may be stopped, remote access may be
disabled, and remote configuration and credentials may be missing or invalid.
They do not contact GitHub or another network service. A missing database is an
error; the commands never create a new empty factory to stand in for it. Local
filesystem permissions are the authority for this administration.

## Reading inspection results

List and inspect show the same safe summary for each retained grant: stable grant
and client IDs, an escaped bounded client display name, permitted scope, resource,
repository coverage, creation and absolute grant
expiry, renewable status, effective status, and recorded revocation time and reason. List includes retained
revoked and expired records, ordered by creation time and then ID. Records remain
subject to the connector's normal cleanup; this is not an indefinite audit log.
Times are UTC and output includes a snapshot time.

Coverage follows the granted scopes. `factory:overview` covers **all registered
repositories, including private repositories and future registrations**;
`factory:usage` (`coverage.factory_usage`) covers all recorded factory work,
including work without a repository.

The task-level and repository-level scopes (`task:discussion`,
`task:code-review`, `repository:usage`, `task:usage`) cover repositories, not
single tasks. JSON shows them as `coverage.repositories`: `scopes` lists which
of them the grant holds, `all` (with `future_repositories`) means all
registered repositories including ones registered later, and otherwise
`repositories` lists the covered `github:owner/name` references. Text output
prints one "Task and repository access" line with the same facts. The client
names the task or repository on each call and every call is checked against
this coverage. Code review requires both `task:discussion` and
`task:code-review`; it reads the named task's own retained code repository,
which can differ from the issue repository. `coverage.usage_task_family` marks
that task usage includes each task's linked report family, including retired
history and later persisted links; a child read includes no parent or
siblings. Refresh preserves scope and coverage; changing either requires fresh
consent. Grants made before repository coverage existed show the repository of
their former single-task or single-repository binding.

An active status means the grant passes the persisted authority and lifetime checks, not that the daemon
is running or reachable. A changed or missing owner authority invalidates access;
local inspection and revocation remain available. Family status does not establish
that a named task, listed repository or evidence source is still available: a
deleted, missing or rebound task or repository denies that call even while
independently authorized overview access can remain usable. Every evidence read
enforces the current coverage.

The renewable grant's absolute lifetime is the duration the owner chose on the
consent page: 1, 3, 6, 12 or 24 calendar months (UTC), default 1. The expiry
shown is that absolute time; renewal never moves it. The permitted scope is the
space-separated set of scopes the owner ticked; the consent page offers every
known scope, so it can include scopes the client did not request. Issued access tokens last at most
15 minutes and never outlive the grant. Inspection shows the number and
earliest/latest expiry of currently unexpired access tokens separately, or none
when absent. Tokens under revoked or invalidated grants are unusable even if
their expiry is still in the future. **Access-token expiry does not end a
renewable grant**: renewal can issue another token until the grant ends or is
revoked. Inspection never displays tokens, token digests, client secrets,
authorization codes, cookies or raw authorization records.

## Results and recovery

Successful revocation is reported only after the database confirms commit. The
very next protected request using any token under that grant is refused,
including on an already connected client; renewal is refused too. Reconnecting
or restarting the daemon does not restore access. Discussion reads recheck
authority before each page and response release.
Bytes already returned cannot be recalled. Revoking a discussion-only grant does
not revoke another overview grant. Revoking a combined grant ends all of its
permissions; inspect the scopes before choosing the target.

`revoke` reports whether it changed the selected grant or it was already revoked.
A known expired grant can still be marked revoked. `revoke-all` reports the number
newly revoked; repeating it successfully reports zero. Its boundary is the
serialized write transaction: new consent committed after that boundary is not
covered. Revocation does not prevent the owner from granting fresh consent later.

| Result | Exit code |
| --- | --- |
| Successful inspection or confirmed revocation, including an empty list or unchanged revocation | `0` |
| Storage, cancellation or output failure | `1` |
| Invalid flags or missing/extra arguments | `2` |
| Selected grant not found, missing factory database or incompatible prerequisites | `3` |

An empty text list says there are no connector grants; JSON uses an empty array.
JSON also carries the factory home and snapshot or confirmed mutation time, with
outcome and changed count for revocation. Results go to stdout; safe diagnostics
go to stderr.

After cancellation or an uncertain storage result, inspect and retry the
idempotent operation. An output failure after commit cannot undo the revocation:
a failed command does not prove access remains available. These guarantees cover
application crashes and daemon/process restarts. SQLite's current
`synchronous=NORMAL` setting does not promise to preserve the latest commit after
whole-machine power loss; restoring an old backup can also restore older state.

For a discussion-only client, connect and consent to one existing task, ask what
needs attention, request supporting evidence, then revoke that grant and confirm
a fresh evidence request is denied. The connector provides six evidence sections,
short summaries, finite continuation pages and explicit gaps; see the
[remote connector guide](../guides/remote-connector.md#discussing-a-selected-task).

For usage, consent to the intended factory, repository or task scope, ask for
recorded costs, then inspect this grant to confirm the coverage. Revoke it and
request fresh usage or a continuation: access must be denied. Already delivered
answers remain with the client. Usage defaults to rolling 30 days and shows exact
server-resolved UTC bounds, historical price basis and missing evidence; see
[usage discussion](../guides/remote-connector.md#discussing-usage-and-historical-costs).

For code and standards, consent to both required scopes, inspect the retained
code target, ask about a committed file and a published rule, compare selected
category and whole-book role counts, and follow source references. Revoke the
grant and confirm a fresh read or continuation is denied. See the
[code and standards guide](../guides/remote-connector.md#discussing-committed-code-and-published-standards)
for illustrative conversations and evidence limits.

See the [command reference](command-reference.md#connector-grants) or
[CLI index](README.md).
