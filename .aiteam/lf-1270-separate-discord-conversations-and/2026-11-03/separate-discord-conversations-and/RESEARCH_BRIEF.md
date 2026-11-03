# Research brief

## Request

Check #1270: establish current Discord/conversation/delivery ownership, existing core contracts, remaining coupling and compatibility risks before shaping.

## Decisions

- **S-01** — mode=refactor, size=large — remaining boundary spans transport lifecycle, conversations, shared approvals, factory effect hosts and persisted recovery across several packages. (agent's call)
- **A-01** — Request for investigation — owner: "check this!" (owner said: comment 6598029014)
- **O-01** — Isolate Discord gateway intake, credentials, conversations, presentation and delivery behind core command/read operations in the existing deployment. (owner said: issue #1270 supplied scope)
- **O-02** — Retire replaced direct factory persistence and daemon-internal paths, exercise Discord behavior through the core contract, and document configuration/operator use. (owner said: issue #1270 supplied completion)
- **K-01** — Preserve owner/channel admission, confirmation/approval policy, redaction, commands, recorded outcomes, retained ticket/code routing, receipts, worktree isolation and immutable captures; factory decisions and publication authority stay in core. (owner said: issue #1270 supplied scope)
- **K-02** — Saved conversations and pending deliveries survive upgrade/restart; preserve transaction fences and current bounded delivery guarantees. Core workflows remain usable with Discord disabled. (owner said: issue #1270 supplied completion)
- **N-01** — Exclude separate processes/releases, new platforms or features, new remote authority, approval policy changes and broad database migration. Remote MCP extraction is not a prerequisite. (owner said: issue #1270 supplied scope)


## Assumptions

- **AS-01** — One repository, one Go module and coordinated in-process release remain the drafting baseline; the issue and current core documentation agree. Shaping may refine component layout. (agent's call)
- **AS-02** — Reuse the current SQLite schema and narrow storage adapter unless shaping identifies a concrete missing ownership operation; physical database separation is unnecessary for the stated logical boundary. (agent's call)
- **AS-03** — Existing private store-symbol aliases can remain only as documented compatibility bridges without granting factory persistence capability; final boundary checks must include relocated hosts and shared approval wiring. (agent's call)
- **AS-04** — Historical conversation upgrade coverage and a live stop/drain/rollback are unverified in this pass. Existing fixture evidence is reusable but cannot stand in for extraction-specific compatibility acceptance. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
