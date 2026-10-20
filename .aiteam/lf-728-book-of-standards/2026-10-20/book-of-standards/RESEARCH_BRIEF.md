# Research brief

## Request

Shape the Book of Standards scope covering MUST-based rules, MCP discovery, CLI administration, evidence-analysis agents, version retention, permissions, validation and migration.

## Decisions

- **d1** — The owner explicitly requests category-queryable MCP retrieval, a MUST-based standard format, CLI management and evidence-based standards agents. The supplied example is explicitly described as potentially excessive, so its fields are not a settled mandatory schema.
- **d2** — The issue requires stack-neutral support, identifiable standards versions per run, continued mandatory factory enforcement and avoidance of conflicting migrated copies. This research pass does not authorize implementation or publication.
- **d3** — The primary user story is applicable MUST-rule retrieval by an agent with an operator-auditable immutable version.
- **d4** — The proposed data model contains categories, standards and revisions, immutable published books, evidence-backed proposals and scoped expiring exceptions.
- **d5** — Applicability uses repository, stack, stage and role with OR within dimensions and AND across dimensions, while universal standards always apply.
- **d6** — MCP offers version-pinned read-only discovery and retrieval, and CLI owns administration, proposal analysis and run-version inspection.
- **d7** — New runs capture a complete book and resumed work uses the same capture, while explicit adoption of another book starts a new run.
- **d8** — Migration moves shared behavioral guidance atomically and preserves factory-owned authority, submission contracts and validation.
- **d9** — Three CLI analysis entry points cover drift, bugs and logs with a default 30-day window and explicit coverage reporting.
- **d10** — Acceptance covers discovery, editing, evidence-based proposals, immutable replay, access checks, conflicts, failure behavior and preservation of stack selection and factory enforcement.
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


## Assumptions

- **a1** — Use three distinct CLI analysis entry points with reusable execution machinery because the owner named three evidence purposes rather than three mandatory implementations.
- **a2** — Agent publication authority remains unresolved and is asked as q1 rather than inferred from the owner's use of adds.
- **a3** — Flat many-to-many categories and exact applicability matching are proposed to keep discovery predictable without a taxonomy or precedence engine.
- **a4** — The existing bounded inventory identifies migration families but is not an exhaustive semantic classification of graph extras or a verified reconstruction of issue #649.
- **a5** — Deployment and unavailable-snapshot policy remain owner decisions q2 and q3, while the rest of this scope assumes local authorized evidence sources and immutable capture.
- **a6** — No authentic mining corpus was evaluated, so effectiveness and proposal quality remain acceptance obligations rather than demonstrated results.
- **a7** — Book ownership defaults to the factory operator, with a required accountable role on every standard and explicitly designated maintainers for edits.
- **a8** — Thirty days and bounded evidence counts are proposed defaults rather than owner-specified definitions of recent.


## Open questions

### q1 — Who may publish standards produced by the learning agents?

_Asked of: Product Owner Questions_

- **human** — Recommended: all three agents create drafts; a designated human maintainer approves publication, revision and retirement.  *(recommended)*
- **automatic** — Allow agents to publish rules automatically after schema and conflict checks; this makes new agent conclusions mandatory for later runs without human review.

### q3 — May work proceed when no verified standards snapshot can be obtained?

_Asked of: Product Owner Questions_

- **stop** — Recommended: stop affected work when no verified snapshot exists; resumed work continues from its retained snapshot and new work may use an exact configured version already retained locally.  *(recommended)*
- **warn** — Allow work to proceed with an explicit missing-standards warning and provenance record; mandatory factory enforcement still applies, but that work cannot claim standards compliance.

### q2 — Where should the first standards book live and which agents must be able to query it?

_Asked of: Technical Questions_

- **local** — Recommended: one factory-managed local book with repository-scoped rules, queried by factory-launched agents through the existing authorized MCP boundary; remote synchronization and independent MCP clients are deferred.  *(recommended)*
- **shared** — Build a shared network service usable by multiple factories and independent MCP clients; this adds remote authentication, service ownership and deployment requirements that must be shaped before completion.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
