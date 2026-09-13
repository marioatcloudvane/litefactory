# Add a scoped MCP interface for in-flight agent communication

Follow-up 3 of 3 from #258.

Depends on: **Define and implement the shared agent communication contract** and the run lifecycle integration in **Adopt the shared communication contract and retire obsolete handoffs** (drafts ISSUE_1.md and ISSUE_2.md; replace with issue links when published).

## Problem and outcome

Agents should discover and call litefactory communication tools during a run. Expose the shared service through a thin MCP adapter so native tools and the existing CLI use the same contract, validation and receipt semantics.

## Scope

- Expose applicable submission/preflight/receipt and advisory status operations as discoverable tools with schemas and actionable feedback.
- Reuse the shared service; do not introduce separate patch, validation, acceptance or delivery logic.
- Bind tool permissions, selectors and lifetime to the authenticated run/attempt. Reject expired or cross-attempt calls.
- Support in-flight status for Research/Spike as well as managed modes.
- Configure and verify explicit Claude/Codex MCP integration.
- Define narrow allowed tools for each execution profile, including Validate, without granting general shell access or additional acceptance authority.
- Handle restart, reconnect/retry, cancellation and closed-attempt behavior consistently with existing service guarantees.

An agent-facing status tool reports overall run activity. MCP request-progress notifications track a particular active MCP request and do not automatically provide this run-status feed.

## Completion criteria

- [ ] CLI and MCP adapters enforce the same applicable contract and receipt semantics.
- [ ] Supported Claude/Codex hosts can discover and invoke permitted tools.
- [ ] Status updates work across applicable modes and remain advisory.
- [ ] Tests verify allowed/denied operations, attempt isolation, expiry, duplicate handling and cancellation/recovery.
- [ ] Validate's narrow tool profile is explicitly tested; existing terminal-output support is preserved unless its replacement is separately justified.
- [ ] Tool failures return useful diagnostics without claiming workflow completion or successful remote publication.

## Dependencies and boundaries

Adapter development may start once the shared service stabilizes. Production activation depends on the run-scoped lifecycle from issue 2. Recommended delivery order is 1 → 2 → 3.

No second communication contract, duplicate workflow business logic or MCP-only progress semantics. Artifact storage and runner telemetry remain separate concerns.

Research and agreed boundaries: #258 and the session RESEARCH_BRIEF.md, runs 0003–0005.

