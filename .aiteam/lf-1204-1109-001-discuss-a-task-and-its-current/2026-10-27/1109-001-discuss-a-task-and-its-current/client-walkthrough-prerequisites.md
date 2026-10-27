# Manual client walkthrough handoff

Manual owner acceptance remains pending. Under the continuation owner guidance, this walkthrough follows the engineering work and does not block engineering completion. It has not been executed or passed.

Use a supported client against a dedicated test connector (`TEST_BASE_URL`) with installation-owner authentication. A real external test database requires a dedicated `TEST_DATABASE_URL`; temporary local SQLite fixtures need no external database. Keep credentials out of captures.

Repeat these four owner actions for overview-only, discussion-only and combined grants, using each available capability:

1. Connect and consent. Confirm the guidance names the available tools: `factory_overview` for overview questions, `task_evidence_read` for the selected task, and both with distinct purposes for combined access.
2. Ask what needs attention. Overview questions should use `factory_overview`; selected-task questions should use `task_evidence_read`.
3. Ask for supporting evidence. Confirm the short answer can be followed with the selected task's recorded evidence and any stated gaps.
4. Revoke access and confirm denial. Use separate overview and discussion grants to confirm that revoking discussion leaves overview available. Revoking a combined grant ends both permissions in that grant.

Retain the client/version and captures of connection, guidance, actual tool choice, answers, evidence and denial, with the observed result and limitations. Current, stale and unknown question/approval fixtures, independent candidate/shared-review changes, source comparisons, migration, dispatch, redaction and serialized-byte measurements remain worker engineering proofs; the owner does not audit those internals.

Local inspection on 2026-10-27 ran `codex --version`, `claude --version`, `codex mcp --help` and `claude mcp --help`. Available clients were Codex CLI 0.160.0 and Claude Code 2.1.311; their help exposes remote MCP configuration and OAuth login. This inspection performed no client configuration, OAuth login, model request or production connection. No test connector URL or authenticated client access was supplied to this worker.

The automated consent-to-six-sections HTTP scenario supplies engineering evidence. It does not establish actual natural-language client tool choice or complete this manual acceptance step.
