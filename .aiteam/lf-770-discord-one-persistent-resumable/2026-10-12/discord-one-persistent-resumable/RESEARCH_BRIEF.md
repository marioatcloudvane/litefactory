# Research brief

## Request

Replace the stateless Discord interpret/narrate/check pipeline with one persistent, resumable, named conversation agent per channel, using typed read tools, queueing, grounding checks and 256k compaction.

## Decisions

- **d1** — Out of scope: factory workflows, Linear and GitHub conversations.


## Assumptions

- **a1** — On provider switch or quota exhaustion, a new session is started, seeded with a litefactory-kept summary of the conversation.
- **a2** — Compaction triggers near 256k tokens, measured from provider usage data; per-provider compaction behaviour is not verified.
- **a3** — Grounding is a code check that numbers and identifiers in the reply occur in that turn's tool results; failure falls back to the raw result via /details.
- **a4** — The agent gets read-only tools; writes stay behind explicit owner confirmation. Owner-only and signature checks are unchanged.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
