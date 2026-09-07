# Implementation Notes — US-8 (Config-governed defaults and size cap)

## Scope of this run

This run implemented **US-8 only**, per explicit instruction. US-1 through
US-7 and US-9 through US-12 are untouched. The uncommitted, never-reviewed
partial US-1/US-2 work already present in the worktree (`internal/git/merge.go`,
`internal/turn/prepare.go`, `internal/git/{errors,exec,worktree}.go`, from a
prior stopped run) was left exactly as found — not read for guidance, not
built upon, not touched.

## Assumptions

- The "config docs update" task (T-8.3) was satisfied by adding the three new
  keys, with their documentation, to `internal/config/template.go`'s generated
  `config.yaml` template (`DefaultFileContents()`), since the repo has no
  separate `docs/` markdown file enumerating individual config keys and this
  template is what the config package's own convention (CFG-3 anti-drift
  check) treats as the source of operator-facing key documentation.
- No upper bound was added to `AutoSyncMaxBehind` — the story only specifies
  "negative is an error"; adding an unrequested ceiling would have been an
  unrequested scope addition.
- The three new fields were placed flat on `Settings` (not a new nested
  stanza), matching the YAML key names given in the story (`auto_sync_base`,
  `auto_sync_max_behind`, `block_run_on_conflict`) and the existing flat style
  of other `settings.*` keys.

## Code Review Advisories

- **CR-A-001** (advisory, non-blocking): the wording of the negative
  `auto_sync_max_behind` config-load error message is slightly awkward,
  though functionally correct and correctly identifies the offending field.
  Left as-is; does not affect AC-8.2 or any test. See `CODE_REVIEW.md` for
  detail.

## Phase 2 — Environment setup

US-8 touches only `internal/config` (in-memory struct defaulting/validation
against YAML input) — no database, no external HTTP call, no message queue.
No story in this run's scope touches a real dependency. Environment setup and
the integration test suite were both skipped per the orchestrator's Phase 2
rule ("no story touches real dependencies -> skip to Phase 3 immediately").

---

# Implementation Notes — US-7 (`lf:sync=` label opt-out)

## Scope of this run

This run implemented **US-7 only**, per explicit instruction ("Implement
US-7"). US-1 through US-6 and US-9 through US-12 are untouched. US-8 (already
implemented and committed in a prior run) was not touched. The uncommitted,
never-reviewed partial US-1/US-2 work already present in the worktree
(`internal/git/merge.go`, `internal/turn/prepare.go`,
`internal/git/{errors,exec,worktree}.go`, from an earlier interrupted run) was
left exactly as found — not read for guidance, not built upon, not touched.
Confirmed via `git status` after implementation: only `internal/daemon/sync.go`
(new), `internal/daemon/tick.go`, `internal/turn/execute.go`, and the two new
test files (`internal/daemon/sync_internal_test.go`,
`internal/daemon/sync_test.go`) changed.

## What was built

- `internal/daemon/sync.go` — `syncLabelPrefix`, `syncRefusal` enum
  (`syncRefusalNone`/`syncRefusalAmbiguous`/`syncRefusalMalformed`),
  `syncDecision` struct (`Resolved bool; Value bool; Refusal syncRefusal;
  MiscasedPrefix bool`, plus a `refused()` method), and
  `resolveSync(labels []string) syncDecision` — copied rung-for-rung from
  `internal/daemon/agent.go`'s `agentLabelPrefix`/`resolveAgent` ladder, not
  from the dead `lf:tools=`/`legacyToolsLabel` pattern, per the story's
  explicit instruction.
- `internal/turn/execute.go` — `turn.Request` gained `SyncOverride *bool`
  (`nil` = no override, config default applies; non-nil = the label's
  resolved on/off). No `validate()` changes needed — nil is a legitimate
  value.
- `internal/daemon/tick.go` — computes `syncOverride := resolveSync(t.issue.Labels)`
  immediately before the existing `turn.Request{...}` literal and passes
  `SyncOverride` through when `.Resolved` is true; no other field in that
  literal was touched.

This story stops at producing the resolved decision and threading it into
`turn.Request` — it does not implement `syncBase` (US-2, not yet built),
config-default reading (US-8, already done separately), or disclosure of the
refusal reason (US-3, not yet built). `syncDecision.Refusal`/`MiscasedPrefix`
are structured so a later disclosure story can build on them without needing
any raw label text, which was never captured or stored anywhere (SEC-5/S-9).

## Assumptions

- None required beyond the story's own architecture notes — AC-7.1 and the
  ladder description in `US-7.md` were unambiguous and directly implementable
  against the existing `agentLabelPrefix`/`modeLabelPrefix` precedent in the
  same package.

## Code Review Advisories

- None. `go-code-reviewer` returned **CLEAN** with zero BLOCKING or ADVISORY
  findings — verified the ladder's precedence order against `agent.go`
  side-by-side, verified no field on `syncDecision` or `turn.Request` can
  carry raw label text (SEC-5/S-9), verified `tick.go`'s integration point
  left every other `turn.Request{...}` field untouched, and confirmed scope
  discipline via `git status` (only the three implementation files changed).
  See `CODE_REVIEW.md`'s second dated section for the full record; the file's
  first section (US-8, from the prior run) was left unchanged.

## Phase 2 — Environment setup

US-7 touches only `internal/daemon` (in-memory label-string parsing) and a
struct field addition in `internal/turn` — no database, no external HTTP
call, no message queue. No story in this run's scope touches a real
dependency. Environment setup and the integration test suite were both
skipped per the orchestrator's Phase 2 rule.

## Testing

`go-unit-test-engineer` added two test files covering every rung of the
ladder (absent, `on`, `off`, miscased prefix ×3 shapes, duplicated/ambiguous
label ×3 shapes, invalid/empty value), sibling-label independence, a
structural SEC-5/S-9 check via `reflect` confirming no field on
`syncDecision` can carry a string, and an end-to-end check that
`turn.Request.SyncOverride` is populated correctly (or left `nil`) at the
`Daemon.run`/`tick` dispatch point for every label shape. 7 test functions
(~21 subtests total), 0 failures.

## Final gate (orchestrator-run, not delegated)

`go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and
`go test -race ./...` were all run directly by this orchestrator across the
whole module (not just the touched packages) and are all green — every
package reports `ok`, no race detector findings, no vet or fmt issues.

## Bug loop

Not entered — zero test failures reported by any phase.

---

# Implementation Notes — US-1 (Measure behind/ahead before every run)

## Scope of this run

This run implemented/verified **US-1 only**, per explicit instruction. It
began from an unusual state: a prior run had already written `Divergence`,
`parseDivergenceCounts`, `divergenceTimeout`, and the `prepare.go` call site,
but the run was stopped by the operator at 49 minutes before any review or
testing, and its commit (`797181a`) had never been checked against US-1's
acceptance criteria. This run's mandate was therefore verification-first:
confirm or correct the existing code, then deliver T-1.4 (unit tests), which
was the only task genuinely missing. US-2's partial work in the same file
(`MergePreview`, `MergeBase`, `recoverFromFailedMerge`) was read only for
pattern comparison and left byte-for-byte untouched.

## What was verified (and found correct, not corrected)

- The single named trap — `git rev-list --left-right --count base...branch`
  emitting `<left> <right>` where "ahead" vs "behind" depends on which ref
  sits on which side of `...` — was checked empirically against real git
  output in a throwaway repo, not by reading the code and assuming. Base on
  the left of `...` yields the count of commits only reachable from base
  (Behind); branch on the right yields commits only reachable from branch
  (Ahead). The existing code's `rangeSpec := base + "..." + w.branch` and
  `Divergence{Ahead: ahead, Behind: behind, ...}` construction were both
  confirmed correct, independently, by two separate agents (the
  implementation engineer and, again, the code reviewer tracing from source
  rather than trusting the first agent's report).
- `checkName` screening on both `base` and `w.branch`, range assembly inside
  `internal/git`, `--end-of-options` usage, the `divergenceTimeout` (30s)
  const and its wiring, and the missing-base-ref classification via
  `classify(err, ErrRevisionNotFound, 128)` were all verified present and
  correct.
- The `prepare.go` call site (`syncBase`, ~line 464-469) was confirmed to
  build `baseRef := origin/<Repo.BaseBranch>` and call `Divergence` after the
  existing fetch, inside the same `CloneGate` acquisition, matching the
  story's required integration point with no TOCTOU gap.

**No production code changes were made.** `internal/git/merge.go` and
`internal/turn/prepare.go` are unmodified by this run — `git status` after
implementation confirmed this. The prior (killed) run got T-1.2 and T-1.3
right on the first attempt.

## What was built

- `internal/git/merge_test.go` (new file) — T-1.4 in full: 4 test functions,
  9 executed subtests. All four ahead/behind combinations (0/0, ahead-only,
  behind-only, both) against real git repos; a side-order regression test
  using deliberately asymmetric, distinguishable counts (base +3, branch +1)
  that pins AC-1.1's exact trap; a missing-base-ref error-path test asserting
  the classified sentinel, not a raw exit code or panic; and a
  `checkName`-screening test confirming malformed ref names never reach the
  git binary. Reuses the existing `internal/git`/`internal/gittest` real-repo
  test harness already used by `clone_test.go`/`worktree_test.go` rather than
  inventing a new one.

## Assumptions

- None required beyond the story's own architecture notes and AC-1.1 — both
  were unambiguous, and the empirical-verification instruction (check real
  git behavior, don't just read the code) was followed literally by both the
  implementation engineer and the code reviewer, independently.

## Code Review Advisories

- None. `go-code-reviewer` returned **CLEAN**, zero BLOCKING or ADVISORY
  findings, scoped strictly to the `Divergence`-related code in `merge.go`
  and `exec.go` plus the `prepare.go` call site. It explicitly re-traced the
  ahead/behind ordering from source itself rather than relying on the
  implementation engineer's report. See `CODE_REVIEW.md`'s US-1 section for
  the full record; the file's earlier US-8/US-7 sections were left unchanged.

## Phase 2 — Environment setup

US-1 touches only `internal/git` (shelling out to a local git binary against
worktree-local refs) and a call-site read in `internal/turn` — no database,
no external HTTP call, no message queue. Consistent with this codebase's
existing convention (`internal/git`'s tests already run real git in temp
directories as unit tests, not integration tests against a provisioned
service), this was treated as pure unit-testable surface. Phase 2 environment
setup and the integration test suite were both skipped.

## Testing

`go-unit-test-engineer` wrote and ran `internal/git/merge_test.go` covering
T-1.4's full list (4 combinations, side-order regression, missing-ref error
path, ref-screening error path). 4 test functions, 9 subtests, 0 failures.

## Final gate (orchestrator-run, not delegated)

`gofmt -l .`, `go vet ./...`, and `go build ./...` were all run directly by
this orchestrator across the whole module and are clean/green. The unit test
engineer separately reported `go test -race ./...` green across all 21
module packages, including `internal/git` with no race detector findings.

## Bug loop

Not entered — zero test failures reported by any phase.

---

# Implementation Notes — US-2, US-3, US-4 (Auto-merge, disclosure, refusal)

## Scope of this run

This run implemented **US-2, US-3, and US-4**, per explicit instruction, as a
fresh start on scope a prior rate-limited run had announced but never written
to disk (confirmed via `git status` clean at the start of this run). US-1,
US-7, and US-8 (already implemented, reviewed, and committed) were left
untouched. US-5, US-6, and US-9 through US-12 remain out of scope.

Sequenced per `plan_brief.md`'s explicit dependency chain: US-2 first (alone),
gated to DONE, then US-3 and US-4 in parallel (different files: US-3 stayed in
`internal/report/run.go`/`doc.go`/`report_test.go`; US-4 touched
`internal/report/reply.go`/`reporter.go` plus `internal/turn/{errors,result,
execute,prepare,report}.go` and one line in `internal/daemon/setup.go`).

## Pre-existing partial code (US-2)

`internal/git/merge.go` already contained unreviewed `MergePreview`,
`MergeBase`, and `recoverFromFailedMerge` from an earlier, separate
interrupted run. Verified against US-2's actual acceptance criteria rather
than assumed correct, per instruction. Most of it was correct and kept
as-is — including, surprisingly, `internal/turn/prepare.go`'s `syncBase`
wiring and `internal/turn/gate_test.go`'s registry edit, which the story
description said were still to be built but turned out to already be present
from the same earlier interrupted run and were verified, not rewritten. One
real bug was found and fixed: `parseMergePreview` split `merge-tree`'s output
on the wrong section boundary (`"\n\n"` instead of tracking the tree-id/path
list/messages boundary correctly), corrupting `TreeSHA` and `Conflicts` on
every real conflict. Caught immediately by a new real-git test
(`TestWorktreeMergePreviewConflict`) before code review ever ran.

## Code Review Advisories

- **CR-A-001** (advisory, non-blocking, US-3): `report.Sync` gained a
  `Reason string` field beyond the architecture notes' originally-listed
  field set (`Outcome, Behind, Ahead, Base, CommitSHA, Conflicts`). The task
  itself flagged this as a judgment call for the engineer to resolve
  (AC-3.2 requires every `skipped` outcome to disclose *why*, and the
  listed field set had no field to carry that). Reviewed and accepted: a
  plain string was chosen over a typed reason enum because `internal/turn`'s
  `syncResult` doesn't yet distinguish skip causes (cap vs. config vs. label
  all collapse into one outcome value) — a string lets a future caller (US-9)
  supply whatever sentence fits without this package needing the caller's
  reason taxonomy. See `CODE_REVIEW.md` for full detail.
- **CR-A-002** (advisory, non-blocking, US-4): no direct end-to-end test
  exercises `ReplyRefusesSync` through the full `Execute` → `PostComment`
  path with a real comment body assertion (T-4.5's three behaviors — agent
  never starts, no run-artifact directory, refusal comment posted — are each
  covered, but not in one single end-to-end test). Left as a coverage gap,
  not a correctness defect; flagged for `go-unit-test-engineer` to consider
  closing in Phase 3.

## BLOCKING finding found and resolved during Phase 1.5

- **CR-B-001** (was BLOCKING, now resolved, US-2): `MergePreview`'s
  `git merge-tree --write-tree --name-only` invocation omitted `-z`, so any
  conflicting path with a non-ASCII byte or a special character (tab,
  backslash, double-quote) came back C-quoted/octal-escaped rather than as
  the literal path — the same hazard `internal/git/status.go` already solves
  correctly for `git status`, but not applied here. This would have broken
  AC-2.2's "the exact conflicting paths are returned" for any real-world repo
  with non-ASCII filenames, and flowed unmodified into both US-3's
  `report.Sync.Conflicts` and US-4's `Reply.ConflictPaths`.
  Fixed by adding `-z` and rewriting `parseMergePreview` to decode
  NUL-terminated, unquoted output; the new output shape (tree-id field,
  then NUL-terminated conflicting paths, then an empty field marking the
  start of the free-form messages section) was verified empirically in a
  throwaway repo via git plumbing commands (`hash-object`/`mktree`/
  `commit-tree`/`update-ref`, since the sandbox blocks `git commit`/`git add`
  even in scratch directories) rather than trusted from documentation alone,
  per the plan's own re-pinning discipline. A regression test
  (`TestWorktreeMergePreviewConflictNonASCIIPath`, using a `café.md`
  conflicting filename) was added and confirmed to fail against the
  pre-fix code and pass against the fix. Re-review verdict: ADVISORY_ONLY,
  confirmed the fix is structurally correct (genuine NUL-split parser, not
  relabeled newline logic), the trailing-NUL-survives-`TrimRight("\n")` edge
  case is handled correctly for the clean case, and no regression to
  S-1/S-4/S-5/S-6/S-8/S-9.

## Phase 2 — Environment setup

None of US-2, US-3, or US-4 touches a real dependency (database, external
HTTP call, message queue) — `internal/git` shells out to a local git binary
against worktree-local refs (this codebase's existing convention already
tests this as real-git-in-temp-directory unit tests, not integration tests),
and `internal/report`/`internal/turn` are pure in-memory logic. Environment
setup and the integration test suite were both skipped per the orchestrator's
Phase 2 rule.

## Assumptions

- US-3's live wiring of `report.Run.Sync` from `internal/turn`'s in-memory
  `syncResult` was deliberately left unbuilt: `turn.Result` has no field to
  carry `syncResult` forward to the reporting layer yet, and building that
  plumbing would require inventing ad hoc carriage ahead of US-9 (which
  persists the fields this wiring properly depends on and is out of scope
  this run). The struct, renderer, and golden tests were built to the task
  table's actual scope (`internal/report` only); `SyncOutcome`'s string
  values were deliberately chosen to match `internal/turn`'s private enum's
  string values exactly, so US-9's eventual wiring is a one-line conversion.
- US-4's `replyKind()` claim in its own architecture notes ("this is a new
  branch in that function") was checked against real code and found
  *incorrect* as written — the engineer instead followed the actual
  established precedent (`ReplyAgentSwitchRefused`'s shape: delivered via
  `ReplyRequest.Refusal`/`req.kind()`, never through `replyKind()`, which
  only maps `CommandKind` values) and did not add a `replyKind()` case. The
  code reviewer independently re-traced this against source and confirmed
  the engineer's actual approach — not the sub-issue's literal wording — is
  correct.
- `turn.Options` had no `BlockRunOnConflict` field before this run (only
  `AutoSyncBase`/`AutoSyncMaxBehind` existed from US-2/US-8's earlier work);
  the US-4 engineer added it and wired it from `internal/daemon/setup.go`
  (the only place `turn.Options` is constructed in production), flagging the
  one file touched outside `internal/report`/`internal/turn` explicitly.

## Testing

Both `go-unit-test-engineer` coverage (via the implementation engineers'
own real-git and whitebox tests, since this codebase's convention tests
`internal/git` as unit tests) and a full-module race run were executed
directly by the orchestrator after Phase 1, before code review, and again
implicitly via the code reviewer's own `go test` runs during and after the
CR-B-001 fix: `go build ./...`, `go vet ./...`, `gofmt -l .` all clean;
`go test -race ./...` green across every package with no race detector
findings, both before and after the CR-B-001 fix.

## Bug loop

Not yet entered as of writing this note — pending Phase 3's dedicated
`go-unit-test-engineer` pass.

---

# Implementation Notes — US-5, US-9 (Render conflicts safely, Persist sync outcomes)

## Scope of this run

This run implemented **US-5 and US-9 in parallel**, per explicit instruction
("Implement US-5 and US-9 together"). Both were unblocked (US-5 depends on
US-4, US-9 on US-1/US-2, all three already done and committed) and land in
disjoint packages (`internal/report` for US-5; `internal/store` plus a single
write-back call site in `internal/turn/prepare.go` for US-9), making this a
genuine parallel fan-out. US-6, US-10, US-11, US-12 remain untouched and out
of scope, per explicit instruction (US-6 in particular deliberately excluded
from this batch — its concurrency test is sized L and would hold both
finished stories at Gate A).

## US-9's stale migration number

`US-9.md`'s own prose cites `0005_run_sync.sql` on top of a `0004` head —
confirmed stale before implementation started, per the run's own
instructions. `internal/store/migrations/` was listed directly at the start
of this run: the actual head is `0006_schedule_block_replies.sql`, so the
correct next migration is `0007_run_sync.sql`. The implementation engineer
was told this directly (not left to discover it) and re-verified it
independently before writing the file. No collision.

## What was built

- **US-5**: `internal/report/reply.go` gained `maxConflictPathEntries` (20),
  `conflictPathPattern` (local denylist: ASCII control chars, `<`/`>`, and
  the standard Unicode bidi-control code points), `conflictPathSpan`
  (whole-string reject-don't-salvage, mirroring `agentSpan`/`commandSpan`),
  `classifyConflictPaths` (loops the full path list without short-circuiting,
  splitting into accepted spans + a rejected count), and
  `conflictPathRejectionSentence` grouped with the three existing refusal
  sentences. `internal/report/run.go`'s existing `conflictsBlock` (from
  US-3/US-4) was extended, not duplicated, to classify-then-truncate-then
  report the rejected count. Backtick was deliberately left off the denylist
  — the engineer's claim that `fenceFor`'s dynamic fence-width computation
  already defeats backtick-based fence breakout was independently re-verified
  by the code reviewer against `internal/report/text.go`, not trusted as
  stated.
- **US-9**: new migration `internal/store/migrations/0007_run_sync.sql` adds
  `sync_outcome`/`sync_behind`/`sync_ahead`/`sync_commit_sha` to `runs`, each
  `NOT NULL DEFAULT` per convention. `internal/store/runs.go` gained the
  `SyncOutcome` type/constants, the four `Run` fields, and
  `DB.RecordRunSync` (read-then-write under `BEGIN IMMEDIATE`, refusing a
  non-empty `CommitSHA` paired with any outcome other than `synced`).

## Deviation found and independently verified (US-9 integration point)

The architecture notes cited `internal/turn/prepare.go:429`/`:505` as the
write-back site (immediately after `syncBase` completes). The engineer found
that no `runs` row exists yet at that point — `allocateRun` (which creates
the row) runs later, in `prepare()` — and placed the write-back immediately
after `allocateRun` instead, still strictly before the existing
`SetSessionState` call documented as prepare's last store write. The code
reviewer independently re-read `prepare()`'s actual current line numbers
(confirming `prepareClone`/`syncBase` at line 117, `allocateRun` at line 216)
and confirmed the deviation is correct, not merely plausible — a stale
citation in the sub-issue, not a functional problem. This is recorded here
per the plan's own re-pinning discipline (line numbers in a sub-issue drift
as the codebase moves; the underlying ordering requirement — write before
any comment-rendering step reads the outcome back — is what was verified,
not the literal line numbers).

## Code Review Advisories

- **CR-A-003** (advisory, non-blocking, US-9): `SyncFailed` is a fully
  supported, tested `SyncOutcome` value, but nothing in `internal/turn`
  currently writes it — a git-level merge failure aborts the turn before
  `allocateRun` creates a row, so a failed sync is never persisted, only
  logged. Verified by the code reviewer as a pre-existing property of
  `prepareClone`'s ordering, not a defect introduced by this run, and it does
  not violate AC-9.2 as literally written (which binds only to a run that
  reaches completion). Recorded in `DRIFT_REPORT.md` as a gap for a future
  story to close, not fixed in this run — closing it would mean persisting a
  run outcome before a run row exists, which is a larger structural change
  than this story's scope.

No BLOCKING findings for either story. `go-code-reviewer`'s verdict:
ADVISORY_ONLY. See `CODE_REVIEW.md`'s newest dated section for the full
record; all four prior sections (US-8, US-7, US-1, US-2/US-3/US-4) are
unchanged.

## Cross-story boundary check

Verified (by the code reviewer, independently, not just asserted by either
engineer): US-9 does not add a conflict-path column (explicitly deferred, per
`plan_brief.md`'s S-9/S-17 and `US-9.md`'s own architecture notes — the
conflict list is rendered live from `MergePreview`, not reloaded from
storage), and US-5 does not import `internal/store` — the two stories'
conflict-related work stays on opposite sides of the boundary the plan draws
between them.

## Phase 2 — Environment setup

US-5 touches only `internal/report` (pure in-memory string classification and
rendering). US-9 touches `internal/store` (SQLite via the existing
`database/sql` + embedded-migration convention already used by every other
migration in this repo — treated as unit-testable, not requiring a
provisioned external database, consistent with this codebase's own
established testing convention for `internal/store`) plus a single call site
in `internal/turn`. Neither story opens a new external HTTP call or message
queue. Environment setup and the integration test suite were both skipped per
the orchestrator's Phase 2 rule.

---

# Implementation Notes — US-6, US-10 (Concurrency verification, Rollback guarantee)

## Scope of this run

This run implemented **US-6 and US-10 in parallel**, per explicit instruction
("continue with stories 6 and 10"). Both were unblocked (both depend only on
US-2, already done and committed) and land on largely disjoint files: US-6
added a new test file in `internal/turn` plus a doc-only comment in
`internal/daemon/gate.go`; US-10 modified `internal/git/errors.go` and
`internal/git/merge.go`. US-11 and US-12 remain out of scope per the run's own
instruction — US-11 needs US-5+US-9 (both done, so it is unblocked but not
requested this run) and US-12 needs US-11+US-10, so it cannot start until a
later run regardless.

The prior run's handoff described US-6's single task (T-6.1) as sized L — an
N-turn, N-branch concurrency test — and noted it was deliberately excluded
from an earlier batch for that reason. `US-6.md` was re-read fresh at the
start of this run rather than assuming that description still held; its
actual content (verification story, no new locking primitive, AC-6.1/AC-6.2)
matched the prior description closely, with no further gap found.

## What was built

- **US-6**: `internal/turn/syncconcurrency_test.go` (new) —
  `TestConcurrentTurnsSyncIndependentlyOnOneClone` (T-6.1): four issues share
  one registered clone; each branch is cut sequentially, the base is advanced
  once, then all four issues' second turns run concurrently via `Execute`.
  Asserts each turn's persisted sync outcome/behind/commit-SHA, distinct merge
  commit SHAs per issue, no cross-branch interference (checked via
  `git ls-tree` on the bare origin, not worktree files, since that is the
  artifact a bug would actually corrupt), and — read off the real gate's
  acquisition counts before/after the concurrent phase — exactly
  `2 * len(issues)` acquisitions during that phase (`prepareClone` +
  `commitAndPush`, no more), zero overlaps, zero deadlines. No sleep-based
  synchronization; deterministic via `sync.WaitGroup` and a mutex-protected
  disambiguation map. `internal/daemon/gate.go`'s existing doc comment on
  `cloneGate` gained a "Keyed on the clone, not the branch (US-6)" section
  (T-6.2) explaining the keying choice and pointing at the new test.
  `gate_test.go`'s `"syncBase": {"prepareClone"}` entry was confirmed already
  present from an earlier run — no edit needed, no design-change test touched.
- **US-10**: `internal/git/errors.go` gained the `ErrMergeTimedOut` sentinel,
  matching the existing doc-comment shape of `ErrMergeConflict`/
  `ErrUnsupportedGitVersion`. `internal/git/merge.go`'s
  `recoverFromFailedMerge` now runs the same abort-then-reverify sequence for
  a timed-out merge that it already ran for a conflicted one, diverging only
  at final classification: it copies the original `*CommandError` (preserving
  Binary/Dir/Args/ExitCode/Stderr) and swaps in `ErrMergeTimedOut` rather than
  `ErrMergeConflict`. `classify()` itself was deliberately left untouched,
  since it intentionally refuses to reclassify an already-timed-out error for
  unrelated conditions (e.g. `ErrRevisionNotFound`) for other callers, and
  that refusal must stay correct; the timeout substitution was written as a
  small local copy-and-swap mirroring `classify()`'s own shape rather than
  changing its contract. Never uses `git reset` (S-5 respected — no `Reset`
  method exists on `*Worktree`). T-10.3 (the SHA-identity/TimedOut proof test)
  was deliberately left to the unit-test-engineer phase, per this team's
  standing rule that implementation engineers do not write test files; the
  code was left directly testable (`recoverFromFailedMerge` factored out,
  callable with a synthetic `*CommandError{TimedOut: true}` against a
  worktree holding a real live merge conflict).

## Code Review Advisories

- **CR-A-004** (advisory, non-blocking, US-10): `recoverFromFailedMerge`
  reuses the caller's `ctx` for the `git merge --abort` rollback call. The
  reviewer traced `runner.run`/`classifyRunError` and confirmed the abort does
  get a genuinely fresh budget when only the *local* per-command timeout
  fired, but flagged a latent risk: if the *ambient* `ctx` were ever
  independently cancelled (not just the per-command timeout), the abort's
  derived context would be born already-done and the rollback would never
  run. Traced against the actual call graph (`turn.Execute`,
  `daemon/pool.go`'s `Quiesce`/`wait`) and confirmed this is not reachable
  today — no in-flight turn's context is ever independently cancelled — so
  it's latent, not live. Left as-is (document the contract, or decouple abort
  from caller cancellation, is the reviewer's suggested follow-up, not
  required for this story).
- **CR-A-005** (advisory, cosmetic, US-6): one comment line in `gate.go`'s new
  doc section (88 chars) breaks the file's otherwise ~76-78 col wrap
  consistency. Left as-is.

No BLOCKING findings for either story. `go-code-reviewer`'s verdict:
ADVISORY_ONLY. Verified (not just read): `gofmt`, `go vet`, `go build ./...`
all clean; `go test ./internal/git/...`, `./internal/turn/...`,
`./internal/daemon/...` pass; the new concurrency test re-run 5x with `-race`
by the reviewer specifically to check for flakiness — none found.
`internal/git`'s `TestCloneMethodSetIsTheAllowlist`/
`TestCloneCannotMutateTheRegisteredClone` and `internal/turn`'s
`TestCriticalSectionsAreSingleAcquisitions` confirmed unchanged/passing. See
`CODE_REVIEW.md`'s newest dated section for the full record; all five prior
sections are unchanged.

## Cross-agent visibility note (not a defect)

Both implementation engineers, working in parallel in the same worktree, each
independently flagged the other's in-progress diff as "unrelated pre-existing
changes" in their own DONE reports — US-6's engineer noticed US-10's
in-flight edits to `internal/git/errors.go`/`internal/git/merge.go`, and
vice versa. This is expected: both agents share one worktree and ran
concurrently. Confirmed the two stories' actual file sets are disjoint
(US-6: `internal/turn/syncconcurrency_test.go`, `internal/daemon/gate.go`;
US-10: `internal/git/errors.go`, `internal/git/merge.go`) — no real overlap
or conflict.

## Phase 2 — Environment setup

Both US-6 and US-10 touch only `internal/git`/`internal/turn`/`internal/daemon`
— shelling out to a local git binary against worktree-local refs and clones in
temp directories, plus in-memory concurrency/gating logic. Consistent with
this codebase's established convention (`internal/git`'s and
`internal/turn`'s tests already run real git in temp directories as unit
tests, not integration tests against a provisioned service), no story in this
run's scope touches a real external dependency (database, external HTTP call,
message queue). Environment setup and the integration test suite were both
skipped per the orchestrator's Phase 2 rule.

---

# Close-out — US-6, US-10 (resumed run, 2026-09-07)

## What was cut off, and what was verified before resuming

The prior run was cut off by an API rate limit after code review had already
returned ADVISORY_ONLY (CR-A-004, CR-A-005, zero BLOCKING), but before the
unit-testing phase ran. Before doing any new work, this run independently
verified — rather than assumed — that the interrupted run's partial output
had actually landed: `git status --short` showed a clean tree and
`git log --oneline` showed commit `cfa2a9f` as HEAD, confirming litefactory
had committed the in-flight work despite the cutoff. `go build ./...`,
`go vet ./...`, and `gofmt -l .` were all clean at that commit, and a
targeted `go test -race ./internal/git/... ./internal/turn/...
./internal/daemon/...` was green, confirming the already-committed
implementation was not broken. Only then did the run proceed to Phase 3.

## Phase 3 — unit testing (this run's actual work)

`go-unit-test-engineer` was invoked for US-6 and US-10 only, told explicitly
not to redo or rework the already-reviewed implementation. It read every
relevant existing file — `internal/git/merge.go`, `errors.go`,
`merge_timeout_internal_test.go`, `merge_test.go`,
`merge_internal_test.go`, `internal/turn/syncconcurrency_test.go`,
`gate_test.go`, `sync_test.go` — before writing anything, and reported that
every acceptance criterion for both stories (AC-6.1, AC-6.2, AC-10.1,
AC-10.2) was already fully proven by tests already on disk. In particular,
T-10.3 — the one gap the interrupted run's own code review had explicitly
flagged as outstanding — turned out to already be closed:
`merge_timeout_internal_test.go`'s `TestRecoverFromFailedMergeTimeout` and
`TestRecoverFromFailedMergeTimeoutAbortFailurePreservesOriginalError` were
written by the same interrupted run, in a file that postdated the code
review's "files reviewed" snapshot — the review was correct about the state
it saw, and the gap it named was closed moments later, before the rate
limit hit.

Net result: zero new test files, zero production-code changes (no bug was
found by the added scrutiny), 661 subtests passing under `go test -race
-count=1 -v ./internal/git/... ./internal/turn/... ./internal/daemon/...`,
`go vet ./...` clean, `gofmt -l` clean on the touched packages. Coverage:
`internal/git` 84.6%, `internal/turn` 84.1%, `internal/daemon` 76.2%.

## Repo-wide verification

Per this run's own instruction, the orchestrator re-ran
`go build ./... && go vet ./... && gofmt -l . && go test -race -count=1
./...` directly across the whole repository, not just the previously-touched
packages. All 22 packages passed (`cmd/litefactory`, `internal/agent`,
`internal/attach`, `internal/cli`, `internal/codex`, `internal/config`,
`internal/daemon`, `internal/git`, `internal/github`, `internal/gittest`,
`internal/harness`, `internal/identity`, `internal/launchd`,
`internal/logging`, `internal/naming`, `internal/planbrief`,
`internal/redact`, `internal/report`, `internal/repos`, `internal/schedule`,
`internal/secrets`, `internal/store`, `internal/turn`), `go vet` and
`gofmt -l .` produced no output. No bug loop was needed — zero failures at
any phase of this resumed run.

## Close-out

`DRIFT_REPORT.md` gained a seventh dated section (US-6/US-10), verdict
CLEAN. `MANIFEST.md`'s Implement checkbox and history were updated to
reflect US-6 and US-10 as delivered (10 of 12 stories now done). No
`APP_CONTEXT.md`/`APP_CONSTRAINTS.md` update was made, per the retirement of
that step from this team's workflow. Remaining for future runs, each needing
its own comment: US-11 (needs US-5 + US-9, both already done — unblocked)
and US-12 (needs US-11 + US-10 — US-10 now done, but still blocked on US-11).
