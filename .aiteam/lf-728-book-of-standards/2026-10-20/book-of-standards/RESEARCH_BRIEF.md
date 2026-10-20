# Research brief

## Request

Shape the Book of Standards scope covering MUST-based rules, MCP discovery, CLI administration, evidence-analysis agents, version retention, permissions, validation and migration.

## Decisions

- **d1** — The owner explicitly requests category-queryable MCP retrieval, a MUST-based standard format, CLI management and evidence-based standards agents. The supplied example is explicitly described as potentially excessive, so its fields are not a settled mandatory schema.
- **d2** — The issue requires stack-neutral support, identifiable standards versions per run, continued mandatory factory enforcement and avoidance of conflicting migrated copies. This research pass does not authorize implementation or publication.
- **d3** — The primary user story is applicable MUST-rule retrieval by an agent with an operator-auditable immutable version.
- **d4** — The proposed model includes categories, standard revisions, immutable books, evidence-backed proposals with hashes and expiry, exceptions, recurring schedules and retained analysis executions.
- **d5** — Applicability uses repository, stack, stage and role with OR within dimensions and AND across dimensions, while universal standards always apply.
- **d6** — MCP provides pinned standards discovery and authorized run/log reads, while CLI provides administration, exact-hash human approval and manual or scheduled analysis controls.
- **d7** — New runs capture a complete book and resumed work uses the same capture, while explicit adoption of another book starts a new run.
- **d8** — Migration moves shared behavioral guidance atomically and preserves factory-owned authority, submission contracts and validation.
- **d9** — The owner requires drift, bug and log analysis to support both CLI invocation and schedules.
- **d10** — Acceptance additionally covers MCP log access, proposal-hash approval and expiry, recurrence and restart recovery, empty-book continuation and unavailable-service provenance.
- **d11** — Proposed standard fields are permanent ID, MUST or MUST NOT title, description and purpose, categories, applicability, owner, compliance evidence, check type, exception policy and lifecycle status.
- **d12** — Published books contain only approved active standards and category metadata, while drafts and retired revisions remain available in administrative history.
- **d13** — Check type is automated, semi-automated or manual, and denotes evidence evaluation rather than a promise to add an executable checker.
- **d14** — A published standard permits exceptions only explicitly, with scope, reason, approving operator and expiry attached to its exact revision.
- **d15** — Unknown filters return a validation error, valid filters with no matches return an explicit empty result, and unknown standard IDs or unavailable versions never fall back to another version.
- **d16** — Conflicting applicable rules block affected work and require maintainer resolution rather than implicit precedence by specificity.
- **d17** — Category IDs and standard IDs are immutable, category renames retain identity, retirement preserves historical retrieval and stale concurrent edits are rejected.
- **d18** — Evidence analysis uses read-only source access, records source identities and time bounds, treats retrieved text as evidence rather than instructions and excludes credentials from proposals.
- **d19** — Analysis processes at most 100 issues, 100 changes and 20 run logs per invocation by default, reports the selected deterministic order and omissions, and allows explicit bounded limits.
- **d20** — Migration acceptance requires every inventoried graph extra to be classified as a migrated standard, retained protocol or authority instruction, or retained role-specific procedure with a reason.
- **d21** — The result proposes a scope for owner review and does not authorize implementation, publication or stage approval.
- **d22** — The owner requires proposal hashes approved by humans and valid for 36 hours by default with configurable duration.
- **d23** — The owner requires analysis agents to access logs through MCP rather than filesystem paths.
- **d24** — The owner requires work to continue when no standards exist.
- **d25** — Proposed approval hashes bind changes, evidence references, scope, base revision and expiry, with stale or expired proposals requiring renewed presentation and approval.
- **d26** — Proposed recurring presets are daily bug analysis at 02:00 UTC, daily logs at 03:00 UTC and weekly drift on Monday at 04:00 UTC, enabled explicitly per repository.
- **d27** — Schedules retain cadence, timezone, repository, analysis kind, evidence selection, enablement, next due time and last outcome across restarts.
- **d28** — Scheduled scans cover from the last successful watermark with one-day overlap up to 30 days, reporting gaps and avoiding overlapping executions or duplicate proposals.
- **d29** — Service outages use a verified retained copy or continue with explicit unavailable provenance, while corrupted pinned captures never silently become empty books.
- **d30** — Proposal approval expires from first presentation and rejects stale bases, changed content, unauthorized identities and duplicate publication.


## Assumptions

- **a1** — Use three distinct CLI analysis entry points with reusable execution machinery because the owner named three evidence purposes rather than three mandatory implementations.
- **a2** — The owner settled publication authority: agents propose and humans approve exact proposal hashes with a configurable 36-hour validity window.
- **a3** — Flat many-to-many categories and exact applicability matching are proposed to keep discovery predictable without a taxonomy or precedence engine.
- **a4** — The existing bounded inventory identifies migration families but is not an exhaustive semantic classification of graph extras or a verified reconstruction of issue #649.
- **a5** — Deployment remains unresolved as q2, while the owner's reply settles continued work in the absence of standards.
- **a6** — No authentic mining corpus was evaluated, so effectiveness and proposal quality remain acceptance obligations rather than demonstrated results.
- **a7** — Book ownership defaults to the factory operator, with a required accountable role on every standard and explicitly designated maintainers for edits.
- **a8** — Thirty days and bounded evidence counts are proposed defaults rather than owner-specified definitions of recent.
- **a9** — The preset times, opt-in enablement, UTC default, overlap and catch-up policies are shaping proposals rather than owner-specified settings.
- **a10** — Treat the owner's continued-work answer as allowing unavailable-service continuation with explicit provenance while preserving capture integrity checks.


## Open questions

### q2 — Where should the first standards book live and which agents must be able to query it?

_Asked of: Technical Questions_

- **local** — Recommended: one factory-managed local book with repository-scoped rules, queried by factory-launched agents through authorized MCP; defer remote synchronization and independent clients.  *(recommended)*
- **shared** — A shared network service for multiple factories and independent MCP clients; remote authentication, service ownership and deployment must also be settled.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
