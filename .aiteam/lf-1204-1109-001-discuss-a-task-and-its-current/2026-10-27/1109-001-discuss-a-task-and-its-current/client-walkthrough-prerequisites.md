# Client walkthrough prerequisites and inspection

Inspection date: 2026-10-27. This is a prerequisite record, not a passing client walkthrough.

Commands executed locally:

```sh
codex --version
claude --version
codex mcp --help
claude mcp --help
```

Available clients: Codex CLI 0.160.0; Claude Code 2.1.311. Their help exposes remote MCP configuration and OAuth login. No client configuration, OAuth login, paid model request, or production connection was performed by this inspection.

The retained walkthrough requires a dedicated test connector URL (`TEST_BASE_URL`) and dedicated test database (`TEST_DATABASE_URL`) when using a real external database, installation-owner consent through the normal browser flow, a supported natural-language client with authorized model access, and fixture records for current, stale and unknown question/approval applicability. The connector's SQLite fixtures can use a temporary local database instead of an external database. Credentials must remain outside captured output.

For each overview-only, discussion-only and combined grant, capture initialize instructions, discovered tools and the actual tool selected for an overview question and a selected-task question as applicable. Ask what needs attention, request supporting evidence, compare the response against stored fixture facts, and revoke the discussion grant before confirming denial independently of overview access. Candidate and shared-review changes must leave the original recorded waits available for comparison. Record client versions, commands, captured outputs, results and limitations.

The automated owner-consent-to-six-sections HTTP scenario is engineering evidence only; it cannot stand in for the natural-language client walkthrough or prove actual client tool choice. No isolated test connector URL or authenticated client access was supplied to this worker, so that walkthrough remains unexecuted.
