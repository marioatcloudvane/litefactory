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
- 2026-09-11 — Follow-up Research run, scope now confirmed by the issue owner:
  reverse `ReplyAgentSwitchRefused` into an actual switch, driven by the
  `Coding Agent:` label, no daemon restart, in-progress session loss
  accepted. Confirmed the pin lives on `segments.agent` (SQLite,
  `internal/store`), traced the exact refusal comparison in
  `internal/turn/prepare.go`, found the existing manual `switch-agent.sh`
  script at repo root (the spec for the DB mutation), confirmed label
  changes are already polled without caching or restart, and surfaced two
  unresolved implementation forks for Plan mode: mutate-in-place vs.
  rotate-to-new-segment, and whether an already-running subprocess must be
  killed on a mid-flight label change. Updated `RESEARCH_BRIEF.md` in place.
  Git history for the original refusal decision could not be retrieved (no
  shell/`git` access in this run's sub-agent roster) — flagged as a residual
  gap, not blocking.

## Notes
