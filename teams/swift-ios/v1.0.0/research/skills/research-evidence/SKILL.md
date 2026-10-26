---
name: research-evidence
description: Preserve bounded Research findings, distinguish observations from assumptions, keep the outcome ledger (S-, O-, N-, K-, A- decisions), run the impact analysis, and submit the node result the factory accepts.
resources:
  - evidence.md
  - node-result.md
---
Investigate the requested scope using authorized read operations. Preserve findings and references in the supplied session, distinguishing observed facts, inference and unavailable evidence. See [evidence rules](evidence.md). Read [node result](node-result.md) before you submit: it carries the worked result for each response. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

The generic `lf_agent` retains your validated candidate. The explicit Research-only
`lf_research_brief` activity then generates both session documents from that same
candidate before `lf_comment` reports it or `lf_ask` opens a question/review wait.
Do not generate the documents yourself. A generation or artifact-validation failure
blocks successful reporting and handoff; private agent completion grants no approval.

When another investigation pass is needed without an owner decision, submit an incomplete result with a nonblank reason, cumulative partial findings, `payload.research.continue=true` and no questions. The factory generates the partial brief and runs this same stage again without a comment, within its persisted allowance. Preserve applicable findings and evidence from prior passes. Real owner questions still use the question route.

# Outcome ledger
Decisions are the outcome ledger Plan reads in RESEARCH_BRIEF.md. Give each a prefixed ID:
- `S-01` work mode and size: `mode=<…>[+<…>], size=small|medium|large — <reason>`; update it under the same ID if the task grows.
- `O-nn` agreed outcome: what must be true when the work is done.
- `N-nn` non-goal: explicitly out of scope.
- `K-nn` must not change: behaviour, data or interfaces the work must keep.
- `A-nn` owner answer: the question it answers, then the owner's reply quoted verbatim, e.g. `q01 "Which channels?" — owner: "b - all!"`. Never paraphrase it away.

End each entry with its source: `(owner said: "<quote or reference>")` or `(agent's call)`. IDs are stable: never renumber or reuse one. Change an entry under its own ID; an entry that no longer holds stays, marked `superseded by <ID>`. Notes about the run itself (passes, tools, retries) are not decisions.

# Impact analysis
Every mode, scaled to size. Look before you conclude, and record each point as a finding with evidence:
- **Touched:** packages and files, config, docs, tests, team roles and skills.
- **Depends on it:** callers, CLI commands, other modes and workflows, external clients.
- **Superseded:** what the new work replaces. Retiring or migrating it is its own `O-`; what must keep working until then is a `K-`.
- **Data and compatibility:** migrations, existing installations and their state, team releases.
Say "none found" with where you looked rather than leaving a point out.
