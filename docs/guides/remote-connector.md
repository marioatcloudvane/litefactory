# Owner-only remote connector

The remote connector is a separate read-only boundary. It is disabled by default,
binds only to a numeric loopback address, and requires a dedicated HTTPS reverse
proxy. It never exposes attempt tools or factory mutation commands.

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
host); what it can access (the exact resource, one pre-ticked checkbox per
requested scope with a one-line explanation, and coverage specific to each
scope); how long (the grant duration); and the Allow and Deny actions. The page uses only its own
fixed inline style and script, allowed by hash in its Content-Security-Policy.
Browser cookies use authenticated encryption, Secure, HttpOnly and
SameSite=Lax. Cookies never authorize MCP. Denial and failed sign-in issue no
connector grant.

The authorization request's `scope` is a space-separated list of known scopes;
the available scopes are `factory:overview`, `task:discussion`, `factory:usage`,
`repository:usage` and `task:usage`. Allow requires
at least one ticked scope; the script disables Allow when none is ticked, and the server refuses an
Allow without a scope, or with a scope that is unknown or was not requested,
with 400 `invalid_scope` and no grant. The grant records the ticked scopes as a
space-separated string. `factory:overview` permits factory-wide overview facts
across all registered repositories, including private and future registrations.
`task:discussion` permits recorded evidence for one existing task selected on the
consent form, such as `github:owner/repository#123`. Enter its canonical reference;
it must already have a local factory session in a registered repository. Consent
does not create a session or fetch the issue. Allow atomically records the grant,
authorization code and immutable task binding. Neither sign-in nor a pending
consent exposes task evidence.

The scopes are independent: an overview grant gains no task discussion or usage
access, and a discussion grant grants no overview or usage access. Usage scopes
require their own explicit consent. A combined grant contains each selected
permission but still binds discussion to only one task. Changing tasks requires
fresh owner consent; refresh cannot change the target. Deleting or rebinding the
local task or repository denies discussion rather than silently binding a reused
issue number. Later recorded evidence for the same retained task is readable
until expiry, revocation or owner-authority invalidation.

The owner chooses the grant duration: 1, 3, 6, 12 or 24 months, default 1. A
month is a calendar month in UTC: the expiry is the consent time plus
`AddDate(0, n, 0)`, so 31 January plus one month is 3 March (2 March in a leap
year). The server accepts only those five values and computes the absolute
expiry at consent; any other value is refused with 400 and no grant. The store
refuses a grant expiring more than 24 months after its creation.

Authorization codes expire in five minutes and bind client, redirect, resource,
granted scope and PKCE. A token request may omit `scope`; if it names one, it
must be exactly the grant's scope set. Access tokens expire within 15 minutes or
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
owner. Initialization negotiates `2025-06-18` or `2025-11-25`; subsequent requests must include a supported
`MCP-Protocol-Version`. Send JSON with `Accept: application/json, text/event-stream`.
Responses are request-scoped JSON, notifications receive 202, authenticated GET
receives 405, and no protocol session is issued. `factory_overview` accepts an
empty object and requires `factory:overview`. `task_evidence_read` requires
`task:discussion` and the consented task binding. `usage_read` requires the
matching `factory:usage`, `repository:usage` or `task:usage` scope and any narrow
target binding. Discovery lists only installed,
authorized tools, and direct calls enforce the same permissions. Initialize
instructions name only those tools and their distinct uses: overview questions
use `factory_overview`; selected-task questions and supporting evidence use
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

Connect and allow `task:discussion` for the existing task you want to discuss.
Ask what needs attention; ask for supporting evidence when you want the recorded
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
| `repository:usage` | One selected registered repository, identified as `github:owner/name`. |
| `task:usage` | One existing canonical task and its persisted report family, including retired stories and links recorded later. |

Repository and task targets are immutable through refresh. Deleting or rebinding
a target denies access. A task usage read includes the selected issue and its
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
