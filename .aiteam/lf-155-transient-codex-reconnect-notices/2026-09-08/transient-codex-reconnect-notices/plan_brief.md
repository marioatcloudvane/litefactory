# Plan Brief — transient-codex-reconnect-notices
# Branch: lf/155-transient-codex-reconnect-notices
# Date: 2026-09-08
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/155
# Citation base: 7696bde984311c3b23429b2a0c73f7130d5d6adf
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions

This generation contains one cohesive story. Its behavioral contract, acceptance criteria, tasks, and architecture belong exclusively to the managed story body; there are no additional cross-story design decisions.

GitHub issues are the sole editable story source. The factory publishes the version-1 generation-1 proposal and supplies frozen revisions and per-run acceptance evidence for implementation. No existing child identities or base revisions were supplied; the proposal requests creation and leaves identity assignment to the factory.

## Workflow

One story ships as a unit, without sibling dependencies. Factory publication and revision freezing precede implementation. The completed story must pass its acceptance checks, review, and tests and submit supplied evidence; final aggregate gates remain required. This run produces a plan only.

## Cross-Story Constraints

Repository constraints and supplied worktree boundaries remain in force. Agents do not access the forge, commit, push, modify runtime state, or manage the live daemon. The root proposal is transient factory transport, excluded from commits by the existing git layer and removed by the successful publication handoff.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Preserve successful Codex outcomes after recoverable stream errors | [#156](https://github.com/marioatcloudvane/litefactory/issues/156) |

## Publication Status

Proposal prepared for factory publication. The factory replaces the Story Index with real numeric GitHub issue links after publishing and verifying the proposal; no child links exist yet in this planning run.

## Out of Scope

- TLS trust, proxy, VPN, or certificate troubleshooting and configuration.
- Recovery or publication of historical worktrees.
- Additional retries, retry budgets, provider lifecycle redesign, or multi-turn terminal-state redesign.
- New warning UI/API, persisted schemas, dependencies, or public contracts.

## Provenance

- Citation base: 7696bde984311c3b23429b2a0c73f7130d5d6adf.
- Requirements: [feature-spec.md](feature-spec.md); architecture: [ARCH_REVIEW.md](ARCH_REVIEW.md).
- Research provides versioned upstream evidence, not verification of the historical incident binary or TLS cause.
- Before implementation, use the factory frozen story revision and recheck cited repository locations against that checkout. Recheck provider event semantics when changing supported Codex versions.
- Headless assumption: proceed with the research recommendation and record conservative diagnostic/terminal semantics in the story; no unanswered owner decision blocks this plan.
