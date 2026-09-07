# Code Review
# Feature: Pre-run branch drift detection and safe auto-sync (US-2, US-3, US-4)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07 (re-review of CR-B-001)
# Status: ADVISORY_ONLY

---

## BLOCKING findings

### CR-B-001: MergePreview does not disable git's path quoting, so conflicting paths can be reported mangled instead of exact — RESOLVED

**File:** `internal/git/merge.go` (`Worktree.MergePreview`, `parseMergePreview`)
**Story:** US-2 / T-2.1
**Category:** Cross-story / Correctness

**Original finding:** `MergePreview` invoked `git merge-tree --write-tree --name-only --end-of-options` without `-z`, so a conflicting path containing a non-ASCII byte (or tab/backslash/double-quote) came back C-quoted and escaped by `core.quotePath` rather than as the literal path, and `parseMergePreview` stored the escaped string verbatim.

**Fix verified:**

1. **`-z` is genuinely present on the invocation.** `internal/git/merge.go:168`:
   ```go
   "merge-tree", "--write-tree", "--name-only", "-z", "--end-of-options", w.branch, base
   ```

2. **The parser genuinely handles NUL-separated fields, not relabeled newline logic.** Diffed the fix against the prior version: the old `parseMergePreview` used `strings.SplitN(out, "\n\n", 3)` for the tree-id/path-list/messages split and `strings.Split(parts[1], "\n")` with `continue` on blank lines for the path list. The new version does `fields := strings.Split(out, "\x00")` and iterates `fields[1:]`, `break`-ing at the first empty field. This is a structurally different parser matched to the `-z` wire format, not a cosmetic rename of the old one.

3. **The message-section-boundary claim is correctly reflected in the code.** Per `git help merge-tree`'s `-z` semantics, the free-form messages section is preceded by an extra NUL (an empty NUL-delimited field), not a blank line. The loop's `if path == "" { break }` is exactly this check, and `TestParseMergePreviewCapsConflictPaths`'s fixture (`...\x00\x00informational: ...`) exercises precisely this boundary and confirms the parser stops there.

4. **The trailing-NUL-survives-trim edge case is handled correctly for the clean case.** `runner.run` (`exec.go:231`) and `runAllowingExit` (`exec.go:266`) both trim only `"\n"` (`strings.TrimRight(..., "\n")`), so a lone trailing NUL from a clean `-z` result (e.g. `"treeid456\x00"`) survives into the parser's input. `strings.Split` on that string yields `["treeid456", ""]`, but the clean branch reads only `fields[0]` and never inspects the trailing empty field, so `TreeSHA` is not corrupted. `TestParseMergePreviewCleanIgnoresExtraOutput` exercises this exact input and passes. No off-by-one or leftover empty string reaches `TreeSHA`.

5. **The new non-ASCII regression test genuinely exercises the fixed path.** `TestWorktreeMergePreviewConflictNonASCIIPath` conflicts a real `café.md` file across base and branch through a real `git merge-tree` invocation and asserts `slices.Contains(preview.Conflicts, "café.md")`. Pre-fix, the missing `-z` would have let `core.quotePath`'s default C-quoting mangle the non-ASCII byte into an escaped string, which the old parser stored verbatim — the assertion would have failed. Traced this against the actual pre-fix diff (`strings.SplitN(out, "\n\n", ...)` had no unquoting step at all), not just taken on the report's word. The test passes against the fixed code.

6. **No regression to previously-verified S-1/S-4/S-5/S-6/S-8/S-9 concerns.** `checkName` screening (S-1), the identity prelude (S-4), `recoverFromFailedMerge`'s rollback and `TimedOut` handling (S-5/S-8), and gate acquisition (S-6, in `internal/turn`, untouched by this diff) are all outside the changed code path. `MaxMergeConflictPaths` enforcement (S-9) is unchanged in logic; its regression test was correctly migrated to a NUL-based fixture and still asserts the cap and the first/last surviving entries.

7. **Build and test verification, re-run directly:**
   - `go build ./...` — clean
   - `go vet ./...` — clean
   - `gofmt -l .` — no files listed
   - `go test -count=1 ./internal/git/...` — ok (28.0s)
   - `go test -count=1 ./internal/turn/...` — ok (48.7s)

**Disposition:** Resolved. No new BLOCKING or ADVISORY issue introduced by the fix.

---

## ADVISORY findings

### CR-A-001: `Sync.Reason` is an implementer-added field not in US-3's original interface list
**File:** `internal/report/run.go:786-792`
**Story:** US-3 / T-3.1
**Category:** Standards
**Finding:** US-3.md's Architecture Notes list `report.Sync{Outcome, Behind, Ahead, Base, CommitSHA, Conflicts []string}` — no `Reason` field. The engineer added `Reason string` to carry the skip/failure explanation, which is a reasonable and arguably necessary addition (AC-3.2 requires a skip to name why, and there is otherwise nowhere for that text to live), and it is fully test-covered (`TestSyncDisclosure`'s "skipped names why" and "reasonless skip" subtests). It is not wrong, but it is an unreviewed interface deviation from the sub-issue.
**Suggestion:** No code change needed. Worth a one-line note in the story's closing record (or plan_brief amendment) that `Sync.Reason` was added beyond the original interface, since US-9 (persistence, out of scope this run) will need a column or equivalent to carry it forward, and the architect should be aware the shape grew.

### CR-A-002: no direct unit coverage of `ReplyRefusesSync` end-to-end
**File:** `internal/report/reply.go` (body() case), `internal/turn/report.go` (`reportSyncRefusal`)
**Story:** US-4 / T-4.1, T-4.4
**Category:** Standards
**Finding:** No test anywhere in the tree references `report.ReplyRefusesSync` (searched the whole repository). `internal/report/reply_test.go` and `report_test.go` cover `ReplyLegacyToolLabel`, `ReplyUnknownCommand`, and `ReplyEmptySpike`, but nothing exercises `RenderReply` with `Kind: ReplyRefusesSync`, and nothing in `internal/turn` exercises `reportSyncRefusal`/`syncRefused` dispatch. This is a test-suite gap, not a production-code defect — production code was verified directly and is correct (see below) — and per this review's scope test files are the test engineers' responsibility, not something to block on here.
**Suggestion:** Flag for the test-writing phase: T-4.5's stated test list ("agent never starts on blocked, no run artifact directory created, refusal comment posted") does not appear to have landed yet for the reply-rendering half specifically.

---

## Verified correct (spot-checks requested by the orchestrator, first review pass)

- **US-4's `replyKind()` omission is correct, not a gap.** Read `internal/turn/report.go` directly: `replyKind(kind CommandKind)` maps only `CommandKind` values and is called solely as the fallback branch of `ReplyRequest.kind()` when `req.refusing()` is false. `ReplyRefusesSync` is delivered through `ReplyRequest.Refusal` (set in `reportSyncRefusal`), and `req.kind()` returns `req.Refusal` directly before ever reaching `replyKind()` — exactly the same path `ReplyAgentSwitchRefused` already uses, which also has no `replyKind()` case. The sub-issue's own architecture note ("this is a new branch in that function, not a reuse of an existing one") describes a model that does not match the established precedent already in the codebase; the engineer's choice to follow the actual precedent (`ReplyAgentSwitchRefused`) rather than the sub-issue's literal instruction is correct and was verified by reading the real dispatch path, not assumed.
- **AC-4.1 (blocked outcome prevents run creation).** `internal/turn/prepare.go:128-130`: the `syncOutcomeBlocked && opts.BlockRunOnConflict` check returns `&syncRefusal{...}` immediately after `prepareClone` returns and strictly before `session.WorktreePath` persistence, `shaBefore` read, segment creation, `allocateRun`, attachment ingestion, or prompt assembly. `Execute` (`internal/turn/execute.go:552-555`) checks `errors.As` for `*syncRefusal` before the generic `*failure` branch, and `syncRefused` (`execute.go:648-655`) sets `DispositionSyncRefused` with no session-state transition and no run row. No `claude` exec, no run-artifact directory, no commit. Confirmed correct end to end.
- **AC-2.1/AC-2.3 (rollback guarantee, BaseBranch never touched).** `internal/git/merge.go`'s `MergeBase` refuses a dirty worktree before touching anything, confirms `base` resolves before merging, captures `HEAD` before and after, and `recoverFromFailedMerge` runs `merge --abort` and re-verifies the SHA matches — a mismatch is its own reported failure, never silently accepted, and `TimedOut` is never followed by an abort attempt (S-8). `Worktree.Push` (`internal/git/push.go`) always pushes `refs/heads/<w.branch>` — the worktree's own branch — never `repo.BaseBranch`; `MergeBase` never pushes at all. Confirmed correct.
- **S-1 (ref screening).** `Divergence`, `MergePreview`, and `MergeBase` each call `checkName("base", base)` and `checkName("branch", w.branch)` before building any command, and the `base...branch` range is assembled inside `Divergence` from the two already-screened strings — never accepted pre-joined. Confirmed correct.
- **S-4 (identity prelude).** `MergeBase`'s args carry `-c user.name=... -c user.email=... -c commit.gpgsign=false` ahead of `merge --no-ff --no-edit`, matching `Commit`'s precedent exactly, and reuses the existing `checkConfigValue` helper rather than duplicating validation. Confirmed correct.
- **S-6 (single gate acquisition).** `internal/turn/gate_test.go`'s `gated` map gains exactly `"syncBase": {"prepareClone"}` with a recorded TEST-10 reason; the `sections` map is unchanged at exactly three entries (`prepareClone`, `commitAndPush`, `commitStoryIndex`). `syncBase` is called from inside `prepareClone`, after `ensureWorktree`, under the one `CloneGate.Acquire` already taken at the top of that function — no second acquisition anywhere in `internal/turn` or `internal/git`. Confirmed correct.
- **S-8 (TimedOut classification).** Confirmed in `recoverFromFailedMerge`: a `*CommandError` with `TimedOut` true is returned as-is, without attempting `merge --abort`, and is never folded into `ErrMergeConflict`.
- **S-9 (conflict cap, and "never persisted" for this phase).** `MaxMergeConflictPaths = 500` is enforced inside `parseMergePreview` at parse time (`internal/git/merge.go:212-214`), not only at render time, and is regression-tested (`TestParseMergePreviewCapsConflictPaths` in `merge_internal_test.go`). Grepped `internal/store` for `SyncConflicts`/`Conflicts`/`SyncOutcome`/`SyncCommitSHA`: no matches — nothing from this phase's work writes the conflict list (or any sync outcome) to the database, consistent with US-9 persistence being explicitly out of scope for this phase.
- **Cross-story `conflictsBlock` reuse (US-3/US-4).** `conflictsBlock` is defined once in `internal/report/run.go` and called from both `syncSection` (US-3, `run.go`) and `Reply.body()`'s `ReplyRefusesSync` case (US-4, `reply.go`) — no duplicate sanitizer, no drifted fencing logic. `RenderReply` (`reporter.go:131-133`) rejects `ReplyRefusesSync` with an empty `ConflictPaths` before rendering, matching the store-side invariant that a blocked outcome always carries at least one conflicting path.
- **`report.ReplyKind` validity for `ReplyRefusesSync` (AC-4.2).** `String()` has an explicit case (`"sync_refused"`), `valid()` has an explicit case (`true`), and `RefusesSync()` is defined and is one of the four predicates OR'd together in both `ReplyRequest.kind()` and `ReplyRequest.validate()`. No enum-exhaustiveness test file exists under that name anywhere in the tree (searched); the plan brief's phrasing appears to describe this general property rather than referencing a literal pre-existing test. The property itself holds.
- **`internal/git/surface_test.go` / `Reset` absence.** `TestCloneMethodSetIsTheAllowlist` and `TestCloneCannotMutateTheRegisteredClone` are unchanged and still `git.Clone`-scoped only; there is no `TestWorktreeMethodSetIsTheAllowlist` yet, correctly deferred to US-12/T-12.1 per the plan. `worktree_test.go`'s zero-value test gained a `MergeBase` case (not literally a "forbidden-mutator allowlist" test as the task report called it, but functionally the right addition — extends `TestZeroValuesRefuseToRunGit`). `Reset` was not added to `Worktree` anywhere in the diff. Confirmed correct.
- **`Worktree`'s doc comment (T-2.4).** Updated to list `Commit`, `Push`, and `MergeBase` as the three mutators. Confirmed correct.
- **Build/vet/format.** `go build ./...`, `go vet ./...`, and `gofmt -l .` are all clean. `go test -count=1 ./internal/git/... ./internal/report/... ./internal/turn/... ./internal/daemon/...` all pass.

---

## Cross-story summary

US-2, US-3, and US-4 integrate cleanly at every boundary checked: `syncResult`/`syncOutcome` (US-2) flow into `report.Sync`/`SyncOutcome` (US-3) and into `Result.SyncConflicts`/`ReplyRefusesSync` (US-4) through simple, one-directional field copies with no duplicated logic and no drifted conventions — `conflictsBlock` is shared correctly, the refusal-sentence constants follow the established pattern of their three siblings, and the `replyKind()`/`ReplyRequest.kind()` split was verified against the actual dispatch code rather than trusted from the sub-issue's own (slightly incorrect) description of it. CR-B-001, the one defect found in the first pass, lived entirely inside `internal/git` at the point conflicting paths are first produced; it is now fixed with a genuinely rewritten NUL-aware parser, an empirically-grounded regression test with a non-ASCII path, and no observed regression to any other verified property in the same file. Because both US-3 and US-4 render whatever conflict list they are handed without re-validating its content (by design — that is explicitly US-5's job), fixing the source in `internal/git/merge.go` is sufficient to fix both downstream renderings; no change was needed in `internal/report` for this finding.

---

## Verdict

ADVISORY_ONLY — 2 advisory suggestions noted (CR-A-001, CR-A-002). CR-B-001 is resolved. Proceeding to environment setup.

---
---

# Code Review — US-5 / US-9
# Feature: Pre-run branch drift detection and safe auto-sync (US-5, US-9)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: ADVISORY_ONLY

---

## Scope

This section reviews only the files tied to US-5 ("Render conflicts in plain
language, safely") and US-9 ("Persist sync outcomes behind the internal/git
boundary"), per the orchestrator's request. US-1 through US-4, US-7, and US-8
are out of scope and were not re-reviewed (see the section above, dated the
same day, for that review). All files were read as uncommitted working-tree
changes on top of commit `8c6144e`.

Files reviewed:
- `internal/report/reply.go` (diff)
- `internal/report/run.go` (diff)
- `internal/report/replykind_test.go` (diff, read for behavior confirmation only — out of grading scope per policy, not critiqued)
- `internal/store/migrations/0007_run_sync.sql` (new)
- `internal/store/runs.go` (diff)
- `internal/store/migrate_test.go`, `internal/store/store_test.go` (diff, read for behavior confirmation only)
- `internal/turn/prepare.go` (diff, plus the surrounding pre-existing `prepareClone`/`syncBase`/`allocateRun`/`SetSessionState` code it now sits between)

`go build ./...`, `go vet ./...`, and `gofmt -l .` are all clean. `go test -count=1 ./internal/report/... ./internal/store/... ./internal/turn/...` all pass.

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-003: `SyncFailed` is a persistable value with no code path that ever writes it, leaving a merge failure with no queryable audit trail
**File:** `internal/store/runs.go:75-82` (`SyncFailed` constant); `internal/turn/prepare.go:117-120, 444-447` (`prepareClone`/`syncBase` error return)
**Story:** US-9 / T-9.1, T-9.2
**Category:** Standards / Cross-story
**Finding:** Verified the engineer's own flagged claim directly. `syncBase` returns a bare `error` (not a `syncResult` carrying a "failed" outcome) whenever `Divergence`, `MergePreview`, or `MergeBase` themselves fail — e.g., `MergeBase` failing partway through a clean-preview merge. That error propagates through `prepareClone` and `prepare()` *before* `allocateRun` is ever called (`prepare()` returns at line 118-120, well before the `allocateRun` call at line 216), so no `runs` row exists for `RecordRunSync` to ever write `sync_outcome=failed` against. The claim holds: `SyncFailed` is unreachable given the current `internal/turn` wiring, and the store-layer round-trip test (`TestRecordRunSyncRoundTripsEveryOutcome`) only proves the column supports it, not that any code path produces it. The practical consequence: a git-level merge failure today is visible only in `daemon.log`, not on any run row — someone querying `runs` for "why did this branch look this way" gets nothing for the failure case, only for the four outcomes that reach `allocateRun`.
**Suggestion:** No code change required for AC-9.1/9.2/9.3 as literally written — they only bind runs that reach completion, and this case never allocates a run row under the current architecture (a pre-existing US-2 design point, not something this diff introduced or was asked to fix). Worth a one-line note in the story's closing record or a drift-report entry so a future reader doesn't mistake `SyncFailed`'s presence in the five-outcome vocabulary for evidence that failures are actually being persisted anywhere yet.

---

## Verified correct

### US-5 (`internal/report/reply.go`, `internal/report/run.go`)

- **AC-5.1, the "safe because of fenceFor" claim, re-verified rather than trusted.** `fenceFor` (`internal/report/text.go:76-89`) computes the fence width as `max(3, longest backtick run in content + 1)`, computed fresh over the already-joined, already-classified path list every time `conflictsBlock` renders it. A conflicting path built entirely of backticks cannot break out of its own fence because the fence is always one backtick longer than the longest run present — this is the same mechanism `filesSection` already relies on for `run.Files` (an existing, previously-reviewed precedent), not a new or narrower claim. Confirmed by reading `fenceFor` directly, not by trusting the doc comment's assertion.
  Separately, ordinary markdown metacharacters (`[`, `]`, `(`, `)`, `#`, `*`, `` ` `` runs shorter than the fence, `|`) are rendered inert because they sit inside a fenced code block, which CommonMark (and GitHub's renderer) treats as literal text with no further markdown parsing — this is *why* `conflictPathPattern` does not need to deny those characters itself, and the denylist is correctly scoped to the two threat classes fencing cannot neutralize: (1) `<`/`>`, because a downstream raw-text scanner (`turn.HasMarker`, which the doc comment correctly notes matches "any line of a body", i.e., the raw comment text, not the rendered markdown) would still see a literal `<!-- lf:status ... -->` sequence regardless of code-fence wrapping; and (2) the Unicode bidi-control code points, because a bidi-reordering "Trojan Source" attack is a rendering-engine behavior applied to the literal bytes, not defeated by code-fencing. Both exclusions are correctly reasoned and the pattern's character class (`\x00-\x1f`, `\x7f`, `<`, `>`, `\x{061c}`, `\x{200e}-\x{200f}`, `\x{202a}-\x{202e}`, `\x{2066}-\x{2069}`) matches the standard Unicode bidi-control set plus the two HTML-comment delimiters plus all ASCII control bytes (control-byte exclusion also independently blocks the newline-based line-injection threat the doc comment separately names). The backtick omission from the denylist is therefore correct, not merely plausible.
- **Whole-path, reject-don't-salvage is genuinely enforced.** `conflictPathSpan` (`reply.go:415-420`) is a single `if !pattern.MatchString(path) { return "", false }; return path, true` — there is no partial-match branch, no substring extraction, and no fallback that renders a prefix. The regex itself is anchored `^[^denylist]+$` over the whole string (Go's RE2 `^`/`$` are text, not line, anchors here since no `(?m)` flag is set), so a single disallowed rune anywhere in the path fails the whole match. Confirmed this is a stricter discipline than `commandSpan`'s stop-at-first-bad-rune behavior, exactly as the doc comments on both functions claim, and exactly as AC-5.1 requires ("never printed as a partial/truncated string").
- **`classifyConflictPaths` does not short-circuit.** The loop (`reply.go:429-439`) uses `continue`/increment-and-fall-through over the full input slice with no `break` or early `return`; every path is classified independently as the doc comment states. No `errors.Join`-style aggregation is needed here since there is nothing to aggregate (a bool-per-path result, not a fallible operation per path), and none was invented where a simple count sufficed.
- **AC-5.2 truncation/rejection interaction is correct and non-double-counting.** Traced `conflictsBlock` (`run.go:877-914`): `classifyConflictPaths` runs first and produces `(accepted, rejected)`; truncation is applied only to `accepted`, producing a separate `overflow` count; the two counts (`overflow`, `rejected`) are rendered independently and additively, never combined into one number and never derived from each other. Verified against `TestReplyRefusesSyncTruncatesAcceptedPathsAtTwentyWithARemainderCount` (23 accepted paths → exactly 20 shown, "and 3 more") and `TestReplyRefusesSyncRejectionAndTruncationAreIndependent` (21 accepted + 1 rejected → "and 1 more" *and* the rejected-count sentence, both present, with the rejected path never appearing in the output). Both pass.
- **AC-5.3.** `conflictPathRejectionSentence` sits immediately after `syncRefusalSentence` in `reply.go`, following the same single-`const`-statement, physically-adjacent grouping pattern the three prior refusal sentences (`toolsRefusalSentence`, `agentRefusalSentence`, `syncRefusalSentence`) already use — there is no single `const ( ... )` block uniting all four (none of the four siblings use one either), so this is consistent with, not a deviation from, the established convention.
- **Boundary discipline confirmed.** `reply.go` and `run.go` import no `internal/store` package; US-5 renders exclusively from the `[]string` it is handed (`Reply.ConflictPaths` / `Run.Sync.Conflicts`), never from a stored row, matching the plan brief's stated boundary.

### US-9 (`internal/store/runs.go`, `internal/store/migrations/0007_run_sync.sql`, `internal/turn/prepare.go`)

- **The deviation from the architecture notes' cited line numbers is correct, verified by reading the actual current code rather than trusted from the report.** `prepare()` calls `prepareClone` (which runs `syncBase` as its last internal step) at line 117, and does not call `allocateRun` until line 216 — genuinely no `runs` row exists at the `syncBase` call site, confirming the engineer's stated reason for moving the write. `RecordRunSync` is called immediately after `allocateRun` succeeds (line 227), strictly before `metaPath` derivation, `commentHistory`, attachment ingestion, prompt assembly, and — critically — strictly before `SetSessionState` (the code's own documented "prepare's last store write, safe on every earlier error" comment at line 312 is unchanged and still true, since the new call sits above it, not below).
- **No comment-rendering path reads the persisted sync outcome yet, so the write-before-render ordering concern the architecture notes raised does not yet have a code path to violate.** Grepped `internal/turn/report.go` for any `report.Sync`/`SyncOutcome` reference: none. US-3's disclosure rendering (`syncSection` in `internal/report/run.go`) is driven entirely by the in-memory `preparation.sync` field carried through `Execute`, not by a DB read-back; wiring `report.Sync` from the persisted `runs` row is explicitly out of scope for both US-3 (already shipped) and US-9 (this story), per both stories' own "NOT in scope" notes. This means the ordering guarantee the architecture notes worried about currently has no consumer to break — a fact worth confirming rather than assuming, which this review did by grepping the actual call sites.
- **`SyncFailed`'s "not currently reachable" claim is correct** — see CR-A-003 above for the detailed trace; recorded there as an advisory rather than a blocking gap, because AC-9.2's promise binds only to a run that reaches `allocateRun`, and a `syncBase`-level failure never does.
- **AC-9.1.** `0007_run_sync.sql` is confirmed the correct next migration number (`0006_schedule_block_replies.sql` is the prior head; migrations are discovered by directory listing and applied in filename order, so no renumbering risk). `TestMigration0007AddsRunSyncColumnsWithSafeDefaults` seeds a pre-migration row, migrates, and confirms the four new columns exist with the documented nullability and that the pre-existing row reads back its documented defaults (`""`, `0`, `0`, `""`). No existing migration-ordering test was modified. `go test ./internal/store/...` passes.
- **AC-9.2.** `sync_commit_sha` is enforced non-null-only-with-`synced` by `RecordRunSync`'s own validation (`runs.go:375-378`, refuses a non-empty `CommitSHA` paired with any outcome other than `SyncSynced`), backed by `TestRecordRunSyncRefusesACommitShaOutsideSynced`. Confirmed the inverse is *not* claimed or required: `syncOutcomeSynced` with `result.Merged == false` (a no-op merge — pre-existing US-2 behavior, unchanged by this diff) correctly leaves `SyncCommitSHA` empty even though the outcome is `synced`, and both the migration's own doc comment and `RunSyncOutcome.CommitSHA`'s doc comment state this precisely ("carries a real value only when sync_outcome = 'synced' *and* the merge actually moved the branch") — a reading of AC-9.2's "only when synced" as a necessary-not-sufficient condition, which is the correct reading given `git.MergeResult.Merged`'s existing semantics.
- **AC-9.3.** `TestRecordRunSyncRoundTripsEveryOutcome` writes and reads back all five `SyncOutcome` values (including the currently-unreachable-from-`internal/turn` `SyncFailed`, exercised directly at the store layer as the AC requires) and passes.
- **`errors.Join`/fail-fast-in-a-loop claim verified true.** `RecordRunSync` is a single read (`SELECT`) followed by a single `UPDATE ... RETURNING`, both inside one `withTx` call — there is no loop over multiple rows or multiple fallible sub-operations to aggregate, so the engineer's claim that this pattern doesn't apply here is correct.
- **Cross-story / no conflict-path column.** Grepped `internal/store` for `conflict`/`Conflicts`: the only hits are unrelated `ON CONFLICT` SQL upsert clauses in other files and the migration's own comment explaining *why* no such column exists. Confirmed no conflict-path list was persisted anywhere in this diff, matching both `plan_brief.md`'s shared decision and US-9.md's explicit "NOT in scope" note.
- **Resource/transaction hygiene.** `RecordRunSync` follows the existing `withTx`/`BEGIN IMMEDIATE` pattern used by `FinishRun`/`CompleteRunReport` exactly; `ErrNotFound` propagates correctly through the wrapped `%w` error to satisfy `errors.Is` (confirmed via `TestRecordRunSyncOnAMissingRunReportsNotFound`, which passes). No goroutines, no unguarded shared state, nothing new to the concurrency surface.

---

## Cross-story summary (US-5 / US-9)

The two stories integrate cleanly and respect the boundary the plan brief draws between them. US-9 persists `sync_outcome`/`sync_behind`/`sync_ahead`/`sync_commit_sha` on the `runs` row and deliberately never touches a conflict-path column; US-5 renders the conflict-path list safely at comment time from the live, in-memory list it is handed and never imports `internal/store`. Neither story's code reaches into the other's territory. The one thing genuinely worth tracking forward (CR-A-003, advisory) is that the five-outcome vocabulary `internal/store` now supports is presently only four-reachable from `internal/turn` — a `SyncFailed` row is never written today, because a git-level merge failure aborts the turn before a `runs` row exists to record it against. This is a pre-existing architectural property of `prepareClone`'s ordering (predating this diff), not a defect introduced by either story, and it does not violate either story's stated acceptance criteria as written.

---

## Verdict

ADVISORY_ONLY — 1 advisory finding (CR-A-003) noted for the drift report. No BLOCKING findings for US-5 or US-9. Proceeding to environment setup / next phase.
