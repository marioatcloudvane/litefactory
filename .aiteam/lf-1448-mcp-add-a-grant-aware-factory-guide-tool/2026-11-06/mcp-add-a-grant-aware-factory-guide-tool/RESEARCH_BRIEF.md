# Research brief

## Request

Add a read-only, grant-aware MCP tool (tentatively factory_guide) that returns concise intent-based guidance (tool order, confirmation and authorization boundaries, unavailable capabilities), built from the same embedded guidance as #1382, without filing or approving anything.

## Decisions

- **S-01** — mode=feature, size=small — one new read-only tool reusing the existing guidance and grant filter; no permission or storage change. (agent's call)
- **O-01** — A read-only guide tool returns a bounded overview or one intent's guidance listing only tools in availableTools(grant), with plain unavailability and reconnect text from missingScopes. (owner said: issue #1448 acceptance criteria 1-3)
- **O-02** — Intent text lives in embedded guidance files shared with #1382, with tests that fail on drift. (owner said: issue #1448 'same embedded guidance source')
- **O-03** — The 'file a feature request' intent describes the brainstorm/proposal route only when those tools are granted, otherwise states the available path truthfully; it never implies the guide files issues, and keeps bug filing separate from feature filing. (owner said: issue #1448 acceptance criterion 2)
- **N-01** — No mutation, auto-approval, issue creation or scope widening by the guide; no redesign of permissions. (owner said: issue #1448 'Requested scope')
- **K-01** — Existing tools, MCP prompts, server instructions and project template keep current behavior. (owner said: issue #1448 acceptance criterion 5)


## Assumptions

- **a1** — The guide is listed for every authenticated grant that has at least one other callable tool, so a fresh client always finds it. This rests on f2 and the 'fresh client' acceptance criterion; the gating scope is a shaping choice. (agent's call)
- **a2** — Input is one optional string 'intent' from a short fixed list; omitted means the bounded overview. Unknown intents return the list, not an error. (agent's call)
- **a3** — Intent names in the issue are examples; the final list follows what tools exist. 'start_workflow' maps to the workflow_* tools and 'check_usage' to usage_read, each only when granted. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
