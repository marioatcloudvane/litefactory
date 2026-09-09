# Plan Brief — Token and Model Metering

- Feature: `we-need-to-add-more-information-about-a`
- Branch: `lf/73-we-need-to-add-more-information-about-a`
- Date: 2026-09-08; run 0008; managed generation 1
- Parent issue: [#73](https://github.com/marioatcloudvane/litefactory/issues/73)
- Citation base: `63e977a98842c19cd49e5d80329a24c306c40d15`
- Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)
- Supporting design: [feature-spec.md](feature-spec.md), [ARCH_REVIEW.md](ARCH_REVIEW.md)

## Shared Decisions

- V1 must support complete descendant trees and observed per-model consumption for both Claude and Codex. Aggregate-only results cannot fulfill the requirement. Exact binary versions and collection semantics are not yet certified; implementation must prove the chosen paths before claiming support.
- Meter tracked main mode runs, all execution attempts and all descendants. Assumption: daemon classification/proposal helper overhead remains excluded and labelled. Use `meter` as the command name.
- Extend existing `internal/agent` contracts and existing provider runners. Vendor parsing belongs in `internal/harness` and `internal/codex`; orchestration in `internal/turn`; SQL and migrations exclusively in leaf `internal/store`; views in `internal/cli` and `internal/report`. Store owns persistence DTOs, mapped by callers. Any `internal/meter` is stdlib-only pure arithmetic/value logic, not an additional Runner or service.
- Stable attempt/execution/parent/source identities, source version and observed model are distinct from configured model. Native counters, category availability, scope, inclusion semantics and collection provenance survive normalization. Unknown, unsupported, partial and complete are explicit; absent is never measured zero.
- Choose one exclusive accounting frontier. Root/model inclusive totals reconcile against it and never add a second charge. Cache subsets and reasoning subcounts must not overlap normalized categories. Cumulative counters require validated baselines/reset scope, including resumed sessions.
- Pin an immutable price-set revision and immutable membership at attempt allocation, including an explicit empty set. Edits append revisions eligible only for new/future attempts. Late observations use the original pin. Existing valuations and previously unpriced attempts never change, even when rates are first added during execution.
- Tokens remain useful without prices. Missing model/category/rate stays unpriced or partial, never free and never filled from Claude cost. Preserve exact decimal amounts and separate currencies; no conversion. Assumption: display half-even rounding at six decimal places; JSON token fields are numeric and exact amounts are decimal strings.
- Numeric incremental journaling retains accepted observations on failure; interrupted/unobserved remainder stays incomplete. Retry/fallback is a separate attempt, persisted before the prior result can be overwritten. Reporting and code-save success keep their existing meanings.
- Managed GitHub stories become the editable implementation authority after factory publication. `.litefactory-plan-proposal.json` is transient transport, never committed; no per-story Markdown files are created. This generation contains only create intents because no existing issue identities or base revisions were supplied.

## Workflow

MTR-1 establishes shared contracts. MTR-2 and MTR-3 can then proceed independently on Claude and Codex acquisition. Their exact-binary scratch probes are mandatory capability gates, before claiming the collectors complete. MTR-4 integrates their outputs into durable attempt accounting; MTR-5 adds editable immutable pricing; MTR-6 adds the shared reporting surface. MTR-7 verifies the combined result. All seven belong to the same first release.

Dependency graph: MTR-1 → {MTR-2, MTR-3}; {MTR-1, MTR-2, MTR-3} → MTR-4 → MTR-5; {MTR-4, MTR-5} → MTR-6; {MTR-2, MTR-3, MTR-4, MTR-5, MTR-6} → MTR-7.

Each story must pass its own review, tests and supplied per-run evidence event immediately upon completion. Evidence names are the exact leading AC identifiers in its frozen GitHub body. Final aggregate gates remain required and do not replace per-story verification. A source capability failure is a blocking finding, not permission to ship incomplete subagent support.

## Cross-Story Constraints

- Preserve Go 1.26, existing module/dependency policy, numbered embedded migrations, explicit migration invocation, ctx-first store access and the one-connection SQLite correctness invariant.
- Preserve worktree, stdin prompts, allowed tools, sandbox/approval policy, credential isolation, frozen story context, cancellation and process-tree shutdown across provider changes. No permission bypass, arbitrary environment inheritance or telemetry listener is authorized.
- Numeric ledgers contain counters and provenance only, never prompts, responses, tool contents or credentials. Collect only active-session-owned metadata with path/symlink isolation, never unrelated sessions or historical logs.
- No database transaction crosses filesystem/process/git/forge I/O. Turn owns an attempt; daemon owns time/backoff. Bound reader/collector lifetimes and cancellation; no lower-layer sleep/retry loops. Clone shared nested values across concurrent boundaries.
- Preserve report post-before-final-state ordering and existing save-success predicates; measurement journaling is separate execution persistence. Renderers consume summary values and do not read databases or logs.
- Validation uses offline replay/process fakes and real migrated SQLite plus existing required checks. Exact-version compatibility probes run separately in external scratch space, with only minimal sanitized fixtures/provenance retained. Required final gates: `make check`, `go build ./...`; no invented integration tags, live-agent committed tests, dist writes or daemon restart.
- Agents do not read/mutate forge state, commit or push. Factory publication supplies identities and freezes story revisions; implementation must consume that supplied bundle.

## Story Index

| ID | Title | Sub-issue |
|---|---|---|

## Publication Status

The complete seven-story version-1 proposal is ready for factory publication for parent `github:marioatcloudvane/litefactory#73`, generation 1. Issue identities were not supplied, so this index deliberately has no invented links or local story references. The factory replaces it with actual GitHub sub-issue links after publication and verification. The pending link state must resolve before Implement consumes the plan.

## Out of Scope

- Historical log rollup, backfill/import commands, automatic historical scans and any in-repository script for that work. The owner wants a separately commissioned outside-repository script; none is created in this run.
- Automatic or manual historical repricing, vendor-cost substitution, invoice/subscription allocation, currency conversion and web price ingestion.
- Helper-call overhead and coding agents beyond Claude/Codex. Exclusions must remain visible rather than being presented as all daemon consumption.
- Production implementation or binary compatibility experiments in this Plan run.

## Provenance

- Citation base: `63e977a98842c19cd49e5d80329a24c306c40d15`; same-session research and architectural review provide repository/source findings.
- Owner requirements in run 0008 override earlier deferral: full subagent support in V1; no historical recalculation; historical rollup outside this repository.
- Known uncertainty: exact deployed Claude stream/transcript and Codex app-server/session metadata capabilities are not yet experimentally certified. The design is approved with mandatory implementation release gates, not a guarantee those versions expose all required fields.
- Re-pin binary schemas and request/model/descendant inclusion semantics before certification or after binary changes. Recheck repository boundaries if implementation starts from a newer revision.
