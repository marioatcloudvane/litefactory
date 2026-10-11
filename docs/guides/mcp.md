# Factory MCP

Factory MCP gives a supported coding-agent launch scoped access to factory results, checkpoints, issue summaries, approved publication and request receipts. It is separate from arbitrary third-party MCP servers and does not provide GitHub credentials to the agent.

Prerequisites: a configured supported agent, a live factory attempt and enabled automatic registration (`settings.mcp_auto_start`, default `true`). The factory installs attempt-specific handlers. An idle daemon does not start an MCP child. See [MCP settings](../configuration/config-yaml.md) and [agent configuration](../configuration/coding-agents.md).

```text
/research Investigate the retry flow and report the findings through the available factory tools.
```

The agent may use permitted native tools as well; ordinary runs need not call MCP. Factory tool feedback separates valid input, engineering, acceptance and remote effects. Preflight diagnoses input without recording proof or approval. Activity is advisory. A receipt acknowledges retained state; `pending` or `uncertain` is not success.

| Mode | Additional factory families |
| --- | --- |
| Research | Preflight, issue summaries, authorized comments, receipts; gated exact-approved creation. |
| Implement | Preflight, summaries, authorized comments, engineering checkpoints and receipts. |
| Validate | Preflight, summaries, authorized comments, retained assessments and receipts; no fresh capture. |
| Plan / Spike | Shared communication and advisory activity; expanded issue/checkpoint families unavailable. |

All modes can have shared communication/activity when installed. MCP refuses publishable/story-bearing Plan results; the existing factory helper route retains its own Plan policy. Discovery and direct calls both enforce permissions. Issue reads contain summaries, not full bodies/comments. Reading an issue does not authorize commenting on it.

Creation requires selected Research mode, `settings.allow_mcp_story_creation: true` (default `false`) and a separate authenticated owner approval. First use `/mcp-create prepare <CreationEnvelope JSON>`; inspect the factory's exact prepared JSON and hash, then approve `/mcp-create <SHA256> linked` or `attach`. Copy the exact returned envelope; editing it needs new preparation and approval. Detailed schema is in [tool families](../mcp-tool-families.md#exact-creation-preparation-and-publication).

Approval lasts 30 minutes, bounded by attempt expiry, and is consumed by the next eligible native Research launch. It is not inherited by resume/replacement. Limits are one batch of at most five artifacts per attempt and twenty artifacts per repository per rolling 24 hours; pending/uncertain effects retain charges. Enabling the setting grants nothing by itself.

For a lost response, retain the request ID and exact submitted bytes, then read the matching receipt. Do not change an uncertain request merely to force another write. Expired, cancelled or replaced attempts cannot regain authority through reconnect or a receipt. Correct refused content under a new request identity after recovering any prior outcome.

Missing tools can mean a disabled setting, unavailable handler, wrong mode, expired attempt or native configuration collision. Inspect run diagnostics and [permissions](../configuration/permissions.md). Native Claude/Codex integrations and factory composition are implemented in this revision; authentic host delivery/isolation observations for #274/#296 remain unverified. Offline fixtures do not establish those observations or successful live GitHub publication.

See [adapter protocol](../mcp-adapter.md), [tool-family reference](../mcp-tool-families.md) and [recovery](../operations/recovery.md).

[Using the factory](README.md)
