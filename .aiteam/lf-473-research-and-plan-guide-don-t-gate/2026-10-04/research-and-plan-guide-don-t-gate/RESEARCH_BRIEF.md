# Research brief

## Request

Challenge the previously proposed #473 scope for real simplification and new failure modes, preserve settled stage behavior, and present a concrete scope tradeoff for owner decision.

## Decisions

- **d1** — The owner prioritizes simplification while preserving the working Research and Plan stage behavior.
- **d2** — The owner requested an adversarial review, which is not acceptance of the previously displayed scope.
- **d3** — GitHub-authoritative routing remains an issue goal, but deferring its rewrite from the first scope is proposed for owner decision.
- **d4** — Moving durable recovery records into a run log is not itself simplification, so the recommended first scope retains existing conversation and publication records.
- **d5** — Reply disposition and refusal delivery must be durably separated so a failed refusal post cannot block later comment processing.
- **d6** — Every step gets typed inherited fields with a response enum from its declared outcomes and no agent-facing revision or patch operations.
- **d7** — Accepted submissions replace the complete current-run result, clear omitted optional fields and never inherit prior-run values.
- **d8** — Preflight and submit enforce the same complete validation and one 1 MiB serialized accepted-result ceiling.
- **d9** — Agent-provided artifact input needed downstream must be retained at acceptance, while factory-generated documents remain separate retryable outputs.
- **d10** — Comment-only submissions lacking required response are rejected without attributing any historical outcome or reason.
- **d11** — Retrying a parked factory effect reuses accepted input and never reruns an already successful agent.
- **d12** — Owner replies reset the stopped step's retry allowance but do not reset or bypass the issue budget.
- **d13** — Definition compatibility changes are proposed for deferral because a captured graph does not guarantee its handlers or resources remain available.
- **d14** — The recommended first scope preserves existing graph conversation routing, pending external effects, legacy routes and activation defaults.
- **d15** — Existing permissions and explicit owner approval requirements remain authoritative throughout routing and recovery.
- **d16** — First-scope evidence must cover queue progress during failed refusal delivery, process-failure and restart survival, typed schemas, replacement replay and cheap-step recovery in both modes.
- **d17** — Code counts are repeated using the saved inventory method with no arbitrary deletion target.
- **d18** — The recommended reduced scope does not yet meet the issue's GitHub-only routing requirement and cannot be treated as completion of the full issue.
- **d19** — Result request replay returns its original receipt without reapplying an older replacement over a newer accepted result.
- **d20** — Reliability improvement and net code reduction remain unmeasured until implementation and relevant verification.


## Assumptions

- **a1** — Preserve the earlier disjoint scoped code inventory and its limits as the measurement baseline rather than an exhaustive mode attribution.
- **a2** — Historical incidents remain owner reports; this pass performed focused source reinspection and adversarial reasoning, with no tests, live daemon observations or code changes.
- **a3** — The proposed reduced scope keeps recovery and conversation records in their current owners; renaming them would not establish a reduction in state or failure paths.
- **a4** — Use the existing stricter 1 MiB retained-result ceiling as the single submit limit because the owner requests one limit but does not request larger results.
- **a5** — Preserve each stage's existing content fields and approval semantics when generating typed schemas; only the shared transport and run isolation change.
- **a6** — The proposed reduced scope leaves definition compatibility unchanged, pending the owner's scope decision; graph portability is not assumed from capture alone.


## Open questions

### q1 — Should this change deliver the smaller reliability scope first or retain the full conversation-routing rewrite?

_Asked of: Product Owner Questions_

- **smaller** — Recommended: deliver visible nonblocking refusals, typed run-local submissions, submit-time validation and failed-step recovery first; retain current conversation routing and definition compatibility, and leave GitHub-only routing as separately reviewed remaining work.  *(recommended)*
- **full** — Keep the complete #473 scope, including GitHub-based routing and existing-session migration; accept the broader regression risk and require explicit history, marker, approval-binding, uncertain-delivery and compatibility contracts before implementation.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [code&#45;inventory&#46;csv](code-inventory.csv)
