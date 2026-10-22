# Research brief

## Request

Shape a read-only, intelligent owner inbox across connected GitHub and Linear projects, settling eligibility, data, interface, permissions, validation and presentation.

## Decisions

- **d1** — The owner explicitly requires a dedicated reasoning agent. Its responsibilities are filtering, grouping, prioritization and output-format selection; the original issue’s fixed-template/no-model restriction is superseded.
- **d2** — The requested inbox spans every connected project and ticket source, with one item identifying the ticket, needed owner action, one-line reason and actionable command or reply. Research does not change workflow or grant approval.
- **d3** — Scope eligibility is current owner approvals, reviews, questions and explicit owner recovery actions, excluding ordinary execution and automatic retry.
- **d4** — Canonical ticket identity deduplicates the inbox into one entry per ticket while preserving every distinct current pending owner request.
- **d5** — Each item retains source, project, link, current workflow/revision, pending requests, reason, supported action and destination, waiting time and evidence references.
- **d6** — The dedicated agent receives only authorized eligible facts and may rank, group and summarize them without inventing actions, urgency or dependencies.
- **d7** — Chat supports the inbox request, project/source filters, stable snapshot pagination and ticket details without adding a universal workflow reply command.
- **d8** — Existing owner and project permissions and execution-time revision validation remain authoritative for every next action.
- **d9** — Partial coverage, uncertain delivery and missing action evidence are explicit, and an empty inbox is asserted only for completely read coverage.
- **d10** — Reasoner failure returns deterministic collected facts ordered by owner recovery, decision review and open question, then oldest waiting time and canonical identity.
- **d11** — Acceptance covers cross-source completeness, deduplication, multiple requests, stale state, failures, unknown times, empty results and overflow without fabricated actions.
- **d12** — Writes, live provider crawling, notifications, new workflow commands, new business records, additional providers and replacement of status are outside scope.


## Assumptions

- **a1** — Interpret “smart” as reasoning over retained facts, with no authority to approve, ship or reply on the owner’s behalf. This follows the original store-facts goal and the current read-only reasoner pattern.
- **a2** — A failed or blocked step belongs only when an owner intervention is actually required. Ordinary scheduling/backoff is excluded, consistent with the current task projection.
- **a3** — Initial-answer size and priority/grouping remain owner decisions q1 and q2; the proposed defaults are ten entries with explicit overflow and action-based priority with oldest waits first.
- **a4** — The precise historical cost-agent implementation remains unverified. Current code demonstrates a general conversation reasoner with cost lookups, not an established dedicated cost-agent identity.


## Open questions

### q1 — How much should the first inbox answer show?

_Asked of: Product Owner Questions_

- **a** — Recommended: show up to 10 ticket entries, fewer when required by the chat limit, with total counts, remaining counts and a “more” follow-up; never silently discard an eligible item.  *(recommended)*
- **b** — Show every entry immediately across as many chat messages as needed; this favors completeness in one response but can overwhelm a large inbox.

### q2 — What should drive priority and grouping?

_Asked of: Product Owner Questions_

- **a** — Recommended: rank owner recovery first, decision reviews second and open questions third, oldest first within each class; let the agent promote items only for recorded urgency or dependency evidence and group by needed action with project labels.  *(recommended)*
- **b** — Group by project and sort oldest waits first within each project; this is predictable but spreads similar decisions across projects.
- **c** — Let the dedicated agent choose ranking and grouping freely from stored facts on each request; this gives more discretion but less stable ordering.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
