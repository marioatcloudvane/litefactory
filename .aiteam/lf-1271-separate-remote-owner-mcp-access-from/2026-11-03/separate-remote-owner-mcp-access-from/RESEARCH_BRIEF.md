# Research brief

## Request

Shape #1271 as a minimal in-process refactor separating existing remote owner MCP transport and authorization lifecycle from core factory ownership, reusing #1269 and preserving #1270's shared approval runtime.

## Decisions

- **S-01** — mode=refactor, size=medium — existing connector ports and core read services narrow the cut, but authorization, retained targets and shared lifecycle require cross-package compatibility review. (agent's call)
- **A-01** — Approach — owner: "check this! (same approach as #1269 and #1270: keep it a minimal separation, reuse the core ownership from #1269)" (owner said: comment 6613086651)
- **O-01** — Isolate existing remote owner transport and authorization lifecycle behind bounded core read operations; retire replaced direct factory access without leaving duplicate policy paths. (owner said: issue #1271)
- **K-01** — Preserve current scopes, repository restrictions, owner identity/generation, revocation enforcement, refusal behavior and stored grants/tokens through upgrade and restart. (owner said: issue #1271)
- **K-02** — Preserve owner approvals, permissions, retained ticket/code connections, receipts, delivery recovery, worktree isolation and immutable captures. Core workflows remain usable with the connector disabled. (owner said: issue #1271)
- **N-01** — No separate process/release, new providers/tools/scopes, new remote authority, new stage steering or confirmation behavior, or agent result broker access. Existing approved capabilities remain existing behavior. (owner said: issue #1271; agent's call on reconciling historical wording with current code)
- **O-02** — Core owns repository enumeration and retained discussion, code and usage target resolution through purpose-specific read contracts, while the connector owns authentication, consent, grant coverage and revocation using those contracts. (agent's call)
- **O-03** — The connector boundary owns remote HTTP startup preparation and shutdown behavior, with daemon composition limited to supplying dependencies and coordinating the shared process lifecycle. (agent's call)
- **O-04** — Retire the mixed connector persistence/target-resolution route and direct daemon authorization reconciliation once their bounded replacements are in use, leaving one authoritative route for each policy. (agent's call)
- **O-05** — Keep authorization data logically connector-owned and workflow data core-owned within the existing physical SQLite database and unchanged stored formats. (agent's call)
- **O-06** — Before replacing uncovered boundaries, establish compatibility checks for unchanged tool results and refusals, target authorization, same-backend binding and startup owner invalidation, extending existing connector, store, read-provider and daemon tests where coverage is missing. (agent's call)
- **O-07** — Demonstrate authorization-state compatibility by exercising the pre-change build, changed build and pre-change build again against persisted state containing valid grants, rotated refresh tokens and revoked grants, with valid access preserved and invalid access refused after restart. (agent's call)
- **O-08** — Update the core ownership, remote connector and local grant administration documentation to describe the final boundary and the verified stop-and-restart rollback route without restoring obsolete authorization state. (agent's call)
- **K-03** — Preserve existing remote tool discovery, inputs, outputs, transport routes, consent screens, refusal semantics, configuration keys and local grant administration behavior. (agent's call)
- **K-04** — Existing reviewed actions, workflows, discussion/publication and stage tools retain their current bounded interfaces and shared core approval, execution and recovery owner. (agent's call)
- **K-05** — Preserve same-backend authentication/target binding, indivisible consent and token transitions, finite repository coverage and authorization checks before acquisition and before releasing protected results. (agent's call)
- **K-06** — Missing dependencies or required core target data must still refuse access, and changing the owner pin away and back must not revive grants even when connector preparation fails. (agent's call)
- **K-07** — Retained ticket/code identity and branch selection must remain authoritative, with no fallback to current configuration or client-selected paths when retained data is missing or inconsistent. (agent's call)
- **K-08** — Every deliverable extraction must leave the current deployment usable, including core workflows with remote access disabled and shared approvals with either transport enabled. (owner said: issue #1271; agent's call on explicit shared-transport coverage)
- **N-02** — Physical database separation, schema migrations, serialized authorization changes and broad relocation of existing projection services are outside this minimal extraction. (agent's call)
- **N-03** — Behavior changes discovered during the refactor and adjacent MCP or confirmation features require separate scope review rather than inclusion in this work. (agent's call)
- **N-04** — Independent versions, multi-host operation, new team or skill changes and mandatory live-provider certification are outside this in-process boundary refactor. (agent's call)


## Assumptions

- **AS-01** — Retain one Go module/process and shared physical SQLite with unchanged schema and serialized authorization; evidence requiring a migration must return for scope review rather than silently expand this extraction. (agent's call)
- **AS-02** — Treat current reviewed actions, workflows, advisory/publication and stage tools as compatibility obligations; do not remove them because the issue's original wording emphasizes reads. This matches current code and #1269's retained invariant K-03. (agent's call)
- **AS-03** — Settled by O-02 through O-05: core owns bounded factory target resolution, connector owns authorization and remote lifecycle preparation, and composition binds them without moving all read providers; exact package names remain a later design choice. (agent's call)
- **AS-04** — Historical connector-state rollback and live-client behavior are not established by this bounded pass. Existing tests support planning their compatibility checks; no owner-only evidence prevents handoff. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
