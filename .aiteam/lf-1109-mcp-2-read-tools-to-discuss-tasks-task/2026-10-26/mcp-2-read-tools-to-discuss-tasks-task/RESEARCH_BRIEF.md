# Research brief

## Request

Shape the seven read-only MCP discussion tools into a buildable scope covering goal, interface, data, permissions, validation, edge cases and exclusions.

## Decisions

- **d1** — The owner requested shaping of seven read-only discussion tools and a new grant category; this scope is proposed for review and grants no implementation or publication approval.
- **d2** — Use the seven named tools and the interface, data, validation, edge-case and success rules in the scope comment as the proposed build boundary.
- **d3** — Read retained local task state and pinned local commits without fetching, starting agents or changing GitHub.
- **d4** — Use canonical task selectors and expose independent revision, approval, acceptance and publication evidence rather than one approved flag.
- **d5** — Use explicit factory, repository or task cost scopes with UTC periods and preserve collection and pricing uncertainty.
- **d6** — Return the published standards book with both selected-category and whole-book counts per role.
- **d7** — Bound serialized responses to 128 KiB, summaries to 1000 characters and calls to the existing 15-second timeout, with revision-bound paging and explicit omissions.
- **d8** — Treat repository main as the registered base branch and report the exact locally resolved ref and pinned commit without silently substituting branches.
- **d9** — Keep existing overview grants unchanged and enforce the independently consented category on discovery and every call.
- **d10** — Exclude writes, per-question agent runs, live synchronization, raw transcripts, arbitrary revisions, historical browsing and new dashboards.


## Assumptions

- **a1** — Extend the owner-authorized remote connector; the supplied issue names remote task discussion and research found an existing connector.
- **a2** — Local retained evidence is sufficient for the first release because the request requires quick read-only discussion without an agent run; outputs disclose freshness and gaps.
- **a3** — Propose factory:discussion as the scope token and one category for all seven tools; owner question q1 must settle its repository exposure.
- **a4** — Dependency #1108 completion remains unverified; supplied research records failed lookup receipt 9000f9cdf4bc700f3590244c1be17d9d and confirms local overview code is present.
- **a5** — Use the registered base branch for the requested main selector because repositories can use different default branch names.
- **a6** — Choose canonical selectors, UTC rolling cost periods, explicit unknown states and bounded paging to remove ambiguity without adding owner configuration.


## Open questions

### q1 — What should the new discussion grant allow a client to read?

_Asked of: Product Owner Questions_

- **single** — Recommended: one factory:discussion category for all seven tools across all registered repositories, including private and future registrations, with that exposure stated on consent and inspection.  *(recommended)*
- **split** — Separate task and cost discussion from repository and standards content into two independently consented categories, both with existing all-repository coverage.
- **restricted** — Require repository selection at consent and cover only explicitly selected repositories, adding grant filtering and excluding future registrations until selected.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
