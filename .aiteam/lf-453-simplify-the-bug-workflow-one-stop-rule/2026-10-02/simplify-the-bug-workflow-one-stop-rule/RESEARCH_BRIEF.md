# Research brief

## Request

Simplify Bug to one flagship analyst conversation loop with explicit owner approval before repair, then one fixer agent that performs its own repair and final validation and submits succeeded to trigger automatic factory push, merge and closure. Reuse core delivery and default branch synchronization, remove substantial obsolete Bug code and use shared JSON communication exclusively.

## Decisions

- **d1** — Owner-set direction: use one analyst conversation loop followed by an approved repair through one fixer agent and a final factory delivery activity; the fixer owns all repair and validation iterations inside its run.
- **d2** — Owner-set requirements: flagship analyst, minimal role Markdown, substantial code reduction, and strict shared JSON communication.
- **d3** — Shaping decision: the primary user story is that an owner reports a bug, supplies missing guidance when asked, and receives a verified fix integrated into main with the issue closed only after integration is confirmed.
- **d4** — Shaping decision: the analyst must record expected behavior, actual behavior, reproduction or concrete evidence, cause, proposed remedy and the verification criterion before declaring diagnosis complete.
- **d5** — Shaping decision: the analyst loop owns diagnosis discussion and approval waiting, while agent and finalization activities use shared durable parking and all workflow transitions remain in DOT.
- **d6** — Shaping decision: blocked, failed, invalid or missing agent results and exhausted budgets park with a preserved reason at the originating node, and any authorized owner reply resumes that node with the reply as guidance.
- **d7** — Shaping decision: keep finite analyst and agent execution budgets, with an owner reply granting one further invocation after exhaustion and no factory-managed fixer retry loop.
- **d8** — Owner-set decision: the fixer resolves failing checks within its own run and performs final validation, reporting blocked or failed if it cannot finish rather than delegating testing to the factory.
- **d9** — Shaping decision: agent outcomes come only from validated retained communication submissions, and terminal text never determines success, failure or routing.
- **d10** — Shaping decision: use graph instances and attempts as the durable record for diagnosis, repair, verification and finalization rather than maintaining parallel Bug lifecycle ledgers.
- **d11** — Shaping decision: each attempt retains its node and identity, input and owner-reply references, structured result and reason, source revision, evidence references and timestamps.
- **d12** — Shaping decision: durable waiting records retain the originating node, question or review identity, publication state and consumed reply identity so restart and duplicate delivery do not lose guidance or repeat work.
- **d13** — Shaping decision: the fixer's submitted validation evidence identifies the approved diagnosis, tested code tree or revision, target-main revision, commands and outcomes, and references to logs, with changed inputs invalidating success.
- **d14** — Owner-set delivery behavior: accepted fixer succeeded triggers factory save/push, merge and confirmed closure through shared delivery capabilities without a second owner review or feature/story lifecycle dependency.
- **d15** — Shaping decision: an uncertain remote effect must be observed before another mutation, an already merged PR proceeds only to remaining closure work, and completion requires confirmed integration and closure.
- **d16** — Shaping decision: the analyst remains read-only, the fixer retains Implement-equivalent worktree rights, and only the factory performs publication, merge and closure under existing repository permissions.
- **d17** — Shaping decision: malformed or contradictory results, missing evidence, unauthorized replies and stale revisions cannot authorize progression or merge.
- **d18** — Shaping decision: remove the test child and factory check orchestration, self-check readers and digest inputs, special exhaustion commands, per-state retry tables and obsolete Bug stores once cutover permits retirement.
- **d19** — Shaping decision: preserve applied migration history and use a forward upgrade for active-store retirement, with both fresh databases and existing databases supported.
- **d20** — Proposed acceptance target: remove at least 7,762 net non-test Go lines against the 15,524-line Bug baseline, charging all replacement shared code against the saving and rejecting mere renaming or relocation as simplification.
- **d21** — Shaping decision: acceptance covers blocked and failed agents, exhausted budgets, malformed or absent submissions, arbitrary owner guidance, duplicate replies, restart recovery, stale checks, partial remote success and the selected legacy-task cutover.
- **d22** — Shaping decision: changing unrelated workflows, redesigning model permissions, bypassing repository protections, deleting audit history and building a new general workflow platform are outside scope.
- **d23** — Shaping decision: remove Bug's forced base-sync exemption and use shared factory synchronization before eligible resumed agent runs, including owner-comment continuations, while retaining existing explicit policy overrides.
- **d24** — Shaping decision: synchronization preserves edits and history and invalidates changed validation inputs, requiring fixer revalidation while requiring new owner approval only when the approved diagnosis or remedy materially changes.
- **d25** — Owner-set decision: repair requires explicit authenticated owner approval of the current diagnosis and remedy, and a changed remedy returns to the analyst for renewed approval.
- **d26** — Owner-set decision: the fixer reports succeeded only after completing final validation, and the factory consumes that accepted result without executing its own validation gate.
- **d27** — Shaping decision: succeeded requires passing applicable checks and satisfaction of the approved verification criterion, while skipped required checks, unresolved failures or missing evidence cannot count as success.


## Assumptions

- **a1** — Treat minimal Markdown as a request to simplify role instructions and prose, not to replace the JSON transport: the owner explicitly requires JSON communication.
- **a2** — Use the repository's configurable flagship tier rather than hard-coding a vendor model; the analyst already declares this tier.
- **a3** — The one-stop rule remains applicable, but any reply resumes work rather than implicitly approving an unapproved diagnosis; after explicit repair approval, fixer success authorizes automatic delivery as the owner directed.
- **a4** — No live task inventory or deployed database state has been inspected; cutover must handle unfinished and partially delivered tasks without assuming none exist.
- **a5** — Historical migration-file reduction is not a success criterion for this rewrite because deleting applied history would create a separate database-baselining project; fewer active stores and less code are the proposed interpretation.
- **a6** — A bug that cannot be reproduced must have concrete alternative evidence and an executable verification criterion, otherwise diagnosis asks the owner instead of declaring the issue fixed.
- **a7** — Existing authorized-owner identity rules, repository merge protections and explicit task cancellation remain applicable; the rewrite removes Bug-specific grant and stop commands rather than granting arbitrary commenters control.
- **a8** — The fixer uses the repository's applicable build, regression and check commands as its validation criteria; no separate factory check recipe execution or new budget system is introduced.
- **a9** — Interpret the owner's current-main request as shared default freshness for eligible work resumptions, not forced branch mutation for every received, duplicate, unauthorized or refused comment; existing Validate immutability and explicit sync overrides remain outside this Bug policy rewrite.
- **a10** — The latest owner reply settles q1 as explicit approval before repair with no second review and q2 as agent-owned validation followed by succeeded; q3 requests an explanation and remains unresolved.
- **a11** — Interpret merge, push and close as the delivery outcome, using the existing factory's safe save/push and remote merge ordering rather than requiring a new local merge-to-main strategy.


## Open questions

### q3 — When this change is installed, may unfinished bugs restart with the analyst while keeping all existing work?

_Asked of: Product Owner Questions_

- **a** — Recommended: preserve each issue, discussion, branch, changes and PR, restart diagnosis in the simplified Bug flow, and ask for approval before further repair; first reconcile any prior merge or closure so delivered fixes are not repaired again.  *(recommended)*
- **b** — Let existing bugs finish under the old flow and use the simplified flow only for new bugs; this avoids restarting diagnosis but requires retaining the old runtime and delays code removal.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
