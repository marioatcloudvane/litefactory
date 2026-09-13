# Adopt the shared communication contract and retire obsolete handoffs

Follow-up 2 of 3 from #258.

Depends on: **Define and implement the shared agent communication contract** (draft ISSUE_1.md; replace with its issue link when published).

## Problem and outcome

The shared contract must become the production path across modes while preserving current workflow authority and resumable work. Integrate the contract through existing transports, then retire superseded input adapters at an explicit compatibility boundary.

## Scope

- Wire Research, Plan, Implement, Validate and Spike to the shared service for final outcomes/comments, questions, artifact references and applicable workflow contributions.
- Adapt the CLI/helper and structured terminal-output paths without duplicating validation or receipt logic. Preserve Validate's restricted execution profile.
- Supply authoritative prior-result snapshots or scoped reads with stable IDs and base revisions for edits. Do not rely on prior prose or assumed context injection.
- Integrate final submission validation and actionable rejection handling. Define absent/malformed output after failure, cancellation and interruption; a finishing skill alone cannot enforce completion.
- Establish run-scoped capability lifetime for advisory progress, including Research/Spike, using the first issue's semantics and the existing interface.
- Map auxiliary classifier/scheduler/conflict callers to shared version/error conventions where useful, preserving their typed payloads.
- Update canonical agent instructions, rendered bundles, documentation and tests with the adapters.
- Track rollout and old-path removal as distinct milestones.

## Retirement requirements

Inventory the Plan v1 file handoff, Implement event-file ingress, Validate v1 assessment parser and production-unreachable Markdown story discovery separately. Remove dead code with its exclusive wiring, preserving shared live helpers.

Migrate or complete supported legacy scopes and drain pending effects before deleting required compatibility readers. Use supported upgrade/admission flows; do not rewrite persisted versions by hand. Existing content/submission upgrades preserve workflow v1, so combined verification retirement is a separate decision.

Preserve shared StoryProgress records/recovery/delivery, v2 internal Plan handoffs/publication, historical reads and migrations required by existing databases.

## Completion criteria

- [ ] Supported modes use the shared contract with equivalent identity, receipt, evidence and acceptance guarantees.
- [ ] Cold starts, continuation, sparse updates, retries, stale revisions, malformed/missing output and failures are verified.
- [ ] Relevant prior state reaches agents explicitly; complete merged results are validated.
- [ ] Validate remains restricted and can return its structured terminal assessment.
- [ ] Run-scoped progress cannot mark work accepted or completed.
- [ ] Every old input path has a verified disposition and compatibility prerequisites.
- [ ] Promised retirement is actually completed. If legacy work cannot drain, explicitly revise scope and create a focused retirement follow-up rather than silently declaring removal complete.

## Boundaries

No MCP server or MCP-specific business logic. Code/artifact contents, vendor stdout parsing, usage transcripts and process diagnostics need not be forced into the business-result schema.

Research and agreed boundaries: #258 and the session RESEARCH_BRIEF.md, runs 0003–0005.

