# Manifest
# Branch: lf-228-coding-agent-switching
# Session: 2026-09-11

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Validate** — verification findings *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through the supplied contract; no
manifest checkbox is written by a Validate run.

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-11 — session created
- 2026-09-11 — Research run completed. Issue #228 ("Coding Agent switching")
  has an empty body and no content beyond a "Check this one!" comment. Found
  that litefactory already has a "coding agent" concept (Claude vs. Codex, via
  `internal/agent`, `internal/daemon`) with a working label-based selection
  mechanism and a deliberate, already-implemented refusal of mid-conversation
  agent switching (`ReplyAgentSwitchRefused`). Also found a prior `.aiteam`
  session named `lf-120-agent-switch-refused-leaves-the-session`, suggesting
  this area was already substantially worked on. Wrote `RESEARCH_BRIEF.md`
  with ranked candidate interpretations and open questions for the owner.

## Notes

