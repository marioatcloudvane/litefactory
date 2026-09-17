# Run 0010 — MCP diagnostic (2026-09-17)

All ten litefactory tools are exposed: `mcp__litefactory__activity_get`, `mcp__litefactory__activity_update`, `mcp__litefactory__communication_preflight`, `mcp__litefactory__communication_receipt`, `mcp__litefactory__communication_submit`, `mcp__litefactory__feedback_preflight`, `mcp__litefactory__issues_comment`, `mcp__litefactory__issues_get`, `mcp__litefactory__issues_list`, `mcp__litefactory__receipts_get`.

Call 1: `mcp__litefactory__activity_get`

Arguments: `{}`

Call 2: `mcp__litefactory__communication_preflight`

Exact arguments:
```json
{
  "expected_revision": 2,
  "operations": [
    {
      "op": "set",
      "target": {
        "field": "comment"
      },
      "value": "Run 0010 MCP re-check: all ten litefactory MCP tools are visible to Codex, including communication_preflight, communication_submit and communication_receipt. The diagnostic call activity_get({}) returned isError=true with exact text: MCP tool call requires approval, but approval policy is never. Current source reassessment is being preserved in the research brief. This update tests the required preflight route; it does not claim submission, acceptance or publication."
    }
  ]
}
```

Both calls returned exactly:
```json
{
  "content": [
    {
      "type": "text",
      "text": "MCP tool call requires approval, but approval policy is never"
    }
  ],
  "isError": true
}
```

No valid preflight response was obtained. No submit or receipt call followed, and no new request ID exists. No CLI helper or terminal application-JSON fallback was used. The response reports an approval-policy barrier; this session does not establish whether dispatch reached the server. No MCP startup/initialization error is exposed. Tool visibility is improved over run 0007, but the required end-to-end route remains unverified. Input validity, engineering, acceptance and remote effects have no new receipt states.

