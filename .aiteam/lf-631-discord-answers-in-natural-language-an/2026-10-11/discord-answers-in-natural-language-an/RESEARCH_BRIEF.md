# Research brief

## Request

Research how to make Discord command replies natural-language answers written by an agent from the factory's data, with findings, a design reusing existing profiles and a story split.

## Decisions

- **d1** — Reuse NativeModel with a new fast-tier, no-tools profile rather than adding a new provider path.
- **d2** — Story split: narrator interface and fallback; native fast model with project-less metering; number/link checker; command wiring with paging and details option; intent step; daemon wiring, config switch, docs.
- **d3** — Owner answers: strict verbatim number/link check (q1 c); narrate every command (q2); language follows latest /ask else English (q3 a).
- **d4** — Natural-language intake is added via a reasoning, no-tools intent agent in the shared service; only enabled commands, validated parameters, clarify when unsure.


## Assumptions

- **a1** — Narration and intent live in the transport-neutral messaging service, not the Discord adapter.
- **a2** — Exact tokens and fixed strings in /question, /answer and /help replies are appended unchanged, outside the narrated text.
- **a3** — Without free text the reply language follows the latest /ask turn, else English.
- **a4** — On agent failure or failed number/link check the raw redacted text is sent, marked as raw.
- **a5** — Intent uses the balanced tier; narration uses the fast tier.


## Open questions

### q5 — Should free text be able to change things without asking first?

_Asked of: Product Owner Questions_

- **a** — Never: anything that changes state needs a plain yes after the bot repeats what it understood.  *(recommended)*
- **b** — Only when the agent is very sure, and show an undo hint.
- **c** — Always run it; no confirmation.

### q4 — How should Discord deliver plain messages to the factory?

_Asked of: Technical Questions_

- **a** — Add a live Discord connection (gateway) that reads messages where the bot is mentioned or in a dedicated channel. Needs the privileged message-content permission in the Discord developer settings. Real natural chat.
- **b** — Keep HTTP only and add one slash command, `/chat <text>`. No new connection or permission, but users still type a slash.
- **c** — Do (b) first, then (a) as a later story.  *(recommended)*

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
