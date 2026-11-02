# Research brief

## Request

Let the owner brainstorm a feature with ChatGPT/Claude (voice, documents) against the real factory as a durable Research discussion with the team's research agents, and turn it into GitHub issues that start directly in Plan from the brainstorm's brief. Also teach clients about the factory through server instructions, MCP prompts and a project template.

## Decisions

- **S-01** — mode=feature+integration+agent-guidance, size=large — brainstorm storage and scope, the two research agents run as a conversation, automatic scans, issue search, drafts, create-and-start into Plan, MCP prompts and guidance; Plan should split it into independent stories, guidance and prompts first. (agent's call, updated after A-02)
- **O-01** — The owner can open, add to and resume a durable brainstorm per repository from a connector client; it keeps ideas, decisions, open questions, related issues and document-derived notes, survives a dropped connection and can be resumed at the desk. (owner said: issue #1297 user journey 1-5)
- **O-02** — Task drafts carry goal, scope, non-goals, must-not-change items and open questions in O-/N-/K-/A- form, an S/M/L size, an impact analysis, related issues and a duplicate warning; there is no start recommendation because every draft goes to Plan. (owner said: issue #1297 journey 6 and A-02)
- **O-03** — After the owner's spoken yes, create-and-start creates exactly one issue per confirmed draft, writes that draft's brief where Plan reads it, and launches Plan directly with a short factory note; it never posts /plan as a comment (f12, f17). (owner said: A-01, A-02; mechanism agent's call)
- **O-04** — A client with no project instructions learns from server instructions and MCP prompts how to brainstorm with the factory; a project template is generated from the same markdown. (owner said: issue #1297 container section)
- **O-05** — Every factual brainstorm question ("do we already have…", "which code", "is there an issue", "how did we solve #N") starts a read-only deep scan by the Research agent automatically, as a background job, with a short spoken answer plus evidence. (owner said: A-03)
- **O-06** — A related and duplicate issue search tool returns matching open and closed issues for a repository, used both in conversation and for each draft's duplicate warning. (owner said: issue #1297 building blocks)
- **O-07** — superseded by O-03 — no park and no Research start; every confirmed draft goes straight to Plan. (owner said: A-02)
- **O-08** — Server instructions start with a short factory overview (what the factory is, its modes, which tool fits which intent, how proposals work); the prompts capability offers at least "Brainstorm a feature with the factory", "What's waiting for me?" and "Review task #N". (owner said: issue #1297 container section)
- **O-09** — Brainstorm and scan answers meant for speech are short and plain: no tables, hashes or links, issue numbers spoken as numbers; evidence stays available in structured fields. (owner said: issue #1297 user journey 2)
- **O-10** — Each created issue carries the draft ledger, a short brainstorm summary and a Sources list naming each document by name, plus a marker linking it to the brainstorm; the owner gets one short Discord summary per confirmed proposal. (owner said: issue #1297 user journey 7; layout agent's call)
- **N-01** — The factory does not process, store or forward raw documents; the client extracts and cites the document name as source. (owner said: issue #1297 user journey 5)
- **N-02** — No code-writing or issue-editing tools: the brainstorm and scan stay read-only, and duplicates are only warned about, never closed or merged. (agent's call)
- **N-03** — No sharing of a brainstorm between people and no second approver; one owner per installation as today. (agent's call)
- **N-04** — Brainstorming from Discord (Nico) is out of scope. (owner said: A-04)
- **N-05** — No Linear support for create-and-start in this issue; GitHub repositories only. (agent's call)
- **K-01** — Existing proposal, confirmation, tier and Discord reaction rules for current actions stay unchanged, and a discussion answer or brainstorm grants no workflow approval. (agent's call)
- **K-02** — Existing tools, scopes and grants behave exactly as today; the new capabilities need a new scope the owner consents to, and an old grant sees no new tools. (agent's call)
- **K-03** — The rule that the factory ignores its own comments, and only runs an issue body written by the project approver, stays unchanged. (agent's call)
- **O-11** — The brainstorm is a Research discussion with the team's two research agents run outside the graph: the Research agent answers facts and scans, the Shaping agent turns the brainstorm into drafts and asks its open questions in the conversation. (owner said: A-02)
- **O-12** — The two research roles are adapted once: one new shared brainstorm skill in the base team carries the out-of-graph contract (live conversation, no loop, short spoken answers, research payload as output), and each role gets a one-line pointer to it; no role copies, no Plan roles in the brainstorm. (owner said: A-06)
- **O-13** — A brainstorm-born issue runs the normal Plan workflow from its brief, with all Plan owner gates as today. (owner said: A-02)
- **N-06** — Talking to the Plan agents during a running Plan (ask or judge a Plan stage) is not in this issue; it is a separate follow-up issue. (owner said: A-05)
- **N-07** — No Research graph run and no builder check for brainstorm-born issues; the brainstorm replaces Research for them. (agent's call)
- **K-04** — The Research graph, the two research roles' behaviour inside it and their result contract stay unchanged. (agent's call)
- **K-05** — The spoken yes approves only creating issues and starting Plan; it approves no Plan stage. (agent's call)
- **A-01** — q1 "How do you confirm create-and-start: a spoken yes, or a yes plus a Discord ✅?" — owner: "a" (owner said)
- **A-02** — q2 "When may a draft recommend starting directly in Plan instead of Research?" — owner: "i think it should always start in a research discussion with the agents; when this finishes, an issue should be created but directly in plan mode and the planner should sketch it out. from there on, we would again follow the path of the planning workflow but also here we could use the agents of course. also, i would say that the agents somehow re-use the planning agents, but without the graph; sketch out how we would need or if at all we would need to adapt the two agents we have in the research mode!?" (owner said)
- **A-03** — q3 "Do context scans run on their own when you ask \"do we already have…?\"" — owner: "b" (owner said)
- **A-04** — q4 "Should Nico (the Discord assistant) also get brainstorms in this issue?" — owner: "no, niko is not in scope here" (owner said)
- **A-05** — q5 "Should talking to the Plan agents during a running Plan be part of this issue?" — owner: "a" (owner said)
- **A-06** — q6 "How should the brainstorm reuse the two Research agents?" — owner: "a" (owner said)
- **O-14** — The issue draft acceptance changes from "a task that starts in Research" to "a task that starts in Plan with the brainstorm brief", following A-02. (owner said: A-02)


## Assumptions

- **a1** — Created issues are linked to the brainstorm by a marker in the issue body and the brainstorm summary is part of the issue body (see O-10). Rests on 'stays on the issue as context'. (agent's call)
- **a2** — The project template is generated from the same guidance markdown at build or request time, not hand-written. Rests on the issue's 'never drifts' wording. (agent's call)
- **a3** — Brainstorms get a new repository-selected scope (no all or future repositories) whose consent page carries the paid-agent cost note, since scans now run automatically (A-03). Create-and-start sits under repository:actions. (agent's call)
- **a4** — Document-derived notes are capped at 4 KB per note and 64 KB total per brainstorm, each tagged with the document name; the client is told when the cap is hit. Rests on existing connector bounds (f15). (agent's call)
- **a5** — A brainstorm stays open until the owner closes it or 30 days pass without activity; closed brainstorms stay readable. (agent's call)
- **a6** — superseded by O-03 — the brainstorm brief is written to the new task's session directory on its task branch as RESEARCH_BRIEF.md, rendered by the existing brief generator from the Shaping agent's ledger. (agent's call)
- **a7** — How to tell it works: manually, a voice brainstorm ends in a new issue whose Plan has started from the brainstorm brief, without typing; automatically, one confirmed proposal makes exactly one issue per draft, a replay makes none, a factory note never triggers a run, Plan finds the brief on the new task, and a fresh client sees the overview and prompts. (agent's call)
- **a8** — The first deliverable is the guidance part (overview, prompts, template), because it is useful alone and the rest builds on it. Plan may reorder. (agent's call)
- **a9** — Only one deep scan runs at a time per brainstorm; further questions queue, and each answer states its cost estimate or that cost is unknown. (agent's call)
- **a10** — Brainstorm agents read the repository's default branch, read-only; they get no worktree for writing. (agent's call)
- **a11** — A team version without the brainstorm skill refuses brainstorms with a plain message until the owner releases and installs one that has it. (agent's call)
- **a12** — The brainstorm agents return the existing research payload (findings, decisions, assumptions), so the brief generator and Plan read it unchanged. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
