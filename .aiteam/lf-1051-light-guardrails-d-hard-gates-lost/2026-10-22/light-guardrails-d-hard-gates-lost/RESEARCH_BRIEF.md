# Research brief

## Request

Shape the established D1–D8 findings into a buildable scope and settle material owner decisions.

## Decisions

- **d01** — This invocation shapes D1–D8 without code changes, implementation tasks, database mutations, commits, pushes or direct publication.
- **d02** — Use current checkout as behavior baseline; retain supplied issue notes as historical claims to verify, not instructions or proof of live incidents.
- **d03** — Cover all eight issue areas and retain existing working dispatch-stop and Bug owner-parking behavior.
- **d04** — Route pass-disagreement handbacks through the Implement coordinator and deduplicate by source comment rather than tested commit.
- **d05** — Persist owner guidance before consumption and acknowledge it at durable attempt-input delivery.
- **d06** — Treat branch distance, merge conflicts and missing planning context as visible caveats while preserving task identity and source evidence.
- **d07** — Retain exact internal command targets and require a new decision when the action resource changed.
- **d08** — Restart unsupported legacy pins through a linked current-graph successor after remote-effect reconciliation.
- **d09** — Make stop notices durable and unique per episode and park Research failures for owner input.
- **d10** — Exact-row repairs require owner-approved before and after values, task exclusion, transactional preconditions, backup, verification and conditional rollback.
- **d11** — The scope comment defines the proposed data records, interface behavior, permissions, validation rules, edge cases and acceptance conditions.


## Assumptions

- **a01** — Light guardrails permits owner-controlled quality and workflow choices but does not implicitly authorize remote effects, broaden tools or change task identity.
- **a02** — Repository workflow examples establish supported behavior, not which package/version a production session has pinned.
- **a03** — The supplied incident references motivate recovery research but are not verified reproductions in this pass.
- **a04** — Existing authenticated ticket-approver and code-owner identities remain the source of role membership; question q02 decides their waiver powers.
- **a05** — Untargeted owner guidance belongs to the current task and its next relevant reasoning step, based on the issue's request to preserve guidance written during execution.
- **a06** — No interim manual-edit exception is included unless the owner selects it in q03; the sanctioned repair is the proposed supported recovery path.


## Open questions

### q01 — Which blockers may an owner waive when shipping?

_Asked of: Product Owner Questions_

- **quality** — Recommended: waive failed or missing suites, unusable reviewer results and quality findings with a reason, but require a known source commit, confirmed process cleanup, resolved authority and reconciled remote effects; snapshot drift requires binding the new commit and showing its blockers before a fresh waiver.  *(recommended)*
- **all** — Allow all displayed blockers, including incomplete cleanup and snapshot drift, after the owner explicitly acknowledges each condition; authority and uncertain remote effects still require resolution.
- **narrow** — Limit waivers to failed tests and review findings; missing checks, missing reviews and operational blockers continue to hold shipping.

### q02 — Who may override ticket acceptance when the code owner and ticket approver differ?

_Asked of: Product Owner Questions_

- **split** — Recommended: retain the existing split; the ticket approver settles acceptance criteria and scope findings, and the code owner may waive technical checks and ship once those ticket decisions are settled.  *(recommended)*
- **either** — Either authenticated owner may waive every quality blocker and ticket acceptance decision; record which owner overrode the other role.
- **ticket** — Only the ticket approver may issue any waiver; the code owner retains the existing shipping role.

### q03 — How broad should the sanctioned exact-row repair be?

_Asked of: Technical Questions_

- **scoped** — Recommended: permit one exact task-owned row per approved repair step across sessions, runs, schedules, stories, graph instances, dispatches, attempts and owner waits, including their scheduling and state fields; prohibit identity, ownership, workflow capture, approval and remote-effect history changes, and reject repairs requiring unapproved related-row changes.  *(recommended)*
- **catalog** — Restrict repairs to an enumerated catalog of known transitions and add operations for wedged dispatches, stale waits and wrong graph states; future unknown state problems require another catalog change.
- **manual** — Provide the bounded repair above plus an interim manual-edit exception requiring a stopped daemon, consistent full backup, reviewed exact diff, preconditions, rollback and post-repair reconciliation; this expands operational authority before the sanctioned tool exists.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
