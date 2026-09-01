# Manifest
# Branch: lf-8-add-codex-as-second-party-item
# Session: 2026-08-31

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(redone — 2026-09-01)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-08-31 — session created
- 2026-08-31 — classified as Research (issue #8: "Evaluate how to plug-in Codex into litefactory")
- 2026-08-31 — Research complete (first pass). `RESEARCH_BRIEF.md` written: the
  Claude Code coupling surface mapped across twelve areas with `path:line`
  citations, Codex CLI's headless surface researched from documentation only,
  seven named gaps, five seam options with one rejected on principle and one on
  architecture, and a three-slice recommendation. **No `codex` binary was
  executed** — the run had no shell — so every Codex claim is tagged `[W]`
  (second-hand) and slice 1 of the recommendation is a read-only probe to convert
  it to measurement, following the existing `CLAUDE_CLI_CONTRACT.md` convention.
- 2026-09-01 — Research redone at the requester's request: the first pass "did
  not land" — not missing content but the wrong shape. `RESEARCH_BRIEF.md`
  rewritten (overwritten in place, old version in git history) around the
  requester's four concrete asks instead of general coupling prose: (1) a
  per-turn GitHub label routing which coding-agent CLI runs — found the
  re-read-every-tick mechanism already exists verbatim as `lf:tools=`'s pattern,
  but found a new, sharper risk: session-identity is directional (litefactory
  pre-computes Claude's session id; Codex's is only discoverable after a run
  starts), so a label flip mid-conversation cannot simply resume; (2)
  `AGENTS.md` vs `CLAUDE.md` and the four-mode team — confirmed the team
  mechanism is Claude-specific, but corrected the first pass's flat "Codex has no
  equivalent" against fresh web research suggesting Codex has its own (unverified)
  subagent and hooks primitives, carried forward as an open tension rather than
  resolved; (3) model-naming vocabulary — corrected a first-pass claim that no
  `--model` flag exists (`runner.go:500-506` unconditionally constructs one) and
  reframed the design problem as a tool-neutral capability-tier vocabulary
  question; (4) an interface/factory design for pluggable runners, sized for a
  later third tool (OpenCode) — grounded in re-verified `internal/harness` I/O,
  corrected the blast-radius table (`internal/report` imports `internal/harness`
  directly, not only via `turn`), and corrected a misapplied `AC_AMENDMENTS.md`
  citation (`runs.owner` is about process-reclaim ownership, not a tool
  discriminator). Also flagged as new, unrelated prior art: the repo's own
  "LF-8" migration comments refer to litefactory's internal multi-turn-session
  milestone, not this GitHub issue. **Still no `codex` binary was executed** —
  the same measurement caveat from the first pass is repeated, not softened.

## Notes

Feature slug: `add-codex-as-second-party-item`. Headless run — no human is
available to answer questions, so open questions are recorded in the brief
rather than asked.
