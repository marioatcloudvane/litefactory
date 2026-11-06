# Research brief

## Request

Add a read-only, grant-aware MCP tool (tentatively factory_guide) that returns concise intent-based guidance (tool order, confirmation and authorization boundaries, unavailable capabilities), built from the same embedded guidance as #1382, without filing or approving anything.

## Decisions

- **S-01** — mode=feature, size=small — one new read-only tool reusing the existing guidance and grant filter; no permission or storage change. (agent's call)
- **O-01** — A read-only guide tool returns a bounded overview or one intent's guidance listing only tools in availableTools(grant), with plain unavailability and reconnect text from missingScopes. (owner said: issue #1448 acceptance criteria 1-3)
- **O-02** — Intent text lives in embedded guidance files shared with #1382, with tests that fail on drift. (owner said: issue #1448 'same embedded guidance source')
- **O-03** — The 'file a feature request' intent describes the brainstorm/proposal route only when those tools are granted, otherwise states the available path truthfully; it never implies the guide files issues, and keeps bug filing separate from feature filing. (owner said: issue #1448 acceptance criterion 2)
- **O-04** — Each intent answer states when to use it, what context is needed, the tool order, the expected result, and for each step whether it reads, proposes, needs the owner's separate yes, or also needs the Discord check mark. (owner said: issue #1448 'For each supported intent')
- **O-05** — When an intent's tools are not callable, the answer says so and names only the consent scopes whose addition would make that intent's tools callable, telling the owner to reconnect and tick them. (owner said: issue #1448 'indicate the missing permission/reconnect step')
- **O-06** — Integration tests cover a real tools/list plus guide call, an unavailable-scope grant, separate confirmation semantics, and every scope subset never seeing an ungranted tool name in the guide. (owner said: issue #1448 acceptance criterion 4)
- **O-07** — The connector guide in docs explains the new tool and its intents. (agent's call)
- **N-01** — No mutation, auto-approval, issue creation or scope widening by the guide; no redesign of permissions. (owner said: issue #1448 'Requested scope')
- **N-02** — No bug-filing tool is built here; that is #1422. (owner said: issue #1448 'separate bug-filing feature')
- **N-03** — No new consent scope and no change to existing scope meanings. (agent's call, pending q3)
- **N-04** — No client-specific behaviour for ChatGPT versus Claude; one response for all clients. (agent's call)
- **N-05** — No new MCP prompts and no change to the three existing prompts. (agent's call)
- **K-01** — Existing tools, MCP prompts, server instructions and project template keep current behavior. (owner said: issue #1448 acceptance criterion 5)
- **K-02** — missingScopes output and the missing-scopes notice stay as they are for every grant. (agent's call, rests on f9)
- **K-03** — Guidance loading stays fail-closed: a missing or oversized intent file fails like other guidance files do. (agent's call, rests on f1)


## Assumptions

- **a1** — The guide is listed for every authenticated grant, needing no scope, so a fresh client always finds it. Rests on f2, f9 and the 'fresh client' acceptance criterion. (agent's call, asked as q3)
- **a2** — Input is one optional string 'intent' from a short fixed list; omitted means the bounded overview. Unknown intents return the list, not an error. (agent's call)
- **a3** — Intent names in the issue are examples; the final list follows what tools exist. 'start_workflow' maps to the workflow_* tools and 'check_usage' to usage_read, each only when granted. (agent's call, list asked as q2)
- **a4** — Voice-friendly bound: the overview lists the callable intents with one line each, and one intent answer stays short (target about 1500 characters), well inside the 8192-byte guidance cap. (agent's call)
- **a5** — The overview answers 'what can this connector do' by listing only intents with at least one callable step, plus one line on what the reconnect would add. (agent's call)
- **a6** — A grant that can call nothing else still gets the guide, which says plainly that nothing is usable and which scopes to tick on reconnect. (agent's call)
- **a7** — The guide is marked read-only in its tool annotations and makes no network or paid call; it only reads embedded files and the grant. (agent's call, rests on N-01)
- **a8** — The bug intent today says no bug tool exists on this connector and points to filing on GitHub directly; when #1422 adds a tool, that tool is added to the same intent. (agent's call, asked as q1)
- **a9** — The guide adds one short line to server instructions through its own per-tool guidance file, like every other tool. (agent's call, asked as q4, rests on f10)


## Open questions

### q1 — What should the guide say when the owner asks how to file a bug?

_Asked of: Product Owner Questions_

- **a** — Say plainly that this connector has no bug-filing tool yet, that the feature-request route is not for bugs, and that the owner can file it on GitHub directly. When #1422 ships, its tool joins the same intent. (recommended: truthful today, no fake tool, matches acceptance criterion 2)  *(recommended)*
- **b** — Leave the bug intent out until #1422 ships. Simpler, but a voice user asking about bugs gets only the generic intent list.
- **c** — Send bugs through the brainstorm route like features. Not recommended: it blurs bug and feature filing, which the issue asks to keep apart.

### q2 — Which intents should the first version cover?

_Asked of: Product Owner Questions_

- **a** — Overview plus the seven from the issue: review a task, discuss or revise a plan, file a feature request, file a bug, check running work, check usage, start a workflow. (recommended: covers every acceptance criterion and maps onto tools that exist)  *(recommended)*
- **b** — Only the three the acceptance criteria test: review/discuss/propose a change, file a feature request, check running work. Smaller, but usage and workflow questions get no guide.
- **c** — One intent per tool family, generated automatically. Not recommended: that is the taxonomy the issue warns against.

### q3 — Which connections may see and call the guide?

_Asked of: Technical Questions_

- **a** — Every signed-in connection, with no new scope. It only reveals what the connection already sees in its tool list and server instructions. (recommended: a fresh client always finds it)  *(recommended)*
- **b** — Only connections with the factory overview scope. Safer on paper, but a brainstorm-only or task-discussion-only connection would never see the guide.
- **c** — A new dedicated scope. Not recommended: every existing connection would need a reconnect, and the issue rules out permission changes.

### q4 — May the server instructions gain one line telling clients the guide exists?

_Asked of: Technical Questions_

- **a** — Yes. Each tool's guidance text already flows into the instructions automatically; the guide adds one short line. Everything else in instructions, prompts and the project template stays byte-identical. (recommended: clients that do read instructions learn to call the guide)  *(recommended)*
- **b** — No. Keep the instructions byte-identical and list the guide only in the tool list. Stricter reading of acceptance criterion 5, but needs a special exception in the shared composer.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
