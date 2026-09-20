# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Current Plan instructions invoke requirements engineering, architecture review, then product-owner decomposition into complete typed stories. They do not define mandatory owner approval gates between those stages. Runtime nevertheless already supports multi-turn discussion: discussion retains identity-bound working context and rejects a simultaneous publication proposal.

Evidence:

- `teams/source/agents/shared/plan-orchestrator.md:10` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_handoff.go:152` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/planbrief/outcome.go:30` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_continuation_test.go:135` @ `7d339edde1326fc509e0a5966d55db88ec77f274`

## f2

Graph foundations can express registered mode actions, bounded control flow and durable execution. The shipped Research DOT demonstrates separate stage-scoped loops, question/review waits and owner-approved advancement. The production activity registry, result context and invocation driver inspected are Research-specific; adding a Plan DOT alone cannot connect Plan execution.

Evidence:

- `internal/graphexec/registry.go:35` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/teamsource/research/research.dot:17` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/graphexec/research_control.go:29` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/research_workflow.go:23` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/research_admission.go:26` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/execute.go:623` @ `7d339edde1326fc509e0a5966d55db88ec77f274`

## f3

Research uses an embedded application-owned DOT/role/skill package and captures definition content for execution. Capture explicitly requires Research mode and ignores installed catalog overrides. Research admission preserves a legacy path and defaults to legacy in this checkout. Plan needs an explicit package, admission and compatibility integration rather than assuming existing team Markdown changes affect graph execution.

Evidence:

- `internal/teamsource/research.go:47` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/teamsource/research.go:117` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/research_admission.go:20` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/research_admission.go:69` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/research_admission.go:113` @ `7d339edde1326fc509e0a5966d55db88ec77f274`

## f4

Complete typed stories require user-story content, canonical acceptance criteria, tasks mapped to criteria, architecture and dependencies. A titles-and-description-only first-loop draft is not a publishable story under this contract. Early issue creation therefore requires an explicit partial-story lifecycle or changed publication contract; private discussion can avoid that expansion.

Evidence:

- `teams/source/skills/planning-contracts/SKILL.md:13` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `teams/source/skills/planning-contracts/SKILL.md:20` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_handoff.go:157` @ `7d339edde1326fc509e0a5966d55db88ec77f274`

## f5

Existing Plan publication is factory-owned and runs only after successful finalization with valid attempt-bound completed transport. It uses existing story publication/revision machinery and a link-only index. Plan readiness does not select Implement; ordinary replies continue Plan. Graph stage completion must be connected carefully so intermediate completed drafts cannot accidentally trigger this final publisher.

Evidence:

- `internal/turn/plan_handoff.go:113` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_handoff.go:147` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_handoff.go:275` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_publication.go:269` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `teams/source/skills/planning-contracts/SKILL.md:53` @ `7d339edde1326fc509e0a5966d55db88ec77f274`

## f6

Durable owner waits authenticate and retain replies, and require resumption at the originating agent. Research separates done from approved and uses stage-scoped response controls. These are reuse candidates, but they do not themselves define Plan draft revisions or dependency-aware invalidation. The existing Plan working context consists of identity plus prose scope, decisions, assumptions and questions, not three independently revisioned stage approvals.

Evidence:

- `internal/store/graph_owner_waits.go:57` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/store/graph_owner_waits.go:121` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/graphexec/research_control.go:29` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/planbrief/outcome.go:32` @ `7d339edde1326fc509e0a5966d55db88ec77f274`

## f7

Inference: engineering-before-detailed-acceptance is workable only if early problem statements give enough success intent and later acceptance can reopen design. Tasks currently reference canonical criteria, so any engineering-stage tasks must remain provisional until criteria and mappings are consistent. A final check should compare the current story, architecture, criteria, tasks and dependencies after any rewinds.

Evidence:

- `teams/source/skills/planning-contracts/SKILL.md:20` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `teams/source/skills/planning-contracts/SKILL.md:24` @ `7d339edde1326fc509e0a5966d55db88ec77f274`

## f8

Existing tests supply useful regression starting points for stage-specific responses, publication ordering, durable graph attempts and Plan discussion without publication. New Plan wiring still needs evidence for all three owner gates, rewind/invalidation, resume under pinned definitions, no intermediate issue creation, and replay-safe final publication. Existing offline coverage is not authentic host evidence.

Evidence:

- `internal/turn/research_shaping_graph_test.go:33` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/research_shaping_graph_test.go:50` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/store/graph_persistence_test.go:367` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_outcome_test.go:103` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
- `internal/turn/plan_continuation_test.go:135` @ `7d339edde1326fc509e0a5966d55db88ec77f274`
