# Implement mode notes — failed-runs-get-committed-under-their (#126)

## Session continuity

This Implement run resumed a prior run that was cut off mid-flight by a
session rate-limit error (HTTP 429). Before resuming, verified via `git
status --short` and `git log --oneline` that the prior run's Phase 1 work
(batch 1: US-1, US-2, US-4, US-6) plus partial Phase-1 batch 2 work (US-3,
US-5) had already been swept into a single commit by litefactory's own
end-of-run inspection, using the rate-limit error text as the commit
message (`15efc68 "You've hit your session limit · resets 3:40pm
(Europe/Vienna)"`). This is itself an instance of the exact bug this
feature fixes — but it happened under the *pre-fix* code, before this
session's US-1/US-2 changes existed to prevent it, so it was not something
this run could have avoided. It is left as-is; litefactory's commit history
is not something this run rewrites (destructive git ops are out of scope
and not something any agent here is permitted to run).

Re-invoked `go-implementation-engineer` for US-3 and US-5 in explicit
"verify and complete" mode rather than assuming either was finished or
redoing them blind. Both reported their core production-code tasks were
already correct as landed by the cut-off run; US-5's only gap was an
undone `internal/harness/doc.go` update, which was closed in the recovery
pass.

## Code Review Advisories

Code review verdict: **CLEAN**. No BLOCKING or ADVISORY findings — see
`CODE_REVIEW.md` for the reviewer's independent verification detail
(the two `Succeeded()` predicates stay separate, `gate_test.go` untouched,
no schema/migration file, US-5's security guardrail uses the preferred
`<note>`/`neutraliseNoteTags`/`capText` option, `prepare`'s
`SetSessionState` ordering invariant intact).

## Bug loop (2 cycles, both closed)

Both cycles found and fixed the same underlying defect in different
compositions: multiple implementation passes independently added a
worktree-path mention to `internal/report/run.go`'s non-success report
wording without visibility into each other's edits, producing a
double-mention. Neither was in the original plan or story text — both were
caught by go-unit-test-engineer's own AC-derived tests, not anticipated by
any story's Architecture Notes.

- **Cycle 1 / BT-001**: `describeRunOutcome`'s `OutcomeFailed`/
  `OutcomeTimedOut`/`OutcomeNoResult` arms each named the worktree path
  *and* `metadataList`'s Commit-line default arm named it again. Fixed by
  making `metadataList`'s Commit-line arm the single canonical site;
  removed the path mention from the three `describeRunOutcome` arms
  (`OutcomeTimedOut` kept its "still in the worktree" sentence, just
  without the path).
- **Cycle 2 / BT-002**: found during cycle 1's re-test, not in the original
  failure set. `outcomeView.withFailure` unconditionally named the
  worktree path on all three of its branches, colliding with
  `metadataList`'s default arm whenever `CommitSHA == ""` (a failure that
  produced no commit at all — e.g. litefactory never reached the agent, or
  cleanup/push also failed after a run failure). Fixed by guarding
  `withFailure`'s `worktreeNote` call on `run.CommitSHA != ""`, the exact
  complement of `metadataList`'s trigger condition, making the two sites
  mutually exclusive by construction. `run_push_failed.golden`
  (`CommitSHA != ""`) is unaffected and confirmed byte-identical.

Both fixes verified independently: full `internal/report` suite green
(`-race -cover`, 81.9% coverage), and a whole-repo `go build ./...`,
`go vet ./...`, `gofmt -l .`, `go test ./...` gate check (run directly by
this orchestrator, not just reported by an agent) is clean across every
package.

## Assumptions recorded (ordinary uncertainty, settled on the most
reasonable reading, not escalated)

- Batched Phase 1 by dependency rather than one giant parallel fan-out:
  batch 1 = US-1, US-2, US-4, US-6 (no unmet code dependencies among them);
  batch 2 = US-3 (plan_brief: ships after US-1/US-2), US-5 (plan_brief:
  needs US-4's new `PromptInput` fields to exist, and its own task table
  cites `T-5.1 depends on US-4's T-4.3`). This is stricter than strictly
  necessary given all six stories land in one run/commit, but matches the
  plan's explicit shipping order and avoided a real risk: two engineers
  independently adding the same struct fields or guard logic without
  visibility into each other's diff.
- `internal/harness/doc.go`'s update (a Cross-Story Constraint with no
  explicit task-table owner) was assigned to the US-5 engineer, as the
  last engineer to touch that package in this run, so it could describe
  both US-4's new fields and US-5's corrective note in one coherent pass.
- Task-type rows marked "test" in each story's Task table (e.g. T-1.3-1.6,
  T-2.3-2.4, T-3.3-3.4, T-4.5-4.7, T-5.4-5.7, T-6.2) were left to
  `go-unit-test-engineer` in Phase 3 rather than written by the
  implementation engineers in Phase 1, per each implementation engineer's
  own stated role boundary ("I never write test files"). This produced
  full coverage without duplication.

## Environment setup (Phase 2)

Skipped — no story in this plan touches a real database, external HTTP
call, or message queue. All test doubles are hand-written fakes, per the
plan's own Tests constraint. No `TEST_BASE_URL`/`TEST_DATABASE_URL` lookup
was needed and none was performed.
