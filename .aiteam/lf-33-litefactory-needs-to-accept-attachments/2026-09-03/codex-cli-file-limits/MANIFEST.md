# Manifest
# Branch: lf-33-litefactory-needs-to-accept-attachments
# Session: 2026-09-03

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-03 — session created (narrow Plan-mode detour: Codex CLI file-reading limits, for issue #33 attachment ingestion)
- 2026-09-03 — research run completed via web-researcher; RESEARCH_BRIEF.md written

## Notes

Scoped narrowly: confirms native file-reading capability, format support and
size/page/resolution limits for the `codex` CLI subprocess, plus tightens the
known byte-size cap (if any) for Claude Code's `Read` tool on images/PDFs. Feeds
litefactory's own ingestion-time rejection thresholds for issue #33.
