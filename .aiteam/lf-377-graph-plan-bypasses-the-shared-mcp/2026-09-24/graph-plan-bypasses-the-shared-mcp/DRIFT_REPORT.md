# Drift report

Verdict: DRIFTED — minor, additive.

Delivered as planned: all three stories (T-001..T-027), hold-back retained, rollout obligations remain pending in plan_brief.md and recorded in a comment beside AdmitPlanPath.

Additions and deviations (with reason):
- `agentresult.Context.PlanGraph` (trusted binding) distinguishes graph from legacy Plan payloads; the payload itself never selects the route.
- Failure routing: story-effect conflict/uncertain outcomes now go to the shipped `failed` node in plan.dot and the checker (needed by AC-010).
- Research runtime changed: retained result is now the only source (AC-037); existing Research tests submit through the shared route first.
- A graph Plan ask must carry at least one question at preflight/submit (found by the contract test author).
- Story-003 needed shared/store changes: dispatch chaining (`ParkGraphInstanceAfter`, `GraphTriggerDispatch`), per-attempt result identity for Plan (`Pass`), and `planbrief.Invalidate` no longer resets dependants that have not reached the invalidated stage (found by the independence test).
- Empty-scope graph Plan sessions now start at the set stage.
Deferred: owner-run tryout and hold-back lift (parent obligations). Follow-up planning: not required.
