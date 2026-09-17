# Run 0011 — Codex MCP diagnostic

Date: 2026-09-17. Tools are visible; no MCP startup/initialization error is exposed. Scope: repeat the #259 assessment and exercise preflight → submit → matching receipt. No fallback is planned.

## Visible litefactory tools

- `mcp__litefactory__activity_get`
- `mcp__litefactory__activity_update`
- `mcp__litefactory__communication_preflight`
- `mcp__litefactory__communication_receipt`
- `mcp__litefactory__communication_submit`
- `mcp__litefactory__feedback_preflight`
- `mcp__litefactory__issues_comment`
- `mcp__litefactory__issues_get`
- `mcp__litefactory__issues_list`
- `mcp__litefactory__receipts_get`

## activity_get

Exact arguments:
```json
{}
```
Exact returned result:
```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"request_id\":\"226a4bd604ad932ed754b86a9dee72ca\",\"result\":{\"status\":\"unavailable\",\"highest_sequence\":0,\"accepted_updates\":0,\"retained_identities\":0}}"
    }
  ],
  "structuredContent": {
    "request_id": "226a4bd604ad932ed754b86a9dee72ca",
    "result": {
      "status": "unavailable",
      "highest_sequence": 0,
      "accepted_updates": 0,
      "retained_identities": 0
    }
  },
  "isError": false
}
```

The call succeeded (`isError:false`). `status:unavailable` describes advisory activity state, not missing tools or an approval denial. This does not yet verify application input or submission.

## Application delivery — completed MCP chain

Each communication tool was called once, in order. No rejected calls, retry, CLI helper or terminal fallback occurred.

### communication_preflight

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
      "value": "Run 0011 rechecked #259 at 3a19dfd16760431cdc86c28dd61ba6b71fbb8d63. All five findings remain: check IDs collide across snapshots because storage keys them by (run_id, reference); global foreground recovery can propagate another run's failure; harvest mislabels post-execution errors as 'coding agent did not start'; orphaned Plan receipts evade recovery; and the shared-result reader still uses the unscoped check lookup. Fresh execution of the retained Go overlay probe reproduced valid preflight followed by the second story's immutable check conflict and repeated global recovery error (PASS, 1.477s; the pass demonstrates the defect, not a fix). Cross-run harvest and orphaned Plan behavior are source-traced, not a live daemon replay. No production changes or full make check.\n\nRecommended repairs remain snapshot-scoped check identity with all readers updated, scoped foreground recovery, truthful execution diagnostics and stage-aware Plan recovery. Research brief and manifest were updated; Research remains the selected mode.\n\nCodex now sees the litefactory MCP tools. activity_get({}) succeeded, with advisory activity unavailable; run 10's approval-policy rejection did not recur on that call. This application update is being delivered through the requested MCP preflight, submit and matching-receipt path. Exact arguments and responses, including the final request identity and independently reported input/engineering/acceptance/remote-effect states, are retained in CODEX_RUN_0011_MCP_DIAGNOSTIC.md. No CLI helper or terminal application-JSON fallback is used. Acknowledgment does not establish acceptance or publication."
    },
    {
      "op": "set",
      "target": {
        "field": "payload.research.request"
      },
      "value": "Repeat the #259 research assessment against this worktree and test Codex MCP delivery through communication_preflight, communication_submit and the matching communication_receipt."
    }
  ]
}
```
Exact returned result:
```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"request_id\":\"\",\"result\":{\"valid\":true,\"complete\":true,\"diagnostics\":[],\"unchecked_paths\":[],\"expected_revision\":2,\"current_revision\":2}}"
    }
  ],
  "structuredContent": {
    "request_id": "",
    "result": {
      "valid": true,
      "complete": true,
      "diagnostics": [],
      "unchecked_paths": [],
      "expected_revision": 2,
      "current_revision": 2
    }
  },
  "isError": false
}
```

### communication_submit

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
      "value": "Run 0011 rechecked #259 at 3a19dfd16760431cdc86c28dd61ba6b71fbb8d63. All five findings remain: check IDs collide across snapshots because storage keys them by (run_id, reference); global foreground recovery can propagate another run's failure; harvest mislabels post-execution errors as 'coding agent did not start'; orphaned Plan receipts evade recovery; and the shared-result reader still uses the unscoped check lookup. Fresh execution of the retained Go overlay probe reproduced valid preflight followed by the second story's immutable check conflict and repeated global recovery error (PASS, 1.477s; the pass demonstrates the defect, not a fix). Cross-run harvest and orphaned Plan behavior are source-traced, not a live daemon replay. No production changes or full make check.\n\nRecommended repairs remain snapshot-scoped check identity with all readers updated, scoped foreground recovery, truthful execution diagnostics and stage-aware Plan recovery. Research brief and manifest were updated; Research remains the selected mode.\n\nCodex now sees the litefactory MCP tools. activity_get({}) succeeded, with advisory activity unavailable; run 10's approval-policy rejection did not recur on that call. This application update is being delivered through the requested MCP preflight, submit and matching-receipt path. Exact arguments and responses, including the final request identity and independently reported input/engineering/acceptance/remote-effect states, are retained in CODEX_RUN_0011_MCP_DIAGNOSTIC.md. No CLI helper or terminal application-JSON fallback is used. Acknowledgment does not establish acceptance or publication."
    },
    {
      "op": "set",
      "target": {
        "field": "payload.research.request"
      },
      "value": "Repeat the #259 research assessment against this worktree and test Codex MCP delivery through communication_preflight, communication_submit and the matching communication_receipt."
    }
  ]
}
```
Exact returned result:
```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"request_id\":\"4cb8e614737e629881546c6b047da4b7\",\"result\":{\"Validation\":{\"valid\":true,\"complete\":true,\"diagnostics\":[],\"unchecked_paths\":[],\"expected_revision\":2,\"current_revision\":2},\"Receipt\":{\"ID\":9,\"ResultID\":1,\"RunID\":475,\"AppliedRevision\":3,\"SubmissionReceiptID\":0,\"Attempt\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"RequestID\":\"4cb8e614737e629881546c6b047da4b7\",\"RequestHash\":\"7b381aac575328783baaf2ee02957ecd6609397e8255f325225a8d0e465b2523\",\"InputState\":\"accepted\",\"Payload\":\"eyJleHBlY3RlZF9yZXZpc2lvbiI6Miwib3BlcmF0aW9ucyI6W3sib3AiOiJzZXQiLCJ0YXJnZXQiOnsiZmllbGQiOiJjb21tZW50In0sInZhbHVlIjoiUnVuIDAwMTEgcmVjaGVja2VkICMyNTkgYXQgM2ExOWRmZDE2NzYwNDMxY2RjODZjMjhkZDYxYmE2YjcxZmJiOGQ2My4gQWxsIGZpdmUgZmluZGluZ3MgcmVtYWluOiBjaGVjayBJRHMgY29sbGlkZSBhY3Jvc3Mgc25hcHNob3RzIGJlY2F1c2Ugc3RvcmFnZSBrZXlzIHRoZW0gYnkgKHJ1bl9pZCwgcmVmZXJlbmNlKTsgZ2xvYmFsIGZvcmVncm91bmQgcmVjb3ZlcnkgY2FuIHByb3BhZ2F0ZSBhbm90aGVyIHJ1bidzIGZhaWx1cmU7IGhhcnZlc3QgbWlzbGFiZWxzIHBvc3QtZXhlY3V0aW9uIGVycm9ycyBhcyAnY29kaW5nIGFnZW50IGRpZCBub3Qgc3RhcnQnOyBvcnBoYW5lZCBQbGFuIHJlY2VpcHRzIGV2YWRlIHJlY292ZXJ5OyBhbmQgdGhlIHNoYXJlZC1yZXN1bHQgcmVhZGVyIHN0aWxsIHVzZXMgdGhlIHVuc2NvcGVkIGNoZWNrIGxvb2t1cC4gRnJlc2ggZXhlY3V0aW9uIG9mIHRoZSByZXRhaW5lZCBHbyBvdmVybGF5IHByb2JlIHJlcHJvZHVjZWQgdmFsaWQgcHJlZmxpZ2h0IGZvbGxvd2VkIGJ5IHRoZSBzZWNvbmQgc3RvcnkncyBpbW11dGFibGUgY2hlY2sgY29uZmxpY3QgYW5kIHJlcGVhdGVkIGdsb2JhbCByZWNvdmVyeSBlcnJvciAoUEFTUywgMS40NzdzOyB0aGUgcGFzcyBkZW1vbnN0cmF0ZXMgdGhlIGRlZmVjdCwgbm90IGEgZml4KS4gQ3Jvc3MtcnVuIGhhcnZlc3QgYW5kIG9ycGhhbmVkIFBsYW4gYmVoYXZpb3IgYXJlIHNvdXJjZS10cmFjZWQsIG5vdCBhIGxpdmUgZGFlbW9uIHJlcGxheS4gTm8gcHJvZHVjdGlvbiBjaGFuZ2VzIG9yIGZ1bGwgbWFrZSBjaGVjay5cblxuUmVjb21tZW5kZWQgcmVwYWlycyByZW1haW4gc25hcHNob3Qtc2NvcGVkIGNoZWNrIGlkZW50aXR5IHdpdGggYWxsIHJlYWRlcnMgdXBkYXRlZCwgc2NvcGVkIGZvcmVncm91bmQgcmVjb3ZlcnksIHRydXRoZnVsIGV4ZWN1dGlvbiBkaWFnbm9zdGljcyBhbmQgc3RhZ2UtYXdhcmUgUGxhbiByZWNvdmVyeS4gUmVzZWFyY2ggYnJpZWYgYW5kIG1hbmlmZXN0IHdlcmUgdXBkYXRlZDsgUmVzZWFyY2ggcmVtYWlucyB0aGUgc2VsZWN0ZWQgbW9kZS5cblxuQ29kZXggbm93IHNlZXMgdGhlIGxpdGVmYWN0b3J5IE1DUCB0b29scy4gYWN0aXZpdHlfZ2V0KHt9KSBzdWNjZWVkZWQsIHdpdGggYWR2aXNvcnkgYWN0aXZpdHkgdW5hdmFpbGFibGU7IHJ1biAxMCdzIGFwcHJvdmFsLXBvbGljeSByZWplY3Rpb24gZGlkIG5vdCByZWN1ciBvbiB0aGF0IGNhbGwuIFRoaXMgYXBwbGljYXRpb24gdXBkYXRlIGlzIGJlaW5nIGRlbGl2ZXJlZCB0aHJvdWdoIHRoZSByZXF1ZXN0ZWQgTUNQIHByZWZsaWdodCwgc3VibWl0IGFuZCBtYXRjaGluZy1yZWNlaXB0IHBhdGguIEV4YWN0IGFyZ3VtZW50cyBhbmQgcmVzcG9uc2VzLCBpbmNsdWRpbmcgdGhlIGZpbmFsIHJlcXVlc3QgaWRlbnRpdHkgYW5kIGluZGVwZW5kZW50bHkgcmVwb3J0ZWQgaW5wdXQvZW5naW5lZXJpbmcvYWNjZXB0YW5jZS9yZW1vdGUtZWZmZWN0IHN0YXRlcywgYXJlIHJldGFpbmVkIGluIENPREVYX1JVTl8wMDExX01DUF9ESUFHTk9TVElDLm1kLiBObyBDTEkgaGVscGVyIG9yIHRlcm1pbmFsIGFwcGxpY2F0aW9uLUpTT04gZmFsbGJhY2sgaXMgdXNlZC4gQWNrbm93bGVkZ21lbnQgZG9lcyBub3QgZXN0YWJsaXNoIGFjY2VwdGFuY2Ugb3IgcHVibGljYXRpb24uIn0seyJvcCI6InNldCIsInRhcmdldCI6eyJmaWVsZCI6InBheWxvYWQucmVzZWFyY2gucmVxdWVzdCJ9LCJ2YWx1ZSI6IlJlcGVhdCB0aGUgIzI1OSByZXNlYXJjaCBhc3Nlc3NtZW50IGFnYWluc3QgdGhpcyB3b3JrdHJlZSBhbmQgdGVzdCBDb2RleCBNQ1AgZGVsaXZlcnkgdGhyb3VnaCBjb21tdW5pY2F0aW9uX3ByZWZsaWdodCwgY29tbXVuaWNhdGlvbl9zdWJtaXQgYW5kIHRoZSBtYXRjaGluZyBjb21tdW5pY2F0aW9uX3JlY2VpcHQuIn1dfQ==\",\"Diagnostics\":\"eyJ2YWxpZCI6dHJ1ZSwiY29tcGxldGUiOnRydWUsImRpYWdub3N0aWNzIjpbXSwidW5jaGVja2VkX3BhdGhzIjpbXSwiZXhwZWN0ZWRfcmV2aXNpb24iOjIsImN1cnJlbnRfcmV2aXNpb24iOjJ9\",\"Observations\":[{\"Sequence\":1,\"RunID\":475,\"Attempt\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"Dimension\":\"received\",\"State\":\"received\"},{\"Sequence\":2,\"RunID\":475,\"Attempt\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"Dimension\":\"input\",\"State\":\"accepted\"}],\"Latest\":{\"Received\":\"received\",\"Input\":\"accepted\",\"Engineering\":\"pending\",\"Acceptance\":\"pending\",\"Publication\":\"pending\"}},\"Snapshot\":{\"meta\":{\"schema_version\":1,\"task\":\"github:marioatcloudvane/litefactory#259\",\"run_id\":475,\"attempt_id\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"mode\":\"research\",\"result_id\":\"result-1\",\"result_revision\":3,\"scope_ref\":\"8f7152d22f98ee5e00ec2e1e0ac75526d248f531b22ed6555c4750ebd14dbfc3\",\"request_id\":\"4cb8e614737e629881546c6b047da4b7\",\"workflow_version\":2,\"submission_contract_version\":2},\"result\":{\"outcome\":\"completed\",\"comment\":\"Run 0011 rechecked #259 at 3a19dfd16760431cdc86c28dd61ba6b71fbb8d63. All five findings remain: check IDs collide across snapshots because storage keys them by (run_id, reference); global foreground recovery can propagate another run's failure; harvest mislabels post-execution errors as 'coding agent did not start'; orphaned Plan receipts evade recovery; and the shared-result reader still uses the unscoped check lookup. Fresh execution of the retained Go overlay probe reproduced valid preflight followed by the second story's immutable check conflict and repeated global recovery error (PASS, 1.477s; the pass demonstrates the defect, not a fix). Cross-run harvest and orphaned Plan behavior are source-traced, not a live daemon replay. No production changes or full make check.\\n\\nRecommended repairs remain snapshot-scoped check identity with all readers updated, scoped foreground recovery, truthful execution diagnostics and stage-aware Plan recovery. Research brief and manifest were updated; Research remains the selected mode.\\n\\nCodex now sees the litefactory MCP tools. activity_get({}) succeeded, with advisory activity unavailable; run 10's approval-policy rejection did not recur on that call. This application update is being delivered through the requested MCP preflight, submit and matching-receipt path. Exact arguments and responses, including the final request identity and independently reported input/engineering/acceptance/remote-effect states, are retained in CODEX_RUN_0011_MCP_DIAGNOSTIC.md. No CLI helper or terminal application-JSON fallback is used. Acknowledgment does not establish acceptance or publication.\",\"artifacts\":[{\"id\":\"research-brief\",\"kind\":\"research_brief\",\"path\":\".aiteam/lf-259-a-duplicate-check-reference-aborts-a/2026-09-13/a-duplicate-check-reference-aborts-a/RESEARCH_BRIEF.md\"}],\"payload\":{\"research\":{\"request\":\"Repeat the #259 research assessment against this worktree and test Codex MCP delivery through communication_preflight, communication_submit and the matching communication_receipt.\"}}}},\"Baseline\":null}}"
    }
  ],
  "structuredContent": {
    "request_id": "4cb8e614737e629881546c6b047da4b7",
    "result": {
      "Validation": {
        "valid": true,
        "complete": true,
        "diagnostics": [],
        "unchecked_paths": [],
        "expected_revision": 2,
        "current_revision": 2
      },
      "Receipt": {
        "ID": 9,
        "ResultID": 1,
        "RunID": 475,
        "AppliedRevision": 3,
        "SubmissionReceiptID": 0,
        "Attempt": "95e1ecba82bd8b68a6c88a6dead7206b",
        "RequestID": "4cb8e614737e629881546c6b047da4b7",
        "RequestHash": "7b381aac575328783baaf2ee02957ecd6609397e8255f325225a8d0e465b2523",
        "InputState": "accepted",
        "Payload": "eyJleHBlY3RlZF9yZXZpc2lvbiI6Miwib3BlcmF0aW9ucyI6W3sib3AiOiJzZXQiLCJ0YXJnZXQiOnsiZmllbGQiOiJjb21tZW50In0sInZhbHVlIjoiUnVuIDAwMTEgcmVjaGVja2VkICMyNTkgYXQgM2ExOWRmZDE2NzYwNDMxY2RjODZjMjhkZDYxYmE2YjcxZmJiOGQ2My4gQWxsIGZpdmUgZmluZGluZ3MgcmVtYWluOiBjaGVjayBJRHMgY29sbGlkZSBhY3Jvc3Mgc25hcHNob3RzIGJlY2F1c2Ugc3RvcmFnZSBrZXlzIHRoZW0gYnkgKHJ1bl9pZCwgcmVmZXJlbmNlKTsgZ2xvYmFsIGZvcmVncm91bmQgcmVjb3ZlcnkgY2FuIHByb3BhZ2F0ZSBhbm90aGVyIHJ1bidzIGZhaWx1cmU7IGhhcnZlc3QgbWlzbGFiZWxzIHBvc3QtZXhlY3V0aW9uIGVycm9ycyBhcyAnY29kaW5nIGFnZW50IGRpZCBub3Qgc3RhcnQnOyBvcnBoYW5lZCBQbGFuIHJlY2VpcHRzIGV2YWRlIHJlY292ZXJ5OyBhbmQgdGhlIHNoYXJlZC1yZXN1bHQgcmVhZGVyIHN0aWxsIHVzZXMgdGhlIHVuc2NvcGVkIGNoZWNrIGxvb2t1cC4gRnJlc2ggZXhlY3V0aW9uIG9mIHRoZSByZXRhaW5lZCBHbyBvdmVybGF5IHByb2JlIHJlcHJvZHVjZWQgdmFsaWQgcHJlZmxpZ2h0IGZvbGxvd2VkIGJ5IHRoZSBzZWNvbmQgc3RvcnkncyBpbW11dGFibGUgY2hlY2sgY29uZmxpY3QgYW5kIHJlcGVhdGVkIGdsb2JhbCByZWNvdmVyeSBlcnJvciAoUEFTUywgMS40NzdzOyB0aGUgcGFzcyBkZW1vbnN0cmF0ZXMgdGhlIGRlZmVjdCwgbm90IGEgZml4KS4gQ3Jvc3MtcnVuIGhhcnZlc3QgYW5kIG9ycGhhbmVkIFBsYW4gYmVoYXZpb3IgYXJlIHNvdXJjZS10cmFjZWQsIG5vdCBhIGxpdmUgZGFlbW9uIHJlcGxheS4gTm8gcHJvZHVjdGlvbiBjaGFuZ2VzIG9yIGZ1bGwgbWFrZSBjaGVjay5cblxuUmVjb21tZW5kZWQgcmVwYWlycyByZW1haW4gc25hcHNob3Qtc2NvcGVkIGNoZWNrIGlkZW50aXR5IHdpdGggYWxsIHJlYWRlcnMgdXBkYXRlZCwgc2NvcGVkIGZvcmVncm91bmQgcmVjb3ZlcnksIHRydXRoZnVsIGV4ZWN1dGlvbiBkaWFnbm9zdGljcyBhbmQgc3RhZ2UtYXdhcmUgUGxhbiByZWNvdmVyeS4gUmVzZWFyY2ggYnJpZWYgYW5kIG1hbmlmZXN0IHdlcmUgdXBkYXRlZDsgUmVzZWFyY2ggcmVtYWlucyB0aGUgc2VsZWN0ZWQgbW9kZS5cblxuQ29kZXggbm93IHNlZXMgdGhlIGxpdGVmYWN0b3J5IE1DUCB0b29scy4gYWN0aXZpdHlfZ2V0KHt9KSBzdWNjZWVkZWQsIHdpdGggYWR2aXNvcnkgYWN0aXZpdHkgdW5hdmFpbGFibGU7IHJ1biAxMCdzIGFwcHJvdmFsLXBvbGljeSByZWplY3Rpb24gZGlkIG5vdCByZWN1ciBvbiB0aGF0IGNhbGwuIFRoaXMgYXBwbGljYXRpb24gdXBkYXRlIGlzIGJlaW5nIGRlbGl2ZXJlZCB0aHJvdWdoIHRoZSByZXF1ZXN0ZWQgTUNQIHByZWZsaWdodCwgc3VibWl0IGFuZCBtYXRjaGluZy1yZWNlaXB0IHBhdGguIEV4YWN0IGFyZ3VtZW50cyBhbmQgcmVzcG9uc2VzLCBpbmNsdWRpbmcgdGhlIGZpbmFsIHJlcXVlc3QgaWRlbnRpdHkgYW5kIGluZGVwZW5kZW50bHkgcmVwb3J0ZWQgaW5wdXQvZW5naW5lZXJpbmcvYWNjZXB0YW5jZS9yZW1vdGUtZWZmZWN0IHN0YXRlcywgYXJlIHJldGFpbmVkIGluIENPREVYX1JVTl8wMDExX01DUF9ESUFHTk9TVElDLm1kLiBObyBDTEkgaGVscGVyIG9yIHRlcm1pbmFsIGFwcGxpY2F0aW9uLUpTT04gZmFsbGJhY2sgaXMgdXNlZC4gQWNrbm93bGVkZ21lbnQgZG9lcyBub3QgZXN0YWJsaXNoIGFjY2VwdGFuY2Ugb3IgcHVibGljYXRpb24uIn0seyJvcCI6InNldCIsInRhcmdldCI6eyJmaWVsZCI6InBheWxvYWQucmVzZWFyY2gucmVxdWVzdCJ9LCJ2YWx1ZSI6IlJlcGVhdCB0aGUgIzI1OSByZXNlYXJjaCBhc3Nlc3NtZW50IGFnYWluc3QgdGhpcyB3b3JrdHJlZSBhbmQgdGVzdCBDb2RleCBNQ1AgZGVsaXZlcnkgdGhyb3VnaCBjb21tdW5pY2F0aW9uX3ByZWZsaWdodCwgY29tbXVuaWNhdGlvbl9zdWJtaXQgYW5kIHRoZSBtYXRjaGluZyBjb21tdW5pY2F0aW9uX3JlY2VpcHQuIn1dfQ==",
        "Diagnostics": "eyJ2YWxpZCI6dHJ1ZSwiY29tcGxldGUiOnRydWUsImRpYWdub3N0aWNzIjpbXSwidW5jaGVja2VkX3BhdGhzIjpbXSwiZXhwZWN0ZWRfcmV2aXNpb24iOjIsImN1cnJlbnRfcmV2aXNpb24iOjJ9",
        "Observations": [
          {
            "Sequence": 1,
            "RunID": 475,
            "Attempt": "95e1ecba82bd8b68a6c88a6dead7206b",
            "Dimension": "received",
            "State": "received"
          },
          {
            "Sequence": 2,
            "RunID": 475,
            "Attempt": "95e1ecba82bd8b68a6c88a6dead7206b",
            "Dimension": "input",
            "State": "accepted"
          }
        ],
        "Latest": {
          "Received": "received",
          "Input": "accepted",
          "Engineering": "pending",
          "Acceptance": "pending",
          "Publication": "pending"
        }
      },
      "Snapshot": {
        "meta": {
          "schema_version": 1,
          "task": "github:marioatcloudvane/litefactory#259",
          "run_id": 475,
          "attempt_id": "95e1ecba82bd8b68a6c88a6dead7206b",
          "mode": "research",
          "result_id": "result-1",
          "result_revision": 3,
          "scope_ref": "8f7152d22f98ee5e00ec2e1e0ac75526d248f531b22ed6555c4750ebd14dbfc3",
          "request_id": "4cb8e614737e629881546c6b047da4b7",
          "workflow_version": 2,
          "submission_contract_version": 2
        },
        "result": {
          "outcome": "completed",
          "comment": "Run 0011 rechecked #259 at 3a19dfd16760431cdc86c28dd61ba6b71fbb8d63. All five findings remain: check IDs collide across snapshots because storage keys them by (run_id, reference); global foreground recovery can propagate another run's failure; harvest mislabels post-execution errors as 'coding agent did not start'; orphaned Plan receipts evade recovery; and the shared-result reader still uses the unscoped check lookup. Fresh execution of the retained Go overlay probe reproduced valid preflight followed by the second story's immutable check conflict and repeated global recovery error (PASS, 1.477s; the pass demonstrates the defect, not a fix). Cross-run harvest and orphaned Plan behavior are source-traced, not a live daemon replay. No production changes or full make check.\n\nRecommended repairs remain snapshot-scoped check identity with all readers updated, scoped foreground recovery, truthful execution diagnostics and stage-aware Plan recovery. Research brief and manifest were updated; Research remains the selected mode.\n\nCodex now sees the litefactory MCP tools. activity_get({}) succeeded, with advisory activity unavailable; run 10's approval-policy rejection did not recur on that call. This application update is being delivered through the requested MCP preflight, submit and matching-receipt path. Exact arguments and responses, including the final request identity and independently reported input/engineering/acceptance/remote-effect states, are retained in CODEX_RUN_0011_MCP_DIAGNOSTIC.md. No CLI helper or terminal application-JSON fallback is used. Acknowledgment does not establish acceptance or publication.",
          "artifacts": [
            {
              "id": "research-brief",
              "kind": "research_brief",
              "path": ".aiteam/lf-259-a-duplicate-check-reference-aborts-a/2026-09-13/a-duplicate-check-reference-aborts-a/RESEARCH_BRIEF.md"
            }
          ],
          "payload": {
            "research": {
              "request": "Repeat the #259 research assessment against this worktree and test Codex MCP delivery through communication_preflight, communication_submit and the matching communication_receipt."
            }
          }
        }
      },
      "Baseline": null
    }
  },
  "isError": false
}
```

### communication_receipt

Exact arguments:
```json
{
  "request_id": "4cb8e614737e629881546c6b047da4b7"
}
```
Exact returned result:
```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"request_id\":\"4cb8e614737e629881546c6b047da4b7\",\"result\":{\"Validation\":{\"valid\":true,\"complete\":true,\"diagnostics\":[],\"unchecked_paths\":[],\"expected_revision\":2,\"current_revision\":2},\"Receipt\":{\"ID\":9,\"ResultID\":1,\"RunID\":475,\"AppliedRevision\":3,\"SubmissionReceiptID\":0,\"Attempt\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"RequestID\":\"4cb8e614737e629881546c6b047da4b7\",\"RequestHash\":\"7b381aac575328783baaf2ee02957ecd6609397e8255f325225a8d0e465b2523\",\"InputState\":\"accepted\",\"Payload\":\"eyJleHBlY3RlZF9yZXZpc2lvbiI6Miwib3BlcmF0aW9ucyI6W3sib3AiOiJzZXQiLCJ0YXJnZXQiOnsiZmllbGQiOiJjb21tZW50In0sInZhbHVlIjoiUnVuIDAwMTEgcmVjaGVja2VkICMyNTkgYXQgM2ExOWRmZDE2NzYwNDMxY2RjODZjMjhkZDYxYmE2YjcxZmJiOGQ2My4gQWxsIGZpdmUgZmluZGluZ3MgcmVtYWluOiBjaGVjayBJRHMgY29sbGlkZSBhY3Jvc3Mgc25hcHNob3RzIGJlY2F1c2Ugc3RvcmFnZSBrZXlzIHRoZW0gYnkgKHJ1bl9pZCwgcmVmZXJlbmNlKTsgZ2xvYmFsIGZvcmVncm91bmQgcmVjb3ZlcnkgY2FuIHByb3BhZ2F0ZSBhbm90aGVyIHJ1bidzIGZhaWx1cmU7IGhhcnZlc3QgbWlzbGFiZWxzIHBvc3QtZXhlY3V0aW9uIGVycm9ycyBhcyAnY29kaW5nIGFnZW50IGRpZCBub3Qgc3RhcnQnOyBvcnBoYW5lZCBQbGFuIHJlY2VpcHRzIGV2YWRlIHJlY292ZXJ5OyBhbmQgdGhlIHNoYXJlZC1yZXN1bHQgcmVhZGVyIHN0aWxsIHVzZXMgdGhlIHVuc2NvcGVkIGNoZWNrIGxvb2t1cC4gRnJlc2ggZXhlY3V0aW9uIG9mIHRoZSByZXRhaW5lZCBHbyBvdmVybGF5IHByb2JlIHJlcHJvZHVjZWQgdmFsaWQgcHJlZmxpZ2h0IGZvbGxvd2VkIGJ5IHRoZSBzZWNvbmQgc3RvcnkncyBpbW11dGFibGUgY2hlY2sgY29uZmxpY3QgYW5kIHJlcGVhdGVkIGdsb2JhbCByZWNvdmVyeSBlcnJvciAoUEFTUywgMS40NzdzOyB0aGUgcGFzcyBkZW1vbnN0cmF0ZXMgdGhlIGRlZmVjdCwgbm90IGEgZml4KS4gQ3Jvc3MtcnVuIGhhcnZlc3QgYW5kIG9ycGhhbmVkIFBsYW4gYmVoYXZpb3IgYXJlIHNvdXJjZS10cmFjZWQsIG5vdCBhIGxpdmUgZGFlbW9uIHJlcGxheS4gTm8gcHJvZHVjdGlvbiBjaGFuZ2VzIG9yIGZ1bGwgbWFrZSBjaGVjay5cblxuUmVjb21tZW5kZWQgcmVwYWlycyByZW1haW4gc25hcHNob3Qtc2NvcGVkIGNoZWNrIGlkZW50aXR5IHdpdGggYWxsIHJlYWRlcnMgdXBkYXRlZCwgc2NvcGVkIGZvcmVncm91bmQgcmVjb3ZlcnksIHRydXRoZnVsIGV4ZWN1dGlvbiBkaWFnbm9zdGljcyBhbmQgc3RhZ2UtYXdhcmUgUGxhbiByZWNvdmVyeS4gUmVzZWFyY2ggYnJpZWYgYW5kIG1hbmlmZXN0IHdlcmUgdXBkYXRlZDsgUmVzZWFyY2ggcmVtYWlucyB0aGUgc2VsZWN0ZWQgbW9kZS5cblxuQ29kZXggbm93IHNlZXMgdGhlIGxpdGVmYWN0b3J5IE1DUCB0b29scy4gYWN0aXZpdHlfZ2V0KHt9KSBzdWNjZWVkZWQsIHdpdGggYWR2aXNvcnkgYWN0aXZpdHkgdW5hdmFpbGFibGU7IHJ1biAxMCdzIGFwcHJvdmFsLXBvbGljeSByZWplY3Rpb24gZGlkIG5vdCByZWN1ciBvbiB0aGF0IGNhbGwuIFRoaXMgYXBwbGljYXRpb24gdXBkYXRlIGlzIGJlaW5nIGRlbGl2ZXJlZCB0aHJvdWdoIHRoZSByZXF1ZXN0ZWQgTUNQIHByZWZsaWdodCwgc3VibWl0IGFuZCBtYXRjaGluZy1yZWNlaXB0IHBhdGguIEV4YWN0IGFyZ3VtZW50cyBhbmQgcmVzcG9uc2VzLCBpbmNsdWRpbmcgdGhlIGZpbmFsIHJlcXVlc3QgaWRlbnRpdHkgYW5kIGluZGVwZW5kZW50bHkgcmVwb3J0ZWQgaW5wdXQvZW5naW5lZXJpbmcvYWNjZXB0YW5jZS9yZW1vdGUtZWZmZWN0IHN0YXRlcywgYXJlIHJldGFpbmVkIGluIENPREVYX1JVTl8wMDExX01DUF9ESUFHTk9TVElDLm1kLiBObyBDTEkgaGVscGVyIG9yIHRlcm1pbmFsIGFwcGxpY2F0aW9uLUpTT04gZmFsbGJhY2sgaXMgdXNlZC4gQWNrbm93bGVkZ21lbnQgZG9lcyBub3QgZXN0YWJsaXNoIGFjY2VwdGFuY2Ugb3IgcHVibGljYXRpb24uIn0seyJvcCI6InNldCIsInRhcmdldCI6eyJmaWVsZCI6InBheWxvYWQucmVzZWFyY2gucmVxdWVzdCJ9LCJ2YWx1ZSI6IlJlcGVhdCB0aGUgIzI1OSByZXNlYXJjaCBhc3Nlc3NtZW50IGFnYWluc3QgdGhpcyB3b3JrdHJlZSBhbmQgdGVzdCBDb2RleCBNQ1AgZGVsaXZlcnkgdGhyb3VnaCBjb21tdW5pY2F0aW9uX3ByZWZsaWdodCwgY29tbXVuaWNhdGlvbl9zdWJtaXQgYW5kIHRoZSBtYXRjaGluZyBjb21tdW5pY2F0aW9uX3JlY2VpcHQuIn1dfQ==\",\"Diagnostics\":\"eyJ2YWxpZCI6dHJ1ZSwiY29tcGxldGUiOnRydWUsImRpYWdub3N0aWNzIjpbXSwidW5jaGVja2VkX3BhdGhzIjpbXSwiZXhwZWN0ZWRfcmV2aXNpb24iOjIsImN1cnJlbnRfcmV2aXNpb24iOjJ9\",\"Observations\":[{\"Sequence\":1,\"RunID\":475,\"Attempt\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"Dimension\":\"received\",\"State\":\"received\"},{\"Sequence\":2,\"RunID\":475,\"Attempt\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"Dimension\":\"input\",\"State\":\"accepted\"}],\"Latest\":{\"Received\":\"received\",\"Input\":\"accepted\",\"Engineering\":\"pending\",\"Acceptance\":\"pending\",\"Publication\":\"pending\"}},\"Snapshot\":{\"meta\":{\"schema_version\":1,\"task\":\"github:marioatcloudvane/litefactory#259\",\"run_id\":475,\"attempt_id\":\"95e1ecba82bd8b68a6c88a6dead7206b\",\"mode\":\"research\",\"result_id\":\"result-1\",\"result_revision\":3,\"scope_ref\":\"8f7152d22f98ee5e00ec2e1e0ac75526d248f531b22ed6555c4750ebd14dbfc3\",\"request_id\":\"4cb8e614737e629881546c6b047da4b7\",\"workflow_version\":2,\"submission_contract_version\":2},\"result\":{\"outcome\":\"completed\",\"comment\":\"Run 0011 rechecked #259 at 3a19dfd16760431cdc86c28dd61ba6b71fbb8d63. All five findings remain: check IDs collide across snapshots because storage keys them by (run_id, reference); global foreground recovery can propagate another run's failure; harvest mislabels post-execution errors as 'coding agent did not start'; orphaned Plan receipts evade recovery; and the shared-result reader still uses the unscoped check lookup. Fresh execution of the retained Go overlay probe reproduced valid preflight followed by the second story's immutable check conflict and repeated global recovery error (PASS, 1.477s; the pass demonstrates the defect, not a fix). Cross-run harvest and orphaned Plan behavior are source-traced, not a live daemon replay. No production changes or full make check.\\n\\nRecommended repairs remain snapshot-scoped check identity with all readers updated, scoped foreground recovery, truthful execution diagnostics and stage-aware Plan recovery. Research brief and manifest were updated; Research remains the selected mode.\\n\\nCodex now sees the litefactory MCP tools. activity_get({}) succeeded, with advisory activity unavailable; run 10's approval-policy rejection did not recur on that call. This application update is being delivered through the requested MCP preflight, submit and matching-receipt path. Exact arguments and responses, including the final request identity and independently reported input/engineering/acceptance/remote-effect states, are retained in CODEX_RUN_0011_MCP_DIAGNOSTIC.md. No CLI helper or terminal application-JSON fallback is used. Acknowledgment does not establish acceptance or publication.\",\"artifacts\":[{\"id\":\"research-brief\",\"kind\":\"research_brief\",\"path\":\".aiteam/lf-259-a-duplicate-check-reference-aborts-a/2026-09-13/a-duplicate-check-reference-aborts-a/RESEARCH_BRIEF.md\"}],\"payload\":{\"research\":{\"request\":\"Repeat the #259 research assessment against this worktree and test Codex MCP delivery through communication_preflight, communication_submit and the matching communication_receipt.\"}}}},\"Baseline\":null}}"
    }
  ],
  "structuredContent": {
    "request_id": "4cb8e614737e629881546c6b047da4b7",
    "result": {
      "Validation": {
        "valid": true,
        "complete": true,
        "diagnostics": [],
        "unchecked_paths": [],
        "expected_revision": 2,
        "current_revision": 2
      },
      "Receipt": {
        "ID": 9,
        "ResultID": 1,
        "RunID": 475,
        "AppliedRevision": 3,
        "SubmissionReceiptID": 0,
        "Attempt": "95e1ecba82bd8b68a6c88a6dead7206b",
        "RequestID": "4cb8e614737e629881546c6b047da4b7",
        "RequestHash": "7b381aac575328783baaf2ee02957ecd6609397e8255f325225a8d0e465b2523",
        "InputState": "accepted",
        "Payload": "eyJleHBlY3RlZF9yZXZpc2lvbiI6Miwib3BlcmF0aW9ucyI6W3sib3AiOiJzZXQiLCJ0YXJnZXQiOnsiZmllbGQiOiJjb21tZW50In0sInZhbHVlIjoiUnVuIDAwMTEgcmVjaGVja2VkICMyNTkgYXQgM2ExOWRmZDE2NzYwNDMxY2RjODZjMjhkZDYxYmE2YjcxZmJiOGQ2My4gQWxsIGZpdmUgZmluZGluZ3MgcmVtYWluOiBjaGVjayBJRHMgY29sbGlkZSBhY3Jvc3Mgc25hcHNob3RzIGJlY2F1c2Ugc3RvcmFnZSBrZXlzIHRoZW0gYnkgKHJ1bl9pZCwgcmVmZXJlbmNlKTsgZ2xvYmFsIGZvcmVncm91bmQgcmVjb3ZlcnkgY2FuIHByb3BhZ2F0ZSBhbm90aGVyIHJ1bidzIGZhaWx1cmU7IGhhcnZlc3QgbWlzbGFiZWxzIHBvc3QtZXhlY3V0aW9uIGVycm9ycyBhcyAnY29kaW5nIGFnZW50IGRpZCBub3Qgc3RhcnQnOyBvcnBoYW5lZCBQbGFuIHJlY2VpcHRzIGV2YWRlIHJlY292ZXJ5OyBhbmQgdGhlIHNoYXJlZC1yZXN1bHQgcmVhZGVyIHN0aWxsIHVzZXMgdGhlIHVuc2NvcGVkIGNoZWNrIGxvb2t1cC4gRnJlc2ggZXhlY3V0aW9uIG9mIHRoZSByZXRhaW5lZCBHbyBvdmVybGF5IHByb2JlIHJlcHJvZHVjZWQgdmFsaWQgcHJlZmxpZ2h0IGZvbGxvd2VkIGJ5IHRoZSBzZWNvbmQgc3RvcnkncyBpbW11dGFibGUgY2hlY2sgY29uZmxpY3QgYW5kIHJlcGVhdGVkIGdsb2JhbCByZWNvdmVyeSBlcnJvciAoUEFTUywgMS40NzdzOyB0aGUgcGFzcyBkZW1vbnN0cmF0ZXMgdGhlIGRlZmVjdCwgbm90IGEgZml4KS4gQ3Jvc3MtcnVuIGhhcnZlc3QgYW5kIG9ycGhhbmVkIFBsYW4gYmVoYXZpb3IgYXJlIHNvdXJjZS10cmFjZWQsIG5vdCBhIGxpdmUgZGFlbW9uIHJlcGxheS4gTm8gcHJvZHVjdGlvbiBjaGFuZ2VzIG9yIGZ1bGwgbWFrZSBjaGVjay5cblxuUmVjb21tZW5kZWQgcmVwYWlycyByZW1haW4gc25hcHNob3Qtc2NvcGVkIGNoZWNrIGlkZW50aXR5IHdpdGggYWxsIHJlYWRlcnMgdXBkYXRlZCwgc2NvcGVkIGZvcmVncm91bmQgcmVjb3ZlcnksIHRydXRoZnVsIGV4ZWN1dGlvbiBkaWFnbm9zdGljcyBhbmQgc3RhZ2UtYXdhcmUgUGxhbiByZWNvdmVyeS4gUmVzZWFyY2ggYnJpZWYgYW5kIG1hbmlmZXN0IHdlcmUgdXBkYXRlZDsgUmVzZWFyY2ggcmVtYWlucyB0aGUgc2VsZWN0ZWQgbW9kZS5cblxuQ29kZXggbm93IHNlZXMgdGhlIGxpdGVmYWN0b3J5IE1DUCB0b29scy4gYWN0aXZpdHlfZ2V0KHt9KSBzdWNjZWVkZWQsIHdpdGggYWR2aXNvcnkgYWN0aXZpdHkgdW5hdmFpbGFibGU7IHJ1biAxMCdzIGFwcHJvdmFsLXBvbGljeSByZWplY3Rpb24gZGlkIG5vdCByZWN1ciBvbiB0aGF0IGNhbGwuIFRoaXMgYXBwbGljYXRpb24gdXBkYXRlIGlzIGJlaW5nIGRlbGl2ZXJlZCB0aHJvdWdoIHRoZSByZXF1ZXN0ZWQgTUNQIHByZWZsaWdodCwgc3VibWl0IGFuZCBtYXRjaGluZy1yZWNlaXB0IHBhdGguIEV4YWN0IGFyZ3VtZW50cyBhbmQgcmVzcG9uc2VzLCBpbmNsdWRpbmcgdGhlIGZpbmFsIHJlcXVlc3QgaWRlbnRpdHkgYW5kIGluZGVwZW5kZW50bHkgcmVwb3J0ZWQgaW5wdXQvZW5naW5lZXJpbmcvYWNjZXB0YW5jZS9yZW1vdGUtZWZmZWN0IHN0YXRlcywgYXJlIHJldGFpbmVkIGluIENPREVYX1JVTl8wMDExX01DUF9ESUFHTk9TVElDLm1kLiBObyBDTEkgaGVscGVyIG9yIHRlcm1pbmFsIGFwcGxpY2F0aW9uLUpTT04gZmFsbGJhY2sgaXMgdXNlZC4gQWNrbm93bGVkZ21lbnQgZG9lcyBub3QgZXN0YWJsaXNoIGFjY2VwdGFuY2Ugb3IgcHVibGljYXRpb24uIn0seyJvcCI6InNldCIsInRhcmdldCI6eyJmaWVsZCI6InBheWxvYWQucmVzZWFyY2gucmVxdWVzdCJ9LCJ2YWx1ZSI6IlJlcGVhdCB0aGUgIzI1OSByZXNlYXJjaCBhc3Nlc3NtZW50IGFnYWluc3QgdGhpcyB3b3JrdHJlZSBhbmQgdGVzdCBDb2RleCBNQ1AgZGVsaXZlcnkgdGhyb3VnaCBjb21tdW5pY2F0aW9uX3ByZWZsaWdodCwgY29tbXVuaWNhdGlvbl9zdWJtaXQgYW5kIHRoZSBtYXRjaGluZyBjb21tdW5pY2F0aW9uX3JlY2VpcHQuIn1dfQ==",
        "Diagnostics": "eyJ2YWxpZCI6dHJ1ZSwiY29tcGxldGUiOnRydWUsImRpYWdub3N0aWNzIjpbXSwidW5jaGVja2VkX3BhdGhzIjpbXSwiZXhwZWN0ZWRfcmV2aXNpb24iOjIsImN1cnJlbnRfcmV2aXNpb24iOjJ9",
        "Observations": [
          {
            "Sequence": 1,
            "RunID": 475,
            "Attempt": "95e1ecba82bd8b68a6c88a6dead7206b",
            "Dimension": "received",
            "State": "received"
          },
          {
            "Sequence": 2,
            "RunID": 475,
            "Attempt": "95e1ecba82bd8b68a6c88a6dead7206b",
            "Dimension": "input",
            "State": "accepted"
          }
        ],
        "Latest": {
          "Received": "received",
          "Input": "accepted",
          "Engineering": "pending",
          "Acceptance": "pending",
          "Publication": "pending"
        }
      },
      "Snapshot": {
        "meta": {
          "schema_version": 1,
          "task": "github:marioatcloudvane/litefactory#259",
          "run_id": 475,
          "attempt_id": "95e1ecba82bd8b68a6c88a6dead7206b",
          "mode": "research",
          "result_id": "result-1",
          "result_revision": 3,
          "scope_ref": "8f7152d22f98ee5e00ec2e1e0ac75526d248f531b22ed6555c4750ebd14dbfc3",
          "request_id": "4cb8e614737e629881546c6b047da4b7",
          "workflow_version": 2,
          "submission_contract_version": 2
        },
        "result": {
          "outcome": "completed",
          "comment": "Run 0011 rechecked #259 at 3a19dfd16760431cdc86c28dd61ba6b71fbb8d63. All five findings remain: check IDs collide across snapshots because storage keys them by (run_id, reference); global foreground recovery can propagate another run's failure; harvest mislabels post-execution errors as 'coding agent did not start'; orphaned Plan receipts evade recovery; and the shared-result reader still uses the unscoped check lookup. Fresh execution of the retained Go overlay probe reproduced valid preflight followed by the second story's immutable check conflict and repeated global recovery error (PASS, 1.477s; the pass demonstrates the defect, not a fix). Cross-run harvest and orphaned Plan behavior are source-traced, not a live daemon replay. No production changes or full make check.\n\nRecommended repairs remain snapshot-scoped check identity with all readers updated, scoped foreground recovery, truthful execution diagnostics and stage-aware Plan recovery. Research brief and manifest were updated; Research remains the selected mode.\n\nCodex now sees the litefactory MCP tools. activity_get({}) succeeded, with advisory activity unavailable; run 10's approval-policy rejection did not recur on that call. This application update is being delivered through the requested MCP preflight, submit and matching-receipt path. Exact arguments and responses, including the final request identity and independently reported input/engineering/acceptance/remote-effect states, are retained in CODEX_RUN_0011_MCP_DIAGNOSTIC.md. No CLI helper or terminal application-JSON fallback is used. Acknowledgment does not establish acceptance or publication.",
          "artifacts": [
            {
              "id": "research-brief",
              "kind": "research_brief",
              "path": ".aiteam/lf-259-a-duplicate-check-reference-aborts-a/2026-09-13/a-duplicate-check-reference-aborts-a/RESEARCH_BRIEF.md"
            }
          ],
          "payload": {
            "research": {
              "request": "Repeat the #259 research assessment against this worktree and test Codex MCP delivery through communication_preflight, communication_submit and the matching communication_receipt."
            }
          }
        }
      },
      "Baseline": null
    }
  },
  "isError": false
}
```

Input validation: valid and complete, no diagnostics or unchecked paths. Input state accepted; applied revision 3. Matching request ID: `4cb8e614737e629881546c6b047da4b7` (run 475). Engineering pending; acceptance pending; publication pending. No remote publication is established by this receipt. Research is complete, with no implementation change; factory post-exit enforcement remains outstanding.

