# Research brief

## Request

Establish the facts and sketch feasibility for #1112: discuss architecture/research/plans through remote MCP, ask the current stage agent or judge, request revisions and approve exact stages without speaking revision hashes. Answer the owner's concern about how much context must cross MCP.

## Decisions

- **S-01** — mode=feature, size=medium — bounded feasibility research for a new task-steering surface spanning existing read, approval, judge and stage-agent foundations; full implementation design belongs to shaping. (agent's call)
- **A-01** — Owner direction for this step — owner: "sketch this out! would this actually allow me to ask questions and discuss a task? e.g. the architecture? this would mean that the mcp would need to send over a lot of context, right?" (owner said: comment 6543449564)
- **O-01** — Enable back-and-forth discussion of a story's architecture, plan or research, with agent/judge consultation and revision or approval without owner-copied commands or revision hashes. (owner said: issue #1112 Goal and Tools)
- **O-02** — Questions and answers stay in chat; a revision or approval decision produces one GitHub summary of the questions discussed and the decision, through the same owner path as GitHub actions. (owner said: issue #1112 Record on GitHub)
- **K-01** — Preserve exact revision and upstream binding, authenticated owner authority, consent, single-use confirmation and stale-target refusal while hiding internal identifiers from speech. (agent's call)
- **K-02** — A question must preserve task stage, candidate and approvals; existing GitHub owner workflows and read-only grants must keep working. (owner said: issue #1112 asks the stage agent without changing the task; agent's call for compatibility)
- **N-01** — This pass sketches feasibility and establishes reuse and gaps; implementation, deployment and live voice/client verification are outside this research step. (agent's call)


## Assumptions

- **AS-01** — Use the remote owner connector, not worker-attempt MCP tools. Fetch only relevant evidence for ordinary chat, and keep expensive agent context loading within the factory for explicit stage-agent questions. This follows #1106's two-path conversation model. (agent's call)
- **AS-02** — Treat saved artifacts and code as the authoritative basis for explanations; do not promise retrieval of hidden reasoning or guaranteed continuation of the exact original model session. Current native resume may fail and fall back cold. (agent's call)
- **AS-03** — Open for shaping: the stage matrix for Research/Plan/Bug, explicit architecture consultation when another stage is current, operation/job polling and cancellation, confirmation classification for paid advisory jobs, bounded follow-up history, summary handoff from chat, concurrent stage changes, and safe recovery of decision-summary publication. Existing sources establish the seams but do not settle these contracts. No owner-only fact blocks this research handoff. (agent's call)
- **AS-04** — #1111's open status is a dependency coordination risk, not proof that #1112 must duplicate its job layer. Shaping should establish the smallest common job contract before deciding the delivery order. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
