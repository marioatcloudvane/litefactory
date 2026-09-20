---
name: research-evidence
description: Preserve bounded Research findings, distinguish observations from assumptions, and return the node result the factory accepts.
resources:
  - evidence.md
  - node-result.md
---
Investigate the requested scope using authorized read operations. Preserve findings and references in the supplied session, distinguishing observed facts, inference and unavailable evidence. See [evidence rules](evidence.md). Read [node result](node-result.md) before you return: it carries the worked envelope for each response. Return the validated result through the invocation's factory-supplied result contract; do not publish directly.

The generic `lf_agent` retains your validated candidate. The explicit Research-only
`lf_research_brief` activity then generates both session documents from that same
candidate before `lf_comment` reports it or `lf_ask` opens a question/review wait.
Do not generate the documents yourself. A generation or artifact-validation failure
blocks successful reporting and handoff; private agent completion grants no approval.
