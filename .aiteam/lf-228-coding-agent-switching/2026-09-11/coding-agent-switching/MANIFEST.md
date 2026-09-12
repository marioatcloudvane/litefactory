# Manifest
# Branch: lf-228-coding-agent-switching
# Session: 2026-09-11

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + per-story sub-issues *(completed iteration, revised)*
- [x] **Implement** — code + tests *(done — `switch-agent-by-label`, all 7 tasks; see
  `TEST_REPORT.md`, `DRIFT_REPORT.md`, `notes.md`)*
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

- 2026-09-12 — Sixth run (narrow fact-finding pass during the Plan
  iteration, not a full Research pass): the owner changed the requirement
  from "resume Claude on switch-back" to "every switch, either direction,
  always cold-starts, never resumes" while keeping "same row, no rotation."
  Traced the exact cold-Claude call path, the "already in use" collision's
  actual trigger (permanent, keyed to the on-disk transcript for that id —
  not concurrency-scoped), Codex's collision-free-by-construction cold path,
  and confirmed `segments.session_ref` is a plain mutable NOT NULL column
  with no uniqueness constraint and no enforced tie to
  `naming.SessionRef(ref, ordinal)` after row creation. Conclusion: the new
  requirement is achievable with no schema/migration change and no
  rotation, but only if the switch mutation also regenerates
  `session_ref` itself (not just `agent`/`resume_token`) and
  `resumeDecision` is forced cold on a same-turn switch. Appended an
  additive addendum to `RESEARCH_BRIEF.md`; did not reopen Open Questions
  5-9 or touch `plan_brief.md`/`feature-spec.md`/`ARCH_REVIEW.md`.

- 2026-09-12 — Seventh run: Plan revision to `switch-agent-by-label`,
  triggered by the owner's illustrated comment ("Agent A -> Id 1223, switch
  to Agent B, Database then carries Agent B -> Id asdf... we should not
  know which session to switch back to anyways") reaffirming the sixth
  run's conclusion. Confirmed the sixth run's fact-finding fully resolves
  the tension (no schema change, no rotation needed) and revised the single
  story accordingly rather than reopening a question to the owner: dropped
  the prior iteration's "resume Claude on switch-back if it ran there
  before" refinement (`resumeDecision`/`previousRun` runs.agent-aware arm,
  BR-5/D-D in the superseded draft) and replaced it with an unconditional
  "force cold on any switch turn" rule; extended the store accessor's
  contract so a switch's single-transaction overwrite also regenerates
  `session_ref` to a value never held by that row before, alongside the
  existing `agent` overwrite and `resume_token` clear; updated the affected
  acceptance criteria (notably AC-002 and AC-005) and doc-comment cleanup
  scope (AC-010) to match; carried forward an explicit evidence-obligation
  inventory per `docs/decisions/nested-agent-evidence.md`, including the
  still-unsatisfied, non-blocking obligation for live external observation
  of the Claude CLI's collision behavior. `plan_brief.md` rewritten in place
  (same single-story Story Index link; Out of Scope's "no new session-id
  scheme" bullet corrected, since regenerating the id in the same row is now
  exactly the mechanism used, not something declined). Preflight was clean
  (valid, complete, no diagnostics) and the revised completed Plan
  contribution was submitted for the same story key (`switch-agent-by-label`,
  intent `update`) — see Notes for the new request id and states. No
  material question remained open for the owner; the sixth run's research
  was sufficient to resolve the apparent conflict without asking.

- 2026-09-12 — Implement run completed for `switch-agent-by-label` (issue #252,
  revision `75fcd752b8f4b40986f1af3a9994afaaf59abab5f0599229559a45b801635054`),
  separated-workflow path. All 7 tasks landed: the new `store.SetSegmentAgent`
  accessor, the in-place switch in `prepareDeliverySegment`, the narrowed
  continuation-only refusal, the unconditional forced-cold resume decision on
  a switch turn, deletion of `switch-agent.sh`, rewritten/added unit tests in
  `internal/turn/execute_test.go` / `internal/turn/delivery_test.go` /
  `internal/store/store_test.go`, and a sequential (no-sleep) integration pin
  proving an in-flight turn finishes under the old agent. Aggregate gates:
  `go build`, `go vet`, `gofmt -l`, `golangci-lint run` all clean whole-repo;
  `go test -race` green for `internal/store`, `internal/report`,
  `internal/naming`, and a targeted race-enabled superset of `internal/turn`
  covering every test this story touched; whole-repo `go test` (minus
  `internal/turn`, run separately due to its size) green except one
  pre-existing, unrelated `internal/teamsource` instruction-parity failure
  (confirmed untouched by this story's diff). Full unscoped `internal/turn`
  suite does not fit the run's gate timeout ceiling (pre-existing, ~200 tests,
  many spawn real git subprocesses) — narrowed to a targeted `-run` superset
  per the run's own instructions rather than raising the timeout. See
  `TEST_REPORT.md`, `DRIFT_REPORT.md`, `notes.md`. Suggest Validate next.

## Notes

- Plan submission request id, first completed iteration (superseded by the
  revision below; kept for correlation history):
  `db6d4e3c3f7633216ea25ad948638421`.
- Plan submission request id, revised iteration (current):
  `8420de915c13e414404edea6d8d86736` — input state `valid`, plan state
  `pending` at submit time (factory ID 45, run 359, attempt
  `fb8aac73a79ff2b012bb94a229d20488`). The factory's issue update/link-fill is
  an asynchronous step after this run; readiness for Implement is set by the
  factory once that publication completes, not by this run.
