# Research brief

## Request

Replace the stateless Discord interpret/narrate/check pipeline with one persistent, resumable, named conversation agent per channel, using typed read tools, queueing, grounding checks and 256k compaction.

## Decisions

- **d1** — Out of scope: factory workflows, Linear and GitHub conversations.
- **d2** — Per-channel session with a stable name; its ID is persisted in a new store table.
- **d3** — One overall time budget per turn replaces per-flow budgets.
- **d4** — Codex chat is out of scope for this change.


## Assumptions

- **a1** — On provider switch or quota exhaustion, a new session is started, seeded with a litefactory-kept summary of the conversation.
- **a2** — Compaction triggers near 256k tokens, measured from provider usage data; per-provider compaction behaviour is not verified.
- **a3** — Grounding is a code check that numbers and identifiers in the reply occur in that turn's tool results; failure falls back to the raw result via /details.
- **a4** — The agent gets read-only tools; writes stay behind explicit owner confirmation. Owner-only and signature checks are unchanged.


## Open questions

### q4 — May the agent propose writes such as drafting an issue or answering a research question?

_Asked of: Product Owner Questions_

- **a** — Yes, as proposals only. The agent describes the action, and the existing explicit owner confirmation must happen before litefactory runs it. It has no write tools.  *(recommended)*
- **b** — No. It reads and explains only. Writes stay on today's commands.
- **c** — Yes, with write tools it can call directly after a chat 'yes'.

### q5 — Do the explicit slash commands stay once the agent exists?

_Asked of: Product Owner Questions_

- **a** — Keep them. They give raw, deterministic answers and are the fallback when no provider is available.  *(recommended)*
- **b** — Remove them. Chat is the only interface.

### q6 — Should the owner be able to reset the conversation?

_Asked of: Product Owner Questions_

- **a** — Yes. A /reset command starts a fresh session for that channel and clears the stored summary.  *(recommended)*
- **b** — No. Only automatic compaction ever ends a session.

### q1 — Which provider runs the persistent chat agent in this change?

_Asked of: Technical Questions_

- **a** — Claude only. Codex is a follow-up. If Claude is unavailable, the deterministic commands answer.  *(recommended)*
- **b** — Claude and Codex together. This needs a new Codex launch path first, so it is much larger.

### q2 — How do we keep the context under about 256k tokens?

_Asked of: Technical Questions_

- **a** — litefactory counts usage. At about 80% of 256k it asks the agent for a summary, then starts a new session seeded with that summary and the last few turns. Provider switch or quota failure use the same path.  *(recommended)*
- **b** — Rely on the provider's own compaction. Behaviour differs per provider and is unverified.
- **c** — Hard reset at 256k with no summary. Simple, but it loses context.

### q3 — What happens when the grounding check fails?

_Asked of: Technical Questions_

- **a** — Send the agent one correction in the same session. If it still fails, send a short notice plus the raw result via /details.  *(recommended)*
- **b** — Skip the retry and send the raw result immediately.
- **c** — Send the reply anyway and flag it as unverified.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
