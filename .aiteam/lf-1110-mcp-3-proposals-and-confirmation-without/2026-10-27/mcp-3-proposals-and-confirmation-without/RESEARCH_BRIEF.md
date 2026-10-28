# Research brief

## Request

Establish current foundations, reuse, collisions and evidence limits for #1110: hash-free owner confirmation of state-changing remote MCP proposals, factory-enforced lifecycle controls, impact tiers and shared Discord/MCP rules.

## Decisions

- **S-01** — mode=feature, size=medium — adds remote action authority over existing read-only MCP and reuses a cross-package Discord confirmation lifecycle. (agent's call)
- **O-01** — Changing action tools return a proposal describing action, scope, cost or duration and exact target; confirm and cancel carry the proposal identifier while the owner uses plain words. (owner said: issue #1110 Behaviour)
- **O-02** — Enforce separate confirming calls, a minimum gap, expiry, single use and owner-visible confirmation audit including client, time and proposal on the factory side. (owner said: issue #1110 Behaviour)
- **O-03** — Reads need no confirmation; cheap reversible changes need conversational confirmation; binding actions additionally need an owner Discord ✅ tap. Discord and MCP share rules. (owner said: issue #1110 Tiers by impact)
- **K-01** — Preserve exact stored target binding, current authorization checks, atomic single-use admission and recovery of uncertain effects when sharing the existing proposal path. (agent's call)
- **N-01** — Treat the agent-job tool suite and task steering tool suite as adjacent slices #1111 and #1112, rather than silently incorporating their full implementation into this foundation. (agent's call)
- **A-01** — q01 "Continue from delivered findings?" — owner: "The Research findings above are delivered. Continue to Shaping with them; do not redo Research." (owner said: comment 6468384740)


## Assumptions

- **AS-01** — Use the remote owner connector as the MCP surface; attempt-scoped worker submission tools are a different authority boundary. This follows #1106 and current connector package separation. (agent's call)
- **AS-02** — Keep internal hashes/digests for integrity; 'without hashes' means the owner is not required to speak or copy them. Existing Discord proposals already retain digests internally. (agent's call)
- **AS-03** — Numeric minimum delay, proposal lifetime, audit retention, tier treatment of unlisted commands, and handling Discord unavailability remain shaping points; this pass does not choose those values. (agent's call)
- **AS-04** — Model-independent safety means enforcing lifecycle and target authority in the factory. An authenticated model-operated MCP call is not independently verified evidence of owner speech; shaping must state the assurance limit for conversational confirmation. (agent's call)
- **AS-05** — Existing pending Discord proposals and OAuth grants require an explicit compatibility decision before migration; do not infer new write authority from existing read grants. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
