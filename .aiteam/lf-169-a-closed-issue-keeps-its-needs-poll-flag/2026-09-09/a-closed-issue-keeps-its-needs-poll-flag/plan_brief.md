# Implementation Plan
# Feature: a-closed-issue-keeps-its-needs-poll-flag
# Parent: github:marioatcloudvane/litefactory#169
# Created: 2026-09-09
# Status: Plan complete; factory publication pending

## Shared Context

Confirmed closed issues can retain ordinary persisted polling intent indefinitely. This plan retires that intent at the existing authoritative closed-issue observation while preserving retry behavior on uncertain observations, session history, comment watermarks and owed reports.

The managed generation-1 proposal is in the worktree-root `.litefactory-plan-proposal.json`, transient factory transport that must never be committed. It contains one complete create story. No existing child identities or revisions were supplied; the factory assigns them, publishes the story and replaces the index below. No per-story Markdown files are produced. Publication and subsequent implementation approval have not occurred in this run.

## Sources

- [Feature specification](feature-spec.md)
- [Architecture review](ARCH_REVIEW.md)
- [Research brief](RESEARCH_BRIEF.md)
- [Project constraints](../../../APP_CONSTRAINTS.md)

Research and transport findings were checked against source revision `c69fc425ceabe775d0987f6a592044983edbb958`. The current source already releases admission for closed skips and excludes them from dispatch count; the historical permanent slot-loss claim is not established for this revision.

## Scope and Assumptions

Authoritative closure retires ordinary retry intent. Failed or uncertain lookups retain backlog. Reopening resumes through existing normal discovery with retained watermarks; no immediate detection guarantee is added. Reporting obligations remain independent of ordinary backlog retirement.

One cohesive lifecycle change is sufficient. Request budgets, scheduler redesign, schedule closure policy, new abandonment states, deletion/access-loss policy, forced reopening scans, deployed-version incident reconciliation and runtime changes are outside scope.

## Architecture and Shared Constraints

The daemon owns closure policy at ordinary backlog materialisation; store owns marker persistence through the existing accessor. Existing forge lookup, admission cleanup, normal discovery and report reconciliation remain the integration boundaries. No migration, package, API, configuration or dependency addition is planned.

Preserve explicit closure classification, context/error propagation, in-flight/capacity ownership and SQLite's single-connection policy. Existing self-contained fake HTTP/temporary SQLite/executor fixtures supply validation; no new infrastructure or integration tags are required. Detailed binding architecture and verification work live in the managed story body.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Retire ordinary polling backlog after confirmed issue closure | [#179](https://github.com/marioatcloudvane/litefactory/issues/179) |

## Delivery and Validation

Implement must consume the factory-frozen published story revision and verify each criterion using its exact evidence identifier. Scoped tests and review precede the supplied per-run evidence event; the factory alone mutates forge state. Aggregate gates remain `make check` and `go test -race -cover ./...`.

This Plan run validates proposal structure and consistency only; it does not implement, execute product tests, publish stories, commit or push.

## Risks and Decisions

Clearing failure must remain visible and retryable, with the persisted row still readable for verification. Tests must distinguish discovery reads from direct backlog reads and preserve owed reports across closure. Existing listing/closure races remain unchanged.

The handoff is ready for factory publication. Implementation is a separate mode.

