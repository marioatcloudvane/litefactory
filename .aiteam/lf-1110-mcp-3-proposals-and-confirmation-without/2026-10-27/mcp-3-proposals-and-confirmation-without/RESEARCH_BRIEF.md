# Research brief

## Request

Establish current foundations, reuse, collisions and evidence limits for #1110: hash-free owner confirmation of state-changing remote MCP proposals, factory-enforced lifecycle controls, impact tiers and shared Discord/MCP rules.

## Decisions

- **S-01** — mode=feature, size=large — shared action authority, Discord reaction approval, durable audit and compatibility changes span several existing workflows. (agent's call)
- **O-01** — Changing action tools return a proposal describing action, scope, cost or duration and exact target; confirm and cancel carry the proposal identifier while the owner uses plain words. (owner said: issue #1110 Behaviour)
- **O-02** — Enforce separate confirming calls, a minimum gap, expiry, single use and owner-visible confirmation audit including client, time and proposal on the factory side. (owner said: issue #1110 Behaviour)
- **O-03** — Reads need no confirmation; cheap reversible changes need conversational confirmation; binding actions additionally need an owner Discord ✅ tap. Discord and MCP share rules. (owner said: issue #1110 Tiers by impact)
- **K-01** — Preserve exact stored target binding, current authorization checks, atomic single-use admission and recovery of uncertain effects when sharing the existing proposal path. (agent's call)
- **N-01** — Treat the agent-job tool suite and task steering tool suite as adjacent slices #1111 and #1112, rather than silently incorporating their full implementation into this foundation. (agent's call)
- **A-01** — q01 "Continue from delivered findings?" — owner: "The Research findings above are delivered. Continue to Shaping with them; do not redo Research." (owner said: comment 6468384740)
- **O-04** — An owner can review a precise proposal, confirm or cancel it without typing an identifier, and inspect its current status and eventual execution result. (agent's call)
- **O-05** — Every proposal records immutable action arguments, repository and exact target revision, impact tier, owner, originating transport/client/grant, creation and expiry times, and an honest cost or duration estimate or explicit unknown. (agent's call)
- **O-06** — Use a factory-clock minimum delay of five seconds after proposal availability and a fifteen-minute lifetime, with a separate confirmation request and no automatic retry of an early confirmation. (agent's call)
- **O-07** — Binding actions require conversational confirmation followed by a ✅ from the configured owner on the factory's specific Discord request before the same proposal expires. (agent's call)
- **O-08** — Discord requests show the action, exact target, impact and estimate, and reactions from bots, other users, other messages or other emoji never authorize execution. (agent's call)
- **O-09** — Discord delivery failure or unavailability leaves binding proposals unexecuted with a visible reason and no bypass. (agent's call)
- **O-10** — At final admission recheck current owner authority, grant scopes and repository coverage, proposal lifetime and target revision, rejecting stale or revoked authority with a request for a fresh proposal where appropriate. (agent's call)
- **O-11** — Allow only explicitly classified actions: analysis, standard drafting and author revisions, and Plan or Research revision/approval use the conversational tier; ship, merge, Bug-fix approval and standard publication use the binding tier. (agent's call)
- **O-12** — Other changing actions require an explicit impact classification before exposure, and unknown actions are rejected rather than assigned the cheap tier. (agent's call)
- **O-13** — Confirmation, cancellation and reaction races admit execution at most once, while repeated requests return the recorded status without repeating external effects. (agent's call)
- **O-14** — Confirmation and cancellation are protocol controls that do not themselves require another proposal, and cancellation cannot promise to undo an execution already admitted. (agent's call)
- **O-15** — Persist an owner-only history for ninety days containing proposal summary and target, owner, originating client/grant or Discord identity, confirmation attempts and decisions with timestamps, reaction evidence, and execution result or uncertainty. (agent's call)
- **O-16** — Expose proposal inspection and paginated audit reads through the authenticated owner connector, and provide Discord confirmation acknowledgments with a history reference. (agent's call)
- **O-17** — Existing read grants retain only read authority, and MCP proposal creation and confirmation require fresh explicit action consent limited to selected repositories. (agent's call)
- **O-18** — At rollout invalidate pending proposals created under the old policy and ask owners to repropose, while preserving admitted executions and their recovery records. (agent's call)
- **O-19** — Route existing Discord conversational changes, including the currently direct standards-author actions, through the shared impact and proposal rules. (agent's call)
- **O-20** — Document the plain-word flow, timing, Discord prerequisite, permission renewal, audit access and recovery from stale, expired, cancelled or uncertain proposals. (agent's call)
- **O-21** — Acceptance must demonstrate both tiers end to end and rejection of early, expired, replayed, stale, cross-client, revoked-grant and unauthorized-reaction attempts, including restart and cancellation races. (agent's call)
- **K-02** — Keep read tools usable without action confirmation and preserve their existing repository authorization boundary. (agent's call)
- **K-03** — Keep internal integrity digests and distinguish execution admission, confirmed external success, failure and uncertain external outcome. (agent's call)
- **N-02** — Do not retire legacy CLI or GitHub hash commands or change worker submission authority in this issue. (agent's call)
- **N-03** — Do not add new job orchestration or stage-steering workflows from #1111 or #1112, a new approval website, configurable tier rules, or cost billing. (agent's call)
- **N-04** — Do not claim deployment, live connector approval or newly passed implementation checks from the retained historical tests. (agent's call)


## Assumptions

- **AS-01** — Use the remote owner connector as the MCP surface; attempt-scoped worker submission tools are a different authority boundary. This follows #1106 and current connector package separation. (agent's call)
- **AS-02** — Keep internal hashes/digests for integrity; 'without hashes' means the owner is not required to speak or copy them. Existing Discord proposals already retain digests internally. (agent's call)
- **AS-03** — Superseded for timing, lifetime, audit retention and unlisted actions by O-06, O-11, O-12 and O-15; Discord failure is settled by O-09. (agent's call)
- **AS-04** — Factory enforcement can prove identity, authority, timing, exact target and single use, but an MCP confirm call cannot independently prove that an owner spoke; q02 asks the owner to choose the required assurance. (agent's call)
- **AS-05** — Superseded by O-17 and O-18: renew action consent, invalidate old pending proposals and preserve admitted work. (agent's call)
- **AS-06** — Proposed scope for q03 is MCP proposal creation for already supported Discord actions plus confirm, cancel, status and audit reads, with new jobs and steering workflows left to #1111/#1112. (agent's call)
- **AS-07** — A Discord plain yes selects only the one current delivered proposal in that owner conversation; ambiguous confirmation does nothing and asks the owner to select a proposal in ordinary language. (agent's call)
- **AS-08** — Bind MCP confirmation and cancellation to the originating client and owner, requiring still-valid action authority; Discord owner cancellation can stop any still-pending proposal visible to that owner. (agent's call)
- **AS-09** — Ninety-day audit retention is a product default rather than a compliance requirement, and audit summaries exclude tokens and secret action content. (agent's call)


## Open questions

### q02 — What proof of your approval must cheap actions require?

_Asked of: Product Owner Questions_

- **a** — Accept conversational confirmation relayed by the authenticated MCP client (recommended): the factory enforces timing, target, permission and single use, while trusting the client to relay your yes; binding actions still require your Discord tap.  *(recommended)*
- **b** — Require your independent Discord tap for every changing action: stronger proof that you approved, but even cheap actions need Discord and this changes the requested conversational tier.

### q03 — Which action tools should this issue make available through MCP?

_Asked of: Product Owner Questions_

- **a** — Expose proposals for actions already supported by Discord, together with confirm, cancel, status and history (recommended): delivers usable MCP actions now while leaving new jobs and stage-steering workflows to #1111 and #1112.  *(recommended)*
- **b** — Build the shared lifecycle, confirm/cancel/status/history surfaces and Discord migration only: new MCP proposal-producing action tools arrive in #1111/#1112, so this issue alone cannot start an MCP action.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
