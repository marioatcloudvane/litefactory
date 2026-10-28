# Research brief

## Request

Brainstorm independent applications in a monorepo, identify Litefactory's larger parts, assess a shared framework/core/Discord/MCP split, and diagram the current solution architecture.

## Decisions

- **S-01** — mode=refactor, size=large — architecture spans orchestration, persistence, transports, providers and versioned team content; this pass establishes boundaries only. (agent's call)
- **O-01** — Explain the current architecture and larger components, with a diagram and advisory split options. (owner said: "Help me brainstorm" and "give me a diagram of the current solution architecture of litefactory!")
- **N-01** — No implementation, module migration, process deployment, commits or publication in this Research pass. (agent's call)
- **K-01** — Any later behavior-preserving extraction must retain owner approvals, attempt authority, result receipts, delivery recovery, worktree isolation and immutable team captures. (agent's call)


## Assumptions

- **AS-01** — Monorepo means one repository containing components with explicit boundaries; independently deployed applications are an option to shape, not a requirement to introduce distributed services everywhere. Based on the issue title and brainstorming request. (agent's call)
- **AS-02** — Prefer library extraction first, core state ownership next, transport process separation later. This is an advisory sequence based on existing seams and shared SQLite coupling, not an approved migration plan. (agent's call)
- **AS-03** — No live deployment inventory, runtime resource measurements or release cadence requirements were established. Component sizes are static source counts; operational independence and generic reuse requirements remain open for shaping. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
