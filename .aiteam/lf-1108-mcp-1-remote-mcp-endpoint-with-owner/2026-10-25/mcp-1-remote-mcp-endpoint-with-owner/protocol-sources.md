# Protocol source observations

Retrieved 2026-10-25 using web search against the official MCP repository. These are paraphrased source observations, not client compatibility tests.

## Authorization
Source: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/basic/authorization.mdx
The authorization flow discovers protected-resource metadata and authorization-server metadata before browser authorization. The documented flow includes PKCE and a resource parameter. Metadata advertises PKCE support. Login and discovery therefore need access before an MCP access token exists; protected MCP operations still require authorization.

## Transport versions
Source: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/basic/transports/streamable-http.mdx
The source reports that revision 2026-07-28 changed Streamable HTTP by removing GET streams and protocol-level sessions. It describes POST requests and JSON or request-scoped SSE responses. This does not establish which versions Claude or ChatGPT support. Pin compatibility during shaping and verify with real clients.
