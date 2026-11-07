> **Superseded in part (updated 2026-11-07).** Later owner decisions and the build changed this research: bug filing now has a tool (`bug_file_propose`, #1422 delivered); the server instructions were rewritten as a short layered text (the per-tool guidance left them and is served by `factory_guide`); `factory_guide` takes an optional `topic`; the instructions carry a job map built from the guide files; a client skill is printed by `litefactory connector skill`; and the guide has workflows for advisory, code reading, standards, publishing a conclusion, cancel and recover, and a brainstorm-only procedure. Where this document disagrees, the delivery reports on issue #1448 and the files under `internal/connector/guidance` are authoritative.

# Research brief

## Request

Add a read-only MCP tool (factory_guide) that returns one central, human-readable markdown guide to the factory: how it works, what each tool does, workflow examples and how to chain tools, trimmed to what this connection can call, with little code around the markdown.

## Decisions

- **S-01** — mode=feature, size=small — one new read-only tool returning one embedded markdown guide, trimmed by the existing grant filter; no permission or storage change. (agent's call)
- **A-01** — Reply to the round-1 questions — owner: "the tool should show a human readable markdown; a full guide on how to use the factory, what tools do and some workflow examples and how to chain tools together. the markdown should be central, not much code around it. give me later the link to the markdown, i want to review it as well!" (owner said)
- **O-01** — superseded by O-08 — A read-only guide tool returns a bounded overview or one intent's guidance listing only tools in availableTools(grant), with plain unavailability and reconnect text from missingScopes. (owner said: issue #1448 acceptance criteria 1-3)
- **O-02** — The guide markdown lives in the same embedded guidance folder as #1382's files, with tests that fail on drift. (owner said: issue #1448 'same embedded guidance source')
- **O-03** — The guide's feature-request example describes the brainstorm and proposal route only when those tools are callable, otherwise says plainly what is available; it never implies the guide files issues, and the bug example stays separate from it. (owner said: issue #1448 acceptance criterion 2)
- **O-04** — Each workflow example shows when to use it, the context needed, the tools in order, the expected result, and for each step whether it reads, proposes, needs the owner's separate yes, or also needs the Discord check mark. (owner said: issue #1448 'For each supported intent'; A-01 'workflow examples and how to chain tools together')
- **O-05** — When the guide leaves sections out, a closing note says so and names only the consent scopes that would make them callable, telling the owner to reconnect and tick them. (owner said: issue #1448 'indicate the missing permission/reconnect step')
- **O-06** — Tests cover a real tools/list plus guide call, an unavailable-scope grant, separate confirmation wording, every catalog tool being described in the guide, every tool named in the guide existing, and no scope subset ever seeing an uncallable tool named. (owner said: issue #1448 acceptance criteria 3-4)
- **O-07** — The connector guide in docs mentions the new tool and points to the markdown. (agent's call)
- **O-08** — The tool returns one human-readable markdown guide to the factory: how it works, how approvals work, what each tool does, workflow examples and how to chain tools. (owner said: A-01)
- **O-09** — The markdown is central and the code around it is minimal: load the file, drop sections whose tools are not callable, append the missing-scope note. Each section names the tools it needs inside the markdown itself. (owner said: A-01 'the markdown should be central, not much code around it')
- **O-10** — Guide outline: what the factory is; how proposals and approvals work; tools by group; workflow examples (review a task, discuss or revise a plan, propose and confirm a change, file a feature request, file a bug, check running work, check usage, start a workflow); what this connection lacks. (agent's call, rests on A-01 and issue #1448 intent list)
- **O-11** — The final delivery report links the guide markdown file at a fixed commit so the owner can review it. (owner said: A-01 'give me later the link to the markdown')
- **N-01** — No mutation, auto-approval, issue creation or scope widening by the guide; no redesign of permissions. (owner said: issue #1448 'Requested scope')
- **N-02** — No bug-filing tool is built here; that is #1422. (owner said: issue #1448 'separate bug-filing feature')
- **N-03** — No new consent scope and no change to existing scope meanings. (agent's call, rests on round-1 q3 recommendation)
- **N-04** — No client-specific behaviour for ChatGPT versus Claude; one response for all clients. (agent's call)
- **N-05** — No new MCP prompts and no change to the three existing prompts. (agent's call)
- **N-06** — No intent argument or intent taxonomy; the tool takes no input and returns the whole trimmed guide. (agent's call, rests on A-01)
- **N-07** — Existing per-tool guidance files and server instructions are not rewritten into the guide; they stay as they are. (agent's call, rests on K-01)
- **K-01** — Existing tools, MCP prompts, server instructions and project template keep current behavior, except one added instructions line pointing to the guide (a9). (owner said: issue #1448 acceptance criterion 5)
- **K-02** — missingScopes output and the missing-scopes notice stay as they are for every grant. (agent's call, rests on f9)
- **K-03** — Guidance loading stays fail-closed: a missing, invalid or oversized guide file fails like other guidance files do. (agent's call, rests on f1)


## Assumptions

- **a1** — The guide is listed for every authenticated grant, needing no scope, so a fresh client always finds it. Rests on f2, f9 and the 'fresh client' acceptance criterion. (agent's call, round-1 q3 option a)
- **a2** — superseded by N-06 — Input is one optional string 'intent' from a short fixed list. (agent's call)
- **a3** — superseded by O-10 — the issue's intents become workflow examples in the guide; 'start a workflow' uses the workflow tools and 'check usage' uses usage_read, each shown only when callable. (agent's call)
- **a4** — superseded — the guide is a full guide, not a short answer. Its file may exceed the 8192-byte cap of per-tool files; it gets its own fixed cap (about 32 KiB) and still fails closed. (agent's call, rests on A-01 'a full guide')
- **a5** — superseded by O-10 — the guide's opening section answers 'what can this connector do'. (agent's call)
- **a6** — A grant that can call nothing else still gets the guide's general sections, plus the note naming which scopes to tick on reconnect. (agent's call)
- **a7** — The guide is marked read-only in its tool annotations and makes no network or paid call; it only reads the embedded file and the grant. (agent's call, rests on N-01)
- **a8** — The bug example says this connector has no bug-filing tool yet, that the feature-request route is not for bugs, and that the owner can file on GitHub directly; when #1422 ships, its tool joins that example. (agent's call, round-1 q1 option a)
- **a9** — Server instructions gain one short line telling clients to call the guide, through its own per-tool guidance file like every other tool. (agent's call, round-1 q4 option a, rests on f10)
- **a10** — A workflow example whose tools are only partly callable is left out with a one-line 'not on this connection' note, rather than shown half-usable. (agent's call, rests on O-05 and AC3)
- **a11** — The owner reviews the guide's wording at the Implement review via the O-11 link; the outline in O-10 is the shape agreed now. (agent's call, rests on A-01)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
