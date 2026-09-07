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
