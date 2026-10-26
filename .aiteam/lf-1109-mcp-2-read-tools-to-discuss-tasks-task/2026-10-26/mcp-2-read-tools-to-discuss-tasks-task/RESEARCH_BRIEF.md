# Research brief

## Request

Establish the facts for seven read-only remote MCP tools that let an owner discuss tasks without launching an agent or posting each question to GitHub, with a new grant category as explicitly directed by the owner.

## Decisions

- **d1** — The owner requested a sketch and explicitly identified this as a new grant category. The retry resumes research; it does not approve a design or authorize implementation.


## Assumptions

- **a1** — Treat this as an extension of the owner-authorized remote connector, not the per-agent-run submission server. The supplied request names remote discussion and current code provides a dedicated read-only connector.
- **a2** — Use retained local state and pinned local repository commits as the initial evidence source. No supplied requirement calls for a GitHub fetch or agent run per read; freshness and absent-source behavior remain shaping points.
- **a3** — The exact new scope token and whether all seven tools share one category remain undecided. A single discussion category is a sketch, not approval of a permission taxonomy.
- **a4** — Dependency #1108 is not independently verified as complete. The retry's repository-local lookup failed; receipt 9000f9cdf4bc700f3590244c1be17d9d reported nothing recorded. Local overview code is present and inspected. The supplied issue context establishes the remote discussion goal.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
