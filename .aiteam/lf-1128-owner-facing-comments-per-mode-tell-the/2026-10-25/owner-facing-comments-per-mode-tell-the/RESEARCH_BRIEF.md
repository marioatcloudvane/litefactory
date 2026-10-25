# Research brief

## Request

Analyze #1128: consistent outcome-led owner comments, a shared owner-report skill, mode-specific emphasis, change-first reruns and recommended next steps, without changing Go or result construction.

## Decisions

- **d1** — This pass is research only. Preserve the issue's boundary: agent-written comment text; no Go, result schema, workflow authority or publication changes.
- **d2** — The requested dependency is present in this checkout: HEAD b8423290 merges #1163 from lf/998-team-inheritence. Shared canonical roles now reside in base-team.


## Assumptions

- **a1** — Treat about 120 words as a target for the agent's comment, excluding factory-added commands, notices, options and structured evidence. The issue explicitly leaves those factory responsibilities unchanged.
- **a2** — The supplied #1004 account is owner-reported motivation, not a reproduced observation. No authentic six-finding report or retained prior-run comparison was inspected.
- **a3** — Whether Bug, Spike, Plan set and sync resolution receive distinct emphasis remains a shaping choice: the proposal says every mode, while its detailed list names only Research, Plan story/technical/acceptance, Implement and Validate.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
