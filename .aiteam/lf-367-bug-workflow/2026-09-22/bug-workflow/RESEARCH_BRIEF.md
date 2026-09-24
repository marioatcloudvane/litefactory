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
- **d7** — Owner answer q1: the existing bug label selects a standalone Bug workflow outside Research, Plan, Implement and Validate, without a Mode label. Made safe by d20: it applies only when no Mode label is present.
- **d8** — Owner answer q2: one approval of the recorded analysis, remedy and success criteria before repair, with renewed approval only for material scope changes.
- **d9** — Owner answer q3, under the stated condition: repair, full-suite checks, PR review, approved integration and issue closure within one Bug workflow without mode switches.
- **d10** — Owner answer q4: a separately executed test graph with durable invocation, recovery, cancellation and result handling; limited to one child at a time, no recursion, no parallel fan-out.
- **d11** — Owner-set communication: agents use MCP and the standard versioned JSON contract, on the shared MCP result service from day one.
- **d12** — Owner-set role format: every agent specification has Role, Voice, Goal, How you are invoked, When you are done, Must Do, Must Not Do, Writing the Report and Output.
- **d13** — Two roles: a Bug analyst (investigation, shaping, PR-feedback triage) and a Bug fixer (repair and bounded corrections).
- **d14** — The graph owns orchestration, publication and delivery; deterministic factory activities run the test child.
- **d15** — Parent and child DOT diagrams are illustrative scope samples, not shipped executable graphs.
- **d16** — REVISED. Seven new Bug-only activities: lf_bug_record, lf_bug_attempt, lf_bug_test_graph, lf_bug_checks, lf_bug_pr, lf_bug_merge, lf_bug_close. Dropped: prepare and repair (merged into attempt), recipe and child record (folded into checks), handoff (child terminal is the result).
- **d17** — REVISED. The test child has two nodes, focused and full, both lf_bug_checks, ending in pass, fail, blocked or cancelled; the parent lf_bug_test_graph maps that terminal to its own outcome.
- **d18** — REVISED. Admit lf_bug_* only for the Bug workflow via a workflow identity beside the five modes; no sixth Mode value, no Mode: Bug label, no Implement enablement.
- **d19** — Owner-set naming rule: lf_implement_* only for activities reusable unchanged by later Implement; lf_bug_* otherwise. All seven are lf_bug_*.
- **d20** — SHAPING DECISION. Label rule: a new task with the bug label and no Mode label runs Bug; an explicit Mode label always wins. Default the owner can overturn.
- **d21** — SHAPING DECISION. Bug is a workflow identity beside the five modes; the engine accepts lf_<workflow>_<capability> for both; the graph declares its workflow instead of a mode.
- **d22** — SHAPING DECISION. The factory runs only commands declared by project configuration; agents never write or change commands; a missing recipe returns blocked. Default the owner can overturn.
- **d23** — SHAPING DECISION. One repair budget lives in one attempt node entered from test failure and PR feedback: one initial fix plus two repairs; analysis keeps eight passes; neither resets on replies or restarts.
- **d24** — SHAPING DECISION. The Bug analyst gets an explicit grant to ask questions and open review waits; the fixer gets none.
- **d25** — SHAPING DECISION. Owner PR feedback returns to the analyst, who classifies it as within the approved remedy (fixer via the shared budget, no new gate) or outside it (renewed approval).


## Assumptions

- **a1** — Root Cause, Impact and Solution are separate sections in one analysis by the Bug analyst, which also shapes the remedy and handles review feedback.
- **a2** — Owner acceptance of one-mode delivery is taken to accept integration and closure inside the Bug workflow.
- **a4** — Earlier #319 proposals are naming history only; #367 does not wait for #319 or enable feature Implement. Confirmed by f17.
- **a6** — The bug record holds expected and actual behavior, environment/version, reproduction, evidence, root cause, preconditions, impact, severity, remedy, complexity and success criteria including the regression test to add.
- **a7** — The execution record holds scope revisions, owner answers, source and target revisions, child call identity and terminal outcome, recipe version, per-attempt and per-check results, budgets and delivery references.
- **a8** — States: analyzing, awaiting owner, ready for repair, repairing, checking, ready for review, integrating, pending closure, blocked, delivered, stopped; engineering, test, acceptance and delivery outcomes are tracked separately.
- **a9** — REVISED. See d20: bug selects Bug only when no Mode label is present. Existing bug-labelled issues with a Mode label are unaffected. Rests on f11.
- **a10** — Only authenticated owner replies authorize repair or delivery; analysis may reproduce but not repair; the fixer edits only the task worktree; only factory activities run checks or perform remote effects.
- **a11** — Before repair require expected vs actual behavior, reproduced or explained equivalent failing evidence, a supported cause, a bounded remedy and observable success criteria.
- **a13** — The lightweight boundary permits localized corrections and excludes product decisions, redesign, destructive migration and cross-repository release.
- **a14** — An unreproduced defect triggers one batched question and stays incomplete if unsupported; a broader remedy or fixer reshape returns to analysis inside Bug.
- **a15** — Regression evidence shows the failure before the fix and success after it, or explains why a before-fix run is unavailable; acceptance needs all mandatory checks on the final source.
- **a16** — A missing recipe, environment failure, unrelated failure, skipped, interrupted, stale or flaky result is recorded distinctly and never counts as a pass.
- **a17** — REVISED. Confirmed by f18: one attempt node with several entry edges shares one counter. Defaults: eight analyst passes; one initial fix plus two repairs; persisted across replies and restarts.
- **a18** — Answers, budgets and completed outcomes survive restart; material scope change invalidates approval; changed source, recipe or target invalidates affected check evidence.
- **a19** — Cancellation stops new work and propagates to the child; duplicate replies and resumed attempts cannot repeat a recorded child call, publication, merge or closure.
- **a20** — REVISED. Existing tasks keep their admitted workflow; removing the bug label cancels nothing and revokes no approval; an explicit Mode label wins over bug (d20).
- **a22** — The parent durably invokes one test child at a time with approved criteria, exact source, recipe and parent identity, and consumes pass, fail, blocked or cancelled with check-level evidence.
- **a23** — The child runs the focused phase before the full phase; pass only when every mandatory check passed on the final source.
- **a24** — A fixable test failure or scoped PR feedback consumes the same repair budget; material feedback returns to analysis and needs renewed approval.
- **a25** — Bug analyst returns ask, done, approved or blocked; Bug fixer returns done, reshape or blocked; local stage completion stays distinct from workflow completion.
- **a26** — MCP preflight, submit and receipt retain each agent candidate and the terminal envelope echoes it; graph activities alone publish. No terminal-text bypass (f17).
- **a27** — Child-graph capability is limited to the test workflow: no recursion, no parallel fan-out.
- **a28** — Delivery needs a tested PR, approval of its current head and target, and confirmed integration before closure; a failed closure stays pending.
- **a29** — Owner rejection stops with retained evidence; an unknown child outcome blocks the parent; exhausted budgets need explicit authorized continuation.
- **a30** — REVISED. lf_bug_attempt reserves the initial and any additional attempt once and prepares the fixer context; lf_agent stays the only way to run the fixer.
- **a31** — REVISED. lf_bug_checks is new capability, not reuse: no factory-run project test runner exists (f13). Allowed commands, timeouts, environment, logs and cancellation are new design.
- **a32** — REVISED. lf_bug_pr, lf_bug_merge and lf_bug_close reuse the GitHub client merge and close calls but need Bug-specific bookkeeping (f14).
- **a33** — Validation must show the Bug lifecycle works without #319 and that unrelated feature workflows gain no new activity authority.
- **a34** — Like-for-like Implement compatibility is not established for any Bug activity, so all use lf_bug_*.
- **a35** — REVISED. The inventory is cut to seven by merging bookkeeping activities; planning may still fold more.
- **a36** — REVISED. Workflow identity is the chosen mechanism (d21); how many places consume the closed mode list is a planning question.
- **a37** — NEW. Where the project recipe is declared is a planning question; this repository's existing full check is the first recipe.
- **a38** — NEW. How an owner reply to the delivery review resumes at the merge step rather than at the graph start is a planning question.
