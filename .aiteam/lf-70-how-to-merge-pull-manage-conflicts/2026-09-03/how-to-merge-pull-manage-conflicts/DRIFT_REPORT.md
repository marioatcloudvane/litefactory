# Drift Report — how-to-merge-pull-manage-conflicts
# Branch: lf/70-how-to-merge-pull-manage-conflicts | Date: 2026-09-07

This section covers the run scoped to **US-2, US-3, and US-4** only, appended
after the earlier US-8, US-7, and US-1 sections (see this file's prior dated
sections for those, each still valid and unchanged).

## Delivered as planned

- **US-2 — Auto-merge the base branch when the merge is clean**
  - T-2.1 `MergePreview` type + `(*Worktree).MergePreview` — delivered, plus a
    real bug fix (`parseMergePreview`'s section-boundary parsing) found and
    corrected before code review, and a second bug (missing `-z` on the
    `merge-tree` invocation, causing non-ASCII conflict paths to come back
    C-quoted) found by code review as BLOCKING (CR-B-001) and resolved with a
    verified fix plus a regression test.
  - T-2.2 `MergeResult` type + `(*Worktree).MergeBase` — delivered, following
    `CommitResult.Committed`'s precedent for `Merged` exactly.
  - T-2.3 `ErrMergeConflict` sentinel — present, verified.
  - T-2.4 `Worktree` doc comment naming `MergeBase` as the third mutator —
    present, verified.
  - T-2.5 `syncBase` wired as the fourth/last step of `prepareClone` — present
    and verified against the architecture notes' exact ordering
    (`fetch → freezeSlug → ensureWorktree → syncBase`), inside the single
    existing `CloneGate` acquisition, with `shaBefore` read after `syncBase`
    completes.
  - T-2.6 `gate_test.go`'s `gated` map gained `"syncBase": {"prepareClone"}`;
    `sections` map confirmed still at exactly three entries.
  - T-2.7 unit tests for clean merge, unclean-preview-leaves-branch-untouched,
    and the `Behind==0` short-circuit — delivered via `internal/git/merge_test.go`,
    `internal/git/merge_internal_test.go`, and `internal/turn/sync_test.go`.
  - AC-2.1, AC-2.2 — delivered and tested, including a dedicated
    `SyncCommitSHA`-non-empty assertion and a non-ASCII conflict-path
    regression test added during the CR-B-001 fix.
  - AC-2.3 ("never pushes to or commits on `Repo.BaseBranch`, in every
    outcome") — delivered; the unit-test pass closed a real gap here, adding
    `TestSyncBaseNeverTouchesOriginAcrossEveryOutcome` to prove this across
    all four outcomes (`up_to_date`/`skipped`/`synced`/`blocked`), not just
    the successful-merge path the implementer had originally covered.
  - S-1, S-4, S-5, S-6, S-8, S-9 (ref-name screening, identity/signing
    prelude, rollback guarantee, single-`CloneGate` scope, `TimedOut`
    classification, conflict-path parse-time cap at 500) — all verified
    directly against source by the code reviewer, not merely accepted from
    the implementer's claims.

- **US-3 — Disclose every automatic change**
  - T-3.1 `report.Sync` struct + `Run.Sync` field — delivered.
  - T-3.2 renderer covering all five outcomes including `skipped` — delivered.
  - T-3.3 golden-file tests, one per outcome — delivered (5 fixtures).
  - AC-3.1 (nil-vs-`up_to_date` byte-identical, no section rendered) —
    delivered and tested precisely.
  - AC-3.2 (every `skipped` outcome discloses why, never silent) — delivered
    via the `Reason` field (see Added Scope below) and tested with and
    without a reason present.
  - S-2 (untrusted conflict-path content) — the renderer reuses the existing
    `conflictsBlock`/`sanitize`/`fenceFor` primitives already used for file
    paths elsewhere in `internal/report`, and a test proves a path containing
    a backtick run cannot break the fence.

- **US-4 — Refuse the run and name the conflicting files**
  - T-4.1 `ReplyRefusesSync` const, `String()`, validity, `body()` case —
    delivered.
  - T-4.2 refusal sentence as a package-level constant — delivered, alongside
    the three existing sibling refusal sentences.
  - T-4.3 dispatch case — delivered, but see Architectural Deviations below:
    the story's own architecture notes described this as "a new branch in
    `replyKind()`," which was checked against source and found incorrect;
    the actual implementation correctly follows the real, already-established
    `ReplyAgentSwitchRefused` precedent instead (delivered via
    `ReplyRequest.Refusal`/`req.kind()`, never through `replyKind()`, which
    only maps `CommandKind` values). Verified independently by the code
    reviewer, tracing from source.
  - T-4.4 wiring the `blocked` outcome to the `*failure` path — delivered:
    `prepare()` returns a `*syncRefusal` immediately after `prepareClone`
    succeeds with a blocked outcome and `BlockRunOnConflict` true, before any
    run-row, segment, or run-artifact-directory creation.
  - T-4.5 tests (agent never starts, no run-artifact directory, refusal
    comment posted) — delivered, and strengthened during Phase 3 into one
    coherent end-to-end scenario (`TestExecuteRefusesASyncOnAConflictingBase`)
    plus a dedicated posted-comment-body test
    (`TestReporterPostsReplyRefusesSyncWithConflictingPaths`), closing a gap
    the code reviewer had flagged as advisory (CR-A-002).
  - AC-4.1, AC-4.2 — delivered and tested as one coherent scenario per AC-4.1,
    plus dedicated enum-exhaustiveness/`String()`/predicate/dispatch tests for
    AC-4.2.
  - S-2 (untrusted conflict-path content in the refusal body) — reuses US-3's
    `conflictsBlock` helper from the same package; no duplicate sanitizer was
    built.

## Added scope

- **`report.Sync.Reason string` field** (US-3): not in the architecture
  notes' originally-listed field set (`Outcome, Behind, Ahead, Base,
  CommitSHA, Conflicts`). Added because AC-3.2 requires every `skipped`
  outcome to disclose *why* it was skipped, and the listed field set had no
  place to carry that. The story's own task description explicitly flagged
  this as a judgment call left to the implementing engineer. Reviewed by
  `go-code-reviewer` as advisory (CR-A-001), accepted as reasonable — a plain
  string rather than a typed enum, since `internal/turn`'s `syncResult`
  doesn't yet distinguish skip causes (cap vs. config vs. label all collapse
  into one outcome value today).
- **`turn.Options.BlockRunOnConflict bool`** (US-4): not explicitly named as
  a new field anywhere in US-2's or US-4's task tables, though clearly
  implied by AC-4.1/T-4.4's dependency on the already-existing
  `BlockRunOnConflict` config key (US-8). Added because `turn.Options` had no
  field to carry the resolved config value into a turn before this point —
  only `AutoSyncBase`/`AutoSyncMaxBehind` existed. Wired from the one
  production call site (`internal/daemon/setup.go`) that constructs
  `turn.Options`.
- **Extra test coverage beyond the task tables' literal line items**: a
  non-ASCII conflict-path regression test (closing CR-B-001), an
  across-every-outcome origin-untouched test (closing an AC-2.3 gap), and
  three new test files covering `ReplyRefusesSync`'s dispatch, enum
  properties, and full end-to-end flow (closing CR-A-002). All are test-only
  additions with no production-code scope change.

## Dropped or deferred

- **Live wiring of `report.Run.Sync` from `internal/turn`'s `syncResult`
  into a real run's rendered comment**: US-3's architecture notes describe
  this as populated "from the `SyncOutcome`/.../`SyncCommitSHA` fields US-9
  persists on the run row" — US-9 is out of scope for this run and was not
  built, so `turn.Result` still has no field carrying `syncResult` forward to
  the reporting layer, and the actual end-to-end disclosure a real run would
  show is not yet connected. The struct, renderer, and golden tests
  (T-3.1–T-3.3, everything the task table itself lists) are fully delivered
  and independently correct; only the cross-package plumbing that US-9's own
  persistence work is meant to introduce is deferred, exactly as the task
  table's own dependency column implies (T-3.1 depends on US-2 T-2.2, not on
  any US-9 task). `SyncOutcome`'s string values were deliberately chosen to
  match `internal/turn`'s private enum exactly, so this wiring will be a
  one-line conversion once US-9 lands.
- US-5, US-6, US-9 through US-12 remain entirely unbuilt, as instructed —
  out of scope for this run.

## Architectural deviations

- US-4's architecture notes state the `replyKind()` mapping "is a new branch
  in that function, not a reuse of an existing one." This was checked
  directly against `internal/turn/report.go` and found to describe the wrong
  mechanism: `replyKind()` only maps `CommandKind` values, and — exactly like
  the existing `ReplyAgentSwitchRefused` — a sync refusal is delivered via
  `ReplyRequest.Refusal`/`req.kind()`, never through `replyKind()`. The
  engineer followed the real, already-established precedent instead of the
  sub-issue's literal (and incorrect) instruction; the code reviewer
  independently re-traced this from source and confirmed the actual approach
  is correct and AC-4.2 is satisfied by it. This is a correction of a stale
  architecture-notes detail, not a design change — no functional deviation
  from what US-4 needed to accomplish.

## Verdict

CLEAN — delivered matches the plan within acceptable micro-decisions (the
`Sync.Reason` field and `BlockRunOnConflict` option were both foreseeable,
narrowly-scoped additions the task tables' own dependency structure implied
were needed but didn't spell out as a named field). One real production bug
(CR-B-001, `merge-tree -z` quoting) was found and fixed inside the same
story's scope during code review, not deferred. The one genuine deferral
(US-3's live wiring) is a direct, correctly-recognized consequence of US-9
being out of scope for this run, not a scope miss — no follow-up planning
cycle is needed for it; it will resolve naturally when a future run
implements US-9 and threads `syncResult` through `turn.Result`.

---

# Drift Report — how-to-merge-pull-manage-conflicts (US-5, US-9 section)
# Branch: lf/70-how-to-merge-pull-manage-conflicts | Date: 2026-09-07

This section covers the run scoped to **US-5 and US-9**, implemented in
parallel per explicit instruction, appended after the earlier US-8, US-7,
US-1, and US-2/US-3/US-4 sections above (each still valid and unchanged).

## Delivered as planned

- **US-5 — Render conflicts in plain language, safely**
  - T-5.1 local path pattern (`conflictPathPattern`) and reject-span helper
    (`conflictPathSpan`) in `internal/report/reply.go` — delivered, following
    `agentNamePattern`/`agentSpan`/`commandSpan`'s existing reject-don't-
    salvage precedent exactly.
  - T-5.2 truncation at 20 accepted entries plus rejected-count/remainder
    reporting — delivered in `internal/report/run.go`'s existing
    `conflictsBlock` (extended, not duplicated, per the story's explicit
    instruction to build on US-3/US-4's existing helper).
  - T-5.3 plain-language wording for the conflict-list section — delivered.
  - T-5.4 injection-payload tests (markdown, HTML comment shaped like
    `lf:status`, bidi-override runes) — delivered, and strengthened during
    Phase 3 with two closed gaps: a positive test proving ordinary markdown
    is accepted-and-safely-fenced rather than falsely rejected
    (over-rejection would itself silently hide real conflicting files behind
    a wrong count), and a test proving whole-path rejection specifically —
    using a distinctive marker string that would survive a
    truncate-to-safe-prefix regression but not a genuine reject-whole
    implementation.
  - AC-5.1 — delivered and tested, including the two gap-closing tests above.
  - AC-5.2 — delivered and tested (truncation-and-rejection-independence
    test already present from Phase 1).
  - AC-5.3 — delivered: `conflictPathRejectionSentence` sits in the same
    constant grouping as `toolsRefusalSentence`/`agentRefusalSentence`/
    `syncRefusalSentence`, with a cross-referencing doc comment. Confirmed by
    reading source directly (both by code review and by the unit-test
    engineer); this criterion is a source-layout property, not a black-box
    behavior, so no test asserts on it directly — recorded here rather than
    covered by a tautological test.
  - S-2 (untrusted conflict-path content) — the denylist plus the existing
    `fenceFor` dynamic-width fence mechanism (not the denylist itself) jointly
    defeat markdown/HTML/bidi injection; the backtick-safety delegation to
    `fenceFor` was independently re-verified against `internal/report/text.go`
    by the code reviewer, not merely accepted as claimed.

- **US-9 — Persist sync outcomes behind the internal/git boundary**
  - T-9.1 migration `0007_run_sync.sql` with the four new columns — delivered.
    The plan's own stale citation (`0005_run_sync.sql` on `0004`) was
    corrected before implementation started, per this run's explicit
    instruction; `0007` was independently re-verified against
    `internal/store/migrations/` by the orchestrator, the implementer, and
    the code reviewer, three times over.
  - T-9.2 writing the four sync fields to the run row after `syncBase`
    completes — delivered, with one deviation from the sub-issue's literal
    line-number citation (see Architectural Deviations below).
  - T-9.3 tests (migration safety, round-trip of all five outcomes) —
    delivered, and strengthened during Phase 3 with a genuine
    `internal/turn`-level wiring test
    (`TestExecuteRecordsTheSyncOutcomeOnTheRunRow`) proving the real call
    site is actually invoked during a real `Execute`/`prepare()` flow with a
    real git merge and a real persisted commit SHA — closing a gap the
    store-level round-trip tests alone could not have caught (a regression
    that stopped calling `RecordRunSync` at its real call site, while
    `RecordRunSync` itself stayed correct, would not have been caught by the
    Phase 1 tests alone).
  - AC-9.1, AC-9.2, AC-9.3 — all delivered and tested.

## Added scope

- None beyond what the task tables and architecture notes already implied.
  No new field, type, or column was added beyond the four named columns and
  the `SyncOutcome`/`RunSyncOutcome` types the story's own architecture notes
  call for.

## Dropped or deferred

- **Conflict-path list persistence** — explicitly out of scope per
  `US-9.md`'s own architecture notes (S-9's parse-time cap is a distinct,
  already-handled concern; the plan defers persisting the path list itself).
  Not built, correctly, per instruction.
- **`SyncFailed` is defined and tested at the store layer but currently
  unreachable from production `internal/turn` code** — a git-level merge
  failure aborts the turn before `allocateRun` creates a row, so this outcome
  value is never actually persisted today, only logged. This is a
  pre-existing structural property of `prepareClone`'s ordering (confirmed by
  the code reviewer, not introduced by this run) and does not violate AC-9.2
  as literally written (which binds only to a run that reaches completion).
  Flagged here as a genuine gap for a future story to consider, not fixed in
  this run — closing it would require persisting a run outcome before a
  `runs` row exists, which is a larger structural change than US-9's scope.
- US-6, US-10, US-11, US-12 remain entirely unbuilt, as instructed — out of
  scope for this run. US-6 in particular was deliberately excluded from this
  batch (its concurrency test is sized L and would have held both finished
  stories at Gate A).

## Architectural deviations

- **US-9's write-back call-site placement.** `US-9.md`'s architecture notes
  and this run's own instructions cited `internal/turn/prepare.go:429`
  (the `syncBase` call site) as where the write-back belongs, "immediately
  after `syncBase` completes." The implementing engineer found this citation
  stale: no `runs` row exists yet at that point in `prepareClone` — the row
  is created later, by `allocateRun`, inside `prepare()` — so there is
  nothing yet to attach a sync outcome to at the literal cited line. The
  write-back was placed immediately after `allocateRun` succeeds instead,
  which is the earliest point a row exists, and still strictly before the
  existing `SetSessionState` call already documented in the code as
  `prepare()`'s last store write. The code reviewer independently re-read
  `prepare()`'s current source (confirming `prepareClone`/`syncBase` at line
  117, `allocateRun` at line 216 — neither matching the sub-issue's cited
  line numbers) and confirmed the deviation is correct: the underlying
  requirement — write before any comment-rendering step reads the outcome
  back for US-3/US-4 — is satisfied, and no live code path today reads a
  persisted sync outcome before this write happens (verified by grep-free
  code review of `internal/turn/report.go`). This is a stale line-number
  citation in a sub-issue written before intervening code changes, not a
  functional or design deviation.

## Verdict

CLEAN — both stories delivered against their acceptance criteria with no
scope drift beyond a stale line-number citation (corrected and independently
verified) and one pre-existing, correctly-diagnosed gap (`SyncFailed`'s
current unreachability) that neither story's AC set requires closing. No
BLOCKING code-review findings; one advisory (CR-A-003) recorded and left
open as a note for a future story rather than fixed out-of-scope. No follow-
up planning cycle is needed for either story; the `SyncFailed`-unreachable
observation is worth a future story's attention if persisting failed-sync
outcomes ever becomes a real product need, but is not urgent — it already
does not silently misreport anything AC-9.2 promises.

---

# Drift Report — how-to-merge-pull-manage-conflicts (US-6, US-10)
# Branch: lf/70-how-to-merge-pull-manage-conflicts | Date: 2026-09-07

This section covers **US-6 and US-10**, across two runs: an Implement run
that was cut off by an API rate limit mid-flight (after implementation and
code review, before unit testing), and a resumption of that same run that
completed Phase 3 onward without redoing the finished work. Both stories
were built in parallel in the interrupted run and land on disjoint file
sets.

## Delivered as planned

- **US-6 — Verify per-issue sync is independent under concurrency**
  - T-6.1 `internal/turn/syncconcurrency_test.go` —
    `TestConcurrentTurnsSyncIndependentlyOnOneClone`: four issues share one
    registered clone, each cut to a distinct branch, then all four issues'
    second turns run concurrently. Asserts persisted outcome/behind/commit-
    SHA per turn, distinct merge-commit SHAs per issue, cross-branch
    isolation verified against the bare origin's `git ls-tree` (not
    worktree state), and — read directly off the real gate's counters —
    exactly `2 * len(issues)` acquisitions during the concurrent phase
    (`prepareClone` + `commitAndPush`, no third acquisition for the merge),
    zero overlaps, zero deadlines. Delivered and independently re-run 5x
    with `-race` by the code reviewer to rule out flakiness, then re-run
    again by the unit-test-engineer and by the orchestrator as part of the
    whole-repo `-race` gate — no flake, no failure, in any run.
  - T-6.2 — `internal/daemon/gate.go`'s existing `cloneGate` doc comment
    gained a "Keyed on the clone, not the branch (US-6)" section explaining
    the keying choice and pointing at the concurrency test as the proof.
  - AC-6.1, AC-6.2 — both delivered and tested exactly as specified;
    confirmed by the unit-test-engineer (this run) to already be fully
    covered before any new test was considered, avoiding duplicate
    coverage.

- **US-10 — Guarantee the branch is untouched when sync fails**
  - T-10.1 — `recoverFromFailedMerge` in `internal/git/merge.go` captures
    the branch's HEAD SHA before `MergeBase` attempts a merge and
    re-verifies it after `merge --abort` on any non-`synced` outcome. Never
    uses `git reset` (S-5 respected — `Reset` does not exist on
    `*Worktree`).
  - T-10.2 — `internal/git/errors.go` gained the `ErrMergeTimedOut`
    sentinel, matching the existing `ErrMergeConflict`/
    `ErrUnsupportedGitVersion` doc-comment shape.
    `recoverFromFailedMerge` copies the original `*CommandError` (preserving
    Binary/Dir/Args/ExitCode/Stderr) and swaps in `ErrMergeTimedOut` rather
    than `ErrMergeConflict` for the timeout case, without touching
    `classify()`'s own contract for unrelated errors.
  - T-10.3 — the interrupted run's code review flagged this as the one
    outstanding gap (no test yet proving the SHA-identity/`TimedOut`
    guarantee), and left `recoverFromFailedMerge` deliberately independently
    callable against a worktree holding a live `MERGE_HEAD` so the proof
    would not need to wait out the real 120s `mergeTimeout`. In fact, by the
    time this run's unit-test-engineer checked, that test already existed —
    `internal/git/merge_timeout_internal_test.go`'s
    `TestRecoverFromFailedMergeTimeout` (asserts `errors.Is(err,
    ErrMergeTimedOut)`, `!errors.Is(err, ErrMergeConflict)`, HEAD SHA
    identical before/after, worktree clean after abort) and
    `TestRecoverFromFailedMergeTimeoutAbortFailurePreservesOriginalError`
    (the abort-itself-fails edge case, CR-A-004's documented concern) — both
    written by the same interrupted run, in a file created after the code
    review's file list was assembled, which is why the review's snapshot
    still showed the gap as open. This run verified both tests exist, pass,
    and genuinely satisfy AC-10.1/AC-10.2 rather than taking the file's
    presence at face value.
  - AC-10.1 — proven across all three non-`synced` outcomes named in the
    criterion: `blocked` (`internal/turn/sync_test.go`'s existing
    `TestSyncBaseBlockedOnConflictLeavesBranchUntouched` and the
    `blocked` case of `TestSyncBaseNeverTouchesOriginAcrossEveryOutcome`),
    `failed` (`internal/git/merge_test.go`'s pre-existing
    `TestWorktreeMergeBaseConflictRollsBack`, explicit `RevParse`
    before/after comparison), and timeout (the two tests above). All via
    explicit SHA comparison, never inferred from the absence of an error.
  - AC-10.2 — proven by `TestRecoverFromFailedMergeTimeout` asserting the
    timeout path returns `ErrMergeTimedOut` and specifically does not
    collapse into `ErrMergeConflict`.

## Added scope

- None. No new file, type, or field was added beyond what T-6.1/T-6.2 and
  T-10.1/T-10.2/T-10.3 already called for.

## Dropped or deferred

- Nothing dropped. Both stories' full task tables and acceptance-criteria
  sets are delivered and tested.
- US-11 and US-12 remain unbuilt, as instructed — out of scope for this run.
  US-11 was already unblocked before this run (US-5 + US-9 done) but not
  requested; US-12 needed US-10, which this run completes, but also needs
  US-11, so it still cannot start.

## Architectural deviations

- None. `classify()` in `internal/git/errors.go` was deliberately left
  untouched rather than folding the timeout substitution into it, since
  `classify()` intentionally refuses to reclassify an already-timed-out
  error for unrelated conditions for other callers — the timeout swap was
  written as a small local copy-and-swap next to `recoverFromFailedMerge`
  instead. This preserves an existing contract rather than deviating from
  the plan; the plan's architecture notes did not specify which function
  should own the substitution.

## Verdict

CLEAN — both stories delivered against their full acceptance-criteria sets,
with the resumed run's only real finding being that the one gap the
interrupted run's code review had flagged (T-10.3) was already closed by
that same interrupted run, just not yet visible to the reviewer's file
snapshot. No new code was needed or written in the resumption; the
unit-test-engineer's job was verification of existing coverage, not
production. No BLOCKING code-review findings surfaced at any point; two
pre-existing advisories (CR-A-004, CR-A-005) remain open as non-blocking
notes. `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `go test -race
-count=1 ./...` green across all 22 packages, re-verified directly by the
orchestrator as the final gate. No follow-up planning cycle is needed for
either story.
