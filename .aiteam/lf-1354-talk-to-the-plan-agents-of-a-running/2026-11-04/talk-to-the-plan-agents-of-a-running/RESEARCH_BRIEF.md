# Research brief

## Request

Check #1354 against current implementation and explain its impact on Research, Bug, Plan and connector workflows.

## Decisions

- **S-01** — mode=feature, size=medium — proposed connector capability crosses Plan target selection, advisory isolation and owner gates, but current implementation already covers its core. (agent's call)
- **A-01** — Owner request — owner: "check this. and tell me how this impacts the workflows!" (owner said: issue comment 6625055491)
- **O-01** — Establish current Plan advisory behavior and workflow impacts before shaping any remaining change. (agent's call)
- **N-01** — Do not authorize Plan approvals or candidate edits through private advisory answers; the supplied draft explicitly excludes conversational approval. (agent's call)
- **K-01** — Preserve Research and Bug advisory behavior and Plan owner gates when considering any remaining extension. (agent's call)


## Assumptions

- **AS-01** — Current checked-out commit is the implementation baseline; the deployed server version may differ and was not observed. (agent's call)
- **AS-02** — Treat between-stage fallback, wider parallel eligibility, and further judgment requirements as shaping choices, not missing owner evidence that blocks factual research. (agent's call)
- **AS-03** — No implementation is authorized by this research pass; the issue draft is context, not settled owner scope. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
