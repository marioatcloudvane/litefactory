# Research brief

## Request

Establish facts for #1269: give factory behavior and state one owner, define bounded interfaces for operator CLI, Discord and remote owner MCP, reuse the completed execution library, and preserve existing behavior and authority.

## Decisions

- **S-01** — mode=refactor, size=large — boundaries span CLI, daemon scheduling, turn execution, ticket adapters, shared persistence and two consumer extractions. (agent's call)
- **A-01** — Analysis request — owner: "Analyze this!" (owner said: comment 6584505656)
- **O-01** — Establish a core owner for admission, scheduling, workflow/owner gates, workspaces, attempt authority, durable results and publication/recovery; expose bounded commands and read views with authorization at that boundary. (owner said: issue #1269 supplied scope)
- **O-02** — Include operator CLI service ownership and necessary ticket-adapter cleanup; retire the direct paths replaced by each extraction while keeping deployment usable. (owner said: issue #1269 supplied scope)
- **K-01** — Preserve observable commands, stopped-daemon local operations, approvals, permissions, qualified ticket identity, retained ticket/code routing and separate ticket approval versus GitHub code-delivery authority. (owner said: issue #1269 supplied scope)
- **K-02** — Preserve live attempt descriptors/broker authority and receipts, locks, interrupted-run recovery, delivery retries, worktree isolation and immutable team captures; include compatibility and recovery for any state migration. (owner said: issue #1269 supplied scope)
- **N-01** — No new providers, user features, remote authority, network API, independent CLI versions, standalone ticket service or comprehensive ticket rewrite; separate processes and multi-host operation are follow-up scope. (owner said: issue #1269 supplied scope)


## Assumptions

- **AS-01** — Use one repository, one Go module and coordinated releases, matching the issue's drafting assumption and landed execution library. Exact package layout and service granularity belong to shaping. (agent's call)
- **AS-02** — Preserve the current consented remote action/advisory surfaces as existing behavior; the older sibling issue's read-only description is not an accurate inventory of today's connector. Shaping must reconcile that wording without expanding authority. (agent's call)
- **AS-03** — Logical ownership need not change physical storage. No schema migration is established as necessary by this pass; any later migration needs explicit compatibility/recovery evidence. (agent's call)
- **AS-04** — This bounded pass establishes source behavior and a focused test baseline, not live upgrade, all command compatibility or exhaustive state ownership. No owner-only fact blocks handoff. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
