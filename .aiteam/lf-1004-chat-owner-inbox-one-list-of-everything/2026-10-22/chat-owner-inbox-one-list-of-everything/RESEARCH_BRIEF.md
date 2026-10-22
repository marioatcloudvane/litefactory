# Research brief

## Request

Establish current behavior and reuse points for an intelligent owner inbox across GitHub and Linear, using a dedicated reasoning agent to filter, group, prioritize and choose presentation without overwhelming the owner.

## Decisions

- **d1** — The owner explicitly requires a dedicated reasoning agent. Its responsibilities are filtering, grouping, prioritization and output-format selection; the original issue’s fixed-template/no-model restriction is superseded.
- **d2** — The requested inbox spans every connected project and ticket source, with one item identifying the ticket, needed owner action, one-line reason and actionable command or reply. Research does not change workflow or grant approval.


## Assumptions

- **a1** — Interpret “smart” as reasoning over retained facts, with no authority to approve, ship or reply on the owner’s behalf. This follows the original store-facts goal and the current read-only reasoner pattern.
- **a2** — A failed or blocked step belongs only when an owner intervention is actually required. Ordinary scheduling/backoff is excluded, consistent with the current task projection.
- **a3** — Priority policy, group ordering, item limits and overflow interaction are unresolved design choices for shaping; no particular ranking policy was inferred.
- **a4** — The precise historical cost-agent implementation remains unverified. Current code demonstrates a general conversation reasoner with cost lookups, not an established dedicated cost-agent identity.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
