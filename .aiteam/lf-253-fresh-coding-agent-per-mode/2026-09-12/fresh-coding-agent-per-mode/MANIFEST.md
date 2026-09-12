# Manifest
# Branch: lf/253-fresh-coding-agent-per-mode
# Session: 2026-09-12

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(completed this run — see history; an
  earlier Plan run had proceeded with Research recorded as skipped)*
- [x] **Plan** — `plan_brief.md` + one typed story submitted *(completed earlier run)*
- [x] **Implement** — code + tests *(completed this run, see history)*
- [ ] **Validate** — verification findings *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through the supplied contract; no
manifest checkbox is written by a Validate run.

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-12 — session created for issue #253 ("Fresh coding agent per mode").
  Supersedes #227 and its seven-story plan; independent replacement, no
  imported stories.
- 2026-09-12 — Plan run completed: `feature-spec.md` written by
  requirements-engineer (no material questions), `ARCH_REVIEW.md` written by
  go-architect (verdict: APPROVED, no owner-facing deviation), `plan_brief.md`
  synthesized, and one typed story ("fresh-coding-agent-per-mode", 3 tasks, 15
  acceptance criteria) submitted via `submission preflight` + `submission submit`
  against descriptor `ec09ba98295eed3d`. Preflight and submit both reported
  `valid=true, complete=true, diagnostics=[]` (request_id
  `1192b533f27a6ee0394672a1ba24b4f7`, contribution ID 44, run 358). A
  post-submit `submission receipt` check showed `InputState: valid` but
  `PlanState: pending` with no story issue link yet recorded — publication
  (issue creation, link-fill, readiness) is an asynchronous factory-side step
  not observed as complete within this run. Readiness for Implement is not yet
  established; do not assume it until a later check shows `PlanState`
  advanced past `pending` with a story link present.
- 2026-09-12 (run 0002) — Research run completed at owner request ("Do it")
  after the Plan run above. `RESEARCH_BRIEF.md` written: independently
  re-derived the mode/segment/prompt mechanism directly from source at commit
  `576208a2e541f9f2b44ce6a812ecddf2dd87c957` (`internal/turn/delivery.go`
  `prepareDeliverySegment`, `internal/turn/prepare.go` `previousRun` /
  `resumeDecision` / `allocateRun`, `internal/harness/prompt.go` `BuildPrompt` /
  `PromptInput`, `internal/turn/sessiondir.go` `findSessionDir`,
  `internal/naming`, `internal/planbrief`) and confirmed it matches, without
  contradiction, the design already frozen into `feature-spec.md` (BR-1..BR-18)
  and `ARCH_REVIEW.md` (D-1..D-10). No materially different design surfaced; no
  Plan decision was reopened. Carried forward, not newly found: the two
  unsatisfied evidence obligations (live conversation-lifecycle behavior and
  agent comprehension of the artifact pointer are unverifiable under
  `docs/decisions/nested-agent-evidence.md`'s sandbox boundary). No material
  owner questions; the pending `PlanState` from the prior entry is restated as
  a process note, not a Research finding.
- 2026-09-12 (run 0003) — Research run triggered by an owner comment ("Start
  it, very wasy job" — typo for "easy", characterizing scope as small, not
  new scope). Checked whether anything invalidates the run 0002
  `RESEARCH_BRIEF.md` rather than re-running the investigation: `git log
  576208a2e541f9f2b44ce6a812ecddf2dd87c957..HEAD` shows no commits touching
  `internal/turn`, `internal/harness`, `internal/naming`, `internal/planbrief`,
  `internal/store`, `internal/daemon`, or
  `docs/decisions/nested-agent-evidence.md`; working tree is clean and matches
  `origin/lf/253-fresh-coding-agent-per-mode`. Confirmed-current, not
  re-derived: `RESEARCH_BRIEF.md` is unchanged, no new investigation was
  performed, and no new owner questions were raised.

## Notes

- Instruction requested one small story, roughly three tasks: (1) consistent
  mode-change resets, (2) existing-artifact pointer in the prompt, (3) focused
  regression tests. Delivered exactly as one story with tasks T-1, T-2, T-3
  matching that breakdown.
- Explicitly out of scope (carried into `plan_brief.md` and the story):
  new database schema, artifact-binding subsystem, generalized handoff system,
  instruction-tree rollout, separate live-agent evidence project.
- Two acceptance criteria carry explicitly unsatisfied evidence obligations per
  `docs/decisions/nested-agent-evidence.md`: AC-005's live-behavior half (that a
  real Claude/Codex conversation actually resets) and AC-009's comprehension
  half (that a reset agent actually reads the named brief). Both are retained,
  not weakened, and must not be marked proven by an offline test suite alone.
- Research (run 0002) independently confirmed the same mechanism and the same
  two unsatisfied evidence obligations from source directly; see
  `RESEARCH_BRIEF.md`. Research (run 0003) confirmed that brief remains
  current with no repository changes since; no re-derivation was needed.
- 2026-09-12 (run 0004) — Implement run completed for story
  "fresh-coding-agent-per-mode" (issue #254, revision
  3487862efecaba6d36c5383c083ebc29ba42fd9c07c5a892865b994a4ff4f502): T-1
  (mode-change segment rotation in `internal/turn/delivery.go`), T-2
  (existing-artifact prompt pointer via new `internal/turn/session_artifacts.go`,
  `naming.ManifestName`, and two new `harness.PromptInput` fields rendered only
  on the cold path), and T-3 (focused regression tests, new
  `internal/turn/mode_reset_test.go` plus additions in
  `internal/turn/session_artifacts_test.go` /
  `internal/harness/prompt_session_artifacts_test.go`) all implemented by
  go-implementation-engineer. Aggregate gates run by the orchestrator: `make
  fmt vet lint` clean (0 issues); full `internal/turn` package passes
  functionally (479s, no race); full-package race runs green for
  `internal/harness`, `internal/naming`, `internal/planbrief`, `internal/codex`,
  `internal/store` (store confirms AC-014's migration inventory unchanged);
  `internal/daemon` confirmed functionally green (83s) after a race-batch
  timeout traced to pre-existing package cost, unrelated to this story. One
  genuinely pre-existing, out-of-scope failure remains in
  `internal/teamsource` (instruction-tree parity across `.claude`/`.codex`
  files this story is barred from touching) — noted, not fixed. Engineering
  submission recorded via `submission preflight` (valid=true, complete=true,
  diagnostics=[]) then `submission submit`: `InputState: valid`,
  `EngineeringState: recorded`, `EffectState: pending`, request_id
  `e2b07543f1ff3141f5e9c310769327a5`, contribution ID 53, run 364.
- Implement (run 0004) notes: two pre-existing repository conditions surfaced while
  running the aggregate test gates, both unrelated to this story's files and
  left as-is per scope: `internal/teamsource`'s
  `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly` fails
  on files this story is barred from touching (`.claude/agents/**`,
  `.codex/skills/**`); and `internal/daemon`'s full-package race suite exceeds
  an 8-minute timeout when bundled with the rest of the repository under
  `-race` (confirmed to pass functionally without `-race` in 83s), a
  pre-existing package-size/race-overhead condition, not a regression from
  this story's code.
- 2026-09-12 (run 0005) — Recovery/correction run following commit `afce2f1`
  ("Apply confirmed conflict-resolution proposal"), which merged main
  (including issue #228's "switch the coding agent from the issue label")
  into this branch and, in resolving the conflict in
  `internal/turn/delivery.go`, took main's side wholesale — deleting this
  story's T-1 mode-rotation logic and breaking the build
  (`internal/turn/delivery.go:378:33: undefined: previous`). The owner's
  diagnosis was verified directly (build failure reproduced exactly as
  described; `prepareDeliverySegment`'s admitted-mode comparison was
  confirmed absent; `mode_reset_test.go`, `session_artifacts.go`,
  `session_artifacts_test.go`, and
  `harness/prompt_session_artifacts_test.go` were confirmed still present)
  before any fix was made. go-implementation-engineer then: (1) restored the
  `modeChanged` comparison and composed it with #228's `last.Agent !=
  string(req.Agent)` in-place-reconcile arm, choosing and documenting the
  precedence that agent-switch reconciliation always wins over a
  simultaneous mode change (the switch's agent-switch case is checked first
  and short-circuits before `modeChanged` is consulted, guaranteeing at most
  one segment mutation per turn by construction) — added
  `TestSimultaneousAgentAndModeChangeReconcilesRatherThanRotates` asserting
  exactly one segment results; (2) also found and re-removed a second,
  undocumented regression the same merge reintroduced — a `p == nil &&
  !HasSessionDelivery` fast path that unconditionally short-circuited
  bundleless-repo turns to `FirstSegmentOrdinal`, bypassing the rotation
  ladder entirely (run 0004 had already removed this once; the merge brought
  it back); (3) rewrote `TestAgentSwitchRefusalFiresRegardlessOfModeChange`
  to drive a genuine continuation (`req.Continuation != nil`), matching
  `prepare.go`'s #228-derived continuation-gated refusal, instead of
  reverting that gating to fit the old test; (4) fixed the tautological
  `TestBuildPromptSessionArtifactsByteIdentity` (now proves a populated vs.
  empty difference before asserting byte-identity) and wrote the
  previously-missing AC-013 offline fixture,
  `TestModeResetPreservesOrdinaryColdTurnMachinery`, comparing a mode-reset
  cold turn against an agent-switch cold turn's sync outcome, attachments,
  comment history, and approved context. Files touched: only
  `internal/turn/delivery.go`, `internal/turn/mode_reset_test.go`,
  `internal/harness/prompt_session_artifacts_test.go` (confirmed via `git
  status --short`). go-code-reviewer returned **CLEAN** on all three files,
  independently confirming the switch-arm exclusivity, the fast-path
  removal's soundness, and each rewritten/added test's fidelity to its AC.
  go-unit-test-engineer ran targeted, race-enabled and functional passes
  across `internal/turn` and `internal/harness` covering AC-001 through
  AC-013 plus an `internal/store` migration sanity check for AC-014, found no
  coverage gaps, and made no changes. Orchestrator then ran the aggregate
  gates: `make fmt vet lint` clean (0 issues); `go test ./internal/turn/...`
  full-package functional green (448.5s); `go test ./internal/harness/...
  ./internal/naming/...` race-enabled green (18.4s / 1.7s); `go test
  ./internal/store/... ./internal/codex/...` race-enabled green (196.1s /
  38.7s); `go test ./internal/daemon/...` functional green (38.3s, `-race`
  intentionally not combined with the full suite per run 0004's already-noted
  pre-existing package-size/race-overhead condition). Per explicit owner
  instruction this run, the two pre-existing, out-of-scope failures
  (`internal/teamsource` instruction-tree parity;
  `TestRunTimeoutKillsTheWholeSubtree`) were left untouched and not
  re-verified. No factory submission was made (no descriptor supplied this
  run); no new story events were emitted; nothing was committed, staged, or
  pushed — that remains the factory's step.
