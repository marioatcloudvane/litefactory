# Research brief

## Request

Establish the facts for #1271: minimal separation of existing remote owner MCP transport and authorization lifecycle from core factory ownership, reusing #1269 and preserving #1270's shared approval runtime.

## Decisions

- **S-01** — mode=refactor, size=medium — existing connector ports and core read services narrow the cut, but authorization, retained targets and shared lifecycle require cross-package compatibility review. (agent's call)
- **A-01** — Approach — owner: "check this! (same approach as #1269 and #1270: keep it a minimal separation, reuse the core ownership from #1269)" (owner said: comment 6613086651)
- **O-01** — Isolate existing remote owner transport and authorization lifecycle behind bounded core read operations; retire replaced direct factory access without leaving duplicate policy paths. (owner said: issue #1271)
- **K-01** — Preserve current scopes, repository restrictions, owner identity/generation, revocation enforcement, refusal behavior and stored grants/tokens through upgrade and restart. (owner said: issue #1271)
- **K-02** — Preserve owner approvals, permissions, retained ticket/code connections, receipts, delivery recovery, worktree isolation and immutable captures. Core workflows remain usable with the connector disabled. (owner said: issue #1271)
- **N-01** — No separate process/release, new providers/tools/scopes, new remote authority, new stage steering or confirmation behavior, or agent result broker access. Existing approved capabilities remain existing behavior. (owner said: issue #1271; agent's call on reconciling historical wording with current code)


## Assumptions

- **AS-01** — Retain one Go module/process and shared physical SQLite; use existing core ownership and neutral result contracts, with no schema or serialized authorization change unless later evidence requires one. This follows current architecture and the owner's minimal-separation direction. (agent's call)
- **AS-02** — Treat current reviewed actions, workflows, advisory/publication and stage tools as compatibility obligations; do not remove them because the issue's original wording emphasizes reads. This matches current code and #1269's retained invariant K-03. (agent's call)
- **AS-03** — The exact package cut and any additional core target-view contract are shaping decisions. Research establishes mixed target resolution and lifecycle wiring, not a requirement to relocate all projection services or split all persistence. (agent's call)
- **AS-04** — Historical connector-state rollback and live-client behavior are not established by this bounded pass. Existing tests support planning their compatibility checks; no owner-only evidence prevents handoff. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
