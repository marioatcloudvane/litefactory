# Research brief

## Request

Add one proposal tool (bug_file_propose: repository, title, description) that the owner confirms with action_confirm under repository:actions. On confirm the factory creates exactly one issue labelled bug (idempotent on replay), returns its URL, and handles it as a normal Bug. Overview guidance tells clients to use it for 'file a bug' and never to invent a workflow path.

## Decisions

- **S-01** — mode=feature+integration, size=medium — new connector tool and owner action on existing proposal machinery, a GitHub create-and-label write, and an automatic Bug start. (agent's call)
- **O-01** — One bug_file_propose proposal, one action_confirm, exactly one GitHub issue labelled bug, URL returned, replay creates no second issue. (owner said: issue #1422 Expectations)
- **O-02** — Overview guidance tells clients to use bug_file_propose when the owner says file a bug, and never to invent a workflow path. (owner said: issue #1422 Expectations)
- **O-03** — Once the issue is filed and labelled, the factory starts Bug analysis on it without an owner comment; repair still waits for /approve-bug. (owner said: "The factory then handles it like any Bug, including starting on its own.")
- **O-04** — The client can report the outcome in plain words: the issue URL once filed (from confirm or action_inspect), or a plain refusal or failure reason, and 'filed but not yet labelled or started' is shown as such and recovered rather than refiled. (agent's call)
- **O-05** — bug_file_propose takes three required inputs, repository, title (at most 256 characters) and description (at most 8 KiB), with no optional fields; its guidance asks the client to put expected vs actual, steps and evidence into the description. (agent's call; field list owner said: issue #1422 Expectations)
- **O-06** — The same action is listed in the owner-action catalogue under one name, so action_propose reaches it too and both routes give the same proposal. (agent's call)
- **O-07** — The filed issue body is the owner's description plus one short line saying it was filed for the owner from a chat client, and a hidden marker used only for replay safety. (agent's call)
- **K-01** — No new consent scope: repository:actions with chosen repositories covers it. (owner said: issue #1422 Expectations)
- **K-02** — Existing action_propose/action_confirm behaviour, workflow_propose and brainstorm task actions must keep working unchanged. (agent's call)
- **K-03** — The Bug workflow itself stays as it is: agent selection, read-only analysis, /approve-bug, repair, delivery, and no labels added beyond bug. (agent's call)
- **K-04** — Existing refusals stay as they are: unregistered repository, repository outside the grant, missing scope, expired or early confirmation, and the redaction check. (agent's call)
- **N-01** — No brainstorm, no Plan start, no factory internals for the owner to decide. (owner said: issue #1422 Goal)
- **N-02** — No duplicate search or merging before filing; filing is fire-and-forget, and a client may search issues separately where it is allowed. (agent's call)
- **N-03** — No Linear filing; a Linear-backed project is refused with a plain reason, because the factory cannot file Bugs on Linear today. (agent's call)
- **N-04** — No Mode, Coding Agent or other label, and no parent or sub-issue link, on the filed issue. (agent's call)
- **N-05** — No fix here for #1421 (empty optional fields refused); it stays a separate Bug. (agent's call)
- **N-06** — No binding tier and no Discord reaction: one conversational confirmation is enough. (agent's call, from owner's 'one confirmation')
- **N-07** — No file or screenshot uploads; evidence is text in the description. (agent's call)


## Assumptions

- **a1** — A dedicated tool name bug_file_propose is added in the connector as a thin wrapper that resolves into the same owner-action proposal as action_propose; confirmation stays with action_confirm. (agent's call)
- **a2** — The bug label is applied by a second call after create, with reconcile-by-marker for replay. (agent's call, from the Validate follow-up pattern)
- **a3** — Tier is conversational (one confirmation), like draft and brainstorm_create_tasks, not binding (no Discord reaction). The issue asks for one confirmation. (agent's call)
- **a4** — Title and description go through the same redaction check as brainstorm issues; a change made by redaction refuses the proposal. (agent's call)
- **a5** — The automatic start follows the brainstorm launch precedent (f12): a private owner-authored start that is never posted on the issue, attempted once, and reported as filed but not started if it does not begin. (agent's call)
- **a6** — The issue is created with the factory's own GitHub credential, as other factory-filed Bugs are, not with the owner's account. (agent's call)
- **a7** — Proof: fakes cover replay, a crash between create and label, refusals and the automatic start; only one owner-run real filing on a test repository can show a live GitHub issue and its analysis. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
