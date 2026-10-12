# Factory MCP

[Documentation](../README.md) · [Using the factory](README.md)

The factory gives each coding-agent run a small set of tools over MCP (Model Context Protocol): saving results, reading issue summaries, posting authorized comments, reporting activity and checking receipts. They are the factory's own tools. They are separate from any third-party MCP servers you add yourself, and they do not give the agent your GitHub credentials.

You do not have to do anything for ordinary work. The agent uses these tools by itself when its role needs them.

## Switching it on

Factory tools are registered for each run when `settings.mcp_auto_start` is `true` (the default). The tools belong to that one run; an idle daemon starts nothing. Settings are in the [configuration reference](../configuration/config-yaml.md) and agent setup is in [coding agents](../configuration/coding-agents.md).

## What each mode gets

| Mode | Factory tools |
| --- | --- |
| Research | Result tools (preflight, submit, receipt), issue summaries, authorized comments, activity, and a tool to draft new issues for your approval. |
| Implement | Result tools, issue summaries, authorized comments, activity and engineering checkpoints. |
| Validate | Result tools, issue summaries, authorized comments, activity and retained assessments. |
| Plan, Spike, Bug | Result tools only (plus activity reporting where it is installed). |

The tools are checked every time they are listed and every time they are called, so a tool that is not allowed for a mode simply is not there. Issue reads return summaries, not full bodies or comments. Being allowed to read an issue does not mean being allowed to comment on it.

Two ideas to know:

- **Preflight** checks an agent's input without saving it or granting anything.
- A **receipt** confirms the factory kept a request. A receipt that says `pending` or `uncertain` is not success.

## Creating new issues from Research

A Research run can propose new issues, but it cannot create them by itself.

1. Creation must be on: `settings.allow_mcp_story_creation: true` (default `false`). Turning it on grants nothing by itself.
2. When the owner's request asks for issues, the agent drafts one to five issues. The factory keeps the draft and posts it as a comment: the exact content, its SHA-256 and the approval command.
3. You approve with `/mcp-create <sha256> linked`, or `/mcp-create <sha256> attach` to also attach the new issues as sub-issues of the source issue. Use the hash exactly as posted. If the content changes, the agent must draft again and you approve again.
4. After approval, the factory creates the issues and posts which ones were created.

Approval lasts 30 minutes. At most five issues per proposal and twenty per repository in any 24 hours are allowed; effects that are still pending or uncertain count towards the limit. If the factory is not sure whether an issue was created, it checks before it tries again, so you do not get duplicates.

## When a response is lost

Keep the request ID and the exact text that was sent, then read the matching receipt. Do not change an uncertain request just to force another write. Once a run has expired, been cancelled or replaced, it cannot regain its rights by reconnecting or by reading a receipt. Send corrected content under a new request after you have recovered the earlier result.

## If tools are missing

Likely causes are a disabled setting, the wrong mode, an expired run, or a conflicting MCP entry in the agent's own configuration. Look at the run logs (`litefactory logs <issue> --only tool`; see the [CLI reference](../cli/command-reference.md)) and the [permissions](../configuration/permissions.md). Story creation and other owner commands are listed in [owner commands](owner-commands.md).

See also [recovery](../operations/recovery.md).
