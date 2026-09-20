# Plan: wire-graph-based-execution-into-plan

- Branch: `lf/318-wire-graph-based-execution-into-plan`
- Session date: 2026-09-20
- Parent: https://github.com/marioatcloudvane/litefactory/issues/318
- Citation base: `5e627cff723dbe5fb722aee05c3f7924a1caab6e` (dispatcher-confirmed current HEAD).

## Shared Decisions

Use the same application-owned layout as Research: an embedded mode DOT, stage-role Markdown, and `skills/<name>/SKILL.md` with supporting Markdown. Add the Plan package beside Research, reusing graph execution, typed drafts, durable owner replies and the existing publisher. No new engine, service, catalog or installed instruction-tree changes are planned.

Owner decisions remain settled: stories → engineering → acceptance; each review covers the whole set; meaningful changes reopen the earliest affected stage and all later stages. Acceptance approval authorizes complete issue creation after the consistency check. Final approval follows on the confirmed published issues. Implementation still requires explicit owner mode selection.

Keep one current set revision, stable story identities, retained history and authenticated revision-bound approvals. Agents propose content; the factory owns approvals, publication and readiness. Renames preserve identity; superseded stories retain historical identities and links outside the active set. Partial drafts cannot reach completed publication.

## Workflow

Ship in dependency order: S1 → S2 → S3. S1 provides the captured package, S2 connects owner review and retained draft state, and S3 connects checked publication and final approval. The feature remains explicitly opt-in until all slices and required evidence are complete; intermediate deliveries do not promise a usable end-to-end Plan graph.

## Cross-Story Constraints

Preserve legacy execution and persisted workflow/content versions. New graph executions capture immutable definitions; resume uses retained bytes. A graph never selects another mode. Keep bounded attempts and durable waits within the existing runtime, without new polling services.

SQL stays in `internal/store`, migrations are embedded SQL and the one-connection pool remains unchanged. The daemon owns time; turn owns one invocation and effect coordination. Factory permissions, redaction, result/helper contracts and existing publication authority remain intact. No live-daemon mutation or `dist/` writes.

Use ordinary Go tests without integration tags. Keep remote/provider effects deterministic in automated checks. Implementation must run `make check`; this Plan run executes no engineering checks. Offline tests establish source behavior only. Authentic host observations remain separately authorized prerequisites to default activation, with unavailable evidence explicitly unverified. Do not launch nested paid agents to manufacture observations.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| S1 | Ship a Plan workflow package matching Research | [#356](https://github.com/marioatcloudvane/litefactory/issues/356) |
| S2 | Shape and approve stories through three durable Plan reviews | [#357](https://github.com/marioatcloudvane/litefactory/issues/357) |
| S3 | Publish reviewed stories and approve the confirmed result | [#358](https://github.com/marioatcloudvane/litefactory/issues/358) |

## Out of Scope

Partial issue creation, per-story stage progression, a replacement publisher or graph engine, installed instruction-tree regeneration, silent legacy migration or retirement, automatic mode changes, changes to Implement/Validate testing responsibilities, visual editing, Temporal, marketplace and fleet management.

## Provenance

Reuses this session's approved `RESEARCH_BRIEF.md`, `feature-spec.md` and source-inspected `ARCH_REVIEW.md`; q1=b, q2=a and q3=a remain binding. No owner questions remain. Opt-in admission and no installed-tree edits are recorded implementation assumptions.

Current Research assets, graph runtime and publisher were inspected for architecture. Historical lookup/operator prose may lag current sources. Recheck changed interfaces against the implementation base before coding; retain the agreed behavior. The referenced nested-agent decision and story-contract manifest files are absent in this checkout; current planning/workflow contracts and Research activation guidance were used. No executed tests, remote story publication or authentic host observations are claimed by these artifacts.
