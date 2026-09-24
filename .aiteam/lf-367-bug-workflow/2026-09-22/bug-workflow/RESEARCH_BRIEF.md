# Research brief

## Request

Shape #367: a standalone bug-label workflow with Root Cause, Impact and Solution analysis, approved repair, a separate full-suite test graph and approved delivery; deliver all missing lf_implement_* activities within #367 for Bug only, without requiring #319.

## Decisions

- **d1** — Owner-set direction: use the existing graph engine and keep the bug workflow lighter than the existing feature workflows.
- **d2** — Owner correction: #319 is not a prerequisite, and #367 must deliver any needed lf_implement_* activities with authority limited to the Bug workflow.
- **d3** — Owner-set content: analysis covers Root Cause, Impact and Solution; repair covers code, unit tests and a suite rerun, with clarification when the bug location is ambiguous.
- **d4** — Shaping decision: keep one versioned bug record instead of requiring a feature story breakdown.
- **d5** — Shaping decision: record engineering completion, validation, owner acceptance and delivery as separate outcomes.
- **d6** — Shaping decision: preserve evidence and unresolved decisions when the lightweight workflow cannot safely complete its agreed repair.
- **d7** — Owner answer q1: the existing bug label selects a standalone Bug workflow outside Research, Plan, Implement and Validate, without requiring a Mode label.
- **d8** — Owner answer q2: obtain one approval of the recorded analysis, remedy and success criteria before repair, with renewed approval only for material scope changes.
- **d9** — Owner answer q3, interpreted under the stated condition: carry repair, full-suite checks, PR review, approved integration and issue closure within one Bug workflow without mode switches.
- **d10** — Owner answer q4: require a separately executed test graph with durable invocation, recovery, cancellation and result handling.
- **d11** — Owner-set communication: agents use MCP and the standard versioned JSON contract.
- **d12** — Owner-set role format: every introduced agent specification includes Role, Voice, Goal, How you are invoked, When you are done, Must Do, Must Not Do, Writing the Report and Output.
- **d13** — Shaping decision: introduce a Bug analyst that combines investigation and shaping and a Bug fixer that handles initial repair and bounded corrections.
- **d14** — Shaping decision: the graph owns orchestration, publication and delivery while deterministic factory activities run the test child.
- **d15** — Shaping decision: present parent and child DOT diagrams as illustrative scope samples rather than executable shipped graph definitions.
- **d16** — Shaping decision: deliver lf_implement_prepare, lf_implement_recipe, lf_implement_checks, lf_implement_repair, lf_implement_record and lf_implement_handoff for one approved bug.
- **d17** — Shaping decision: deliver lf_implement_test_graph, lf_implement_pr, lf_implement_merge and lf_implement_close for the separate test child and approved bug delivery.
- **d18** — Shaping decision: admit the exact new lf_implement_* activity set only within Bug and leave feature Implement enablement and lf_implement_next outside this scope.


## Assumptions

- **a1** — Root Cause, Impact and Solution remain separate sections produced by the Bug analyst, which also shapes the remedy and handles review feedback.
- **a2** — The owner's acceptance of delivery if it stays in one mode is taken to accept the proposed integration-and-closure boundary within one standalone Bug workflow.
- **a3** — All four earlier questions are settled by the reply; the revised scope is a new draft for review, not blanket owner approval of its new details.
- **a4** — Earlier #319 activity proposals are historical naming context only; #367 owns the required Bug-only activity contracts and does not wait for #319 or enable feature Implement execution.
- **a5** — The primary story is one reporter taking one defect from observed failure to a scoped repair and confirmed delivery using the current issue, branch and repository context.
- **a6** — The bug record contains expected behavior, actual behavior, affected environment/version, reproduction steps and status, evidence references, root cause, trigger, preconditions, impact, severity rationale, remedy, complexity and observable success criteria.
- **a7** — The execution record contains analysis and approved scope revisions, owner answers, source and target revisions, graph versions, child run and durable call identities, test recipe/environment, attempts/outcomes, remaining budgets and delivery references.
- **a8** — States distinguish analyzing, awaiting owner, ready for repair, repairing, checking, ready for review, integrating, pending closure, blocked, delivered and stopped, with engineering, validation, acceptance and delivery tracked separately.
- **a9** — The existing bug label selects the new workflow within normal repository/task admission, and existing issue comments and durable replies provide reports, questions and approvals without a new dashboard or public API.
- **a10** — Only authenticated owner replies authorize repair or delivery; analysis permits bounded reproduction but no product repair, the fixer edits only the task worktree within approved scope, and factory activities alone perform approved remote effects.
- **a11** — Before repair, require concrete expected versus actual behavior, a reproduced failure or equivalent existing failing evidence, a supported cause, a bounded remedy and observable success criteria; unsupported hypotheses remain labeled and block repair.
- **a12** — Use descriptive severity levels critical, high, medium and low with a consequence-based rationale, and complexity small, moderate or large with an explanation, as reporting fields rather than automatic authority.
- **a13** — The lightweight boundary permits localized corrections within existing behavior and excludes remedies requiring product decisions, architecture redesign, destructive migration or coordinated cross-repository release.
- **a14** — An unreproduced defect triggers a batched request for missing context and remains incomplete if unsupported; a broader remedy returns to analysis inside Bug without automatically selecting another workflow.
- **a15** — Regression evidence must demonstrate the recorded failure before the fix and success after it, or explicitly explain an unavailable before-fix run; acceptance requires all mandatory checks against the final source revision.
- **a16** — A missing test recipe, environment failure, unrelated failing check or flaky result is recorded distinctly and cannot silently become a passing result or authorize unrelated changes.
- **a17** — Default to eight analyst invocations and two repair attempts after the initial fix, with configurable persistent accounting delivered in #367 and no dependency on #319.
- **a18** — Preserve answers, budgets and completed outcomes across restart, invalidate approvals after material scope changes, and invalidate affected check evidence when source, recipe or relevant target changes.
- **a19** — Cancellation stops new work and propagates to the child; duplicate replies and resumed attempts cannot repeat a recorded child invocation, publication, integration or closure effect.
- **a20** — For new admitted tasks the bug label takes precedence over feature-mode labels, while existing tasks retain their admitted workflow and label removal alone does not cancel work or revoke approval.
- **a21** — Workflow validation must cover label routing, analysis approval, child invocation/recovery/cancellation, regression and full-suite failure, stale evidence, repair exhaustion, changed scope, delivery approval and confirmed closure.
- **a22** — The parent durably invokes one test child at a time using approved criteria, exact source, recipe/environment and parent identity, then consumes its pass, fail, blocked or cancelled outcome with check-level evidence.
- **a23** — The test child runs regression and focused checks before the full suite and required quality checks, may stop early on failure, and must complete every mandatory check before returning pass.
- **a24** — A fixable test failure or scoped PR feedback consumes the same repair budget, while material feedback returns to the bounded analyst loop and needs renewed scope approval.
- **a25** — The Bug analyst returns ask, done, approved or blocked, and the Bug fixer returns done, reshape or blocked, through a versioned Bug result contract that keeps local stage completion distinct from workflow completion.
- **a26** — MCP preflight, correction, submission and matching receipt recovery retain each agent candidate, its terminal envelope echoes that candidate, and graph activities alone publish reports or open waits.
- **a27** — The separate-child capability is limited to the required test workflow, excluding arbitrary recursive graph invocation and parallel child fan-out.
- **a28** — Delivery requires a tested PR and approval of its current head and target, integration confirmation precedes closure, and a failed closure remains pending rather than claiming delivery finished.
- **a29** — Owner rejection stops with retained evidence, unknown child outcomes prevent parent advancement, and exhausted budgets require explicit authorized continuation rather than an automatic reset.
- **a30** — lf_implement_prepare binds the approved scope and initial attempt, lf_implement_repair atomically reserves only additional attempts, and lf_agent remains the sole invocation mechanism for the Bug fixer.
- **a31** — lf_implement_recipe binds mandatory focused and full-suite checks, lf_implement_checks executes a phase, lf_implement_record persists evidence, and lf_implement_handoff returns a source-bound child result without granting owner approval.
- **a32** — lf_implement_test_graph durably invokes or recovers one child, lf_implement_pr presents the tested change, lf_implement_merge checks current approval and integration, and lf_implement_close confirms issue closure.
- **a33** — Scope validation must demonstrate the full Bug lifecycle without #319 and reject use of the new activities by unrelated feature workflows.
