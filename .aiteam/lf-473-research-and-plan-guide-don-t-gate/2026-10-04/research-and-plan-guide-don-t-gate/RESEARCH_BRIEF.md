# Research brief

## Request

Shape #473 into a reviewable scope for simplifying the shared Research and Plan core, preserving successful stages and owner approval; no implementation or mode change.

## Decisions

- **d1** — The owner prioritizes simplification while preserving the working Research and Plan stage behavior.
- **d2** — The shape stage proposes a shared-core scope for owner review without implementing changes or granting approval.
- **d3** — GitHub comments and stable factory markers are the authority for owner guidance, while the issue lock serializes execution.
- **d4** — Local persistence is limited conceptually to issue lock, issue budget and run log, with accepted inputs and external-effect recovery records inside the run log.
- **d5** — Permanent reply refusals receive a visible explanation and consumption, while transient reporting failures park delivery without blocking later comment intake.
- **d6** — Every step gets typed inherited fields with a response enum from its declared outcomes and no agent-facing revision or patch operations.
- **d7** — Accepted submissions replace the complete current-run result, clear omitted optional fields and never inherit prior-run values.
- **d8** — Preflight and submit enforce the same complete validation and one 1 MiB serialized accepted-result ceiling.
- **d9** — Validated artifacts are captured at submission so later process exits or artifact edits cannot invalidate accepted answers.
- **d10** — Comment-only submissions lacking required response are rejected without attributing any historical outcome or reason.
- **d11** — Retrying a parked factory effect reuses accepted input and never reruns an already successful agent.
- **d12** — Owner replies reset the stopped step's retry allowance but do not reset or bypass the issue budget.
- **d13** — Captured definitions continue existing workflows, with no automatic topology migration on shipped definition changes.
- **d14** — Existing graph conversations and pending effects retain recoverability while supported legacy routes and activation defaults remain unchanged.
- **d15** — Existing permissions and explicit owner approval requirements remain authoritative throughout routing and recovery.
- **d16** — Regression evidence must cover queue progress, process-failure survival, typed schemas, replacement isolation, uncertain effects and cheap-step retry in both modes.
- **d17** — Code counts are repeated using the saved inventory method with no arbitrary deletion target.


## Assumptions

- **a1** — Preserve the earlier disjoint scoped code inventory and its limits as the measurement baseline rather than an exhaustive mode attribution.
- **a2** — Historical retry totals and attribution examples remain owner reports; no live daemon observations, tests or production changes occurred in this shaping pass.
- **a3** — Treat pending external-effect metadata and processed-comment references as run-log facts, not separate authoritative conversation state, to satisfy simplification and safe recovery together.
- **a4** — Use the existing stricter 1 MiB retained-result ceiling as the single submit limit because the owner requests one limit but does not request larger results.
- **a5** — Preserve each stage's existing content fields and approval semantics when generating typed schemas; only the shared transport and run isolation change.
- **a6** — Continue a captured workflow definition rather than automatically migrating topology, since preserving working stages and recoverability is the owner's stated priority.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [code&#45;inventory&#46;csv](code-inventory.csv)
