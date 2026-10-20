# Research brief

## Request

Shape the Book of Standards scope covering MUST-based rules, MCP discovery, CLI administration, evidence-analysis agents, version retention, permissions, validation and migration.

## Decisions

- **d1** — The owner explicitly requests category-queryable MCP retrieval, a MUST-based standard format, CLI management and evidence-based standards agents. The supplied example is explicitly described as potentially excessive, so its fields are not a settled mandatory schema.
- **d2** — The issue requires stack-neutral support, identifiable standards versions per run, continued mandatory factory enforcement and avoidance of conflicting migrated copies. This research pass does not authorize implementation or publication.
- **d3** — The primary user story is applicable MUST-rule retrieval by an agent with an operator-auditable immutable version.
- **d4** — The local SQL model includes categories, standard revisions and applicability, immutable books, resumable authoring sessions, proposals and approvals, exceptions, schedules, analysis results and run-version references.
- **d5** — Applicability uses repository, stack, stage and role with OR within dimensions and AND across dimensions, while universal standards always apply.
- **d6** — MCP provides pinned standards discovery and authorized run/log reads, while CLI and Discord provide administration, human hash approval and manual or scheduled analysis controls.
- **d7** — New runs capture a complete book and resumed work uses the same capture, while explicit adoption of another book starts a new run.
- **d8** — Migration moves shared behavioral guidance atomically and preserves factory-owned authority, submission contracts and validation.
- **d9** — The owner requires drift, bug and log analysis to support both CLI invocation and schedules.
- **d10** — Acceptance additionally covers normal Discord creation and follow-up, exact proposal approval replies, ambiguous and stale replies, cancellation, restart recovery and SQL/CLI parity.
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
- **d31** — The owner selected local SQL storage for the Book of Standards.
- **d32** — The owner restricts standards reachability to CLI and Discord rather than the issue tracker.
- **d33** — CLI and Discord expose the same standards, authoring-session and schedule operations using shared SQL records, permissions and approval outcomes.
- **d34** — Issue-tracker standards commands, approvals, notifications and proposal publication are excluded from this feature.
- **d35** — Discord operations require a bound or explicit repository and authorization mapped to an existing operator identity.
- **d36** — Cross-interface inspection and approval never reset proposal expiry or produce duplicate publication.
- **d37** — The owner requires an assisted standard-creation agent exposed as a CLI create function.
- **d38** — The proposed agent surface contains four focused roles: an authoring assistant and drift, bug and log analysts.
- **d39** — Assisted creation retains sessions and unanswered questions in SQL and resumes through either CLI or Discord without requiring an interactive terminal.
- **d40** — Retain one proposal SHA-256 hash for the earlier exact-hash approval requirement while recognizing that immutable SQL proposal IDs and revisions could provide equivalent binding.
- **d41** — Discord integrates standards operations into ordinary conversation without adding standards slash commands or generic work-control authority.
- **d42** — Conversational action confirmation and standard publication approval remain separate gates, while valid publication approval needs no additional confirmation of itself.
- **d43** — SQL publication atomically retains the human approval and new immutable book version.
- **d44** — CLI functions cover discovery, categories, assisted creation/editing, validation, proposal review and approval, retirement, analysis results, schedules, versions, run provenance, exceptions and standards configuration.
- **d45** — The owner requires standards to be part of normal Discord interaction rather than slash commands.
- **d46** — Conversational publication approval must identify the proposal hash or explicitly reply to its exact retained proposal message.
- **d47** — Ambiguous replies, old drafts, quoted text, bot messages and inferred agreement cannot authorize publication.
- **d48** — Persist project/session/proposal-message associations and clarify unresolved targets after restart rather than guessing.
- **d49** — No Discord standards slash commands are included in this feature.


## Assumptions

- **a1** — Use three distinct CLI analysis entry points with reusable execution machinery because the owner named three evidence purposes rather than three mandatory implementations.
- **a2** — The owner settled publication authority: agents propose and humans approve exact proposal hashes with a configurable 36-hour validity window.
- **a3** — Flat many-to-many categories and exact applicability matching are proposed to keep discovery predictable without a taxonomy or precedence engine.
- **a4** — The existing bounded inventory identifies migration families but is not an exhaustive semantic classification of graph extras or a verified reconstruction of issue #649.
- **a5** — The owner selected one local factory-managed book with CLI and Discord as the human entry surfaces.
- **a6** — No authentic mining corpus was evaluated, so effectiveness and proposal quality remain acceptance obligations rather than demonstrated results.
- **a7** — Book ownership defaults to the factory operator, with a required accountable role on every standard and explicitly designated maintainers for edits.
- **a8** — Thirty days and bounded evidence counts are proposed defaults rather than owner-specified definitions of recent.
- **a9** — The preset times, opt-in enablement, UTC default, overlap and catch-up policies are shaping proposals rather than owner-specified settings.
- **a10** — Treat the owner's continued-work answer as allowing unavailable-service continuation with explicit provenance while preserving capture integrity checks.
- **a11** — Interpret issue-tracker reachability as a restriction on the human control and reporting surface, preserving read-only issue evidence and authorized factory-agent MCP access.
- **a12** — Discord retains its configured single-owner and allowed-channel authorization while adding standards-specific control rather than assuming general approvals already exist.
- **a13** — Interpret create one as assisted creation of an individual standard inside the book, with category proposals allowed in the same change.
- **a14** — The owner's hashing question requests explanation rather than revoking the earlier explicit hash-approval requirement, so retain the simple hash recommendation for review.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
