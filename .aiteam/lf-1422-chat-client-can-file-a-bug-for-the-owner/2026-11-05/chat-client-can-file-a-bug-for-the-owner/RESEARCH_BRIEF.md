# Research brief

## Request

Add one proposal tool (bug_file_propose: repository, title, description) that the owner confirms with action_confirm under repository:actions. On confirm the factory creates exactly one issue labelled bug (idempotent on replay), returns its URL, and handles it as a normal Bug. Overview guidance tells clients to use it for 'file a bug' and never to invent a workflow path.

## Decisions

- **S-01** — mode=feature+integration, size=medium — new connector tool and owner action on existing proposal machinery plus a GitHub label write. (agent's call)
- **O-01** — One bug_file_propose proposal, one action_confirm, exactly one GitHub issue labelled bug, URL returned, replay creates no second issue. (owner said: issue #1422 Expectations)
- **O-02** — Overview guidance tells clients to use bug_file_propose when the owner says file a bug, and never to invent a workflow path. (owner said: issue #1422 Expectations)
- **K-01** — No new consent scope: repository:actions with chosen repositories covers it. (owner said: issue #1422 Expectations)
- **N-01** — No brainstorm, no Plan start, no factory internals for the owner to decide. (owner said: issue #1422 Goal)
- **K-02** — Existing action_propose/action_confirm behaviour, workflow_propose and brainstorm task actions must keep working unchanged. (agent's call)


## Assumptions

- **a1** — A dedicated tool name bug_file_propose is added in the connector as a thin wrapper that resolves into the same owner-action proposal as action_propose; confirmation stays with action_confirm. (agent's call)
- **a2** — The bug label is applied by a second call after create, with reconcile-by-marker for replay. (agent's call, from the Validate follow-up pattern)
- **a3** — Tier is conversational (one confirmation), like draft and brainstorm_create_tasks, not binding (no Discord reaction). The issue asks for one confirmation. (agent's call)
- **a4** — Title and description go through the same redaction check as brainstorm issues; a change made by redaction refuses the proposal. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
