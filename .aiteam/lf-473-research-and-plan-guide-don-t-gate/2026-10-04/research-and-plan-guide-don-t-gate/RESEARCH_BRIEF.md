# Research brief

## Request

Complete the full #473 shaping scope selected by q1 full, including explicit history, marker, approval-binding, uncertain-delivery, existing-session migration and definition-compatibility contracts; preserve successful stages and owner approval.

## Decisions

- **d1** — The owner prioritizes simplification while preserving the working Research and Plan stage behavior.
- **d2** — The owner selected q1 full, settling scope breadth while the revised detailed contract remains subject to owner review.
- **d3** — GitHub comments become the reply-routing authority for the full Research and Plan scope.
- **d4** — Keep lock, budget and run-log execution/effect facts while removing duplicated conversation state from routing, without treating renamed storage as simplification.
- **d5** — Reply disposition and refusal delivery must be durably separated so a failed refusal post cannot block later comment processing.
- **d6** — Every step gets typed inherited fields with a response enum from its declared outcomes and no agent-facing revision or patch operations.
- **d7** — Accepted submissions replace the complete current-run result, clear omitted optional fields and never inherit prior-run values.
- **d8** — Preflight and submit enforce the same complete validation and one 1 MiB serialized accepted-result ceiling.
- **d9** — Agent-provided artifact input needed downstream must be retained at acceptance, while factory-generated documents remain separate retryable outputs.
- **d10** — Comment-only submissions lacking required response are rejected without attributing any historical outcome or reason.
- **d11** — Retrying a parked factory effect reuses accepted input and never reruns an already successful agent.
- **d12** — Owner replies reset the stopped step's retry allowance but do not reset or bypass the issue budget.
- **d13** — Supported active definitions continue with captured topology/resources and compatible handlers, while genuinely unavailable execution support parks visibly without losing accepted work.
- **d14** — Existing graph conversations migrate through publication reconciliation and marker binding while legacy routes, pending effects and activation defaults remain supported.
- **d15** — Existing permissions and explicit owner approval requirements remain authoritative throughout routing and recovery.
- **d16** — Full-scope evidence covers history completeness, marker trust/mutation, candidate-bound approval, concurrency, migration and definition changes alongside queue, submit and recovery regressions.
- **d17** — Code counts are repeated using the saved inventory method with no arbitrary deletion target.
- **d18** — The owner rejected narrowing and selected the complete issue scope including GitHub-only routing and existing-session migration.
- **d19** — Result request replay returns its original receipt without reapplying an older replacement over a newer accepted result.
- **d20** — Reliability improvement and net code reduction remain unmeasured until implementation and relevant verification.
- **d21** — Recognize versioned factory markers only with configured factory authorship and binding to an issued run-log effect.
- **d22** — Fetch complete relevant history through a fixed comment-ID cutoff and park incomplete reads without consuming unseen guidance.
- **d23** — Use current unconsumed guidance but require a new comment to revise already-consumed guidance.
- **d24** — Deleted prompts or changed prompt marker/review content require a fresh prompt identity before any approval can advance the stage.
- **d25** — Approval binds to the exact displayed candidate and conflicting guidance yields a revised review rather than automatic advancement.
- **d26** — Migration reconciles confirmed and uncertain effects before switching routing, replacing unbindable prompts without rerunning successful agent work.
- **d27** — Refusal delivery and uncertain external effects retain stable identities and pending authored content independently of queue intake.
- **d28** — Budget exhaustion requires existing owner budget control rather than repeated replies silently granting more spending.
- **d29** — Shutdown can pause continuation but cannot revoke a durably accepted answer.
- **d30** — Shared physical records remain where legacy or unrelated workflows need them, while migrated Research and Plan reply routing no longer depends on those conversation tables.


## Assumptions

- **a1** — Preserve the earlier disjoint scoped code inventory and its limits as the measurement baseline rather than an exhaustive mode attribution.
- **a2** — Historical incidents remain owner reports; this continuation settles scope from saved evidence and the owner's full-scope selection, without tests, live daemon observations or code changes.
- **a3** — Treat execution facts and unpublished-effect content as run-log recovery data; physical shared delivery storage need not be removed if it stops serving as reply-routing authority.
- **a4** — Use the existing stricter 1 MiB retained-result ceiling as the single submit limit because the owner requests one limit but does not request larger results.
- **a5** — Preserve each stage's existing content fields and approval semantics when generating typed schemas; only the shared transport and run isolation change.
- **a6** — Preserve captured topology/resources and support for active definitions rather than automatically migrate topology, based on the owner's priority of preserving working stages.


## Open questions

### q2 — Do we split #473 into a net-deleting Phase 1 and a gated Phase 2 for GitHub routing?

_Asked of: Product Owner Questions_

- **phased** — Recommended: Phase 1 now (typed submit, submit-time validation, visible consumed refusals, stop and resume). Decide GitHub routing after measuring the code count.  *(recommended)*
- **full** — Keep everything in one change as in q1, accepting added machinery and migration risk.
- **phase1only** — Do Phase 1 only and drop GitHub-only routing from this issue entirely.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [code&#45;inventory&#46;csv](code-inventory.csv)
