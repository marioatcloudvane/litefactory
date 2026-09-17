# Implement run 0015 notes

## Dispatch order

- US-001 (#309) and US-002 (#310) dispatched in parallel to two
  `go-implementation-engineer` agents — independently deliverable per the plan,
  touching disjoint file sets (US-001: store migration + check lookup + turn
  snapshot plumbing; US-002: store scoped receipt/progress queries + turn
  recovery isolation).
- Combined build/vet checked clean after both landed before starting US-003.
- US-003 (#311) dispatched afterward, depending on both — it consumes US-002's
  `joinSessionCleanup` seam and `*SubmissionRecoveryError` type, and layers
  phase-aware harvest reporting on top.
- `go-unit-test-engineer` dispatched once, after all three stories landed, to
  write comprehensive AC-mapped tests across all three stories using real
  disposable SQLite (repo convention) — no mocks, no new test tags.

## Assumptions recorded during this run

- Treated "disposable SQLite databases and controlled runners" (per plan
  guidance) as the existing repo's ordinary unit-test convention (temp-file
  SQLite + fake/controlled agent runners already used throughout
  internal/store and internal/turn tests), not as a Phase-2-style external
  dependency requiring `TEST_DATABASE_URL`/`TEST_BASE_URL`. No external test
  environment variables were needed or consulted; Phase 2 (environment setup)
  was skipped as a result — all three stories' tests are self-contained.
- `StoryProgress.Finish` was kept at its existing 3-argument signature
  (`ctx, runID, reason`) rather than adding a `parent` parameter, to avoid
  churn to existing call sites; it now derives its own registered parent
  internally. Recorded as the US-002 engineer's judgment call, consistent with
  the architecture guidance's "legacy Finish stays run-scoped."
- Added a one-line guard to `publishPlanHandoff` (`result.Err != nil ||`) as
  part of US-003 — not explicitly named in the story's known-file list, but
  necessary to satisfy AC-005's "local factory failure still blocks... success-only
  publication" for Plan runs specifically (mirrors a guard `save()` already
  had). Judged a minimal, in-scope, no-op-on-happy-path fix rather than scope
  creep.

## Aggregate gate limitation

See TEST_REPORT.md for full detail: the literal single `make check` invocation
could not be executed to completion within this orchestrator's per-call time
ceiling, because of one pre-existing, unrelated `internal/daemon` test
(`TestRetirementExecutionRealLifecycle`, confirmed untouched by any of these
three stories via empty `git status`/diff on that package) that does not
finish within a 9-minute budget, plus `internal/turn`'s very large full-suite
parallel test volume also exceeding a 9-minute budget independent of `-race`.
Both were reproduced directly by this orchestrator and independently observed
by three separate sub-agents during this run. `make`'s constituent gates
(fmt, vet, lint) were run directly and passed; the test gate was run in
scoped, bounded batches covering every package this work touches plus every
other package in the repo except `internal/daemon`, all green. This is
reported as a known limitation, not claimed as a full `make check` pass.

## Code review

Per this run's explicit brief: "This is a 'new separated workflow' Implement
run... Do NOT perform acceptance/code/architecture/plan review." No
`go-code-reviewer` was dispatched; that step is reserved for Validate, invoked
separately later.


## Run 0017 — status clarification and aggregate recovery

The supplied factory state marks US-001, US-002 and US-003 engineering complete. They were treated as read-only satisfied implementation dependencies. Current changed-file bytes match their retained digest. No completed story was reimplemented or resubmitted.

The requested answer is that the repair exists in the worktree but has not landed as a saved implementation commit: HEAD is still the planning commit 9308144 and the 24 implementation/test files remain modified/untracked. No forge reads were made; remote merge/deployment is not claimed.

The remaining literal make check was attempted once unchanged with a 480-second outer bound, as the Makefile disables its internal Go test timeout. Format/vet/lint passed, but the full race suite timed out (exit 124). Build also passed. The failure's underlying test is not established by the captured output. Per the authorized recovery scope, report this specific failure and stop rather than changing completed engineering.

No acceptance/code/architecture/plan review occurred. No new story events or aggregate success proof were emitted. Plan orphan recovery remains deferred. Factory save/push and acceptance remain separate.


## Run 0018 assumptions and limits

The approved scope is aggregate recovery only. Completed story engineering and existing evidence are retained without reimplementation. A temporary Go launcher bounds the required make race gate to seven minutes per package so failures retain stack traces; no Makefile or production file was edited. Four packages exceeded that total runtime budget while their active tests were only 0–9 seconds old. Do not describe this observation as a proven daemon test hang. Aggregate completion remains outstanding; factory-owned save and acceptance are separate. See run0018-aggregate-report.md.
