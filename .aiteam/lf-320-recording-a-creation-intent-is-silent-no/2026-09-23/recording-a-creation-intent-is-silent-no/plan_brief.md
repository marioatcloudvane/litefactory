# Creation approval feedback plan

- Feature: `recording-a-creation-intent-is-silent-no`
- Branch: `lf/320-recording-a-creation-intent-is-silent-no`
- Session date: 2026-09-23
- Parent: https://github.com/marioatcloudvane/litefactory/issues/320
- Citation base: `208e5990bebb44cffeece025bc0eb8e1ae49c9df`

## Shared Decisions

- Owner selected option A: truthful feedback and guidance; execution and publication authority remain unchanged.
- Share pure presentation guidance across proposal and acceptance replies. Caller-supplied descriptive context distinguishes eligible legacy Research, graph-unavailable and unavailable/unknown contexts; unknown never implies eligibility.
- Approval alone starts no publication. Eligible legacy Research requires a further ordinary comment; graph Research cannot publish, and another ordinary comment does not unlock that ability.
- The 30-minute approval lifetime begins at the trusted owner-comment timestamp. Renewal needs a fresh approval; proposal reuse remains conditional on currency and eligibility.
- Future instructions stay conditional so retained feedback does not promise current grant usability or eventual publication.

## Workflow

Ship proposal guidance first, then durable acceptance acknowledgment. The second story depends on the first story's shared guidance. Acceptance content and recovery ship together.

Plan does not authorize Implement. The factory publishes stories, fills the links below and establishes readiness only after terminal processing and save. Implement records executed evidence; acceptance remains a separate workflow decision.

## Cross-Story Constraints

- Preserve authentication, exact consent matching, source/mode checks, expiry enforcement, rejection explanations and graph/legacy publishing boundaries.
- Reuse existing authority and delivery mechanisms. Guidance discovery never prepares or launches a run or binds a grant.
- Keep report rendering free of store/turn imports, SQL in store, and retry time in daemon. Preserve redaction, trusted source identity and the SQLite single-connection invariant.
- Evidence is scoped to the retained implementation snapshot. Ordinary renderer and real SQLite/local forge tests prove offline behavior, not deployed/live or nested-agent capability.
- Implement runs `make check`; no integration build tags, daemon changes, `make build`, writes to `dist/`, commits or pushes by agents.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| creation-proposal-guidance | Explain approval timing and publishing limits in creation proposals | [#375](https://github.com/marioatcloudvane/litefactory/issues/375) |
| creation-approval-acknowledgment | Reliably acknowledge recorded creation approvals through retries and restarts | [#376](https://github.com/marioatcloudvane/litefactory/issues/376) |

## Out of Scope

Automatic execution or publication, graph publishing enablement, mode switching, approval grammar/lifetime/permission changes, scheduled expiry reminders, historical acknowledgment backfill, proposal drafting redesign, general rejection redesign and protected instruction-tree changes.

## Provenance

Scope follows the retained research and the owner's option-A selection, confirmed by the request to plan. No product questions remain. Recorded assumptions are reuse of current authority/delivery primitives, upfront rather than scheduled expiry guidance and no backfill of consumed approvals.

Current architecture inspection uses citation base `208e5990bebb44cffeece025bc0eb8e1ae49c9df`; earlier research used `21b40106fdbc51bbf5fade401067e98b1c76c8f9` and remains historical. Re-pin source references if the implementation base changes. Live daemon revision and GitHub behavior remain unverified; this Plan executed no implementation tests.

The referenced `docs/story-contract-instruction-manifest.md` and `docs/decisions/nested-agent-evidence.md` are absent. Supplied factory/skill contracts govern serialization and evidence. No authentic nested-agent evidence obligation is introduced by this feature.
