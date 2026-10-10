---
name: research-evidence
description: Preserve bounded Research findings, distinguish observations from assumptions, and submit the node result the factory accepts.
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
