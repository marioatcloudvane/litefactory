# Research brief

## Request

Establish the facts for seven read-only remote MCP tools that let an owner discuss tasks without launching an agent or posting each question to GitHub, with a new grant category as explicitly directed by the owner.

## Decisions

- **d1** — The authenticated owner requested a sketch and explicitly identified this as a new grant category. This pass establishes facts for shaping; it does not implement or approve a design.


## Assumptions

- **a1** — Treat this as an extension of the owner-authorized remote connector, not the per-agent-run submission server. The parent issue describes Claude/ChatGPT remote discussion and the repository has a dedicated read-only connector.
- **a2** — Use retained local state and pinned local repository commits as the initial evidence source. No requirement supplied here calls for a GitHub fetch or agent run per read; freshness and absent-source behavior remain shaping points.
- **a3** — The exact new scope token and whether all seven tools share one category remain undecided. A single discussion category is a sketch, not owner approval of a permission taxonomy.
- **a4** — Dependency #1108 is not independently verified as complete. Its repository-local lookup failed without a recorded receipt; local overview code is present and was inspected. Parent #1106 lookup confirmed the remote Claude/ChatGPT framing.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
