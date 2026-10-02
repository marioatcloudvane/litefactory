# Research brief

## Request

Heavily simplify Bug to two loops using lf_loop and lf_agent, followed by one final activity that merges to main and closes. First establish a clear bug and proposed solution with a flagship analyst and minimal role Markdown; then repair until verified fixed. Remove substantial old Bug machinery and use communication JSON exclusively. The latest owner instruction supersedes the older proposed multi-activity graph.

## Decisions

- **d1** — Owner-set direction: only loop, agent and final merge/close activity types; two stages, diagnosis/solution then repair/verification. This is the latest explicit request, not the older issue sketch.
- **d2** — Owner-set requirements: flagship analyst, minimal role Markdown, substantial code reduction, and strict shared JSON communication.
- **d3** — Shaping decision: the primary user story is that an owner reports a bug, supplies missing guidance when asked, and receives a verified fix integrated into main with the issue closed only after integration is confirmed.
- **d4** — Shaping decision: the analyst must record expected behavior, actual behavior, reproduction or concrete evidence, cause, proposed remedy and the verification criterion before declaring diagnosis complete.
- **d5** — Shaping decision: loops own durable question publication and resumption while all transitions, including reshape back to diagnosis and finalization back to verification, remain declared in DOT.
- **d6** — Shaping decision: blocked, failed, invalid or missing agent results and exhausted budgets park with a preserved reason at the originating node, and any authorized owner reply resumes that node with the reply as guidance.
- **d7** — Shaping decision: automatic loop attempts remain finitely bounded, and an owner reply after budget exhaustion authorizes one further attempt without erasing previous attempt history.
- **d8** — Shaping decision: a failed verification command is repair feedback within the remaining loop budget, whereas unavailable verification or an exhausted loop becomes an owner question.
- **d9** — Shaping decision: agent outcomes come only from validated retained communication submissions, and terminal text never determines success, failure or routing.
- **d10** — Shaping decision: use graph instances and attempts as the durable record for diagnosis, repair, verification and finalization rather than maintaining parallel Bug lifecycle ledgers.
- **d11** — Shaping decision: each attempt retains its node and identity, input and owner-reply references, structured result and reason, source revision, evidence references and timestamps.
- **d12** — Shaping decision: durable waiting records retain the originating node, question or review identity, publication state and consumed reply identity so restart and duplicate delivery do not lose guidance or repeat work.
- **d13** — Shaping decision: verification records bind command outcomes and log references to the diagnosis, proposed revision, target-main revision and trusted recipe identity, with changed inputs invalidating the pass.
- **d14** — Shaping decision: finalization owns PR creation or reuse, any selected delivery review, merge and issue closure, retaining remote object identities and dispatched versus confirmed effects in attempt data.
- **d15** — Shaping decision: an uncertain remote effect must be observed before another mutation, an already merged PR proceeds only to remaining closure work, and completion requires confirmed integration and closure.
- **d16** — Shaping decision: the analyst remains read-only, the fixer retains Implement-equivalent worktree rights, and only the factory performs publication, merge and closure under existing repository permissions.
- **d17** — Shaping decision: malformed or contradictory results, missing evidence, unauthorized replies and stale revisions cannot authorize progression or merge.
- **d18** — Shaping decision: remove the test child, self-check readers and digest inputs, special exhaustion commands, per-state retry tables and obsolete Bug stores once the chosen cutover permits retirement.
- **d19** — Shaping decision: preserve applied migration history and use a forward upgrade for active-store retirement, with both fresh databases and existing databases supported.
- **d20** — Shaping decision: acceptance requires a measured net reduction against the recorded Bug source baseline, separately accounting for added shared code so moving code or renaming files cannot count as simplification.
- **d21** — Shaping decision: acceptance covers blocked and failed agents, exhausted budgets, malformed or absent submissions, arbitrary owner guidance, duplicate replies, restart recovery, stale checks, partial remote success and the selected legacy-task cutover.
- **d22** — Shaping decision: changing unrelated workflows, redesigning model permissions, bypassing repository protections, deleting audit history and building a new general workflow platform are outside scope.


## Assumptions

- **a1** — Treat minimal Markdown as a request to simplify role instructions and prose, not to replace the JSON transport: the owner explicitly requires JSON communication.
- **a2** — Use the repository's configurable flagship tier rather than hard-coding a vendor model; the analyst already declares this tier.
- **a3** — The issue's one-stop and any-owner-reply rule remains in scope because the newer two-loop instruction does not withdraw it; resumption grants another attempt rather than treating arbitrary text as merge approval.
- **a4** — No live task inventory or deployed database state has been inspected; cutover must handle unfinished and partially delivered tasks without assuming none exist.
- **a5** — Historical migration-file reduction is not a success criterion for this rewrite because deleting applied history would create a separate database-baselining project; fewer active stores and less code are the proposed interpretation.
- **a6** — A bug that cannot be reproduced must have concrete alternative evidence and an executable verification criterion, otherwise diagnosis asks the owner instead of declaring the issue fixed.
- **a7** — Existing authorized-owner identity rules, repository merge protections and explicit task cancellation remain applicable; the rewrite removes Bug-specific grant and stop commands rather than granting arbitrary commenters control.
- **a8** — The existing trusted repository check recipe and finite attempt bounds are reused; changing their format or adding new budget configuration is outside scope.


## Open questions

### q1 — Which owner approvals should remain between diagnosis, repair and merge?

_Asked of: Product Owner Questions_

- **a** — Recommended: move directly from a complete diagnosis to repair, then require explicit owner approval of the verified change before merging; this removes the analysis review while preserving control over main.  *(recommended)*
- **b** — Run both loops and merge automatically after verification, subject to repository protections; this gives the simplest unattended workflow.
- **c** — Keep explicit approval of both the diagnosis and the verified change; this preserves the current two review points inside the permitted activities.

### q2 — What evidence should allow the repair loop to finish?

_Asked of: Technical Questions_

- **a** — Recommended: the fixer builds and tests, then the loop runs the repository check recipe once as a factory-owned pass/fail gate on the exact proposed revision; remove the test child and its phase stores.  *(recommended)*
- **b** — Accept the fixer's structured reproduction and passing test evidence without a separate factory run; this removes more code but makes verification depend on the agent's report.

### q3 — How should unfinished Bug tasks move to the replacement workflow?

_Asked of: Technical Questions_

- **a** — Recommended: retire old execution instances and restart unfinished tasks at diagnosis, preserving their branches, changes, discussion and PRs; reconcile pending merge or close effects first, and require fresh verification and approval before new delivery.  *(recommended)*
- **b** — Keep the old runtime until all existing tasks finish, then remove it; this avoids restarting work but delays most code removal.
- **c** — Translate each old task's state into the new loops; this preserves more progress but adds migration and compatibility code.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
