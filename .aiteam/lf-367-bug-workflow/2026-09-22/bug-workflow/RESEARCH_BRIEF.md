# Research brief

## Request

Research #367: a lighter graph-driven bug lifecycle with clarification, Root Cause, Impact and Solution analysis, followed by code repair, unit-test updates and a graph-driven suite rerun; reuse #319 activities after that dependency is completed.

## Decisions

- **d1** — Owner-set direction: use the existing graph engine and keep the bug workflow lighter than the existing feature workflows.
- **d2** — Owner-set dependency: wait for #319 to be completed before implementing the activities-dependent bug workflow.
- **d3** — Owner-set content: analysis covers Root Cause, Impact and Solution; repair covers code, unit tests and a suite rerun, with clarification when the bug location is ambiguous.
- **d4** — Shaping decision: keep one versioned bug record instead of requiring a feature story breakdown.
- **d5** — Shaping decision: record engineering completion, validation, owner acceptance and delivery as separate outcomes.
- **d6** — Shaping decision: preserve evidence and unresolved decisions when the lightweight workflow cannot safely complete its agreed repair.


## Assumptions

- **a1** — Interpret '# intent per category' as three separately identified analysis sections produced by one analysis role, because the request specifies content rather than three agents.
- **a2** — Proposed lifecycle boundary is approved integration and confirmed issue closure, subject to q3; no current authorization to deliver is inferred.
- **a3** — Entry, analysis-to-repair authority and test-graph composition remain proposals pending q1, q2 and q4; the remaining scope rules are shaping defaults.
- **a4** — Dependency activity names and contracts from #319 remain provisional; verify delivered capabilities before adopting them, and expose unavailable prerequisites as blocked rather than substituting ad hoc execution.
- **a5** — The primary story is one reporter taking one defect from observed failure to a scoped repair with evidence, using the current issue, branch and repository context.
- **a6** — The bug record contains expected behavior, actual behavior, affected environment/version, reproduction steps and status, evidence references, root cause, trigger, preconditions, impact, severity rationale, remedy, complexity and observable success criteria.
- **a7** — The execution record contains analysis revision, owner answers, authorized scope revision, source revision, test recipe and environment, attempts and outcomes, remaining repair budget, workflow state and PR/integration references when applicable.
- **a8** — States distinguish analyzing, awaiting owner, ready for repair, repairing, checking, ready for review, blocked, delivered and stopped, with completion separately qualified by engineering, validation, acceptance and delivery.
- **a9** — The interface uses existing issue and launch surfaces for explicit selection, comments for the three analysis sections and progress/results, and durable owner replies for questions and approvals; no new dashboard or public API is required.
- **a10** — Only the recognized task owner can answer authorization gates; issue text and evidence cannot expand permissions, analysis is read-only apart from bounded reproduction activity, and repair is confined to the authorized task worktree and scope.
- **a11** — Before repair, require concrete expected versus actual behavior, a reproduced failure or equivalent existing failing evidence, a supported cause, a bounded remedy and observable success criteria; unsupported hypotheses remain labeled and block repair.
- **a12** — Use descriptive severity levels critical, high, medium and low with a consequence-based rationale, and complexity small, moderate or large with an explanation, as reporting fields rather than automatic authority.
- **a13** — The lightweight boundary permits localized corrections within existing behavior and excludes remedies requiring product decisions, architecture redesign, destructive migration or coordinated cross-repository release.
- **a14** — An unreproduced defect triggers a batched request for missing context and then remains incomplete if still unsupported; a broader remedy preserves the analysis and asks the owner how to continue without automatically switching modes.
- **a15** — Regression evidence must demonstrate the recorded failure before the fix and success after it, or explicitly explain an unavailable before-fix run; acceptance requires all mandatory checks against the final source revision.
- **a16** — A missing test recipe, environment failure, unrelated failing check or flaky result is recorded distinctly and cannot silently become a passing result or authorize unrelated changes.
- **a17** — Reuse #319's delivered configured repair budget, provisionally its default of two repair attempts after the initial fix, and stop incomplete with evidence and remaining failures when exhausted.
- **a18** — Preserve durable answers, budgets and completed outcomes across restart, invalidate affected approvals after material scope changes, and invalidate test evidence when its source or recipe changes.
- **a19** — Cancellation stops new work and preserves the partial result; duplicate replies or resumed attempts must not repeat a recorded publication or delivery effect.
- **a20** — Existing non-Bug workflows retain their selection and behavior, and an unavailable bug dependency produces an explicit unavailable result before repair starts.
- **a21** — Workflow validation must cover the normal repair path, ambiguous input, unreproduced bugs, rejected or stale approval, failed or interrupted tests, budget exhaustion, restart and each permitted lifecycle boundary.


## Open questions

### q1 — How should users start the bug workflow?

_Asked of: Product Owner Questions_

- **a** — Recommended: add explicit Bug selection to the existing issue and launch surfaces; a plain bug label alone does not authorize execution.  *(recommended)*
- **b** — Keep existing mode selection and offer a bug-specific path within it; users still move between the existing modes.

### q2 — What should authorize repair after analysis?

_Asked of: Product Owner Questions_

- **a** — Recommended: pause once for the owner to approve the recorded cause, remedy and success criteria; that approval permits repair and checks within that scope, with another question only for a material change.  *(recommended)*
- **b** — Starting Bug authorizes analysis, repair and checks automatically when the defect is reproduced and the remedy stays within the lightweight boundary; pause only for ambiguity or a broader change.

### q3 — Where should the bug lifecycle finish?

_Asked of: Product Owner Questions_

- **a** — Recommended: carry the bug through repair, full-suite validation, a reviewable PR, explicit approval of the current change and target, integration and confirmed issue closure; reuse existing delivery permissions and keep engineering, acceptance and delivery states separate.  *(recommended)*
- **b** — Finish at a reviewable PR with full-suite results; acceptance, merge and issue closure remain outside this workflow.
- **c** — Finish after repair and relevant unit checks, with a durable handoff to Validate for the required full-suite rerun and later delivery; the bug remains incomplete until that result returns.

### q4 — Does testing require a separately executed child graph?

_Asked of: Technical Questions_

- **a** — Recommended: express test selection, execution, result recording and bounded repair as graph-controlled steps using #319's delivered activities; include full-suite validation in the selected lifecycle without adding general child-graph execution.  *(recommended)*
- **b** — Require a separately invoked test graph with its own durable run, cancellation, recovery and result contract; include that extra engine capability and its larger scope.
