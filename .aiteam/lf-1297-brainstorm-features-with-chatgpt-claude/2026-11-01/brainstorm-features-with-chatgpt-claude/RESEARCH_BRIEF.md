# Research brief

## Request

Let the owner brainstorm a feature with ChatGPT/Claude (voice, documents) against the real factory, keep it as a durable brainstorm, and turn it into task drafts that become GitHub issues which start in Research, Plan or parked. Also teach clients about the factory through server instructions, MCP prompts and a project template.

## Decisions

- **S-01** — mode=feature+integration+agent-guidance, size=large — new durable brainstorm storage, a new action type, an MCP prompts capability and guidance content; split into independent parts. (agent's call)
- **O-01** — The owner can brainstorm by voice with a client, with a durable resumable brainstorm that keeps ideas, decisions, open questions, related issues and document-derived notes. (owner said: issue #1297 user journey 1-5)
- **O-02** — Task drafts carry goal, scope, non-goals, open questions in O-/N-/K-/A- form, an S/M/L size, related issues, a duplicate warning and a recommended start (Research, Plan or park). (owner said: issue #1297 user journey 6)
- **O-03** — Create-and-start goes through the existing proposal and confirmation flow, creates exactly one issue per confirmed draft and posts /research or /plan itself. (owner said: issue #1297 acceptance)
- **O-04** — A client with no project instructions learns from server instructions and MCP prompts how to brainstorm with the factory; a project template is generated from the same markdown. (owner said: issue #1297 container section)
- **N-01** — The factory does not process, store or forward raw documents; the client extracts and cites the document name as source. (owner said: issue #1297 user journey 5)
- **K-01** — Existing proposal, confirmation, tier and Discord reaction rules for current actions stay unchanged, and a discussion answer or brainstorm grants no workflow approval. (agent's call)


## Assumptions

- **a1** — Created issues are linked to the brainstorm by a marker in the issue body and the brainstorm summary is posted as an issue comment. Resting on how the issue describes 'stays on the issue as context'. (agent's call)
- **a2** — The project template is generated from the same guidance markdown at build or request time, not hand-written. Rests on the issue's 'never drifts' wording. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
