# Owner-only remote connector

The remote connector is a separate read-only boundary. It is disabled by default,
binds only to a numeric loopback address, and requires a dedicated HTTPS reverse
proxy. It never exposes attempt tools or factory mutation commands.

This foundation supplies OAuth, durable grants and the MCP transport. Remote
serving stays closed until **Local control of connector grants (#1187)** registers
a working local command host with trusted daemon composition. Setting
`enabled: true` cannot bypass that gate. **Factory overview in Claude and ChatGPT
(#1188)** supplies the overview projection and verifies both hosted clients.
Without a projection, authenticated `tools/list` returns an empty list. These
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
The escaped consent page names the client ID, redirect host, exact resource,
`factory:overview` scope and absolute expiry, and explains that access covers all
registered repositories, including private and future registrations. Browser
cookies use authenticated encryption, Secure, HttpOnly and SameSite=Lax. Cookies
never authorize MCP. Denial and failed sign-in issue no connector grant.

Authorization codes expire in five minutes and bind client, redirect, resource
and PKCE. Access tokens expire within 15 minutes or at grant expiry, whichever
comes first. Refresh rotates without extending the 30-day absolute grant expiry.
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
it cannot retract bytes already returned.

## Protocol, limits and diagnostics

Every MCP request, including initialize, ping and listing, requires a
factory-issued Bearer token for the exact resource and scope. Initialization
negotiates `2025-06-18` or `2025-11-25`; subsequent requests must include a supported
`MCP-Protocol-Version`. Send JSON with `Accept: application/json, text/event-stream`.
Responses are request-scoped JSON, notifications receive 202, authenticated GET
receives 405, and no protocol session is issued. The only optional tool is
`factory_overview`, accepting an empty object.

The application enforces 64 KiB bodies, 256 KiB serialized MCP responses,
2 requests/second per source with burst 10, 10/second globally with burst 30,
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
