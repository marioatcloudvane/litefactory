# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

At 5ecc55e64fea7b26aea94134ce1de7b1890c91f8, story-worker is balanced in base source and all four 1.0.0 teams. PR #1294 merged as 9aa6922d19ff340df3f660bd65813e2e64577b69 partially reverted #1293. Today's unmodified Implement worker therefore uses balanced for every size. This replaces the previous all-flagship/no-op finding.

Evidence:

- `teams/source/base-team/implement/story-worker.md:4`
- `teams/go-development/v1.0.0/implement/story-worker.md:4`

## f2

Validate acceptance is now balanced; code, architecture, repair and reporter are flagship. Reviewer routing is explicitly excluded by the owner.

Evidence:

- `teams/source/base-team/validate/validate-acceptance.md:4`
- `teams/source/base-team/validate/validate-code.md:4`

## f3

Captured node lf_model_tier overrides the role tier. Delivery resolves that tier through local provider model configuration and populates both MainModel and specialist tier/model evidence. Allowed tiers remain flagship, balanced and fast. End-to-end persisted provenance was not exercised.

Evidence:

- `internal/teamsource/research.go:241`
- `internal/turn/research_delivery.go:39`
- `internal/turn/research_delivery.go:66`
- `internal/turn/delivery.go:88`
- `internal/graphexec/invoke_agent.go:34`

## f4

Control schema and decoding admit story.size, story.sizing_valid and story.fallback only in Plan; other modes allow response only. Plan's fact evaluator is existing reusable behavior, but its current mode boundary excludes Implement.

Evidence:

- `internal/graphexec/research_control.go:52`
- `internal/graphexec/research_control.go:87`
- `internal/turn/plan_parallel.go:33`

## f5

Implement graph validation still rejects lf_if through its action whitelist. Its control response vocabulary is completed/incomplete/blocked.

Evidence:

- `internal/graphexec/implement.go:65`
- `internal/graphexec/implement.go:190`

## f6

Implement validation requires exactly one non-resolver worker. Worker success must reach its own lf_source result node; worker sync success paths must reach the same worker and incoming paths must pass sync. Any branch design must preserve these safety properties while relaxing the single-worker shape.

Evidence:

- `internal/graphexec/implement.go:247`
- `internal/graphexec/implement.go:300`
- `internal/graphexec/implement.go:467`
- `internal/graphexec/implement.go:485`

## f7

Sizing lives in Plan's story-set entry. Implement runtime and handoff use the worker ledger and approved story revisions, without loading Plan sizing facts. The worker runtime already knows parent, generation and story key, which can identify authoritative saved sizing; generation/revision compatibility needs design attention. No size field exists in the inspected Implement/managed/approved story records.

Evidence:

- `internal/turn/plan_parallel.go:33`
- `internal/turn/implement_graph.go:37`
- `internal/turn/implement_steps.go:628`
- `internal/store/implement_stories.go:38`
- `internal/store/managed_stories.go:29`

## f8

Plan growth records fallback and returns to the normal Plan route without changing the size. No Implement resizing path was found in the inspected Implement runtime. The owner excludes introducing mid-run resizing.

Evidence:

- `internal/planbrief/plan_parallel.go:321`
- `internal/turn/implement_graph.go:37`

## f9

Impact: likely touched are base Implement DOT and generated four 1.0.0 team graphs/manifest, factory graph condition/check policy, Implement fact activities and their tests. Dependents include team capture/delivery, daemon dispatch capacity and saved graph replay; shared control changes can affect Plan. Existing unbranched worker routing is superseded for newly captured updated graphs; no role retirement is needed. Plan already persists size, so no new data migration is established by this pass. Old plans intentionally lack sizing; retained graph/capture compatibility and the authoritative generation lookup remain shaping checks. Recent #1268 extracted generic execution, leaving factory mode policies under internal; keep this policy out of the neutral library.

Evidence:

- `teams/source/base-team/implement/implement.dot:59`
- `teams/teams.json:1`
- `internal/turn/step_capacity.go:98`
- `internal/turn/implement_graph.go:37`
- `internal/graphexec/research_control.go:52`

## f10

The inspected result and capacity paths do not hard-code one worker name: retainedAnswer passes lf_source to latestResearchAgentAttempt, which filters the named agent; GraphDispatchSlotNeed computes costs for dispatch.Node from the retained profile. Handoff uses session/story identity, not node name. This supports multiple mutually exclusive workers in principle, but does not prove a branched graph works end to end.

Evidence:

- `internal/turn/implement_steps.go:203`
- `internal/turn/research_comments.go:291`
- `internal/turn/step_capacity.go:83`
- `internal/turn/implement_steps.go:628`

## f11

Plan's sizing_valid requires S/M/L plus a nonblank, valid UTF-8, single-line reason of at most 280 characters; a mismatched parallel route version also invalidates it. Size alone is therefore not the existing validity rule. Old Plan sizing requirements intentionally allow unsized records.

Evidence:

- `internal/turn/plan_parallel.go:33`
- `.aiteam/lf-1226-plan-size-every-story-s-m-l-in-the-plan/2026-10-26/plan-size-every-story-s-m-l-in-the-plan/RESEARCH_BRIEF.md:1`

## f12

The current worker node binds story-worker role/standards, implement-result and stack skill, slot budget 3 and three node attempts. The story ledger owns repair counting. Routing must not accidentally add repair attempts or bypass synchronization. This pass used code, saved briefs, git history and GitHub issue/PR reads; no tests, live model invocation, cost measurement or manual L-story acceptance ran.

Evidence:

- `teams/source/base-team/implement/implement.dot:59`
- `internal/turn/implement_policy.go:26`

## f13

Rechecked at 527f975df19eddae480acce7e9fdfe6acdc628b2: base story-worker remains balanced and the current graph still has one worker with role/standards/skills and a dedicated result source; this confirms f1's current-tier conclusion at the newer checkout.

Evidence:

- `teams/source/base-team/implement/story-worker.md:4`
- `teams/source/base-team/implement/implement.dot:59`

## f14

PlanState contains story-set sizing and revision markers but no generation field; Implement handoff identifies the worker by session and reads approved revisions by parent/generation/key. Therefore simply reading the latest parent PlanState is not sufficient evidence of an approved generation match. The scope requires trustworthy binding and stable retained routing facts; the storage representation is a Plan design choice.

Evidence:

- `internal/planbrief/plan_workflow.go:18`
- `internal/turn/implement_steps.go:628`
- `internal/store/managed_stories.go:18`

## f15

Captured Implement admission pins definition and content/topology digests; resume verifies the running instance's retained definition. Compatibility scope therefore preserves existing captures while applying routing to newly captured updated definitions. No capture migration is included.

Evidence:

- `internal/turn/implement_graph.go:190`

## f16

Shaping impact extends f9: optional durable sizing metadata and its generation/revision binding are in scope if needed for stable routing; affected interfaces are factory graph conditions, validation and internal persisted facts, with no new user command, external API or permission grant. Existing worker contracts and other role behavior remain unchanged. The source graph is the shared behavior definition; no new prose guidance is needed. Good behavior: a valid M inherits its captured role tier while an unsized story selects flagship; bad behavior: all missing sizes silently use balanced or a repair switches sizing basis. No live transcript or model run was added; existing saved research and code supply the evidence.

Evidence:

- `teams/source/base-team/implement/implement.dot:59`
- `internal/turn/plan_parallel.go:33`
- `internal/turn/implement_steps.go:628`
- `internal/store/managed_stories.go:29`
