# Owner-only remote connector

The remote connector is a separate owner-authorized boundary. Reads stay read-only; changing actions use explicit consent and shared confirmation. It is disabled by default,
binds only to a numeric loopback address, and requires a dedicated HTTPS reverse
proxy. It never exposes attempt tools or worker submission authority. Only enabled existing owner actions are available through proposals.

This foundation supplies OAuth, durable grants and the MCP transport. Remote
serving stays closed until **Local control of connector grants (#1187)** registers
a working local command host with trusted daemon composition. Setting
`enabled: true` cannot bypass that gate. **Factory overview in Claude and ChatGPT
(#1188)** supplies the overview projection and verifies both hosted clients.
Without an authorized installed projection, authenticated `tools/list` returns an empty list. These
instructions do not establish hosted-client compatibility or deploy a proxy.

## Prerequisites

Use the persisted installation owner's positive numeric GitHub account ID, not a
handle or a repository-specific approver. Create a dedicated GitHub OAuth app
with callback `https://cloudvane.at/litefactory/oauth/github/callback`. The app
requests no repository permissions; its upstream token is used only to fetch a
fresh `/user` identity and is discarded. The numeric result must match both the
configured pin and the current persisted installation owner.

Store the OAuth app secret and a separate random **32-byte** cookie encryption
key in the keychain. Store the cookie key as standard base64. Configuration holds
references only. Environment references are supported for foreground development;
launchd requires keychain references. Never reuse the bot token or webhook
signing secret. Secret resolution failures keep the listener closed.

```yaml
remote_connector:
  enabled: true
  listen: "127.0.0.1:8788"
  resource_url: "https://cloudvane.at/litefactory/mcp"
  issuer_url: "https://cloudvane.at/litefactory"
  owner_id: 123456 # replace with the installation owner's numeric ID
  github_client_id: "your-dedicated-oauth-app-client-id"
  github_secret:
    keychain: {service: litefactory-connector, account: github-app}
  cookie_secret:
    keychain: {service: litefactory-connector, account: browser-key}
  allowed_origins:
    - "https://cloudvane.at" # required for the consent page's browser POST
    # Add exact HTTPS client origins only when the client sends Origin.
```

The issuer and resource must share an exact HTTPS origin and retain the shown
paths. Other HTTPS hosts and valid ports are permitted with matching proxy,
OAuth callback and origin configuration. URLs cannot contain credentials,
queries, fragments or encoded paths. The listener cannot bind a public interface
or resolve a hostname. Unknown configuration keys remain errors. Restart the
daemon after configuration changes. A changed owner identity or conflicting
configured pin irreversibly invalidates existing grants and pending browser
transactions; changing back does not revive them.

## Fixed nginx routes

Configure TLS on the existing HTTPS virtual host. Discovery lives **outside**
`/litefactory`; routing only the application prefix prevents client discovery.
Keep the existing webhook and chat routes independent, with their existing
budgets. Expose only these exact connector locations:

```nginx
# In http {}, once:
limit_req_zone $binary_remote_addr zone=connector_source:10m rate=2r/s;
limit_req_zone $server_name zone=connector_global:1m rate=10r/s;

# Install this connector-only snippet as connector-proxy.conf. Replace the
# fixed Host if your configured public origin differs. Do not include it in
# the webhook or chat location.
proxy_pass http://127.0.0.1:8788;
proxy_set_header Host cloudvane.at;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For "";
proxy_set_header Forwarded "";
proxy_set_header X-Forwarded-Host "";
proxy_set_header X-Forwarded-Proto https;
proxy_set_header Connection "";
proxy_http_version 1.1;
proxy_buffering off;
proxy_request_buffering off;
proxy_read_timeout 30s;
proxy_send_timeout 30s;
client_max_body_size 64k;
limit_req zone=connector_source burst=10 nodelay;
limit_req zone=connector_global burst=30 nodelay;
limit_req_status 429;
# Neither URI queries nor response headers may reach a log. OAuth codes and
# state occur in browser URLs; generic upstream error logging can include them.
access_log off;
error_log /dev/null crit;
error_page 429 = @connector_overload;

# In the TLS server {}, use the same snippet for each exact location:
location = /.well-known/oauth-protected-resource/litefactory/mcp { include connector-proxy.conf; }
location = /.well-known/oauth-authorization-server/litefactory { include connector-proxy.conf; }
location = /litefactory/oauth/register { include connector-proxy.conf; }
location = /litefactory/oauth/authorize { include connector-proxy.conf; }
location = /litefactory/oauth/github/callback { include connector-proxy.conf; }
location = /litefactory/oauth/consent { include connector-proxy.conf; }
location = /litefactory/oauth/token { include connector-proxy.conf; }
location = /litefactory/oauth/revoke { include connector-proxy.conf; }
location = /litefactory/mcp { include connector-proxy.conf; }
location @connector_overload {
  access_log off;
  add_header Retry-After 1 always;
  return 429;
}
```

The snippet directives belong in the included file, not directly at `http` scope.
Use a dedicated TLS host if other virtual-host logging captures connector URLs.
Disable header/body capture in any outer load balancer or observability agent.
Only the local proxy may reach the backend; the application requires loopback
peers, validates the fixed Host and trusts only the overwritten `X-Real-IP` header.
It never constructs public URLs from incoming Host or forwarded headers. A
present Origin must exactly match the configured allowlist; absent Origin is
allowed for server-to-server calls. There is no credentialed wildcard CORS.

## Authorization and revocation

Clients register up to ten exact HTTPS redirects and a bounded display name. The
name is client supplied, not a verified vendor identity. Registration supports
`none`, `client_secret_basic` and `client_secret_post`, with no downgrade between
methods. Metadata URL fetching is unsupported. S256 PKCE is mandatory.

Each new grant requires fresh GitHub verification and an explicit Allow action.
The escaped consent page shows, in separate cards: who is asking (the client
name, marked as client supplied and unverified, the client ID and the redirect
host); what it can access (the exact resource, one checkbox per known scope
with a one-line explanation, and coverage specific to each scope); how long (the grant duration); and the Allow and Deny actions. The page uses only its own
fixed inline style and script, allowed by hash in its Content-Security-Policy.
Browser cookies use authenticated encryption, Secure, HttpOnly and
SameSite=Lax. Cookies never authorize MCP. Denial and failed sign-in issue no
connector grant.

The authorization request's `scope` is a space-separated list of known scopes;
the available scopes are `factory:overview`, `task:discussion`, `factory:usage`,
`repository:usage`, `task:usage`, `task:code-review`, `repository:actions` and
`standards:management`. The consent page
offers every known scope: the scopes the client requested are pre-ticked, the
others are shown unticked and the owner may tick them. This matters when a
client reuses an old registration and requests fewer scopes than the connector
now offers. Following RFC 6749 §3.3, the issued scope may then differ from the
request on the owner's instruction; the token response's `scope` names what was
actually granted. Allow requires at least one ticked scope; the script disables
Allow when none is ticked, and the server refuses an Allow without a scope, or
with an unknown scope, with 400 and no grant. `task:code-review` still requires
`task:discussion`. The grant records the ticked scopes as a space-separated
string. `factory:overview` and `factory:usage` are factory-wide: overview facts
across all registered repositories, including private and future registrations,
and all recorded usage.

The task-level and repository-level scopes (`task:discussion`,
`task:code-review`, `repository:usage` and `task:usage`) cover
**repositories, not single tasks**. When any of them is ticked, the owner
chooses their repository coverage on the consent page: the checkbox **All
repositories (including ones registered later)**, ticked by default, or
individual registered repositories from a list of checkboxes (untick "All
repositories" first). There is no free-text field. The client names the task
(`github:owner/repository#123`) or repository (`github:owner/repository`) on
each tool call, and the connector checks on every call that the task exists
locally and belongs to a covered repository, or that the repository is covered.
Code review reads the task's own retained code repository, derived from the
task; it is never chosen by the client. Consent does not create a session or
fetch an issue. Allow atomically records the grant, authorization code and
repository coverage. Neither sign-in nor a pending consent exposes evidence.

`repository:actions` uses a **separate, explicit finite selection** of registered
repositories. Its boxes start unticked. “All repositories” applies only to the
read scopes above; it never grants action access or includes future repositories.
`standards:management` separately authorizes changes to the shared installation-wide
standards book. Reading a repository as standards-authoring context also requires
the corresponding read permission and coverage. Existing grants gain neither
scope automatically. Reconnect and allow fresh consent to use actions; refreshing
a token preserves its existing grant and does not expand authority.

A refused Allow answers with a short page that names what was wrong, for
example "choose all repositories or at least one registered repository for
task:discussion", "repository not registered: …" or "task:code-review also
needs task:discussion; tick both". Nothing is granted, and the pending consent
stays usable for its ten minutes: go back, change the selection and allow again.
Refused tool calls name the reason in the tool result the same way, for example
"Access refused: task not found: …" or "Access refused: repository not covered
by this grant: …". Reasons name only references, never credentials or local
paths.

The scopes are independent: an overview grant gains no task discussion or usage
access, and a discussion grant grants no overview or usage access. Usage scopes
require their own explicit consent. Refresh keeps the grant's scope and
repository coverage; changing either requires fresh owner consent. A listed
repository that is later deleted or renamed is no longer covered. Deleting or
rebinding a local task denies it rather than silently following a reused issue
number. Later recorded evidence for covered tasks is readable until expiry,
revocation or owner-authority invalidation.

Grants made before repository coverage existed were bound to one task or
repository. The upgrade keeps them working: each now covers the repository of
its former binding, so other tasks in that repository become readable under
the same scopes, and nothing outside it.

The owner chooses the grant duration: 1, 3, 6, 12 or 24 months, default 1. A
month is a calendar month in UTC: the expiry is the consent time plus
`AddDate(0, n, 0)`, so 31 January plus one month is 3 March (2 March in a leap
year). The server accepts only those five values and computes the absolute
expiry at consent; any other value is refused with 400 and no grant. The store
refuses a grant expiring more than 24 months after its creation.

Authorization codes expire in five minutes and bind client, redirect, resource,
granted scope and PKCE. A token request may omit `scope`; if it names one, it
must be a non-empty subset of the grant's scope set (for example the client's
original, narrower request). Tokens always carry the grant's whole scope, on
code exchange and on refresh, and the response's `scope` says so. Access tokens expire within 15 minutes or
at grant expiry, whichever comes first. Refresh rotates without extending the
absolute grant expiry chosen at consent.
Replaying a spent refresh credential revokes the entire family, even after a
restart; a lost refresh response therefore requires fresh owner consent.

`POST /litefactory/oauth/revoke` accepts form-encoded `token`, optional
`token_type_hint` and the client's registered authentication. The stored token
type determines its effect: an access token revokes only itself; a refresh token,
including a retained spent ancestor, revokes its entire family. A different
client cannot revoke it. Success, repeated requests and unknown tokens have the
same empty 200 response. Invalid client authentication and cross-client requests
are refused. Grant ID lookup, revoke-all and grant administration stay local.
Durable revocation is checked on each protected request admitted after commit;
it cannot retract bytes already returned. Discussion rechecks authorization
before each page and before releasing its response. Revoking a discussion-only
grant leaves separately consented overview grants unchanged; revoking a combined
grant ends every permission held by that grant.

## Protocol, limits and diagnostics

Every MCP request, including initialize, ping and listing, requires a
factory-issued Bearer token for the exact resource and current installation
owner. A request without a valid token receives 401 with
`WWW-Authenticate: Bearer resource_metadata="…", scope="…"`; the `scope`
attribute lists every known scope, so a client authorizing again asks for all
of them. A tool call whose grant lacks the tool's scope receives 403 with
`error="insufficient_scope"` and the required `scope`. Initialization negotiates `2025-06-18` or `2025-11-25`; subsequent requests must include a supported
`MCP-Protocol-Version`. Send JSON with `Accept: application/json, text/event-stream`.
Responses are request-scoped JSON, notifications receive 202, authenticated GET
receives 405, and no protocol session is issued. `factory_overview` accepts an
empty object and requires `factory:overview`. `task_evidence_read` requires
`task:discussion` and a `task_ref` in a covered repository. `usage_read` requires
the matching `factory:usage`, `repository:usage` or `task:usage` scope and, for
repository and task usage, a repository the grant covers. Discovery lists only installed,
authorized tools, and direct calls enforce the same permissions. Initialize
instructions name only those tools and their distinct uses: overview questions
use `factory_overview`; questions about a named task and supporting evidence use
`task_evidence_read`; recorded usage and supported historical cost questions use
`usage_read`. Usage-only grants receive guidance for usage only. Reinitialize
after fresh consent to receive current guidance.
No grant or initialization cache expands another grant's permissions.

Task discussion supplies a deterministic factual summary of at most 1,000 Unicode
code points after redaction, including any truncation marker. Its complete
serialized UTF-8 JSON-RPC response is at most 128 KiB (131,072 bytes), including
envelope, metadata, escaped text, continuations and both text and structured
representations. Usage answers have the same summary and complete serialized-response limits,
including their historical price basis and continuation metadata. The existing
overview response ceiling remains 256 KiB.

The application enforces 64 KiB bodies, 2 requests/second per source with burst 10, 10/second globally with burst 30,
32 handlers, four simultaneous tool reads, a 15-second tool context and a
20-second request context. HTTP header/read/write/idle timeouts are 3/5/25/30
seconds. GitHub requests have a 10-second deadline. Capacity refusals return
429 with Retry-After; oversized input returns 413. Providers must honor context
cancellation. Shutdown cancels reads and drains HTTP work for at most five seconds.

Retained state is capped at 1,000 clients, 100 browser transactions, 128 active
grants, 1,000 total retained grants and 100,000 records of each credential type.
Expired state is cleaned in bounded batches; live references and spent refresh
lineage are never evicted to admit work. Source-rate buckets are capped at 4,096.
A state-capacity refusal requires expiry cleanup or a new authorization later.

Connector diagnostics use the daemon's injected structured logger and log
rotation. Records include an internal correlation ID, fixed outcome/reason,
duration and a safe tool label. Unauthenticated refusals use `unavailable` without
reading the body; malformed/unknown tool names use `unknown`. No credentials,
codes, cookie contents, request bodies, query strings or overview content are
logged. Dependency errors are sanitized. A failed store or owner check denies
access; it never enables anonymous operation.

## Discussing a selected task

Connect and allow `task:discussion` for the repositories whose tasks you want to
discuss, then name the task in the conversation. Ask what needs attention; ask for supporting evidence when you want the recorded
basis. The client uses `task_evidence_read` with `task_ref` and one `section`:
`status`, `results`, `questions`, `research`, `plan` or `approvals`. An optional
`limit` is an integer from 1 to 50. Arbitrary paths, URLs, unknown arguments and
duplicate arguments are refused. This access cannot edit evidence, answer a
workflow question, approve a Plan, change modes or start work; use the existing
owner workflows for those actions.

Evidence includes source references, revisions and observation times, with
explicit gaps for missing, archived-only, unlinked, unsupported or unreadable
records. Different sections can have different observation times; they are not
one atomic snapshot of the whole task. A story's parent-owned evidence is limited
to that story and necessary shared decisions. Sibling and dependency details
outside the task grant remain withheld and are identified as gaps.

Questions retain recorded choices and recommendations, answers, waiting status
and original revision bindings. Their current, stale or unknown applicability is
separate from whether a wait remains recorded. A newer candidate or shared review
can make an unanswered recorded question stale; missing or incomparable bindings
make applicability unknown. Recorded approval is distinguished from a draft or
missing evidence, and is current only when its saved basis still matches.
Engineering completion, acceptance and publication are separate recorded facts.

Each evidence page contains at most 50 items within a 64 KiB page budget. Follow
`next_cursor` only while `has_more` is true, using the same task, section and grant.
Cursors are protected, advancing, time-limited and revision-bound. If evidence
changes, restart without a cursor; do not combine old pages with the new revision.
An oversized unreadable item is an explicit gap and does not prevent later
readable evidence from being reached. Capacity or redaction failure refuses the
read safely rather than returning raw evidence. Credentials and host paths are
protected in text, links, metadata and structured output; audit records contain
safe identifiers and outcomes rather than evidence content.

To confirm access has ended, revoke the relevant grant locally and ask the client
for fresh supporting evidence. The new read must be denied. This cannot erase
evidence already delivered to or retained by the client.

These instructions describe the supported contract. They do not claim a completed
Claude or ChatGPT walkthrough; hosted-client verification needs its own configured
test installation, owner consent and captured client/version results.

## Discussing usage and historical costs

In the existing client, request usage access for the scope you need and allow it
as the installation owner. The consent page explains each scope separately:

| Scope | Recorded work covered |
| --- | --- |
| `factory:usage` | All factory usage, including recorded work without a repository. |
| `repository:usage` | Any covered registered repository, named on each call as `github:owner/name`. |
| `task:usage` | Any existing canonical task in a covered repository, named on each call, and its persisted report family, including retired stories and links recorded later. |

Repository coverage is kept through refresh. Each call is checked against it;
deleting or rebinding a task or listed repository denies access. A task usage read includes the selected issue and its
saved graph and legacy story links once each; reading a child includes neither
its parent nor its siblings. Task discussion consent alone never grants this
broader usage family, repository totals or factory totals. Revocation, expiry
and installation-owner generation checks apply before acquisition, on each
continuation and before response release.

Ask, for example, “What did this repository use in the last seven days?” or
“Why is this task's full cost unknown?” The client calls `usage_read` with
`scope` equal to `factory`, `repository` or `task`. Repository and task reads
require `repository` in canonical form; task reads also require a positive
`issue`. Factory reads take neither target. Optional `mode`, `agent` and `model`
filters use the existing meter's semantics. Paths and arbitrary URLs are refused.

| Period argument | Server-resolved UTC interval |
| --- | --- |
| Omitted, or `rolling_30_days` | From the observed server time minus 720 hours up to that time. |
| `today` | UTC midnight up to the observed server time. Exactly at midnight this is a valid empty interval. |
| `rolling_7_days` | From the observed server time minus 168 hours up to that time. |
| `all_history` | Both bounds explicitly unbounded (`null`). |
| Explicit `from` and/or `to` | Timezone-aware RFC3339 instants normalized to UTC; an omitted side is unbounded. |

Do not combine a named period with explicit bounds. An explicit lower bound must
precede the upper bound when both are supplied. Every new read uses one server
clock observation and reports the exact bounds, UTC zone, inclusive lower bound
and exclusive upper bound. The interval selects whole attempts by their start
time; descendants and measurements of a selected attempt are not cut off at the
upper bound. Omitting the period means rolling 30 days, not all history.

`view` is `summary` (the default), `breakdown` or `details`. Breakdown accepts
`by` as a list of `mode`, `agent`, `model` and `run`; its default is `run`.
Run rows keep the canonical repository along with issue and run, so equal issue
and run numbers in different repositories remain distinct. Amounts are exact
decimal USD strings from saved valuations. Referenced historical rates and their
price revision describe the estimate's basis; the current price catalogue never
reprices a read. The summary previews up to 50 rate entries, with
`price_basis_count` and `price_basis_has_more` indicating the full basis size.
Use `details` and its continuations for every measurement's referenced rates. Missing records, unknown models, missing token dimensions,
unpriced work and collection gaps keep the full total unknown while preserving
any known subtotal, including zero. No matching records is distinct from
measured zero. A complete supported local estimate is still not an invoice or a
claim that every past operation was collected. Daemon helper overhead is excluded.

Supporting responses contain at most 50 ordered items (`limit` may lower that)
and fit a 64 KiB page budget. Follow `next_cursor` while `has_more` is true,
keeping the original arguments and grant. Each page repeats the original UTC
bounds, whole-snapshot totals and content revision. The snapshot is closed before
the first response; pages use an immutable sanitized projection. Cursors bind
the grant, target, filters, selected family, view, section, revision, policy and
expiry. Live cursors replay the same evidence; new ledger data requires a fresh
read without a cursor.

Complete summaries and other answers needing no continuation retain no view.
The final page retires its view immediately. New questions replace the oldest
idle retained view when needed, so three successive ordinary questions do not
wait for earlier views to expire. A cursor for a completed, expired or replaced
view requires a fresh read. Busy reads are never evicted; concurrent saturation
can still produce a safe capacity refusal. Views expire within five minutes,
with at most two per grant and four globally, at most 16 MiB per view and a
64 MiB global ceiling including construction reservations.

Acquisition uses lower server-only ceilings of 10,000 source rows and 16 MiB
encoded source data, within the existing 15-second tool deadline. Clients cannot
raise these limits. Narrow the scope or period after a limit refusal. Acquisition,
cancellation, redaction or authorization failures withhold the evidence and
release temporary resources. Text, structured fields and metadata use the same
redaction and host-path protection. Summaries stay within 1,000 Unicode code
points; the complete serialized response stays within 128 KiB, including JSON
escaping, envelope, metadata and both output representations.

Usage discussion reads existing local records only. It cannot collect or recover
missing history, fetch or edit prices, forecast spending, reconcile invoices,
change workflow records, grant approvals or start work. See [usage and costs](../meter.md)
for accounting details and [local grant inspection](../cli/connector-grants.md)
for checking or ending access. These are the supported contracts, not a claim of
completed hosted-client verification.

## Discussing committed code and published standards

Allow both `task:discussion` and `task:code-review` for the repositories whose
tasks you want reviewed. The client names the task on each call; the connector
derives that task's retained code repository, which can differ from the issue
repository, and checks that the task's issue repository is covered. Existing
overview and discussion grants do not gain code access. Revocation, expiry and installation-owner changes end access,
including follow-up evidence reads.

Use the existing client's conversation to ask about a committed file and a
published standard. `repo_read`, `repo_grep`, `repo_log` and `repo_diff` provide
committed task/base evidence; `standards_read` and `standards_role_counts` provide
the verified published book and role applicability. Tool availability and
initialization guidance require the installed providers and both live scopes on
the exact retained target. Combined grants preserve their other tool purposes.
Discussion cannot edit code or standards, run code or tests, approve work or
start implementation.

Repository evidence identifies the actual fully qualified local ref, full commit,
observation time and file/line references. Choose only `task` or `base`; arbitrary
revisions and unrelated history are refused. Commit diffs use the selected
reachable commit's first parent (the empty tree for a root); base-to-task diffs
compare the two pinned tree endpoints directly. Local evidence does not promise
remote freshness. Missing refs or objects, shallow history, binary content,
unsupported entries and size limits appear as gaps. Symlinks and submodules are
committed entries, not permission to follow their targets.

Keep the returned view token for related reads and follow protected continuation
tokens to reach later evidence. A view retains its commit or published book
revision and digest; a new view observes current local evidence. Views expire
within 15 minutes and never outlive the grant. Expired, unavailable or changed
identity evidence requires an explicit fresh read; do not combine versions.
Evidence pages contain at most 50 items and 32 KiB of sanitized text. Complete
serialized answers remain within 128 KiB and factual summaries within 1,000
Unicode code points. Narrow a request when a source exceeds its acquisition
limit. Credentials and host paths are protected before paging.

Search results are extracted after complete files are sanitized. A patch is
withheld with a named gap when its changed files contain sensitive content that
could be cut across diff hunks; use sanitized file reads at the same view instead.
When sanitization changes line layout, the response names the gap and omits exact
line links rather than pointing to the wrong committed line. A local mirror or
unrecognized remote can provide commit/ref/path evidence without a web permalink.

Role counts have two separate labels: **selected categories** and **whole
published book**. Compare them at the same book revision and digest. Each rule
counts once for each applicable role; adding role counts does not give the number
of unique standards. A known empty category has zero selected rules while the
whole book can remain nonempty. Current publication does not establish which
standards applied to an earlier run, and matching a rule to code is interpretation
unless the retrieved evidence directly supports the claim.

### Illustrative conversations

These examples explain the expected conversation; they are illustrative fixtures,
not live results, approvals or claims of a completed hosted-client walkthrough.
Symbolic commit and book values stand for the full identifiers returned by tools.

**Example A — code and a published rule**

Question: “Does the committed limit match the published review rule?”

Short answer: “The task's committed `limits.go` sets `Limit = 3`. Published rule
`review-limit` requires a positive limit. The value is positive, so this line
supports that narrow interpretation; it does not prove runtime enforcement.”

Supporting evidence: `repo_read` on `task`, actual ref `refs/heads/review-task`,
full task commit `<TASK_COMMIT>`, `limits.go:3`; `standards_read`, standard
`review-limit`, category `review`, verified book revision `<BOOK_REVISION>` and
digest `<BOOK_DIGEST>`. Include the returned observation time with each source.

**Example B — compare count bases**

Question: “How many published rules apply to the story worker in review, and in
the whole book?”

Short answer: “Selected categories: review has one rule for `story-worker`.
Whole published book: two rules apply to `story-worker`. Both counts use the
same publication. A rule applying to two roles is counted once for each role.”

Supporting evidence: `standards_role_counts`, category `review`, selected-category
and whole-book labels, the same `<BOOK_REVISION>` and `<BOOK_DIGEST>` as
`standards_read`. The illustrative book has `review-limit` in `review` and
`test-evidence` in `testing`; both apply to `story-worker`, with `review-limit`
also applying to `validate-architecture`. Its known `empty` category has zero
selected counts without changing the whole-book counts.

**Example C — a safe refusal or named gap**

Question: “Read my uncommitted change, or another repository's main branch.”

Short answer: “This consent covers committed task/base evidence in the selected
repository. It does not authorize those sources.”

Supporting evidence: the named task, its covered repository and the refused
request. For a permitted source whose local ref or object is missing, report
that named gap instead of silently choosing another branch. After revocation,
request fresh evidence and confirm it is denied; already delivered answers cannot
be recalled.

Owner review is a short existing-client sequence: consent; ask about code and a
standard; compare the labeled counts; follow source references and a later page;
then observe a named gap or safe refusal and revoke access. Engineering fixtures,
continuation integrity, redaction and final serialized-size measurements are
worker checks. Hosted-client verification still requires a configured test
installation, owner consent and captured client/version evidence.

## Sibling interfaces

The local-control story supplies a trusted `GrantControl` implementation and
registers `LocalControl` against the same database. Readiness must verify the
real local command host, and its list/revoke-one/revoke-all operations must use
the durable store seams. No YAML readiness flag exists. The daemon's
`ConnectorControl` composition field remains absent until that implementation is
installed. Safe grant summaries omit credential and browser state.

The overview story supplies only `OverviewProvider`, returning bounded text and
structured read results and respecting cancellation. It must enforce its own
combined output shaping within the transport ceiling. Neither sibling creates
new authorization middleware or exposes factory mutation capabilities.

## Plain-word owner actions

Ask the connected client for an existing action in ordinary language, for example
“diagnose this repository.” It calls `action_propose` with the action, fixed
arguments and a client request key. The client keeps proposal identifiers and
history cursors internally: you do not copy a hash or identifier. Discovery
lists enabled commands, subactions and their argument schemas. Each proposal
returns its fixed impact tier for review. Unknown or unclassified changing
actions are refused; the neighboring job and task-steering suites are
not added by this confirmation foundation.

Review the returned action, arguments, repository or explicit installation scope,
exact target and revision, impact, owner, originating client and grant, creation
and expiry times, and estimate (or explicit unknown). Say yes only after reviewing
those facts. The client makes a **separate** `action_confirm` call no earlier than
`confirm_not_before`: at least five seconds after the proposal became available.
Proposals expire fifteen minutes after creation. An early refusal leaves it
pending; the client must not queue consent or automatically retry that refusal.
Changed facts require a fresh proposal, rather than editing a reviewed one.

The fixed tiers for currently enabled operations are:

| Tier | Existing actions |
| --- | --- |
| Conversational confirmation | Draft preview, diagnose, research answer; standards analysis start; standards author start, answer, revise, edit, continue, validate, complete and rebase; proposal edit; exception propose. |
| Conversational confirmation and Discord ✅ | Feature creation from its fixed preview (the existing action named `confirm`); standards author publish; proposal approve and publish; exception approve and revoke. |

Reads require no action confirmation. Future enabled ship, merge or Bug-fix
approval actions must retain the binding tier; this feature does not create
missing handlers or grant worker authority.

For a binding action, configure the factory's Discord owner and authorized channel
first (see [Discord setup](../discord.md)). After conversational confirmation,
the factory sends a specific approval request showing the action, exact target,
impact, estimate and expiry. Tap its normal Unicode ✅ as the configured non-bot
owner. Another user, bot, custom emoji, message or channel cannot authorize it.
When multiple approval channels are configured, the client must select an allowed
destination. A failed or uncertain send leaves the proposal unexecuted with a
visible reason; there is no alternate confirmation bypass. Only reaction evidence
received before expiry counts. A missed reaction needs fresh observed input within
the remaining lifetime, or a new proposal after expiry.

Use ordinary requests such as “cancel that proposal,” “what happened to it?” or
“show my proposal history.” The client calls `action_cancel`, `action_inspect` or
paginated `action_history` directly; these controls do not create more proposals.
A proposal is admitted at most once. Repeating a control returns recorded status;
cancellation wins only before admission and cannot undo admitted work. The
original client and grant must confirm an MCP proposal; token refresh within that
grant works, but a replacement grant cannot adopt it. Appropriately authorized
current-owner clients can inspect history across clients.

Status distinguishes waiting for conversation, waiting for Discord, admitted,
executing, succeeded, failed, uncertain, cancelled, expired and invalidated.
Admission, message delivery and queued background work are not proof of external
success. Inspect a pending background result using its recorded reference.
For stale, expired, cancelled or invalidated work, resolve the reason and ask for
a fresh proposal. For an uncertain effect, inspect operation receipts and actual
external state before proposing anything else; never blindly replay it.

History retains authorized proposal summaries, exact targets, provenance, timed
confirmation attempts and decisions, reaction evidence and results for ninety
days after terminal completion. Active and unresolved recovery records remain as
needed. Discord acknowledgments include a history reference. Revoked permissions
or owner changes still prevent inspection even when a client remembers an ID.

At rollout, pending proposals from the old confirmation policy are invalidated
with a request to propose again. Already admitted, executing and uncertain work
keeps its recorded invocation and recovery identity. Existing read access remains
read-only. Run one policy-aware daemon against the migrated store; rolling back
to an older policy must not revive invalidated pending work. This guide describes
the implementation contract, not evidence of live client approval or deployment.

## Durable workflow jobs

Use the shared workflow tools for background log analysis and standards authoring.
`workflows_list` needs no project argument. Follow its cursor for later authorized
entries; an optional project filter includes usable independent definitions and
those bound to that project. Similar labels can identify different workflows, so
keep the returned stable reference and exact version. `workflow_describe` shows
inputs, target rules, permissions, examples, continuation and result contracts
without starting work. An unavailable adapter cannot admit work.

For log analysis, explicitly choose the registered repository, absolute UTC
`after` and `before` instants and `dry_run`. Ask the client to resolve a relative
period before proposing, and review the frozen interval and filing destination.
Dry-run saves the analysis without filing issues. For standards authoring, choose
book-only scope or an explicit registered project as reading context; drafting
never approves or publishes standards. A fixed-project definition infers its
configured target, shows that association and accepts no project override,
including nested input. No omitted project uses an ambient default.

Ask the client to call `workflow_propose` with the discovered reference/version,
validated inputs and a fresh client request key. Review the frozen definition,
version, binding, target, permissions, impact, deadline and destination. Then use
a separate `action_confirm`, under the same five-second minimum, fifteen-minute
expiry and original-client/grant rules as existing actions. Proposal creation
launches no agent. Confirmation returns queued admission and a durable job
reference promptly; admission does not mean the analysis has finished.

Return later through `workflow_job_get`. The same reference survives connection
loss and daemon restart. Jobs show queued, running, waiting-for-input, succeeded,
failed or uncertain state, timestamps, domain references, retained results and
the next allowed action. Results include confirmed filed-issue links or proposed
standards when available. Follow result cursors with the same job and revision;
raw logs and captured private prompts are not remote results. Current grants
still govern reads. Revocation can make a remembered reference inaccessible.

For author questions, the client keeps the exact job, question identifiers and
author-session revision. Ask it to call `workflow_continue_propose` with those
facts and your answers, review the new proposal and confirm separately. The next
attempt belongs to the same job and author session. Stale or overlapping answers
are refused; continuation cannot switch project, binding or workflow version.
Partial answers may leave the job waiting for more input.

Keep the same request key and inputs when recovering a lost proposal response.
The owner-scoped ledger returns the original operation, including across clients;
changed inputs with that key conflict. A second client cannot adopt the original
client's confirmation authority. An uncertain effect requires receipt inspection,
not a fresh key to repeat work. Saved partial findings and confirmed issue links
remain useful even when filing is uncertain.

Discord delivery is separate: pending, delivered, failed or unavailable. Jobs work
without Discord. Notices use an explicitly configured authorized project or
owner/factory destination; no arbitrary channel is inferred. An outage or an
ambiguous send must not erase results or cause blind repeat delivery. Read job
status directly while delivery is unavailable. One background attempt runs at a
time, leaving controls responsive. Each description advertises its bounded domain
deadline; running-job cancellation and automatic uncertain-effect retries are
outside this release.

Existing generic actions, CLI analysis/authoring and Discord entry points remain
supported. Installed adapter authors should follow the [adapter contract and
examples](workflow-adapters.md). Server installation and configuration, rather
than remote client text, select registered adapters and optional notice targets.
Leave notice destinations absent for a safe no-Discord configuration.

For a deployment check, use a test installation and its test repositories. In
both configured Claude and ChatGPT clients, obtain explicit consent, list and
describe workflows, review and confirm one dry-run analysis, reconnect and read
its result, then start book-only authoring and answer a returned question through
a separately confirmed continuation. Repeat authoring with explicit project
context and inspect two installed fixed-project examples with different inputs;
an attempted project override must fail. Revoke a grant and observe denied reads.
For live notices, the owner configures an authorized test destination, confirms a
job, compares its saved result with the notice and observes delivery independently.
These short owner checks supplement deterministic engineering fault tests; they
are not instructions to inject crashes, race answers or test production services.
Record the client/version and actual observation without claiming hosted
compatibility from local fixtures.
