# Research brief

## Request

Research how to make Discord command replies natural-language answers written by an agent from the factory's data, with findings, a design reusing existing profiles and a story split.

## Decisions

- **d1** — Reuse NativeModel with a new fast-tier, no-tools profile rather than adding a new provider path.
- **d2** — Story split: narrator interface and fallback; native fast model with project-less metering; number/link checker; command wiring with paging and details option; daemon wiring, config switch, docs.


## Assumptions

- **a1** — Narration lives in the transport-neutral messaging service, not the Discord adapter, so other transports reuse it.
- **a2** — /question and /answer, and /help, are not narrated because exact tokens and command lists must stay verbatim.
- **a3** — Without free text the reply language follows the latest /ask turn, else English.
- **a4** — On agent failure or failed number/link check the raw redacted text is sent, marked as raw.


## Open questions

### q1 — How strict should the number and link check be?

_Asked of: Product Owner Questions_

- **a** — Strict: every digit sequence and link must appear exactly in the raw data. Safest, but the agent cannot write "about 1.4 million" or "4 seconds" for 4s, so more replies fall back to raw.
- **b** — Strict on links and IDs, tolerant on numbers: allow rounding and unit changes if the value matches the raw data within 1%. The agent can write natural numbers. The check is more complex.
- **c** — Tell the agent to copy numbers verbatim and check strictly. Replies sound a bit more technical but rarely fall back.  *(recommended)*

### q2 — Which commands get narrated?

_Asked of: Product Owner Questions_

- **a** — health, status, meter, prices and the finished diagnose report only.  *(recommended)*
- **b** — As (a), plus /question as a sentence, with the token line kept exact and appended unchanged.
- **c** — Everything except /help and /answer.

### q3 — Which language is used when a command has no free text?

_Asked of: Product Owner Questions_

- **a** — The language of your latest /ask turn, else English.  *(recommended)*
- **b** — A configured reply language, default English, no guessing.
- **c** — Always English.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
