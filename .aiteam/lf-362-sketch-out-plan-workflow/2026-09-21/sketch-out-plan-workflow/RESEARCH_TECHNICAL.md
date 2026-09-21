# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

At repository revision 21f4d1771133983c4fd97e68bfd683611c190331, current Plan sequences requirements-engineer, go-architect, then proxy-product-owner. Requirements already include acceptance criteria; final decomposition adds measurable criteria, tasks and dependencies. This differs from story-first planning.

Evidence:

- `teams/source/agents/shared/plan-orchestrator.md:11`
- `teams/source/agents/shared/requirements-engineer.md:11`
- `teams/source/agents/shared/proxy-product-owner.md:11`

## f2

The shipped Research graph separates investigation and shaping, with stage-local named outputs, question/review waits, failure reporting and explicit owner approval. It provides the structural template. Plan graph production registration is explicitly absent in current documentation; second-mode support is exercised by offline fixtures.

Evidence:

- `internal/teamsource/research/research.dot:16`
- `docs/workflow/research-graph.md:10`
- `internal/graphexec/shared_policy_test.go:12`

## f3

Generic lf_agent, lf_if, lf_loop, lf_comment and lf_ask are mode-admitted mechanisms. WorkflowPolicy supplies domain decoding, authorization, artifacts, presentation and effects. Generic action spelling alone does not authorize Plan behavior. Research-only brief generation is not a Plan document writer.

Evidence:

- `internal/graphexec/registry.go:37`
- `internal/turn/workflow_policy.go:13`
- `internal/turn/research_workflow.go:39`
- `docs/workflow/research-graph.md:22`

## f4

Completed typed stories require user story, acceptance criteria, tasks, architecture and dependencies. Validation requires at least one criterion and task. Story-only phase output therefore cannot be submitted as a completed typed Plan. Existing discussion persistence saves scope, decisions, assumptions and questions, rather than dedicated structured stage/story drafts.

Evidence:

- `internal/planbrief/typed_content.go:36`
- `internal/planbrief/typed_content.go:196`
- `internal/turn/typed_plan.go:166`

## f5

Existing architect guidance covers reuse, boundaries, interfaces, invariants, dependencies and test seams. It can inform the technical role, but currently expects a feature specification. Current planning-contracts requires complete stories, so applying it unchanged to story-only shaping would conflict with the owner's requirement.

Evidence:

- `teams/source/agents/go-team/go-architect.md:11`
- `teams/source/skills/planning-contracts/SKILL.md:17`

## f6

Research authorization binds approval to an authenticated owner reply, source stage and displayed unchanged candidate. Graph waits release the worker and correlated replies resume their source. These are existing guardrails to preserve in a Plan policy; they do not make new Plan approval rules automatic.

Evidence:

- `internal/turn/research_owner_resume.go:64`
- `docs/workflow/research-graph.md:35`

## f7

Current Plan already has discussion versus completion, current-base publication checks, link-only index replacement and readiness gates. Project constraints forbid per-story Markdown and require readiness invalidation on authorized Plan iterations. New conversational stages must compose with these owners, not bypass them.

Evidence:

- `internal/turn/plan_handoff.go:1`
- `.aiteam/APP_CONSTRAINTS.md:209`
- `.aiteam/APP_CONSTRAINTS.md:222`

## f8

Inference: staged review can reduce premature technical commitment and separate user intent from feasibility and proof. It also adds review round trips. Missing design rules are upstream-change invalidation, durable draft identity, final task/criterion assembly, bounded conversation recovery and separation of criterion agreement from implementation acceptance. The advisory data artifact records a DOT skeleton, role/skill layout and these open points.

Evidence:

- `.aiteam/lf-362-sketch-out-plan-workflow/2026-09-21/sketch-out-plan-workflow/PLAN_WORKFLOW_DRAFT.md:1`
- `internal/planbrief/typed_content.go:245`
- `internal/turn/research_owner_resume.go:108`

## f9

At revision 739ebf47c6392161eb63f4779028e5d977fe010f, the graph registry admits five generic activity names plus mode-prefixed capabilities, so lf_plan_story fits current spelling admission while lf_story does not; spelling admission alone does not register behavior.

Evidence:

- `internal/graphexec/registry.go:37`

## f10

Existing forge code creates and attaches story sub-issues, exposes unattached partial outcomes for recovery, and updates issue title/body with documented fresh-base and read-back requirements; these are reusable operations rather than a first-loop draft graph contract.

Evidence:

- `internal/github/subissues.go:118`
- `internal/github/subissues.go:289`
- `internal/github/managed_issues.go:33`

## f11

The current managed story state helper verifies numeric issue and parent identity but closes stories with reason completed; retiring an irrelevant planning story requires different semantics.

Evidence:

- `internal/github/story_state.go:9`
- `internal/github/story_state.go:34`

## f12

Current Plan handoff returns from discussion without publishing a proposal, publishes complete plans through the existing publication owner, and only later marks readiness; early incomplete sub-issue publication therefore requires a separate admitted draft contract.

Evidence:

- `internal/turn/plan_handoff.go:141`
- `internal/turn/plan_handoff.go:245`
- `internal/turn/plan_handoff.go:264`
