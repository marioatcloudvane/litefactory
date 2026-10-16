# Research brief

## Request

Replace the Discord stateless reply pipeline with one persistent per-channel standard agent supporting Claude and Codex, typed read tools, native compaction and flagged grounding failures.

## Decisions

- **d1** — Factory workflows and Linear or GitHub conversations remain out of scope.
- **d2** — Persist one stable per-channel conversation identity with provider resume handle, generation, turn state, completion cursor and usage metadata.
- **d3** — One five-minute budget covers the agent turn and all tool calls, with cancellation propagated.
- **d4** — Owner q1 and remarks require standard agent selection with both Claude and Codex supported, replacing the Claude-only exclusion.
- **d5** — Owner q2 selects native provider compaction rather than application-generated summaries or a fixed reset threshold.
- **d6** — Owner q3 selects publication of grounding failures with a visible unverified label.
- **d7** — Owner q4 permits write proposals only, with existing explicit confirmation and no agent write tools.
- **d8** — Owner q5 retains deterministic slash commands and raw /details.
- **d9** — Owner q6 excludes a reset command.
- **d10** — Use typed authorized read tools for status, owner waits, meter, issue details and diagnosis with validated inputs and explicit result errors.
- **d11** — Serialize turns per channel and persist turn/delivery progress for restart recovery without intentionally duplicating completed replies.
- **d12** — Check current factual numbers and factory identifiers against current tool results, require fresh reads for current state and remove the model checker.
- **d13** — Acceptance includes both-provider resume and native compaction continuity, follow-up memory, ordered restart recovery, isolation, permissions and unchanged write gates.


## Assumptions

- **a1** — Provider switch is not automatic: retain the existing provider-bound session on quota failure, timeout or configuration changes and use slash-command availability notices instead of silently starting a new conversation.
- **a2** — 256k is an approximate product target; actual compaction follows each provider's supported limits and must be demonstrated in the accepted chat path.
- **a3** — Grounding validation targets factual numbers and factory identifiers; ordinary prose, proposals and remembered conversational references do not require fabricated current tool evidence.
- **a4** — Agent tools are read-only, remembered consent is not fresh authorization, and owner-only/signature checks remain unchanged.
- **a5** — Durable provider conversation storage survives the existing 24-hour delivery-record purge; no new indefinite application transcript archive or cross-provider summary is introduced.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
