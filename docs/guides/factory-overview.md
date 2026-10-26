# Factory overview in Claude and ChatGPT

The owner can request `factory_overview` with `{}` through the remote connector.
The read covers all registered repositories, including private repositories and
later registrations. It returns factory health, current activity, published
unanswered owner questions and reviews, and confirmed publications from the
inclusive 168-hour UTC window ending at generation time. It cannot approve or
change factory work.

## Before connecting

Follow [remote connector setup](remote-connector.md) for the owner pin, dedicated
GitHub OAuth app, HTTPS proxy, credentials, fixed routes and logging controls.
Use [local connector grant controls](../cli/connector-grants.md) to inspect and
stop access. Enabling the connector requires the daemon's trusted local control
host and overview provider; configuration alone does not establish readiness.

For verification, provision a dedicated test factory with synthetic repositories
and set an explicit `TEST_BASE_URL` for its public HTTPS endpoint. Do not test
revocation, restart, replay or rate limits against production. A remote-client
account with permission to add a custom connector and the test installation's
GitHub owner account are also required. Do not record credentials, authorization
URLs with queries, cookies, or private overview payloads as evidence.

## Connect each hosted client

Use the ordinary web clients. Local desktop MCP configuration, API fixtures and
the MCP Inspector do not prove hosted-client compatibility.

- In Claude, add a custom remote connector with the configured HTTPS MCP URL.
  Select OAuth sign-in and **Register automatically** when the client offers an
  OAuth-client choice. The server does not support metadata-URL registration.
  Complete GitHub owner sign-in and the explicit consent page. Enable the
  connector in a conversation and request the factory overview. Team and
  Enterprise permissions can restrict adding connectors; record the actual
  account settings. See [Claude's connector instructions](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp).
- In ChatGPT web, use the available custom MCP connection settings for the
  account. Current documentation describes Plugins > Add custom MCP server;
  legacy workspaces may expose Apps > Create or developer-mode settings. Enter
  the HTTPS URL, select OAuth, complete owner consent and discover the tools.
  Install/select the connection in a conversation and request the overview.
  Workspace roles and plan settings can restrict access. See
  [OpenAI's connection instructions](https://help.openai.com/en/articles/12584461-connecting-custom-mcp-servers-in-chatgpt).

Provider setup documentation was checked on 2026-10-26; menu names and account
availability can change. This server offers one read tool, without a custom UI
or search/fetch tools. Its supported protocol revisions are `2025-06-18` and
`2025-11-25`. Record the revision actually negotiated by each hosted client.
The refresh capability uses the existing `factory:overview` scope; do not add
authorization scopes or weaken the grant checks to accommodate a client.

## Reading the result

Check source read times, observation times and coverage before treating an empty
list as idle. Health older than five minutes is stale; missing health remains
unknown. Owner waits have their own oldest-first limit and do not expire after
seven days. Historical confirmations without a recorded confirmation time do
not enter recent publications and produce a coverage warning.

Activity, waits and publications each retain at most 50 entries. Summaries have
a 512-byte UTF-8 limit, and the complete text/structured response has a 256 KiB
limit. Truncation and omitted repository coverage remain visible. Partial-source
failures retain readable sections with warnings; a failure to read any source
is a tool error. The overview reads local recorded facts, without polling remote
comments or guaranteeing that a previously confirmed link remains accessible.

## Hosted verification record

**2026-10-26: unresolved; neither hosted client has been verified.** This worker
has no browser automation or authenticated hosted-client session, no supplied
test HTTPS deployment (`TEST_BASE_URL`), and no test owner account credentials.
The example public URL in the foundation guide is configuration guidance, not
evidence of a deployed or authorized test target. No live endpoint was invoked,
and no running daemon or grant was changed.

| Check | Claude web | ChatGPT web |
| --- | --- | --- |
| Account access, plan and connector settings | Not available | Not available |
| Registration method and negotiated revision | Not executed | Not executed |
| HTTPS login and explicit owner consent | Not executed | Not executed |
| Overview invocation and freshness/coverage inspection | Not executed | Not executed |
| Renewal after access-token expiry, without renewed consent | Not executed | Not executed |
| Local grant revoke; next admitted call and renewal refused | Not executed | Not executed |
| Reconnect and test-daemon restart preserve revocation | Not executed | Not executed |
| Non-owner, audience/scope, expiry and refresh replay refusals | Not executed | Not executed |
| Serialized output and configured proxy limits | Not executed | Not executed |

For each later check, record UTC date, client/account settings, test deployment
identity, registration choice, negotiated protocol and a redacted outcome. Check
refresh after the original access token expires (at most 15 minutes), rather
than treating a second immediate call as proof of renewal. Inspect the exact
test grant locally, revoke it, and verify that the next newly admitted request
and renewal fail. Reconnect and restart only the dedicated test daemon, then
repeat the refusal check. New consent may create a new grant; it does not revive
the revoked grant. Keep failed or unavailable checks unresolved. Automated local
connector regressions provide engineering evidence separately and cannot turn
this table into a hosted acceptance pass.
