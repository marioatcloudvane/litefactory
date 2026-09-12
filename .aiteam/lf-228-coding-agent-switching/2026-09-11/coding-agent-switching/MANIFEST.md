# Manifest
# Branch: lf-228-coding-agent-switching
# Session: 2026-09-11

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(completed iteration)*
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
- 2026-09-11 — Third Research run, triggered by the owner changing this
  issue's `Coding Agent:` label as a live smoke test of the very routing
  mechanism this issue is about ("re-run the research, since i updated the
  agent... I want to see if it works as designed"). Re-verified all five
  load-bearing claims from the prior run against current repository state
  (`internal/turn/prepare.go`, `internal/store`, `switch-agent.sh`, label
  polling in `internal/daemon`): zero drift, all citations still accurate,
  with a few additional identifiers recorded (a second refusal comparison
  point at `prepare.go:1004`, the `agentSwitchRefusal`/
  `DispositionAgentSwitchRefused` type chain, the `RunnerSet`/`agent.Runner`
  shape). Also checked, as requested, whether the repository itself can show
  which agent handled this particular research run — it cannot: the
  `segments`/`runs` tables prove which tool handled a litefactory-dispatched
  coding turn, but this research pass is outside that dispatch machinery
  entirely, and no in-repo artifact records researcher/model identity for a
  research run. Recorded as a stated limitation, not a defect. Updated
  `RESEARCH_BRIEF.md` in place. Open Questions 5–8 (mutate-in-place vs.
  rotate, mid-flight subprocess kill, bare-label-edit trigger model, fate of
  `switch-agent.sh`) remain unresolved — this run was a verification pass,
  not a scope conversation, so they were not asked about or answered.

- 2026-09-12 — Fourth Research run, owner request: "show me the details
  about the open questions please!" — a report-only pass, no new
  investigation or scope change. `RESEARCH_BRIEF.md` already had full detail
  for every open question (Open Questions 5-9), so it was left unchanged on
  disk; this run's job was to surface that existing detail back to the owner
  in the final message, laid out as one question per block with its
  solution options and tradeoffs, per the later-run reporting format.
- 2026-09-12 — Fifth Research run, owner feedback recorded verbatim:
  Q5 overwrite in place; Q6 let in-flight work finish, switch applies to the
  next task; Q7 no new trigger plumbing needed, rely on existing per-comment
  label re-read; Q8 delete `switch-agent.sh` once automated (not kept as a
  fallback); Q9 ignore the unrelated `.codex/AGENT_ROSTER.md` note and drop
  it from tracking. Owner also stated an explicit simplicity constraint
  ("keep it simple, basically this is only a few lines that need to change,
  no rocket science here!"), recorded as a directive for Plan/Implement, not
  independently re-validated against the code this run. Moved Open Questions
  5-9 from open to resolved, updated the Constraints & Risks entries that
  depended on them, and updated Recommended scope for Plan mode accordingly.
  No new investigation opened. With all nine open questions now resolved
  and only the pre-existing, explicitly non-blocking git-history residual
  gap remaining, `RESEARCH_BRIEF.md` now states the brief is ready to hand
  to Plan mode.
- 2026-09-12 — Plan run completed. `requirements-engineer` produced a ready
  `feature-spec.md` directly from `RESEARCH_BRIEF.md` with no further
  questions (all five prior Research rounds had already resolved scope);
  `go-architect` reviewed it and wrote `ARCH_REVIEW.md`, judging the scope
  proportionate to the owner's simplicity constraint and adding a small
  number of structural corrections (retract the old delivery pin as part of
  the store mutation; return the mutated in-memory segment, not a stale
  copy; compute the Claude-resume fact separately from `previousRun` rather
  than narrowing it). Given how small and interdependent the change is (one
  store accessor, one restructured branch in `internal/turn/delivery.go`,
  one narrowed refusal condition and one resume-decision refinement in
  `internal/turn/prepare.go`, comment rewrites, test rewrites, and one
  deleted script — none independently shippable or valuable on its own),
  synthesized into a single story, `switch-agent-by-label`, rather than
  splitting it across several. Wrote `plan_brief.md` (link-only Story
  Index) and submitted the completed typed Plan contribution via
  `litefactory submission preflight` (clean: valid, complete, no
  diagnostics) then `litefactory submission submit`
  (request id `db6d4e3c3f7633216ea25ad948638421`, input state `valid`,
  plan state `pending` — the factory's issue creation/link-fill is an
  asynchronous step that happens after this run; readiness for Implement is
  set by the factory once that publication completes, not by this run).
  No material questions remained; no assumptions needed owner sign-off
  beyond what Research had already resolved.

## Notes

- Plan submission request id (for correlation if a reply is ever lost):
  `db6d4e3c3f7633216ea25ad948638421`.
