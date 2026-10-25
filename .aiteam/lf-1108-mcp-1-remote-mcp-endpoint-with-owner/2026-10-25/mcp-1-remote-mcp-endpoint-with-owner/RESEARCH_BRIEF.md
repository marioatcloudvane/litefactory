# Research brief

## Request

Establish the current foundations and gaps for issue #1108: an HTTPS Streamable HTTP MCP endpoint at https://cloudvane.at/litefactory/mcp, owner-only GitHub-backed OAuth issued by the factory, scoped revocable tokens, one factory_overview read tool, and secret-free connection/call/refusal logging.

## Decisions

- **d1** — The supplied issue defines this slice as one read tool plus authentication and transport. Further tools, actions and confirmations are outside its stated scope.


## Assumptions

- **a1** — Treat the configured connector owner as one stable numeric GitHub account, consistent with current owner identity conventions. Whether to pin it explicitly or derive it from the daemon owner remains a shaping decision.
- **a2** — Interpret factory_overview as a bounded projection of recorded factory facts with freshness and partial-coverage information, rather than remote issue-history scraping. This follows the read-only goal and existing bounded projections.
- **a3** — The overview schema, recent-delivery time window and meaning of delivered (reports, feature deliveries, or both) are unresolved design details; existing records support investigation during shaping.
- **a4** — No live endpoint, nginx host configuration, OAuth app credentials or real Claude/ChatGPT connection was inspected. Client registration, callback compatibility, token lifetime/refresh/revocation operations and end-to-end acceptance remain unverified.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Technical findings: [protocol&#45;sources&#46;md](protocol-sources.md)
