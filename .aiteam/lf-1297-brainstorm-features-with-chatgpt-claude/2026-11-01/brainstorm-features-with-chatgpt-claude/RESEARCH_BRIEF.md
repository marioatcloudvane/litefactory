# Research brief

## Request

Let the owner brainstorm a feature with ChatGPT/Claude (voice, documents) against the real factory, keep it as a durable brainstorm, and turn it into task drafts that become GitHub issues which start in Research, Plan or parked. Also teach clients about the factory through server instructions, MCP prompts and a project template.

## Decisions

- **S-01** — mode=feature+integration+agent-guidance, size=large — new brainstorm storage and scope, a context scan, issue search, task drafts, a create-and-start action, an MCP prompts capability and guidance content; Plan should split it into independent stories, with the guidance and prompts part deliverable first. (agent's call)
- **O-01** — The owner can open, add to and resume a durable brainstorm per repository from a connector client; it keeps ideas, decisions, open questions, related issues and document-derived notes, survives a dropped connection and can be resumed at the desk. (owner said: issue #1297 user journey 1-5)
- **O-02** — Task drafts carry goal, scope, non-goals, open questions in O-/N-/K-/A- form, an S/M/L size, related issues, a duplicate warning and a recommended start (Research, Plan or park). (owner said: issue #1297 user journey 6)
- **O-03** — Create-and-start goes through the existing proposal and confirmation flow, creates exactly one issue per confirmed draft, and then starts the chosen workflow itself so the issue never sits idle; it does so by a direct launch plus a short factory note, not by posting /research or /plan (see f12). (owner said: issue #1297 acceptance; mechanism agent's call)
- **O-04** — A client with no project instructions learns from server instructions and MCP prompts how to brainstorm with the factory; a project template is generated from the same markdown. (owner said: issue #1297 container section)
- **O-05** — A read-only context scan per repository (code, open and closed issues, recent work, standards) runs as a background job, returns a short spoken answer plus evidence, and can be read or cancelled later, following the advisory job pattern. (owner said: issue #1297 user journey 3)
- **O-06** — A related and duplicate issue search tool returns matching open and closed issues for a repository, used both in conversation and for each draft's duplicate warning. (owner said: issue #1297 building blocks)
- **O-07** — Park means the issue is created and no workflow starts; the brainstorm records which drafts were parked. (agent's call)
- **O-08** — Server instructions start with a short factory overview (what the factory is, its modes, which tool fits which intent, how proposals work); the prompts capability offers at least "Brainstorm a feature with the factory", "What's waiting for me?" and "Review task #N". (owner said: issue #1297 container section)
- **O-09** — Brainstorm and scan answers meant for speech are short and plain: no tables, hashes or links, issue numbers spoken as numbers; evidence stays available in structured fields. (owner said: issue #1297 user journey 2)
- **O-10** — Each created issue carries the draft ledger, a short brainstorm summary and a Sources list naming each document by name, plus a marker linking it to the brainstorm; the owner gets one short Discord summary per confirmed proposal. (owner said: issue #1297 user journey 7; layout agent's call)
- **N-01** — The factory does not process, store or forward raw documents; the client extracts and cites the document name as source. (owner said: issue #1297 user journey 5)
- **N-02** — No code-writing or issue-editing tools: the brainstorm and scan stay read-only, and duplicates are only warned about, never closed or merged. (agent's call)
- **N-03** — No sharing of a brainstorm between people and no second approver; one owner per installation as today. (agent's call)
- **N-04** — Brainstorming from Discord (Nico) is not part of this issue; storage stays channel-neutral so a follow-up can add it. (agent's call, pending q4)
- **N-05** — No Linear support for create-and-start in this issue; GitHub repositories only. (agent's call)
- **K-01** — Existing proposal, confirmation, tier and Discord reaction rules for current actions stay unchanged, and a discussion answer or brainstorm grants no workflow approval. (agent's call)
- **K-02** — Existing tools, scopes and grants behave exactly as today; the new capabilities need a new scope the owner consents to, and an old grant sees no new tools. (agent's call)
- **K-03** — The rule that the factory ignores its own comments, and only runs an issue body written by the project approver, stays unchanged. (agent's call)


## Assumptions

- **a1** — Created issues are linked to the brainstorm by a marker in the issue body and the brainstorm summary is part of the issue body (see O-10). Rests on 'stays on the issue as context'. (agent's call)
- **a2** — The project template is generated from the same guidance markdown at build or request time, not hand-written. Rests on the issue's 'never drifts' wording. (agent's call)
- **a3** — Brainstorms get a new repository-selected scope (no all or future repositories), like task:advisory; deep scans also need the advisory cost note. Create-and-start sits under repository:actions. (agent's call)
- **a4** — Document-derived notes are capped at 4 KB per note and 64 KB total per brainstorm, each tagged with the document name; the client is told when the cap is hit. Rests on existing connector bounds (f15). (agent's call)
- **a5** — A brainstorm stays open until the owner closes it or 30 days pass without activity; closed brainstorms stay readable. (agent's call)
- **a6** — A Plan start on a fresh issue uses the issue body's ledger as Plan's input; no Research brief is generated. (agent's call)
- **a7** — How to tell it works: the manual acceptance in the issue (a voice brainstorm ends in a started Research task without typing), plus automated tests that one confirmed proposal makes exactly one issue per draft, a replay makes none, a factory note never triggers a run, and a fresh client sees the overview and prompts. (agent's call)
- **a8** — The first deliverable is the guidance part (overview, prompts, template), because it is useful alone and the rest builds on it. Plan may reorder. (agent's call)


## Open questions

### q1 — How do you confirm create-and-start: a spoken yes, or a yes plus a Discord ✅?

_Asked of: Product Owner Questions_

- **a** — Spoken yes is enough (conversational), like starting a log analysis today. Works in the car with no phone tap. One proposal can hold several drafts; you see the titles and starts before saying yes. (recommended: issues are reversible and you are already the only person on the grant)  *(recommended)*
- **b** — Yes plus a Discord ✅ (binding), like approving a stage. Safer against a misheard yes, but you must tap your phone for every batch.
- **c** — Mixed: spoken yes for park and Research, ✅ only when a draft starts in Plan.

### q2 — When may a draft recommend starting directly in Plan instead of Research?

_Asked of: Product Owner Questions_

- **a** — Only when the draft has a goal, at least one outcome and one non-goal, no open questions, size S or M, and no duplicate warning. Otherwise it recommends Research. You can still override in the conversation. (recommended: Plan needs a settled scope; anything less wastes a Plan run)  *(recommended)*
- **b** — Always recommend Research; you pick Plan by saying so.
- **c** — No rule; the assistant judges each draft.

### q3 — Do context scans run on their own when you ask "do we already have…?"

_Asked of: Product Owner Questions_

- **a** — Cheap lookups (issue search, code search) run on their own and answer in seconds. The deep scan, which runs a paid agent, starts only when you ask for it or say yes to the assistant's offer. (recommended: fast answers without surprise cost)  *(recommended)*
- **b** — Every such question starts a deep scan automatically. Best answers, highest cost.
- **c** — Nothing runs without you asking, not even the cheap lookups.

### q4 — Should Nico (the Discord assistant) also get brainstorms in this issue?

_Asked of: Product Owner Questions_

- **a** — Not now. Build it for ChatGPT/Claude only, but store brainstorms in a way Discord can use later. A follow-up issue adds Nico. (recommended: keeps this already large issue from growing)  *(recommended)*
- **b** — Yes, in this issue: Nico can open, resume and turn a brainstorm into tasks too.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
