# Research brief

## Request

Challenge the saved assumptions for #367, a standalone bug-label workflow (analysis, approved repair, separate test graph, approved delivery) with Bug-only lf_bug_* activities and no dependency on #319, against the current code before shaping continues.

## Decisions

- **d1** — Owner-set direction: use the existing graph engine and keep the bug workflow lighter than the existing feature workflows.
- **d2** — Owner correction: #319 is not a prerequisite, and #367 delivers its missing activities with authority limited to the Bug workflow.
- **d3** — Owner-set content: analysis covers Root Cause, Impact and Solution; repair covers code, unit tests and a suite rerun, with clarification when the bug location is ambiguous.
- **d4** — Keep one versioned bug record instead of a feature story breakdown.
- **d5** — Record engineering completion, validation, owner acceptance and delivery as separate outcomes.
- **d6** — Preserve evidence and unresolved decisions when the lightweight workflow cannot safely complete its repair.
- **d7** — Owner answer q1: the existing bug label selects a standalone Bug workflow outside Research, Plan, Implement and Validate, without a Mode label. Its interaction with existing bug-labelled issues that carry Mode labels is challenged in f11 and left open.
- **d8** — Owner answer q2: one approval of the recorded analysis, remedy and success criteria before repair, with renewed approval only for material scope changes.
- **d9** — Owner answer q3, under the stated condition: repair, full-suite checks, PR review, approved integration and issue closure within one Bug workflow without mode switches.
- **d10** — Owner answer q4: a separately executed test graph with durable invocation, recovery, cancellation and result handling. Restated as the largest cost in f4.
- **d11** — Owner-set communication: agents use MCP and the standard versioned JSON contract.
- **d12** — Owner-set role format: every agent specification has Role, Voice, Goal, How you are invoked, When you are done, Must Do, Must Not Do, Writing the Report and Output.
- **d13** — Two roles: a Bug analyst (investigation and shaping) and a Bug fixer (repair and bounded corrections).
- **d14** — The graph owns orchestration, publication and delivery; deterministic factory activities run the test child.
- **d15** — Parent and child DOT diagrams are illustrative scope samples, not shipped executable graphs.
- **d16** — Proposed inventory for one approved bug: lf_bug_prepare, lf_bug_recipe, lf_bug_checks, lf_bug_repair, lf_bug_record, lf_bug_handoff. The count is challenged in f16.
- **d17** — Proposed inventory for the test child and delivery: lf_bug_test_graph, lf_bug_pr, lf_bug_merge, lf_bug_close.
- **d18** — Admit lf_bug_* only for the Bug workflow, with no cross-prefix exception and no Implement enablement. The mechanism is open (f12).
- **d19** — Owner-set naming rule: lf_implement_* only for activities reusable unchanged by later Implement; lf_bug_* otherwise. All ten are lf_bug_*.


## Assumptions

- **a1** — Root Cause, Impact and Solution are separate sections in one analysis by the Bug analyst, which also shapes the remedy and handles review feedback.
- **a2** — Owner acceptance of one-mode delivery is taken to accept integration and closure inside the Bug workflow.
- **a4** — Earlier #319 proposals are naming history only; #367 does not wait for #319 or enable feature Implement. Confirmed by f17.
- **a6** — The bug record holds expected and actual behavior, environment/version, reproduction, evidence, root cause, preconditions, impact, severity, remedy, complexity and success criteria.
- **a7** — The execution record holds scope revisions, owner answers, source and target revisions, graph versions, child run and call identity, recipe, attempts, outcomes, budgets and delivery references.
- **a9** — REVISED. The bug label is a candidate selector, not a sufficient one. Recommended rule: bug selects the Bug workflow only when no Mode label is present; an explicit Mode label wins. Alternative: require the bug label plus an explicit owner trigger. Rests on f11; needs shaping to settle.
- **a10** — Only authenticated owner replies authorize repair or delivery; analysis may reproduce but not repair; the fixer edits only the task worktree; only factory activities perform remote effects. Bug analyst question permission needs an explicit grant (f7).
- **a11** — Before repair require expected vs actual behavior, reproduced or equivalent failing evidence, a supported cause, a bounded remedy and observable success criteria.
- **a13** — The lightweight boundary permits localized corrections and excludes product decisions, redesign, destructive migration and cross-repository release.
- **a14** — An unreproduced defect triggers one batched question and stays incomplete if unsupported; a broader remedy returns to analysis inside Bug.
- **a17** — REVISED. Default eight analyst invocations and two repair attempts after the initial fix, persisted across restarts. Whether lf_loop can express one shared budget across two loops is unverified (f15); may need a new persisted counter.
- **a20** — REVISED, replaces bug-label precedence. Existing tasks keep their admitted workflow; removing the label does not cancel work or revoke approval. Precedence of bug over Mode labels for new tasks is withdrawn as unsafe (f11).
- **a22** — The parent durably invokes one test child at a time with approved criteria, exact source, recipe and parent identity, and consumes pass, fail, blocked or cancelled with check-level evidence.
- **a23** — The child runs regression and focused checks before the full suite and quality checks and returns pass only when every mandatory check passed on the final source.
- **a25** — Bug analyst returns ask, done, approved or blocked; Bug fixer returns done, reshape or blocked; local stage completion stays distinct from workflow completion.
- **a26** — MCP preflight, submit and receipt retain each agent candidate and the terminal envelope echoes it; graph activities alone publish. The Bug policy must not use a terminal-text bypass (f17).
- **a27** — Child-graph capability is limited to the test workflow: no recursion, no parallel fan-out.
- **a28** — Delivery needs a tested PR, approval of its current head and target, and confirmed integration before closure; a failed closure stays pending.
- **a31** — REVISED. lf_bug_recipe, lf_bug_checks, lf_bug_record and lf_bug_handoff are new capability, not reuse: no factory-run project test runner was found (f13).
- **a32** — REVISED. lf_bug_pr, lf_bug_merge and lf_bug_close reuse the GitHub client merge and close calls but need Bug-specific or generalized proposal bookkeeping (f14).
- **a33** — Validation must show the Bug lifecycle works without #319 and that unrelated feature workflows gain no new activity authority.
- **a34** — Like-for-like Implement compatibility is not established for any of the ten contracts, so all use lf_bug_*.
- **a35** — NEW. The ten activities are a starting inventory. Shaping should test each for necessity; prepare, record and handoff are the likeliest to merge into neighbors (f16).
- **a36** — NEW. Giving Bug its own activity namespace needs either a Bug mode value or a workflow-identity concept beside mode. The recommended direction is a workflow identity so no Mode: Bug label appears (f12).
- **a8** — States distinguish analyzing, awaiting owner, ready for repair, repairing, checking, ready for review, integrating, pending closure, blocked, delivered and stopped; engineering, validation, acceptance and delivery are tracked separately.
- **a15** — Regression evidence shows the failure before the fix and success after it, or explains why a before-fix run is unavailable; acceptance needs all mandatory checks on the final source.
- **a16** — A missing recipe, environment failure, unrelated failure or flaky result is recorded distinctly and never counts as a pass.
- **a18** — Answers, budgets and completed outcomes survive restart; material scope change invalidates approval; changed source, recipe or target invalidates affected check evidence.
- **a19** — Cancellation stops new work and propagates to the child; duplicate replies and resumed attempts cannot repeat a recorded child call, publication, merge or closure.
- **a24** — A fixable test failure or scoped PR feedback consumes the same repair budget; material feedback returns to analysis and needs renewed approval.
- **a29** — Owner rejection stops with retained evidence; an unknown child outcome blocks the parent; exhausted budgets need explicit authorized continuation.
- **a30** — lf_bug_prepare binds the approved scope and first attempt; lf_bug_repair reserves only additional attempts; lf_agent stays the only way to run the fixer.
