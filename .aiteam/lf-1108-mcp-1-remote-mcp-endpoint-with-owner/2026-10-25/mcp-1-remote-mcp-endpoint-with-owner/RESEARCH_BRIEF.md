# Research brief

## Request

Establish the current foundations and gaps for issue #1108: an HTTPS Streamable HTTP MCP endpoint at https://cloudvane.at/litefactory/mcp, owner-only GitHub-backed OAuth issued by the factory, scoped revocable tokens, one factory_overview read tool, and secret-free connection/call/refusal logging.

## Decisions

- **d1** — This slice exposes one read-only overview tool plus the authentication, transport and operational controls needed to use it safely.
- **d2** — The primary story is that the owner connects a supported hosted assistant, signs in through GitHub and receives a current factory overview without opening the daemon host.
- **d3** — The configured numeric GitHub owner ID is pinned explicitly and must match the daemon owner; missing or conflicting identity disables connector authorization.
- **d4** — Browser login verifies GitHub identity afresh and requires explicit consent naming the client, resource, granted scope and repository coverage before issuing a connector grant.
- **d5** — Discovery, registration and browser OAuth routes are public, while every MCP operation including initialization and tool discovery requires a factory-issued bearer token with factory:overview scope and the exact MCP resource audience.
- **d6** — Authorization uses single-use five-minute codes, S256 PKCE, exact registered HTTPS redirects, bound state and client identity, 15-minute access tokens and rotating refresh-token families with a 30-day absolute expiry.
- **d7** — Refresh-token replay revokes its family, local owner administration can list and revoke one grant or all grants, and revocation is enforced on the next request and after daemon restart.
- **d8** — Persisted authorization records comprise registered clients and redirects, expiring login/code transactions, and owner-client-resource-scope grants with token digests, expiry, refresh lineage and revocation status.
- **d9** — Dynamic client registration is supported with bounded records and strict metadata validation, grants require owner consent, and client metadata URL fetching and general client-management APIs are outside this slice.
- **d10** — The public interface comprises the exact HTTPS MCP resource, standards discovery, registration, authorization, GitHub callback, token exchange and revocation routes, plus local owner grant administration.
- **d11** — The overview tool accepts an empty object and returns schema version, generation time, source timestamps, health, active runs, published unanswered owner waits, recent confirmed deliveries and explicit coverage warnings.
- **d12** — Run records contain repository, issue reference and URL, workflow, stage, state and start time; wait records contain the same task reference, wait kind, bounded question or review summary, since time and published comment URL.
- **d13** — Delivery records contain a stable identifier, kind, task reference, confirmed destination URL and confirmation time, with unavailable historical times omitted from the recent window and reported as a coverage limitation.
- **d14** — Overview lists are independently capped at 50 entries, ordered by newest start for runs, oldest outstanding wait for waits and newest confirmation for deliveries, with stable identity tie breaks and truncation flags.
- **d15** — Missing or stale data is reported as unknown or stale rather than empty or healthy, source failures produce explicit partial results, and failure to read any overview source returns a tool error.
- **d16** — Protocol acceptance targets Streamable HTTP revisions 2025-06-18 and 2025-11-25 with explicit negotiation, request-scoped responses, no required persistent GET stream and rejection of unsupported revisions.
- **d17** — The connector is disabled by default, binds only to loopback behind nginx, uses fixed configured public URLs and indirect secret references, and fails closed on invalid security configuration.
- **d18** — Initial proxy limits are 64 KiB request bodies, 2 requests per second per source IP with burst 10, a global 10 requests per second with burst 30, at most four concurrent tool reads and a 15-second tool deadline.
- **d19** — Proxy streaming responses disable buffering, response timeouts exceed the tool deadline, excess requests receive 429 and invalid origins are rejected without affecting existing webhook routes.
- **d20** — Structured audit records capture authorization outcomes, MCP connects, tool names, duration and refusal category using internal correlation identifiers while excluding credentials, URL queries, request bodies and overview content.
- **d21** — Per the owner's q3 answer, acceptance requires real HTTPS login, token refresh and overview invocation in both claude.ai and ChatGPT, plus non-owner, audience, scope, expiry, replay, revocation, restart, output-bound and proxy-limit checks.
- **d22** — GitHub credentials are used only for identity verification and are never accepted as MCP bearer credentials or forwarded to a connector.
- **d23** — Daemon shutdown cancels pending overview reads, and authentication or store failures must not turn into anonymous access or fabricated successful results.
- **d24** — Per the owner's q1 answer, connector grants cover all daemon-registered repositories including private repositories, with consent stating that later registrations are included.
- **d25** — Per the owner's q2 answer, recent delivery means confirmed owner-facing reports and review publications during the rolling seven-day UTC interval ending at result generation, without code merge or deployment claims.
- **d26** — The overview rejects unexpected arguments and returns equivalent structured and text results within a combined 256 KiB limit, with summaries capped at 512 UTF-8 bytes and trailing entries removed with explicit truncation warnings when necessary.
- **d27** — Health includes heartbeat age, worker occupancy, discovery pause and dispatch holds, with source timestamps and an explicit stale label after five minutes.
- **d28** — Changing the configured owner invalidates all existing connector grants.


## Assumptions

- **a1** — Use an explicitly pinned numeric owner ID matching the daemon owner rather than silently adopting a different local GitHub login; this follows the repository's stable identity convention.
- **a2** — The overview is a bounded projection of recorded factory facts rather than remote issue-history scraping; this follows the read-only goal and existing projections.
- **a3** — The seven-day recent-report window and list caps of 50 remain shaping defaults, while the owner explicitly selected confirmed reports in q2.
- **a4** — Live nginx configuration, OAuth application setup and real connector compatibility remain unverified; authentic deployment acceptance is required rather than inferred from documentation.
- **a5** — Dynamic registration is the initial interoperability route; both providers document automatic registration, so metadata URL fetching is deferred.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Technical findings: [protocol&#45;sources&#46;md](protocol-sources.md)
