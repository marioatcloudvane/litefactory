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

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
