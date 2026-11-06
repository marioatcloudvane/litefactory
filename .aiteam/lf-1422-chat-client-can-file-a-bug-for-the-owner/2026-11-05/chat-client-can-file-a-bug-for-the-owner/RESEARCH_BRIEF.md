# Research brief

## Request

Add one proposal tool (bug_file_propose: repository, title, description) that the owner confirms with action_confirm under repository:actions. On confirm the factory creates exactly one Bug issue on the project's ticket tracker (GitHub with the bug label, or Linear with the project's bug label), idempotent on replay, returns its link, and starts it as a normal Bug. Overview guidance tells clients to use it for 'file a bug' and never to invent a workflow path.

## Decisions

- **S-01** — mode=feature+integration, size=large — new connector tool and owner action on existing proposal machinery, create-and-label writes on both GitHub and Linear, a Linear bug-label setting and recognition, and an automatic Bug start. (agent's call; grew from medium after A-01)
- **O-01** — One bug_file_propose proposal, one action_confirm, exactly one issue carrying the bug label on the project's ticket tracker (GitHub or Linear), its link returned, and a replay creates no second issue. (owner said: issue #1422 Expectations, extended by A-01)
- **O-02** — Overview guidance tells clients to use bug_file_propose when the owner says file a bug, and never to invent a workflow path. (owner said: issue #1422 Expectations)
- **O-03** — Once the issue is filed and labelled, on GitHub or Linear, the factory starts Bug analysis on it without an owner comment; repair still waits for /approve-bug. (owner said: "The factory then handles it like any Bug, including starting on its own." and A-01)
- **O-04** — The client can report the outcome in plain words: the issue URL once filed (from confirm or action_inspect), or a plain refusal or failure reason, and 'filed but not yet labelled or started' is shown as such and recovered rather than refiled. (agent's call)
- **O-05** — bug_file_propose takes three required inputs, repository, title (at most 256 characters) and description (at most 8 KiB), with no optional fields; its guidance asks the client to put expected vs actual, steps and evidence into the description. (agent's call; field list owner said: issue #1422 Expectations)
- **O-06** — The same action is listed in the owner-action catalogue under one name, so action_propose reaches it too and both routes give the same proposal. (agent's call)
- **O-07** — The filed issue body is the owner's description plus one short line saying it was filed for the owner from a chat client, and a hidden marker used only for replay safety. (agent's call)
- **O-08** — For a project whose ticket connection is Linear, bug_file_propose creates the issue in that connection's team and project, adds the Linear label the owner has chosen as the bug label, starts Bug analysis the same way, and is replay-safe like GitHub. (owner said: A-01)
- **O-09** — The Linear connection gets one optional setting naming the existing Linear label to treat as bug (its label ID, set like the mode labels); a Linear project without it is refused with a plain remedy that says how to set it. (agent's call: the adapter writes only explicitly mapped labels, f17)
- **O-10** — On Linear, an issue carrying that mapped label is recognised as a Bug, exactly as the bug label is on GitHub, so the analysis and later owner replies follow the normal Bug workflow. (agent's call, needed for O-08 to work, f18)
- **K-01** — No new consent scope: repository:actions with chosen repositories covers it. (owner said: issue #1422 Expectations)
- **K-02** — Existing action_propose/action_confirm behaviour, workflow_propose and brainstorm task actions must keep working unchanged. (agent's call)
- **K-03** — The Bug workflow itself stays as it is: agent selection, read-only analysis, /approve-bug, repair, delivery, and no labels added beyond bug. (agent's call)
- **K-04** — Existing refusals stay as they are: unregistered repository, repository outside the grant, missing scope, expired or early confirmation, and the redaction check. (agent's call)
- **K-05** — Linear mode labels, lifecycle states and every existing Linear connection keep working unchanged; a connection without the new bug setting behaves as today. (agent's call)
- **N-01** — No brainstorm, no Plan start, no factory internals for the owner to decide. (owner said: issue #1422 Goal)
- **N-02** — No duplicate search or merging before filing; filing is fire-and-forget, and a client may search issues separately where it is allowed. (agent's call)
- **N-03** — superseded by O-08: Linear filing is now in scope. (owner said: A-01)
- **N-04** — No Mode, Coding Agent or other label, and no parent or sub-issue link, on the filed issue. (agent's call)
- **N-05** — No fix here for #1421 (empty optional fields refused); it stays a separate Bug. (agent's call)
- **N-06** — No binding tier and no Discord reaction: one conversational confirmation is enough. (agent's call, from owner's 'one confirmation')
- **N-07** — No file or screenshot uploads; evidence is text in the description. (agent's call)
- **N-08** — No creating a new label in Linear or GitHub, and no guessing a Linear label by its name. (agent's call)
- **N-09** — No Linear filing for log analysis or Validate follow-ups; they stay as they are. (agent's call)
- **A-01** — Review of the GitHub-only scope — owner: "One change from the owner: Linear as well, not only GitHub. For a project whose ticket connection is Linear (cockpit, MAR-…), bug_file_propose creates a Linear issue in that project's team, with the bug label or the equivalent Linear label the project already uses, and starts the Bug analysis the same way. It is idempotent on replay, exactly as for GitHub. Everything else in the scope stays as you drafted it." (owner said)


## Assumptions

- **a1** — A dedicated tool name bug_file_propose is added in the connector as a thin wrapper that resolves into the same owner-action proposal as action_propose; confirmation stays with action_confirm. (agent's call)
- **a2** — The bug label is applied by a second call after create, with reconcile-by-marker for replay. (agent's call, from the Validate follow-up pattern)
- **a3** — Tier is conversational (one confirmation), like draft and brainstorm_create_tasks, not binding (no Discord reaction). The issue asks for one confirmation. (agent's call)
- **a4** — Title and description go through the same redaction check as brainstorm issues; a change made by redaction refuses the proposal. (agent's call)
- **a5** — The automatic start follows the brainstorm launch precedent (f12): a private owner-authored start that is never posted on the issue, attempted once, and reported as filed but not started if it does not begin. (agent's call)
- **a6** — The issue is created with the factory's own credential for that tracker (the GitHub bot, or the Linear publisher member), as other factory-filed issues are, not with the owner's account. (agent's call)
- **a7** — Proof: fakes cover replay, a crash between create and label, refusals and the automatic start on both trackers; only owner-run real filings, one on a GitHub test repository and one on the cockpit Linear project, can show a live issue and its analysis. (agent's call)
- **a8** — The repository argument stays the registered repository; for a project like cockpit the factory files in that project's Linear ticket connection, so the client never names a Linear team. (agent's call)
- **a9** — On Linear the replay marker is the durable creation UUID rather than a hidden body line; the short provenance line from O-07 stays. (agent's call, from f16)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
