# graph-plan-wedges-the-whole-parent

- Branch: `lf/366-graph-plan-wedges-the-whole-parent`
- Session date: 2026-09-23
- Parent: [#366](https://github.com/marioatcloudvane/litefactory/issues/366)
- Citation base: `40fa12605a4c5bb5b366a6e2806add039c7cab40` (retained research); architecture checked the current supplied worktree.
- Status: completed typed proposal prepared; publication, links, save and readiness remain factory-owned and pending.

## Shared Decisions

- GitHub controls verified child lifecycle; lifecycle, Plan approval, retirement authority and effect settlement remain separate facts. External closure never manufactures engineering or acceptance evidence.
- Story-specific conditions are retained outcomes and holds. Shared storage, parent identity/authority and repository-wide API failures retain ordinary failure/backoff behavior.
- Reconciliation observes current remote facts before replay and preserves original request identity and authority. Ambiguous historical ordering remains a story-local hold.
- Daemon owns eligible scheduling and bounded recovery; turn owns reconciliation; pure Plan policy owns membership/dependencies/readiness; store owns all SQL and guarded durable completion.
- One shared active-membership policy governs selection, assembly, publication and readiness. Inactive stories retain unresolved debt; independent valid approvals survive, but changed active scope needs renewed assembled review.

## Workflow

Deliver lifecycle adoption first, then independent durable recovery, then reporting/reply handling and whole-path verification. The dependency chain is `adopt-child-lifecycle` → `recover-story-effects` → `report-and-verify-plan-progress`; the final story also depends directly on lifecycle adoption. Only factory publication and successful save establish readiness. An ordinary reply keeps Plan selected; after readiness the owner may request `/implement this plan`.

## Cross-Story Constraints

- Preserve existing persisted workflow/content versions, frozen-scope ownership and independent evidence requirements.
- Keep SQL in store, the single SQLite connection, context-first accessors, embedded additive migrations and expected-revision protection. No transaction spans remote I/O.
- Reuse current package and parent-ownership boundaries; no new worker pool, scheduler, background synchronization service or turn clock.
- Tests use existing ordinary Go fixtures with real SQLite and controlled HTTP when proving persistence/discovery. No integration tags, synthetic live-runtime claims, deployment or nested-agent execution. Implement must run `make check` with pinned lint and race tests; no `make build` or writes to `dist/`.
- Evidence inventories live in each typed story. All implementation and execution prerequisites remain unsatisfied in this Plan run.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| adopt-child-lifecycle | Adopt GitHub child lifecycle without blocking independent Plan stories | [#372](https://github.com/marioatcloudvane/litefactory/issues/372) |
| recover-story-effects | Recover interrupted story effects independently and preserve retirement authority | [#373](https://github.com/marioatcloudvane/litefactory/issues/373) |
| report-and-verify-plan-progress | Explain story holds and verify complete Plan progress across restart | [#374](https://github.com/marioatcloudvane/litefactory/issues/374) |

## Out of Scope

General GitHub synchronization, arbitrary parent-closure behavior, graph kill switches, legacy dispatch removal, promotion-edge cleanup, new UI/API/commands, automatic reopening/moving/rebinding, blind replacement creation, deployment, daemon restart and manual store repair.

## Provenance

Owner `/plan this` continues the settled GitHub-source-of-truth scope and withdraws the earlier reopen-first question. No remaining material owner question. Existing polling is assumed to provide eligible passes; no progress is promised during shared service outages. Historical missing authority/ordering can leave a local hold without blocking independent work.

Retained research is source analysis at the citation base; architectural review checked present owners and fixtures. Implementation must recheck current APIs and revisions before changing them. No tests, forge reads or deployment observations were performed in Plan. Supplied session lacked saved PLAN_CONTEXT.json; retained research and owner history supplied continuation. Referenced instruction-manifest and nested-agent-evidence documents are absent; supplied factory contracts and available workflow rules govern. These documentation gaps do not expand scope.
