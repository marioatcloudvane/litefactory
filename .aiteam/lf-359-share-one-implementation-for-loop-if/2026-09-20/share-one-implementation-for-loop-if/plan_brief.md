# Plan: share-one-implementation-for-loop-if

- Branch: `lf/359-share-one-implementation-for-loop-if`
- Date: 2026-09-20
- Parent: [#359](https://github.com/marioatcloudvane/litefactory/issues/359)
- Citation base: `8fb96c32aab20f7ac5c245139448236d44b8aea1`
- Status: completed proposal; factory publication, save and readiness are pending.

## Shared Decisions

- Reuse existing packages and records. One registry per admitted mode supplies explicit authorization and a small static policy; no new framework or dynamic plugin registry.
- Shared mechanics use `lf_agent`, `lf_if`, `lf_loop`, `lf_comment` and `lf_ask`. Domain effects can remain scoped. Research owns its result vocabulary, validation and presentation.
- Keep captured role permissions, candidate/attempt identity, durable delivery and authenticated owner approval authoritative. Stage completion never creates approval.
- Convert Research completely. Generic action renaming changes the definition digest; old captures are unsupported, without aliases, migration or data deletion.

## Workflow

Deliver US-001, then US-002. Both are required for feature completion. Each includes implementation and verification; the second exercises the assembled final behavior.

## Cross-Story Constraints

- Keep production activation defaults and non-graph behavior unchanged.
- SQL remains in `internal/store`; retain physical tables, the single SQLite connection and current ownership/concurrency rules.
- No protected instruction-tree edits are identified. Captured Research assets and shared documentation are the intended instruction surface.
- Use normal repository tests with local durable fixtures and injected external runners/transports. No integration tags or special integration-test names.
- Run `make check` and confirm empty `gofmt -l .` during implementation. Planning executed no tests and certifies no authentic provider operation.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Share workflow mechanics and convert Research to generic actions | [#360](https://github.com/marioatcloudvane/litefactory/issues/360) |
| US-002 | Make Research brief generation explicit and prove workflow reuse | [#361](https://github.com/marioatcloudvane/litefactory/issues/361) |

## Assumptions and Risks

The approved scope is sufficient; no owner questions remain. Existing result envelopes and persisted kinds remain suitable. This is a modest refactor, with no hours estimate: explicit mode authorization, document-validation ordering and retained effect identities are the main risks.

## Out of Scope

Plan DOT/design/publication, other production mode conversions, backward compatibility, automatic data deletion, default activation, arbitrary uploaded workflows, executable graph expressions, dynamic plugins, a general document framework and live-provider certification.

## Provenance

The retained Research brief and latest owner corrections define scope; `feature-spec.md` and `ARCH_REVIEW.md` record requirements and current-source findings at the citation base. Typed stories in the submitted contribution become authoritative only through factory publication. Recheck source citations if the implementation base moves.

The local skill references `docs/decisions/nested-agent-evidence.md` and `docs/story-contract-instruction-manifest.md` are absent. Available workflow, test-evidence and Research graph documentation supply the recorded evidence boundary. This plan neither satisfies nor retires unrelated authentic host-observation obligations.
