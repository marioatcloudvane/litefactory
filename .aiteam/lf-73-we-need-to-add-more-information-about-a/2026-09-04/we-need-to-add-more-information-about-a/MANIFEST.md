# Manifest
# Branch: lf/73-we-need-to-add-more-information-about-a
# Session: 2026-09-04

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-04 — session created
- 2026-09-04 — Research complete: RESEARCH_BRIEF.md written (codebase-tour ran; web-researcher had no web access — issue #73 needs `lf:tools=web` label for a follow-up on Claude Code/Agent SDK wire-format questions)
- 2026-09-04 — Research re-run with `lf:tools=web` access: web-researcher resolved all six previously-open Claude Code/Agent SDK wire-format questions against Anthropic's primary docs (cost/token breakdown, per-model usage, sub-agent attributability, cache pricing stability, streaming granularity, SDK/CLI schema parity). RESEARCH_BRIEF.md updated in place — codebase findings unchanged, Domain/Problem Space, Constraints & Risks, Open Questions and Recommended Scope revised.
- 2026-09-04 — Research re-run a third time, answering two owner follow-up questions from the issue thread: (1) whether/how sub-agent-level token usage can be reconstructed after the fact from litefactory's own run logs and Claude's documented event-stream format, and (2) the same cost/token/attribution questions already answered for Claude, now answered for Codex, plus the design implication of more coding agents being added later. codebase-tour re-read the log-capture code (`internal/harness/stream.go`, `internal/codex/runner.go`) and the agent-abstraction layer (`agent.Runner`, `turn.RunnerSet`) directly; web-researcher (had `lf:tools=web` access) answered both the Claude sub-agent-reconstruction mechanics and the Codex/OpenAI parity questions against official docs and Codex's own protocol source. Confirmed: Claude's per-model/per-token result data is already arriving on litefactory's stdin and is silently dropped by the existing parser (closes a prior open question); raw event streams for both agents are already persisted verbatim to disk (`NNNN.jsonl`) and unparsed past two event types, so reconstruction needs new parsing code, not new capture; sub-agent-level reconstruction is possible in outline but has real gaps (no documented "true final message" marker, litefactory doesn't yet pass the `--forward-subagent-text` flag needed for full sub-agent visibility, cost must be estimated not read); Codex has comparable token granularity to Claude but no cost field, no per-model breakdown, a weaker (undocumented) sub-agent-attribution story, and an explicitly unstable wire protocol per its own maintainers. RESEARCH_BRIEF.md updated in place — prior sections preserved, new subsections and revised Constraints & Risks / Open Questions / Recommended Scope added.

## Notes

