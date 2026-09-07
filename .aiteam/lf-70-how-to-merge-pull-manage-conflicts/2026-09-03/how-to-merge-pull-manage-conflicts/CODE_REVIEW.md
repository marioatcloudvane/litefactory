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

**Update (2026-09-07, US-11 review):** This gap is now closed. `internal/report/run_test.go`/`replykind_test.go` and `internal/turn`'s test suite (`TestReporterPostsReplyRefusesSyncWithConflictingPaths` and the wider `ReplyRefusesSync` coverage added alongside US-11's own `ReplySuggestsResolution` tests) exercise `RenderReply(Kind: ReplyRefusesSync)` and `reportSyncRefusal` directly. No further action needed.

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

---
---

# Code Review — US-6 / US-10
# Feature: Concurrent sync independence proof, and the rollback-on-failure guarantee
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: ADVISORY_ONLY

---

## Scope

This section reviews only the files tied to US-6 ("Verify per-issue sync is
independent under concurrency") and US-10 ("Guarantee the branch is untouched
when sync fails"), passed inline by the orchestrator as Phase 1 task reports
against uncommitted working-tree changes on branch
`lf/70-how-to-merge-pull-manage-conflicts`. US-1 through US-5 and US-7 through
US-9 are out of scope and were not re-reviewed (see the two sections above).

Files reviewed:
- `internal/turn/syncconcurrency_test.go` (new) — US-6
- `internal/daemon/gate.go` (diff, doc comment only) — US-6
- `internal/git/errors.go` (diff, `ErrMergeTimedOut` sentinel) — US-10
- `internal/git/merge.go` (diff, `recoverFromFailedMerge` and doc comments on `MergeBase`) — US-10

Also read for context, not critiqued: `internal/turn/gate_test.go` (the
`sections`/`gated` maps and `testGate` instrumentation the new test relies
on), `internal/turn/prepare.go` (`syncBase`, the caller of `MergeBase`),
`internal/git/exec.go` (`runner.run`, `classifyRunError`, and the
`mergeTimeout` constant, to verify the abort call's context/timeout
semantics), `internal/daemon/pool.go` (`Quiesce`/`wait`, to check whether an
in-flight turn's context is ever cancelled by shutdown).

Verification run directly, not taken on the reports' word:
- `gofmt -l internal/turn/syncconcurrency_test.go internal/daemon/gate.go internal/git/errors.go internal/git/merge.go` — clean
- `go vet ./internal/turn/... ./internal/daemon/... ./internal/git/...` — clean
- `go build ./...` — clean
- `go test ./internal/git/...` — ok (40.7s)
- `go test ./internal/turn/... -run 'TestConcurrentTurnsSyncIndependentlyOnOneClone|TestCriticalSectionsAreSingleAcquisitions' -v` — both PASS
- `go test ./internal/turn/... -run TestConcurrentTurnsSyncIndependentlyOnOneClone -race -count=5` — ok (64.8s), no flakiness, no race
- `go test ./internal/git/... -run 'TestCloneMethodSetIsTheAllowlist|TestCloneCannotMutateTheRegisteredClone' -v` — both PASS, unchanged
- `go test ./internal/turn/... ./internal/daemon/...` (full packages) — both ok

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-004: `recoverFromFailedMerge`'s abort call reuses a caller-supplied `ctx` with no documented contract that it must never be independently cancellable, which is silently load-bearing for S-5
**File:** `internal/git/merge.go:369` (`recoverFromFailedMerge`)
**Story:** US-10 / T-10.1, T-10.2
**Category:** Error handling / Cross-story (S-5 rollback guarantee)
**Finding:** Traced the actual mechanics rather than taking the doc comment's claim at face value. `runner.run(ctx, timeout, args...)` (`exec.go:207-234`) always derives `cmdCtx, cancel := context.WithTimeout(ctx, timeout)` fresh per call, so a *previous* call's `cmdCtx` expiring (the 120s `mergeTimeout` firing on the merge itself) does not poison the *next* call's `cmdCtx` — the abort genuinely gets its own full 120s budget, exactly as the doc comment claims, as long as `ctx` itself carries no deadline of its own and was not independently cancelled.

The gap is what happens when that assumption doesn't hold. `classifyRunError` (`exec.go:283-307`) checks `ctx.Err() != nil` *before* checking `cmdCtx`'s own deadline, so a merge failure caused by the ambient `ctx` itself expiring or being cancelled — as opposed to the local `mergeTimeout` firing — is reported with `TimedOut == false` (its `Err` becomes `ctx.Err()`, not `context.DeadlineExceeded`). `recoverFromFailedMerge` still runs unconditionally in that case (`timedOut` just ends up false), and it reuses the *same*, already-expired-or-cancelled `ctx` for `w.runner.run(ctx, mergeTimeout, "merge", "--abort")`. `context.WithTimeout(ctx, timeout)` inherits the earlier of the two deadlines/cancellations, so that abort's own `cmdCtx` is born already-Done: the abort process is killed almost immediately, `abortErr != nil`, and the function returns the generic wrapped error at line 370 (`"...%w (abort also failed: %v)"`) rather than a classified sentinel. That is a correctly *reported* failure — S-5's "reported, never silently accepted" is honored — but the underlying working tree is left with `MERGE_HEAD`/index state from the aborted merge never actually cleaned up (the abort command itself never got to run), which would then trip `MergeBase`'s own dirty-worktree refusal (`ErrWorktreeUncommittedChanges`) on every subsequent sync attempt against that branch — effectively bricking that branch's auto-sync until someone manually intervenes.
  I checked whether this is reachable today: `internal/turn/execute.go`'s `Execute(ctx, ...)` receives `ctx` as a bare parameter with no `context.WithTimeout`/`WithDeadline` wrapping anywhere in `internal/turn`, and `internal/daemon/pool.go`'s `Daemon.Quiesce`/`pool.wait()` confirm that neither shutdown path cancels an in-flight turn's own context — quiesce only *waits* on a separate context for the pool to drain, and production shutdown "goes through wait and starts nothing" (i.e., it does not touch the worker's `ctx` at all). So under this codebase's current call graph, `ctx` reaching `MergeBase` is not, in practice, independently cancelled mid-merge, and the risk is currently latent rather than live.
**Suggestion:** No functional change required today given the verified call graph above, but the safety of reusing `ctx` for the abort is an unstated invariant on `Worktree.MergeBase`'s contract, and S-5 is explicitly a "High" severity guarantee — worth either (a) a doc-comment note on `MergeBase` stating that its `ctx` must not be cancelled/deadlined independently of process shutdown, since the rollback abort depends on it still being live, or (b) decoupling the abort call from the caller's own cancellation (e.g. `context.WithoutCancel(ctx)` before applying `mergeTimeout`) so the rollback step is guaranteed to get a chance to run regardless of what happens further up the call chain in the future. Either is acceptable; leaving the assumption undocumented and untested is the actual gap.

### CR-A-005: one comment line in `gate.go`'s new doc section breaks the file's established ~76-78 column wrap
**File:** `internal/daemon/gate.go:62`
**Story:** US-6 / T-6.2
**Category:** Idiom / Style
**Finding:** Every other line in `cloneGate`'s doc comment (including the rest of the new "Keyed on the clone, not the branch" section this diff adds) wraps at 68-78 characters. The one line `// internal/turn's prepareClone critical section now includes a merge of the base branch` is 88 characters — a purely cosmetic outlier `gofmt` does not enforce but the surrounding paragraph's own convention does.
**Suggestion:** Rewrap that line to match the rest of the paragraph. No functional impact.

---

## Notes for the orchestrator (not code defects)

- **T-10.3 (AC-10.1/AC-10.2 proof) is genuinely open, not silently skipped.** Confirmed by reading the tree: `internal/git/merge_test.go` and `internal/git/merge_internal_test.go` (unchanged by this diff) exercise `MergeBase`'s conflict-recovery path, but nothing in the tree currently constructs a synthetic `*CommandError{TimedOut: true}` or otherwise drives `recoverFromFailedMerge` down the new `ErrMergeTimedOut` branch, and nothing asserts the branch SHA is byte-identical before/after a `blocked`/`failed`/timed-out attempt end-to-end. This matches the team's convention that implementers do not write tests (T-10.3 is explicitly reserved for the unit-test-engineer phase) and the implementer's own report that `recoverFromFailedMerge` was left independently callable against a worktree holding a live `MERGE_HEAD` specifically so that test can be written without waiting out the real 120s `mergeTimeout`. Flagging so Phase 2 test-writing scope does not drop AC-10.1/AC-10.2 — as of this review, no test in the tree proves either acceptance criterion.
- **`errors.Join`-vs-fail-fast-in-a-loop pattern: not applicable to this diff.** `recoverFromFailedMerge` is a straight-line sequence (abort, then re-verify, then classify) with no loop over multiple fallible operations to aggregate. The only loop introduced by either story is `TestConcurrentTurnsSyncIndependentlyOnOneClone`'s per-issue assertion loop, which uses `t.Errorf`/`t.Fatalf` directly against `*testing.T` — the idiomatic and expected pattern for a test's per-case assertions, not a production error-aggregation site this pattern would apply to.
- **US-6's test does genuinely prove AC-6.1/AC-6.2, not just "no error occurred."** The concurrent phase's gate-acquisition delta (`2 * len(issues)`, read off `testGate.report()`'s real counters rather than inferred) is the load-bearing assertion: a branch-keyed lock or any second acquisition wrapped around the merge would show up as a count above `2 * len(issues)`, and a missing acquisition (e.g. `prepareClone` silently skipping `syncBase`'s participation in the existing gate) would show up as a count below it — neither direction is masked. Cross-branch interference is checked by `git ls-tree` against origin per branch (each branch's own concurrent-phase file present, every other issue's absent), not by trusting `Result.Pushed`/`SyncOutcome` alone. Concurrency is driven by a single `sync.WaitGroup` with per-goroutine writes to disjoint slice indices (no shared mutable state raced outside the one `sync.Mutex`-guarded `calls` map, which is itself only used to derive a per-issue call sequence number, not to gate execution order) — no sleeps, no polling, no non-deterministic ordering assumed anywhere in the assertions. Re-ran the test 5 times with `-race`: no flakiness, no race detected.

---

## Cross-story summary (US-6 / US-10)

US-6 and US-10 touch adjacent code — both sit inside `internal/git`'s merge path and `internal/turn`'s `syncBase` step — but do not functionally interact: US-6's new test drives only the happy/synced path (a clean merge with something genuinely behind to sync) and never exercises US-10's new `ErrMergeTimedOut` branch or the abort-then-reverify recovery path, so there is no boundary between the two stories' actual code changes to verify beyond "both build and both pass together," which they do. `internal/turn/gate_test.go`'s `"syncBase": {"prepareClone"}` entry and three-section `sections` map — the one piece of shared fixture state both stories' work depends on — are confirmed unchanged and correct. Neither diff introduces a new locking primitive, a `Reset` call, string-built git arguments, or a goroutine without a clear exit path. The one substantive risk surfaced (CR-A-004) is a latent, currently-unreachable one given this codebase's actual context-cancellation model, and the one gap surfaced (T-10.3) is a correctly-scoped, not-yet-written test rather than a silently dropped requirement.

---

## Verdict

ADVISORY_ONLY — 2 advisory findings (CR-A-004, CR-A-005) noted for the drift report, plus two notes for the orchestrator to carry into test-planning (T-10.3 still open; `errors.Join` pattern confirmed not applicable here). No BLOCKING findings for US-6 or US-10. Proceeding to environment setup / next phase.

---
---

# Code Review — US-11
# Feature: Suggest a resolution when a run is refused for a conflict
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: BLOCKING

---

## Scope

This section reviews only US-11 ("Suggest a resolution when a run is refused
for a conflict"), the first story in this feature where repository file
content leaves litefactory for a model. Per the architect's flag and the
orchestrator's explicit instruction, `.claude/skills/shared/security-scanner.md`
was run against the new/changed files in addition to the ordinary review
checklist; its findings are reported both inline (marked **[SECURITY]**) and
in a dedicated table below, as the skill's own output format requires.

Files created (read in full):
- `internal/store/migrations/0008_conflict_proposals.sql`
- `internal/store/conflict_proposals.go`
- `internal/store/conflict_proposals_test.go`
- `internal/agent/conflict.go`
- `internal/agent/conflict_test.go`
- `internal/turn/conflict.go`
- `internal/turn/conflict_test.go`
- `internal/turn/conflict_e2e_test.go`
- `internal/git/conflict_test.go`
- `internal/report/conflict_proposal_test.go`

Files modified (read as diffs against the pre-US-11 tree, plus enough of the
surrounding unchanged code to verify the diff's own claims):
- `internal/git/merge.go` (`Worktree.ConflictBlob`)
- `internal/git/exec.go` (`showBlobTimeout`)
- `internal/git/exec_internal_test.go`, `internal/git/worktree.go` (doc comment)
- `internal/config/config.go`, `internal/config/validate.go`, `internal/config/template.go`
- `internal/daemon/setup.go`
- `internal/turn/errors.go`, `internal/turn/prepare.go`, `internal/turn/execute.go`, `internal/turn/result.go`, `internal/turn/report.go`, `internal/turn/fixture_test.go`
- `internal/report/reply.go`, `internal/report/reporter.go`, `internal/report/replykind_test.go`, `internal/report/testdata/reply_suggests_resolution.golden`

Also read, to verify specific claims rather than trust them: `internal/agent/proposal.go` (the pattern this story is meant to follow end to end), `internal/daemon/proposal_adapter.go` and `internal/harness/runner.go`'s `ProposeSchedule` (the established "narrowing interface + concrete Runner implementation" precedent), `internal/codex/runner.go` (confirmed it also implements `ProposeSchedule`), `internal/harness/prompt.go`'s `PromptInput` (confirmed no content field was added), `internal/git/exec.go`'s `checkName` (confirmed the screening `ConflictBlob` relies on), `internal/report/run.go`'s `conflictsBlock`/`text.go`'s `sanitize`/`fenceFor` (confirmed US-11 reuses them rather than duplicating).

Verification run directly:
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l` over every US-11 `.go` file (created and modified) — no files listed
- `go test -count=1 ./internal/agent/... ./internal/store/... ./internal/git/... ./internal/turn/... ./internal/report/... ./internal/config/... ./internal/daemon/...` — all `ok`
- `go test -race -count=1 ./internal/turn/... -run Conflict -v` — every US-11 test PASS, no race

---

## Security Findings

**Scope:** `internal/agent/conflict.go`, `internal/turn/conflict.go`,
`internal/turn/prepare.go` (the `syncBase` collection call site),
`internal/turn/execute.go`/`report.go` (the post-gate dispatch and reply
assembly), `internal/store/conflict_proposals.go` and its migration,
`internal/report/reply.go`'s `conflictProposalBlock`, and
`internal/config/config.go`'s three new settings — i.e. every file this story
touches, since this is the first place repository content leaves the process
for a model.
**Rulebook:** not found (searched for a `security:` section in any rulebook
file passed by the caller and for a repository-level rulebook file; none
exists) — applied the OWASP-based generic principles this skill falls back to.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ✅ | `treeSHA`/`path` reaching `git show` in `Worktree.ConflictBlob` are screened by the existing `checkName` (empty/dash-prefix/control-byte rejection) before either reaches argv, matching every other `internal/git` caller; `--end-of-options` additionally forecloses flag injection. The model's JSON response is parsed with `DisallowUnknownFields` and a second-JSON-value rejection (`agent.ParseConflictResolution`), then independently re-validated in `internal/turn` (`validateConflictResolution`) against the caller's own known-good path set before anything is persisted — a defence-in-depth pair, not a single point of trust. |
| 2 | Auth & authz | ✅ | No new externally reachable endpoint or command is added. The feature's entire access control is the `suggest_conflict_resolution` config flag, which defaults to `false` in both the Go zero-value path (`config.Settings{}`'s zero value *and* an explicit `SuggestConflictResolution: false` in `config.Defaults()`) and the generated `config.yaml` template (`%t` interpolation of that same default) — confirmed by reading `internal/config/config.go`, `internal/config/template.go`, and `internal/daemon/setup.go` directly, not asserted from a comment. |
| 3 | Secrets & configuration | ✅ | No secret, token, or credential is introduced. The three new config keys (`suggest_conflict_resolution`, `conflict_proposal_ttl_hours`, `conflict_content_max_bytes`) are all plain operator-facing settings with range-validated numeric bounds (`internal/config/validate.go`). |
| 4 | Data handling | ⚠️ | Two distinct findings, detailed below as CR-A-006 and CR-A-007/S-10 note. Summary: (a) the raw conflicting content shown to the model is **never persisted** — verified structurally, not just by absence of an obvious bug, via `TestConflictProposalsSchemaHasNoContentColumn`'s `pragma_table_info` introspection of both `conflict_proposals` and `conflict_proposal_files`, which would fail if a future edit ever added a content column (AC-11.9, S-9, S-17: **compliant**); but (b) local revalidation (`validateConflictResolution`) checks a model-returned path against the *full* `MergePreview.Conflicts` list rather than against the narrower set of paths actually shown to the model, so a response naming an excluded (binary/oversized/non-UTF-8) file's path still passes and could be persisted and posted as a "suggested resolution" for content the model never received — see CR-A-006. |
| 5 | Dependencies & supply chain | ✅ | No new dependency. `internal/agent/conflict.go` imports only `bytes`, `encoding/json`, `errors`, `fmt`, `io`, `strings` — the same stdlib set `proposal.go` already uses. |
| 6 | Error handling | ✅ | Every advisor/validation/persistence failure collapses to a single generic log line (`logger.Warn("conflict-resolution advisor failed", ...)` etc.) and one fixed reply sentence ("No suggested resolution could be produced for this conflict.") — no internal error text, model output, or stack detail is ever echoed into the posted GitHub comment. |

**Summary:** The mitigations this story's own architecture notes call out — cap-and-exclude-whole-files, deliver untrusted content last on stdin under a "data, not instructions" label, reject-don't-salvage on the model's answer, never persist raw content, collect inside the gate but call the model outside it — are all present, all correctly targeted at the threats they name, and (unusually for this kind of review) verified structurally rather than by trusting a doc comment: the schema-introspection test for AC-11.9, the non-ASCII/marker-shaped rejection tests reused from US-5, and the direct read of `checkName`/`ConflictBlob` all hold up. The one genuine security-relevant gap (CR-A-006, local revalidation's allowed-path set is wider than what was actually sent to the model) is real but narrow — it cannot leak anything that was never collected, only mislabel an excluded file's fabricated "resolution" as legitimate — and is rated ADVISORY below. The highest-priority item for this codebase overall is not a defect the scanner can flag on this axis at all: **because no concrete `Runner` implements the `ConflictAdvisor` interface (see CR-B-002), the entire outbound-content pathway this review was convened to examine has never been exercised against a real model integration and cannot leak anything in production today, by construction — which is safe, but also means the security sign-off this story exists to gate cannot yet certify the actual end-to-end pathway, only the orchestration around a pathway that does not exist yet.**

---

## BLOCKING findings

### CR-B-002: No concrete `Runner` implements `ConflictAdvisor`, so AC-11.1 cannot be satisfied by any production code path — only by a test stub
**File:** `internal/turn/conflict.go:119-143` (`ConflictAdvisor`, `conflictAdvisorFor`); absence confirmed in `internal/harness/runner.go` and `internal/codex/runner.go`
**Story:** US-11 / T-11.9
**Category:** Cross-story / Standards (Definition of Done)

**Finding:** `conflictAdvisorFor` narrows a `turn.RunnerSet` entry into a `ConflictAdvisor` by type-asserting for a `ProposeConflictResolution(ctx, agent.ConflictResolutionRequest) (agent.ConflictResolution, error)` method. Grepped both production `Runner` implementations directly: `internal/harness/runner.go` and `internal/codex/runner.go` each implement `ProposeSchedule` (the schedule-advisor's equivalent narrowing method) but **neither implements `ProposeConflictResolution`**. This is not a theoretical gap — it was independently confirmed by reading both files end to end, not inferred from the engineer's own report.

The practical consequence: `conflictAdvisorFor(req.Agent, opts.Runners)` returns `ok == false` for *every* `agent.Kind` this daemon can ever be configured with in production, every time, unconditionally — regardless of whether `suggest_conflict_resolution` is `true` or `false`. `attemptConflictSuggestion`'s `if ok { ... }` branch (`internal/turn/conflict.go:229-235`) is therefore permanently dead code in the shipped binary: every blocked run with suggestions enabled will always take the "no advisor is registered for this coding-agent tool" path, set `Result.ConflictSuggestionFailed = true`, and post the one-line "No suggested resolution could be produced for this conflict" sentence — never the suggestion AC-11.1 describes. The only place `ConflictAdvisor` is ever satisfied anywhere in this tree is `internal/turn/fixture_test.go`'s `stubConflictRunner`, a test-only type whose own doc comment says so explicitly: "so a test can register it under `RunnerSet` and drive both call sites the way a real `*harness.Runner` eventually will" (emphasis on *eventually* — an admission, in the shipped code's own comments, that the real implementation does not exist).

AC-11.1 reads: *"A blocked run with suggestions enabled posts one comment carrying the conflicting files, a per-file suggested resolution, the model name, a proposal ID and the confirmation phrase; the branch SHA is unchanged after the run."* This is a claim about what the *system* does, not what a stubbed test proves it is capable of doing given a component that does not exist. As shipped, no sequence of user actions, config changes, or the architect's own described "flip the flag after sign-off" escape hatch can ever make AC-11.1 true, because the component the whole feature depends on — a concrete advisor implementation — was never built.

Three considerations pushed against calling this BLOCKING, and none of them survive scrutiny:

1. *"The package-placement list in the architecture notes only names four packages (`agent`, `turn`, `store`, `report`)."* True, but that list is describing where the **new types and orchestration logic** live, not enumerating every file the story touches — `internal/git` is also touched (for `ConflictBlob`) despite not being on that list, and the notes explicitly justify that as expected, not a violation. The same architecture notes separately state the `ConflictAdvisor` interface is "satisfied by type-asserting a `Runner` out of `turn.RunnerSet`" — a sentence that only makes sense as a description of working behavior if some `Runner` actually satisfies it.
2. *"The feature defaults to off, so this is consistent with waiting for sign-off before it matters."* The default-off flag is a real and correctly-implemented mitigation for the *security* exposure (nothing can leak, because nothing can be called) — see the Security Findings summary above. It does nothing to redeem the *functional* promise AC-11.1 makes. A reviewer who signs off on enabling this feature, believing AC-11.1 was met (as the story's own "Ships in the phase opened after a blocking security review... signs off, and `suggest_conflict_resolution` defaults to false until that sign-off, flippable by the architect or that review" note implies), would flip a flag that produces no different behavior than leaving it off — silently, since the failure mode is indistinguishable from "the advisor tried and failed" in the posted comment.
3. *"This is legitimate deferred follow-up, the same way other watch-outs were deferred."* Every other deferred concern in US-11.md is named explicitly in its own "NOT in scope" list (branch/index/commit writes, the confirmation grammar, editing a suggestion, a second model call, a reasoning tier). Wiring a concrete `ConflictAdvisor` implementation is not on that list. Its absence from an otherwise carefully enumerated list of exclusions is itself evidence that this was not a reviewed, approved scope decision — it was an implementer's own judgment call, made and documented candidly (the "when it exists" comment), but not one the story's own text authorizes.

The established precedent in this exact codebase — `internal/daemon/proposal_adapter.go`'s `scheduleAdvisor`/`newProposalGenerator`, paired with `harness.Runner.ProposeSchedule` **and** `codex.Runner.ProposeSchedule`, both concretely implemented — shows that "narrowing interface + concrete Runner implementation" has always shipped as one unit of work in this codebase before now. US-11 breaks that precedent without the story's own text authorizing the break.

**Verdict on the orchestrator's specific question:** This materially fails AC-11.1. It is **BLOCKING**, not a legitimate deferred follow-up. The rollout gate (`suggest_conflict_resolution` defaulting to `false`) is the correct and sufficient reason this is *not* a security incident today, and it is also not a reason to treat AC-11.1 as satisfied — those are two separate questions, and the story's Definition of Done binds to the second one. A story cannot be marked done against an acceptance criterion its shipped code structurally cannot make true.

**Required fix:** One of two paths, and this is an architect/orchestrator decision rather than mine to make unilaterally:
- (a) Wire a concrete `ProposeConflictResolution` method on `harness.Runner` (and, if Codex is meant to support suggestions too, on `codex.Runner`), following the exact tool-free, `--tools ""`, no-worktree pattern `harness.Runner.ProposeSchedule` already establishes (`internal/harness/runner.go:70-115`) — within this story, before it is marked done; or
- (b) Have the architect explicitly amend `US-11.md`'s Tasks/Acceptance-Criteria/NOT-in-scope sections to defer the concrete-Runner wiring to a named follow-up story, and narrow AC-11.1's wording to describe only the stub-verified orchestration behavior this diff actually delivers — so the story's own acceptance criteria stop claiming behavior the shipped system cannot produce.

Either is acceptable engineering; leaving AC-11.1 stated as-is while the code cannot satisfy it, with no record of a scope decision authorizing that gap, is not.

**Update (2026-09-07, re-review): RESOLVED.** The engineer added `ProposeConflictResolution` to both `internal/harness/runner.go` (immediately after `ProposeSchedule`) and `internal/codex/runner.go` (immediately before `Run`), each mirroring its respective package's `ProposeSchedule` shape exactly. Verified independently rather than taken on the report's word — see the dedicated re-review section at the end of this document for the full verification trail. Both concrete `Runner` types now satisfy `turn.ConflictAdvisor`, confirmed by a temporary static-assertion probe (`var _ ConflictAdvisor = (*harness.Runner)(nil)`, `var _ ConflictAdvisor = (*codex.Runner)(nil)`) added to `internal/turn`, compiled successfully, and removed. `conflictAdvisorFor` is no longer permanently dead code in the shipped binary for either registered agent kind.

---

## ADVISORY findings

### CR-A-006: [SECURITY] Local revalidation checks a model-returned path against the full conflicting-path set, not the narrower set of paths actually shown to the model
**File:** `internal/turn/conflict.go:156-177` (`validateConflictResolution`); call site `internal/turn/conflict.go:245` (`validateConflictResolution(resolution, sr.conflicts)`)
**Story:** US-11 / T-11.8
**Category:** Security / Data handling
**Finding:** `attemptConflictSuggestion` sends the advisor only `sr.files` — the subset of conflicting paths that survived `collectConflictContent`'s binary/non-UTF-8/oversized exclusion (`syncBase`, `internal/turn/prepare.go:587-590`). But it revalidates the advisor's response against `sr.conflicts` — the *full* `MergePreview.Conflicts` list, excluded files included. `validateConflictResolution`'s own doc comment says it checks "every path... one of the paths that actually conflicted", which is technically true of `sr.conflicts` (every one of those paths did conflict), but reads naturally as "the paths this call actually collected and sent" — which is `sr.files`, a strict subset when any file was excluded.

The practical effect: a model response naming an excluded file's path — one whose content was binary, non-UTF-8, or too large to have ever been shown to the model — currently passes local revalidation, gets persisted as a `store.ConflictProposal` row, and gets posted to the issue as a "suggested resolution" for a file the model never examined. This does not violate AC-11.4 as literally written ("a response naming a path outside `MergePreview.Conflicts`... produce no proposal" — an excluded file's path is *inside* that list, just not inside what was sent), so it is not a correctness bug against the letter of the acceptance criterion. But it weakens the defence-in-depth story this exact check exists to provide: an issue owner reading a posted "suggestion" for a large binary asset (say) that was correctly excluded from the model's context would have no way to know the resolution shown for it is fabricated rather than derived from anything real.
**Suggestion:** Narrow the `allowed` argument at the call site from `sr.conflicts` to the paths in `sr.files` (i.e. `agent.ConflictFileContent.Path` for each element actually sent), so a path the model was never shown is rejected the same way an out-of-scope path already is. This is a one-line change at the call site plus, ideally, deriving the allowed set directly from what was sent rather than threading a second list through `syncResult`.

### CR-A-007: `collectConflictContent` does not stop once the total content budget is exhausted, so `CloneGate`-held time scales with the number of conflicting files rather than with useful collected content
**File:** `internal/turn/conflict.go:49-90` (`collectConflictContent`, `capturedConflictContent`)
**Story:** US-11 / T-11.6
**Category:** Performance / Concurrency (S-6/S-10 gate-holding)
**Finding:** The loop in `collectConflictContent` iterates every path in `conflicts` (up to `git.MaxMergeConflictPaths = 500`) and calls `reader.ConflictBlob` — a `git show` subprocess — for each one *before* checking whether the running `*total` has already reached `totalMaxBytes`. At the production defaults (`conflict_content_max_bytes` = 16 KiB, `conflictFilePerFileMaxBytes` = 4 KiB), at most about four files can ever be admitted under the total budget — but a blocked merge with, say, 200 conflicting files still spawns 200 sequential `git show` invocations, each doing real I/O and process-exec overhead, all while `prepareClone`'s single `Options.CloneGate` acquisition is held (this collection call happens *inside* `syncBase`, which is deliberately still under the same gate as `MergePreview`, per S-10). Every other turn queued behind the same registered clone waits for all of it — precisely the serialization-minimization property `AC-PAR-3.2` and the codebase's own repeated "one acquisition, minimal blocking" design language (see `CloneGate`'s doc comment) exist to protect.

This does not violate AC-11.5 (the cap and exclusion behavior themselves are correct and tested — `TestCollectConflictContentExcludesWholeOverTotalBudget` proves files past the budget are excluded, just not that they're skipped cheaply) or AC-11.7 (the model call itself is still correctly outside the gate). It is a resource/latency concern specific to how expensive it is to *decide* what to exclude, not a correctness gap.
**Suggestion:** Once `*total` has already reached `totalMaxBytes`, skip the remaining paths without calling `ConflictBlob` for them at all (they can be appended to `excluded` directly) — bounding gate-held time by the configured content budget rather than by how many files happened to conflict in the base branch.

---

## Verified correct

- **S-10's "collect inside the gate, call the model outside it" split is genuinely enforced, not merely documented.** `syncBase` calls `collectConflictContent` at `internal/turn/prepare.go:588`, strictly inside `prepareClone`'s one `Options.CloneGate.Acquire`/`defer release()` pair (`prepare.go:429-433`). `attemptConflictSuggestion` — the only call site of `advisor.ProposeConflictResolution` in the tree — is invoked from `Execute` (`execute.go:593`) only after `prepare(...)` has already returned, i.e. after `prepareClone`'s deferred release has already run. `TestConflictAdvisorNeverCalledWhileCloneGateIsHeld` (AC-11.7) proves this dynamically with a `recordingGate` wrapper that tracks live hold state and fails if the advisor is invoked while held; re-ran it directly under `-race`, passes.
- **The turn's own `ctx` reaches the advisor call unwrapped, never `context.Background()`.** `attemptConflictSuggestion(ctx context.Context, ...)` receives the same `ctx` `Execute` was called with and passes it straight through to `advisor.ProposeConflictResolution(ctx, ...)` with no `context.WithTimeout`/`WithDeadline` wrapping — matching the same "no ambient deadline imposed here" discipline `prepareClone`'s own doc comment states for git operations, and matching the architecture notes' explicit instruction.
- **AC-11.1's non-Runner-dependent half is genuinely correct.** Read `internal/turn/report.go`'s `reportSyncRefusal` and `internal/report/reply.go`'s `ReplySuggestsResolution` case directly: the posted comment (when a proposal exists) carries the conflicting files (`conflictsBlock(r.ConflictPaths)`), a per-file resolution (`conflictProposalBlock(r.ConflictProposal.Files)`), the model name and proposal id (`"Suggested resolution from `%s` (proposal `%d`)"`), and the confirmation phrase (`conflictProposalConfirmationSentence`, unconditional on this reply kind). `TestExecuteSuggestsAResolutionOnABlockedRun` proves the branch SHA is unchanged (`git rev-parse HEAD` before/after, via a stubbed advisor) and that all five elements land in the posted body. Everything *except* "a real coding-agent tool actually produces a resolution in production" is correct — see CR-B-002 for the one piece that is not.
- **AC-11.2 (advisor failure/timeout).** `TestExecuteSkipsSuggestionWhenAdvisorFails` proves a failing advisor still produces the full ordinary conflict refusal (conflicting paths present), writes no `ConflictProposal` (`result.ConflictProposal == nil`), and adds exactly the one "No suggested resolution could be produced for this conflict" line — verified both at the `Result` level and by rendering through `ReportTurn` to the actual posted body. A timeout is not separately tested at the `internal/turn` level, but is handled identically: `advisor.ProposeConflictResolution`'s own error return (of any cause, including a context deadline) takes the same `err != nil` branch in `attemptConflictSuggestion` — confirmed by reading the code, since there is exactly one error-handling branch for "the call failed," not one for "failed" and a separate one for "timed out."
- **AC-11.3 (disabled path byte-identical) is proven at both layers, not just asserted.** At the `internal/turn` level, `TestExecuteMakesNoAdvisorCallWhenDisabled` proves zero advisor calls and that `Result.ConflictProposal`/`ConflictSuggestionFailed` are both left at their zero values when `SuggestConflictResolution` is `false`. At the `internal/report` rendering level, `TestReplyDisabledSuggestionIsByteIdenticalToThePlainRefusal` renders a plain `ReplyRefusesSync` with both US-11 fields left at zero and asserts none of the three US-11-specific strings ("No suggested resolution", "This is a suggestion only", "Suggested resolution from") appear — i.e. the rendering path is structurally guaranteed identical, not merely observed to look the same in one example, because `reportSyncRefusal`'s `switch` (`internal/turn/report.go:439-445`) only sets either field when `SuggestConflictResolution` was true for the turn (verified by reading the call graph: `attemptConflictSuggestion` returns immediately at its very first line when `!opts.SuggestConflictResolution`, before any log line or field mutation).
- **AC-11.4 (out-of-scope path, conflict markers) is tested at three independent layers.** `agent.ParseConflictResolution` rejects a marker-carrying response at parse time (`TestParseConflictResolutionRejectsConflictMarkers`); `internal/turn`'s `validateConflictResolution` independently re-checks both conditions as defence in depth (`TestValidateConflictResolutionRejectsAnOutOfScopePath`, `TestValidateConflictResolutionRejectsAConflictMarker`, `TestValidateConflictResolutionRejectsADuplicatePath` — the duplicate-path check is not named by the AC but is a reasonable extension of the same "one path, one answer" discipline); and both are proven end-to-end through `Execute` (`TestExecuteRejectsAnOutOfScopeSuggestion`, `TestExecuteRejectsASuggestionCarryingConflictMarkers`), confirming no proposal is created or persisted on either violation.
- **AC-11.5 (cap and exclusion) is tested with real boundary values, not just "some cap exists."** `TestCollectConflictContentExcludesWholeOverPerFileBudget` constructs a hunk exactly one byte over `conflictFilePerFileMaxBytes` and confirms whole-file exclusion (never truncation — the function has no truncation code path at all, only inclusion or exclusion). `TestCollectConflictContentExcludesWholeOverTotalBudget` sizes a total budget to admit exactly one of two files and confirms the second is excluded whole with the first kept exactly as collected. Binary (NUL byte) and non-UTF-8 detection are both exercised in `TestCollectConflictContentCapsAndExcludes`, alongside a no-markers-at-all case (a delete/rename conflict merge-tree cannot represent with markers) and an unreadable-blob case — all five exclusion reasons AC-11.5 and the story's own watch-out #2 name are covered, and none of them truncates.
- **AC-11.6 (terminal state, no sleep/poll).** Read `attemptConflictSuggestion` and its full call chain top to bottom: no `time.Sleep`, no polling loop, no channel wait for an external reply — every path is a single synchronous call (advisor call, then optional expiry call, then optional persistence call) that returns immediately. `Execute` returns the `Result` from `attemptConflictSuggestion` directly to its own caller with no further waiting.
- **AC-11.8 (single-transaction supersede) is verified both by reading `db.withTx`'s scope and by direct row-count assertion.** `CreateOrGetConflictProposal`'s entire body — the source-comment replay check, `supersedePendingConflictProposals`, the `INSERT` for the new proposal, and the per-file inserts — all run inside one `db.withTx(ctx, func(tx *sql.Tx) error { ... })` closure, so a crash or error partway through rolls back atomically; there is no window where two proposals for one session are both `pending`. `TestConflictProposalSupersedesEarlierPending` proves this concretely with a direct `SELECT COUNT(*) ... WHERE status = 'pending'` query after creating a second proposal, asserting exactly 1.
- **AC-11.9 (never persist raw content) is verified structurally, exactly as the orchestrator's brief asked.** `TestConflictProposalsSchemaHasNoContentColumn` queries `pragma_table_info` for both `conflict_proposals` and `conflict_proposal_files` and asserts the exact column list — `{id, session_id, trigger_comment_id, model, status, created_at, resolved_at}` and `{id, proposal_id, ordinal, path, resolution}` respectively — with no content/hunk/blob column present in either. A future migration that reintroduced such a column would fail this test rather than shipping silently, which is the structural guarantee the brief asked for rather than an absence-of-bug argument. Independently confirmed by reading `0008_conflict_proposals.sql` directly: no column beyond `path`/`resolution` exists on the files table.
- **Rollout gate (`suggest_conflict_resolution` defaults to `false`) verified in both places named.** Go zero-value path: `config.Settings{}`'s zero value for a `bool` field is already `false`, and `config.Defaults()` additionally states it explicitly (`SuggestConflictResolution: false`) rather than relying on the implicit zero value — belt and suspenders. Generated template: `internal/config/template.go`'s `%t` verb is fed `d.Settings.SuggestConflictResolution`, which is `false` from `Defaults()` unless a caller explicitly overrides it before rendering — confirmed by reading the `fmt.Sprintf` call's argument list directly, not assumed from the presence of the key.
- **`internal/git`'s existing screening discipline is correctly extended, not bypassed, for the one new git call.** `ConflictBlob`'s `treeSHA` and `path` arguments both go through the same `checkName` every other `internal/git` caller-supplied value does (empty/dash-prefix/control-character rejection) before being concatenated into the single `<tree>:<path>` argv element, and `--end-of-options` precedes it — the same option-injection defence `MergePreview` and `MergeBase` already apply. `TestWorktreeConflictBlobRefusesUnscreenedNames` proves both parameters are independently screened. `git.Worktree`'s doc comment was correctly updated to list `ConflictBlob` under the read-only half, not the three-mutator list (`Commit`, `Push`, `MergeBase`) — confirmed correct, since `git show` touches neither the working tree, the index, nor a ref.
- **Cross-story reuse of US-5's rendering machinery is real, not merely claimed.** `conflictProposalBlock` (new, `internal/report/reply.go`) calls the *same* `conflictPathSpan`, `sanitize`, and `fenceFor` functions `conflictsBlock` already uses — read the function bodies directly to confirm no second denylist regex, no second fence-width calculation, and no second sanitizer was introduced. The one difference (`conflictProposalBlock` drops a file silently on a rejected path rather than counting it, versus `conflictsBlock`'s reject-and-count) is explicitly and correctly documented as intentional defence-in-depth rather than the primary reject-don't-salvage gate, which already ran in `internal/turn`'s `validateConflictResolution` before a proposal was ever persisted.
- **`harness.PromptInput` carries no conflict-content field.** Read the full struct definition in `internal/harness/prompt.go`; confirmed no field of any kind related to conflicting file content, hunks, or resolutions was added — watch-out #1 of the architecture notes is honored.
- **No new dependency, no new package.** `internal/agent/conflict.go` imports only the stdlib set `proposal.go` already uses. Every new file lands in one of the four packages the architecture notes name (plus the one `internal/git` addition the notes themselves say is expected). No new `internal/daemon` file was added, matching "Nothing new in internal/daemon for this story" — confirmed by `git status`, which shows only `internal/daemon/setup.go` modified (the three-field `Options` wiring), no new file.
- **Migration number.** `0008_conflict_proposals.sql` is confirmed the correct next head by directly listing `internal/store/migrations/`: `0001` through `0007` already exist, `0008` is next and unclaimed by any other in-flight story's migration.
- **Build/vet/format/tests, re-run directly rather than trusted from the task report.** `go build ./...`, `go vet ./...` — both clean. `gofmt -l` over every `.go` file this story created or modified — no output. `go test -count=1` across `internal/agent`, `internal/store`, `internal/git`, `internal/turn`, `internal/report`, `internal/config`, `internal/daemon` — all `ok`. `go test -race -count=1 ./internal/turn/... -run Conflict -v` — every US-11 test (16 of them) individually PASS, no race detected.

---

## Cross-story summary

US-11 integrates correctly at every boundary its architecture notes name: US-4's `blocked` outcome is the sole trigger (`syncOutcomeBlocked` gates both the ordinary refusal and the new collection step); US-5's path-rendering discipline is reused rather than duplicated for both the conflicting-path list and the new per-file resolution block; US-8's config plumbing follows the exact `AutoSyncBase`/`BlockRunOnConflict` copy-in pattern already established in `Options`; and US-9's `0007` migration is correctly the prior head with `0008` next. The `internal/report` cannot-import-`internal/turn` boundary is respected exactly as the notes require: `ReplySuggestsResolution` is a `report`-owned kind dispatched through `ReplyRequest.Refusal`/`kind()`, not through the `CommandKind`-keyed `replyKind` mapping (which needed no new case, since this is a state-of-the-world refusal like `ReplyRefusesSync` and `ReplyAgentSwitchRefused` before it, not a command answer).

Where this story falls short is not at a story-to-story boundary but at the boundary between this story and the two packages (`internal/harness`, `internal/codex`) that would have to change for its central promise to ever be true in a running system. Everything upstream of that boundary — collection, capping, exclusion, the narrow bounded request/response shape, strict parsing, local revalidation, persistence, superseding, expiry, rendering, and the gate-timing discipline the security review cares most about — is built correctly and is more thoroughly verified, both by the implementer's own tests and by this review's independent re-derivation, than most stories this reviewer has seen. That work is not wasted by CR-B-002; it is exactly the work a concrete `Runner` implementation would plug into. But until that plug exists, the story cannot honestly be marked done against its own AC-11.1.

---

## Verdict

**BLOCKING** — 1 blocking finding (CR-B-002) and 2 advisory findings (CR-A-006, CR-A-007) for US-11. Route CR-B-002 to the engineer(s) owning `internal/harness`/`internal/codex` (or to the architect, if the resolution is to re-scope AC-11.1 instead — see "Required fix" above) before Phase 2 testing proceeds on this story. CR-A-006 and CR-A-007 are noted for the drift report and do not block. This does not reopen or affect the ADVISORY_ONLY verdicts already recorded above for US-2/US-3/US-4, US-5/US-9, or US-6/US-10.


---
---

# Re-review — US-11 / CR-B-002
# Feature: Suggest a resolution when a run is refused for a conflict
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAR

---

## Scope

Re-review of the single prior BLOCKING finding, CR-B-002 ("No concrete
`Runner` implements `ConflictAdvisor`"). Per the orchestrator's instruction,
only the two files tied to the fix were reviewed in full, plus
`internal/turn/conflict.go` (the `ConflictAdvisor` interface and
`conflictAdvisorFor`) and `internal/agent/conflict.go` (the request/response
types and `ConflictResolutionPrompt`) for context only — neither was
re-reviewed for its own correctness, both having already passed in the prior
US-11 pass.

Files reviewed:
- `internal/harness/runner.go` (new method `ProposeConflictResolution`)
- `internal/codex/runner.go` (new method `ProposeConflictResolution`)

Read for context only, not re-graded:
- `internal/turn/conflict.go` (`ConflictAdvisor`, `conflictAdvisorFor`)
- `internal/agent/conflict.go` (`ConflictResolutionRequest`,
  `ConflictResolution`, `ConflictResolutionPrompt`)

---

## CR-B-002: RESOLVED

**Original finding:** neither `harness.Runner` nor `codex.Runner` implemented
`ProposeConflictResolution`, so `conflictAdvisorFor` could never find a real
advisor in production and AC-11.1 could not be satisfied by any shipped code
path.

**Fix verified against all four of the orchestrator's specific questions:**

1. **The new methods actually satisfy `ConflictAdvisor`; `conflictAdvisorFor`
   will find them.** `turn.ConflictAdvisor` requires exactly
   `ProposeConflictResolution(ctx context.Context, req
   agent.ConflictResolutionRequest) (agent.ConflictResolution, error)`.
   `harness.Runner.ProposeConflictResolution` (`internal/harness/runner.go:124`)
   and `codex.Runner.ProposeConflictResolution`
   (`internal/codex/runner.go:195`) both match that signature exactly. Rather
   than trust the signature match by inspection alone, a temporary
   compile-time probe was added to `internal/turn` —
   `var _ ConflictAdvisor = (*harness.Runner)(nil)` and
   `var _ ConflictAdvisor = (*codex.Runner)(nil)` — built and vetted clean,
   then removed (working tree confirmed unchanged by `git status` afterward).
   `conflictAdvisorFor`'s `runner.(ConflictAdvisor)` type assertion will
   therefore succeed for both registered agent kinds in production, and
   `go build ./...` / `go vet ./...` are both clean across the whole repo.

2. **Tool-free/bounded-call discipline is real, not just claimed.**
   `harness.Runner.ProposeConflictResolution` passes `--tools ""` (empty tool
   surface, `internal/harness/runner.go:137`) and no `--allowedTools`,
   `cmd.Dir = r.home` (never `req.WorktreePath` — there is no worktree in this
   request type at all), and no session id or `--resume` flag, matching
   `ProposeSchedule`'s shape token for token.
   `codex.Runner.ProposeConflictResolution` passes `--sandbox read-only`,
   `--ignore-user-config`, `--ignore-rules`, and `cmd.Dir =
   filepath.Dir(r.binary.Path)` — also never a worktree — again matching
   `ProposeSchedule` exactly. Both methods derive their deadline with
   `runCtx, cancel := context.WithTimeout(ctx, r.timeout)` off the `ctx`
   parameter actually passed in by the caller, never `context.Background()`;
   `defer cancel()` follows immediately. `r.timeout` is the same
   package-configured wall-clock budget `Run`/`ProposeSchedule` already use in
   each file — read directly rather than assumed — so it is a fresh,
   independent `WithTimeout` call each invocation, not a shared or leaked
   deadline from any other in-flight call in the same turn; this is the same
   "own deadline, distinct from the turn's ambient context" property
   `ProposeSchedule` already has, and the new methods' own doc comments state
   this correctly.

3. **Untrusted-content-last / "data, not instructions" prompt discipline is
   what's actually used, not rolled separately.** Both new methods' first
   line of substantive work is `prompt, err :=
   agent.ConflictResolutionPrompt(req)` — grepped both files for any other
   prompt-construction (`fmt.Sprintf` building a Claude/Codex prompt string,
   any second call site) and found none; the only `Sprintf`-built prompt in
   the two-file diff is inside `agent.ConflictResolutionPrompt` itself, which
   was already verified clean in the prior US-11 pass (delivers conflicting
   file content last, under an explicit "data, not instructions" label, after
   the branch/base orientation strings). Neither runner method appends,
   prepends, or otherwise reconstructs any part of the prompt itself.

4. **Build/vet/format/race verification, re-run directly:**
   - `go build ./...` — clean
   - `go vet ./...` — clean
   - `gofmt -l .` — no files listed
   - `go test -count=1 ./internal/turn/... ./internal/harness/... ./internal/codex/... ./internal/agent/...` — all `ok`
   - `go test -race -count=1 ./...` — all 22 packages `ok`, run four times total (once as the primary verification pass, three additional full-suite `-race` re-runs specifically hunting for the reported flake) — zero `FAIL`, zero `panic`, across all four runs
   - `go test -race -count=10 ./internal/harness/... -run TestRunTimeoutKillsTheWholeSubtree -v` — 10/10 `PASS` in isolation

   **On the reported transient flake:** read `TestRunTimeoutKillsTheWholeSubtree`
   directly (`internal/harness/runner_test.go:636`). It drives a real shell
   script that backgrounds a `sleep 120`, records its PID/PGID to disk, and
   asserts the whole process subtree is dead after a 1-second
   `config.Claude.TimeoutSeconds`, through `f.run(t)` — which calls
   `Runner.Run`, the pre-existing method, never `ProposeConflictResolution`
   (which this test does not exercise at all; it has no code path to
   `ProposeConflictResolution`). The test is timing-sensitive against real OS
   process scheduling under a 1-second wall-clock budget, which is exactly
   the shape of test that flakes under full-suite CPU contention and not
   under isolation or a quiet machine. It passed on every run performed here,
   both as part of the full `-race` suite (4/4 runs) and in 10/10 repeated
   isolated runs. The engineer's characterization — a genuine pre-existing
   flake in `Run`'s subprocess-timeout/kill-subtree behavior, unrelated to
   this diff — is confirmed, not merely taken on their word.

**Disposition:** Resolved. No new BLOCKING or ADVISORY issue found in either
of the two files. No regression to the security posture verified in the
prior pass: neither new method grants a tool surface, attaches a worktree, or
constructs a prompt outside `agent.ConflictResolutionPrompt`.

---

## Verdict

CLEAR — CR-B-002 is resolved. No new findings in the two files reviewed.
Proceeding to environment setup / next phase for US-11.

---
---

# Code Review — US-12
# Feature: Apply a confirmed resolution and complete the merge
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: BLOCKING

---

## Scope

This section reviews only US-12 ("Apply a confirmed resolution and complete
the merge"), the final story in this feature and the only one where
model-produced text becomes committed bytes. Per the orchestrator's
instruction, `.claude/skills/shared/security-scanner.md` was run against
every file listed below; its findings are reported in the dedicated table
below. US-1 through US-11 are out of scope and were not re-reviewed (see the
sections above).

This story's code is the product of two prior interrupted sessions plus a
just-completed implementation-engineer pass. The engineer's own report claims
several things (the ownership-check fix, the new daemon dispatch layer, the
`turn/report.go` wiring, the golden-fixture regeneration, and the
`errors.Join` addition in `checkResolvedFiles`). Every one of those claims was
traced against the actual source below rather than taken on the report's
word; the results are recorded against each claim individually.

Files read in full:
- `internal/confirm/confirm.go`, `confirm_test.go`, `doc.go`
- `internal/git/commit.go`, `worktree_apply.go`, `surface_test.go`
- `internal/report/link.go`
- `internal/store/conflict_proposals.go`, `migrations/0009_conflict_proposal_confirmation.sql`
- `internal/turn/apply.go`, `apply_test.go` (read for behaviour confirmation)
- `internal/daemon/confirm.go`, `executor.go` (new/changed), `daemon.go`, `tick.go`, `setup.go` (diffs plus enough surrounding code to verify each claim)

Files read in relevant part (diffs plus enough surrounding context to verify
the specific claims above):
- `internal/turn/prepare.go` (`prepareClone`, `syncBase`, `syncEnabled`)
- `internal/turn/report.go` (`ReplyRequest.ConfirmKind`/`.Confirm`, `kind()`, `validate()`)
- `internal/turn/command.go` (`KindConfirm` grammar)
- `internal/turn/gate_test.go` (the `sections`/`gated` maps)
- `internal/report/reply.go` (`ReplyKind` additions, `ConfirmReply`, rendering)
- `internal/report/replykind_test.go`, `internal/report/testdata/reply_suggests_resolution.golden`
- `internal/daemon/fixture_test.go` (`newDaemon`)

Verification run directly, not taken on the report's word:
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l` over `internal/confirm internal/git internal/report internal/store internal/turn internal/daemon` — no files listed
- `go test -count=1 ./internal/confirm/... ./internal/git/... ./internal/store/... ./internal/report/...` — all `ok`
- `go test -count=1 -run 'ApplyConfirmedResolution|Confirm' ./internal/turn/... -v` — all 8 tests PASS

---

## Security Findings

**Scope:** every file this story touches — `internal/confirm`, the three new
`*git.Worktree` mutators and the trailer additions in `internal/git`, the
confirmation gate and agentless apply turn in `internal/turn`, the new daemon
dispatch layer in `internal/daemon`, the confirmation-reply rendering in
`internal/report`, and the schema addition in `internal/store` — since this
is the only story in the feature where model-produced text becomes committed
bytes.
**Rulebook:** not found (no `security:` section in any rulebook file passed
by the caller, and no repository-level rulebook file exists) — applied the
OWASP-based generic principles this skill falls back to, the same as the
US-11 pass.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ✅ | `confirm.Parse` restricts the phrase to a fixed lowercase-letters-and-spaces charset before comparing against `Phrase`, whole-match only, no partial credit (S-14). `checkResolvedFiles`/`containedResolvedPath` in `internal/git` independently re-screen every path a confirmed proposal names — absolute, `..`, symlink ancestor or leaf, or simply unlisted are all refused, and this lives inside `*Worktree` itself, never a caller-side string-prefix check (S-13, verified below). `CompleteMergeOptions.validate()` screens every trailer value for control characters before it reaches argv. |
| 2 | Auth & authz | ✅ | `ValidateConfirmation`'s ownership check is genuinely numeric-ID-based (`ownerUserID != commenterUserID`, `internal/turn/apply.go:126`), matching `turn.Filter.Classify`'s `c.Author.ID == f.identities.OwnerUserID` pattern and `daemon.go`'s `t.issue.AssignedTo(...)` precedent exactly — never a login/handle comparison, and never (as the pre-fix bug reportedly did) a comparison against the issue's own author. Traced independently; see CR-A-008 below for a related but non-blocking observation about where this check is actually reachable from. |
| 3 | Secrets & configuration | ✅ | No secret, token, or credential introduced. The three new trailers and the `ConfirmedBy` value are all either litefactory-controlled (`Model`, an `agent.Kind` string) or the confirming owner's own forge login — the only person who can ever reach this path per finding #2. |
| 4 | Data handling | ✅ | The confirmed resolution text was already persisted in US-11 (out of scope here); this story adds no new raw-content persistence. `MaxConflictProposalFiles` and the existing per-file/total content caps are unaffected. Commit trailers and the rendered disclosure carry only the proposal id, the confirming handle, and the model name — verified in `internal/git/commit.go` and `internal/report/reply.go` — never raw comment text, and never a `Co-authored-by:` trailer for the model (S-16, explicit in `TrailerConfirmedBy`'s own doc comment). |
| 5 | Dependencies & supply chain | ✅ | No new dependency in any file this story touches. |
| 6 | Error handling | ✅ | Every `ReplyConfirm*`/`ReplyAppliesResolution` rendering branch in `internal/report/reply.go` references only `r.Confirm.ProposalID` (a numeric id) and fixed sentences — never `c.Body` or any other raw comment text (S-14, AC-12.2's "refusals never echo raw comment text" requirement, confirmed by direct reading, not inference). `agentSpan`/`confirmedBySpan` apply reject-don't-salvage screening to `Model`/`ConfirmedBy` before they are ever rendered. |

**Summary:** The classic OWASP-shaped risks this story could have introduced —
injection, broken auth, secret leakage, unsafe error disclosure — are all
correctly mitigated and independently verified against the actual source,
not the report's claims. The one serious defect this review found (CR-B-003,
below) is not a security-classification issue in the OWASP sense — it leaks
nothing and grants no unauthorized access — but it is a correctness defect in
the exact mechanism (S-15's TOCTOU re-verification) the story's own threat
model depends on to keep the confirmation gate meaningful, so it is reported
here for visibility even though it does not fit a row in the table above.

---

## BLOCKING findings

### CR-B-003: `prepareClone` runs `syncBase` unconditionally, even for the confirmed-apply turn, contradicting its own documented design and undermining S-15's re-verification
**File:** `internal/turn/prepare.go:472-477` (`prepareClone`); doc comment at `internal/turn/prepare.go:407-426`; also `internal/turn/apply.go:234-237`
**Story:** US-12 / T-12.6
**Category:** Cross-story / Correctness / Concurrency (S-15)

**Finding:** `ExecuteApplyResolution`'s own doc comment states, in `apply.go`:

> "It never runs `syncBase`: a confirmed resolution already knows exactly
> what it is merging and has its own re-verification, so a second,
> independent auto-sync attempt over the same branch would be redundant at
> best and a second, differently-decided merge at worst."

`prepareClone`'s own doc comment repeats the same claim under its "confirmed
(US-12)" heading: "applying the confirmed proposal is this section's fifth
step, **after** `syncBase`" — implying `syncBase` runs and then the confirmed
step follows, which is literally true, but the reader is meant to conclude
`syncBase` is inert for this path. It is not gated at all.

Reading `prepareClone`'s actual body (`prepare.go:436-528`) top to bottom:

```go
worktree, err := ensureWorktree(ctx, opts, clone, session, repo, branch, logger)
...
sync, err := syncBase(ctx, opts, req, worktree, repo, author, logger)   // line 472 — unconditional
if err != nil {
    return session, "", nil, syncResult{}, nil, err
}

if confirmed == nil {                                                   // line 477 — the ONLY branch on confirmed
    return session, branch, worktree, sync, nil, nil
}
// ... the confirmed-apply logic follows, using a *fresh* RevParse read ...
```

There is no `if confirmed == nil { sync, err = syncBase(...) }` guard, and no
`else` that skips it. `syncBase` runs for *every* call to `prepareClone`,
including the one `ExecuteApplyResolution` makes with a non-nil `confirmed`.
Its return value (`sync`) is then silently discarded by the caller —
`apply.go:298`: `_, _, _, _, applyResult, err := prepareClone(...)` — which is
itself evidence this was not meant to run: nothing downstream of
`ExecuteApplyResolution` ever reads it.

`ExecuteApplyResolution`'s call into `prepareClone` builds `minimalReq :=
Request{SessionID: req.SessionID, Issue: Issue{Number: session.IssueNumber}}`
(`apply.go:297`) — a bare `Request` with `SyncOverride` left at its zero
value (`nil`). `syncEnabled` (`prepare.go:609-614`) therefore falls through to
`opts.AutoSyncBase`, which defaults to `true` in production
(`plan_brief.md`'s Shared Decisions: `AutoSyncBase` default `true`). So in the
*default* production configuration, every confirmed-apply turn:

1. Re-measures `Divergence` against the base branch.
2. If behind and within `AutoSyncMaxBehind`, runs a fresh `MergePreview`
   against the **current** base tip — not the tip the proposal was computed
   against.
3. If that fresh preview is clean — which it will be whenever the base has
   moved in a way compatible with the branch's own unresolved conflicting
   content since the proposal was made (a later base commit reverting or
   superseding the original conflicting hunk, or simply a base commit landed
   after the propose turn's own conflict was independently resolved
   upstream) — `MergeBase` actually runs and **commits a second, unconfirmed
   merge** onto the task branch, moving `HEAD`, before the confirmed-apply
   logic ever runs.

This has two consequences, either of which is a real defect in this story's
central guarantee:

- **AC-12.5's "exactly one merge commit" is violated** whenever step 3 above
  fires: the branch now carries an ordinary, unconfirmed auto-sync merge
  commit *and* the confirmed-resolution merge commit, not "exactly one."
  Nothing in `ExecuteApplyResolution` checks for or reports this — the
  auto-sync merge is entirely invisible to the confirmation's own disclosure.
- **S-15's re-verification is silently contaminated.** The `confirmed`
  section's own SHA check (`prepare.go:483-499`) reads `currentBranchSHA`
  via `worktree.RevParse(ctx, "HEAD", ...)` **after** `syncBase` has already
  run and (in the scenario above) already moved `HEAD`. The comparison
  against `stored.BranchSHA` is meant to answer "has anything happened to
  this branch since the proposal was computed that the owner should know
  about" (AC-12.4) — but it cannot distinguish genuine external drift from
  this turn's own unrequested side effect. A proposal that is still perfectly
  valid to apply can now be marked `superseded` purely because
  `ExecuteApplyResolution`'s own internal call graph moved the branch out
  from under it, and the owner is told "comment again to ask for a fresh
  suggestion" for no real reason. Conversely, in the rarer case where the
  auto-sync merge itself is what changes `currentBaseSHA` to equal
  `stored.BaseSHA` by coincidence, the two side effects could in principle
  interact in ways neither this function nor its tests reason about.

I traced whether the existing test suite would have caught this and found it
would not, by construction: `TestApplyConfirmedResolutionSupersedesOnStaleBase`
advances the base with a commit to an *unrelated* file
(`further.txt`), which leaves the original conflicting hunk untouched, so the
fresh `MergePreview` `syncBase` runs stays unclean (`blocked`) and no merge is
actually performed — the bug is present but not triggered by that specific
scenario. `TestApplyConfirmedResolutionHappyPath` makes no intervening base
commit at all, so `divergence.Behind` does not change between propose and
confirm and the same "still blocked, no side effect" path is taken. Neither
test constructs the scenario — a base commit that resolves or bypasses the
original conflicting hunk — that would make the fresh preview clean and
expose the extra commit. This is a real, currently-untested gap in production
behavior, not a hypothetical.

I also confirmed this is not intentional idempotent housekeeping:
`prepareClone`'s own "No claude run between this step and `ensureWorktree`"
reasoning (used to justify not releasing/retaking the gate) presumes nothing
else mutates the branch in between — `syncBase` mutating it silently
contradicts that same paragraph's premise.

**Required fix:** Skip `syncBase` entirely when `confirmed != nil`, matching
both doc comments' stated intent exactly:

```go
var sync syncResult
if confirmed == nil {
    sync, err = syncBase(ctx, opts, req, worktree, repo, author, logger)
    if err != nil {
        return session, "", nil, syncResult{}, nil, err
    }
} else {
    // no auto-sync for a confirmed-apply turn — see doc comment
}
if confirmed == nil {
    return session, branch, worktree, sync, nil, nil
}
```

(or equivalent restructuring — the precise shape is the engineer's to choose,
but the outcome must be that no `Divergence`/`MergePreview`/`MergeBase` call
happens on the confirmed-apply path). Add a regression test that advances the
base branch, between propose and confirm, with a commit that makes a fresh
merge-tree preview against the *current* base clean (e.g., reverting the
original conflicting hunk on the base side) while the branch's own confirmed
resolution is still what was proposed, and assert: (a) the resulting branch
carries exactly one new commit — the confirmed merge — not two; (b) the
proposal is applied, not spuriously superseded, when `stored.BaseSHA`/
`stored.BranchSHA` genuinely still describe the state the confirmation should
be judged against per S-15's intent (i.e., the test should demonstrate the
fix distinguishes "the base moved in a way that matters" from "the base
moved in a way `syncBase` would have auto-merged anyway").

---

## ADVISORY findings

### CR-A-008: `ValidateConfirmation`'s non-owner refusal is real and correctly implemented, but unreachable through the live daemon comment pipeline — AC-12.2's "reported in the thread" is exercised only at the unit level
**File:** `internal/turn/apply.go:126-128` (`ValidateConfirmation`); `internal/turn/classify.go:245-252` (`Filter.Classify`); `internal/daemon/tick.go:1317-1326` (`handleComment`)
**Story:** US-12 / T-12.5, T-12.9
**Category:** Cross-story / Standards

**Finding:** Traced the full path a `/confirm` comment takes before it can
ever reach `ValidateConfirmation`. `Daemon.handleComment` calls
`d.filter.Admit(ctx, t.session.ID, c)` and returns immediately — via
`Consume` and no reply — whenever `!admission.Triggers()`
(`tick.go:1317-1326`). `Admission.Triggers()` delegates to
`Verdict.Triggers()`, which is `true` only when `Classification ==
store.ClassificationInstruction` (`classify.go:179`), and `Filter.Classify`
sets that classification only when `c.Author.ID == f.identities.OwnerUserID`
(`classify.go:249-251`) — the *same* `Identities().OwnerUserID`
`handleConfirm` later passes to `ValidateConfirmation` as `ownerUserID`. In
other words: by the time a comment's body is even parsed by
`turn.ParseCommand` (let alone reaches `KindConfirm` and `handleConfirm`),
its author has already been proven, by the admission filter, to be the same
account `ValidateConfirmation`'s ownership check compares against. A comment
from any other account is silently consumed with no reply at all — the same
path every ordinary third-party comment takes — and never reaches
`confirm.Parse` or `ValidateConfirmation`.

This means `ConfirmationNotOwner` is not, in practice, reachable via the live
comment-processing pipeline today: it is a correct, independently
unit-tested defense-in-depth backstop (`TestApplyConfirmedResolutionRefusesNonOwner`
calls `ValidateConfirmation` directly with a mismatched `strangerUserID`,
bypassing `Admit` entirely), not a behavior a real "stranger comments
`/confirm`" scenario would trigger end-to-end. AC-12.2 reads "A confirmation
from a non-owning author is refused, writes nothing, leaves the status at
`pending`, and is reported in the thread" — the first three clauses hold
(nothing is ever written and the status never moves, because the comment
never reaches any write path), but "reported in the thread" would only be
true if a non-owner's `/confirm` comment somehow classified as
`ClassificationInstruction` and reached `handleConfirm`, which by
construction it cannot under the current admission-filter design.

This is exactly the same class of "correct, tested, but currently
unreachable given the present call graph" situation this team's own prior
review flagged as advisory rather than blocking (see CR-A-004 in the
US-6/US-10 section above), and the code's own doc comment on
`ValidateConfirmation` is candid about it ("the daemon's admission filter
has, by construction, already refused every comment whose author is not this
same owner id before a body is ever classified as a confirmation control at
all"). It is not a defect in this story's code — it is a property of a
design decision (single-owner comment admission) that predates US-12 and is
shared with every other factory-owned control (`/schedule` has the identical
property). Recorded here because AC-12.2's literal wording implies a visible
reply that the current design does not, in fact, ever produce for this
scenario.
**Suggestion:** No code change required. Worth a one-line note in the
story's closing record (or a drift-report entry) that AC-12.2's "reported in
the thread" clause is verified only at the `ValidateConfirmation` unit level,
consistent with `/schedule`'s pre-existing precedent, and that a real
non-owner `/confirm` attempt in production is silently dropped like any
other third-party comment rather than answered.

### CR-A-009: `fixture_test.go`'s `newDaemon` helper does not merge `overrides.Confirmer` or `overrides.ConflictProposalTTL`, blocking a future daemon-level test of the confirmation dispatch path from injecting a fake confirmer
**File:** `internal/daemon/fixture_test.go:996-1063` (`newDaemon`)
**Story:** US-12 / T-12.5, T-12.9
**Category:** Standards / Testability

**Finding:** Confirmed directly: `newDaemon`'s override-merging block
(`fixture_test.go:1015-1062`) has an `if overrides.X != nil { opts.X =
overrides.X }` branch for every `daemon.Options` field except `Confirmer` and
`ConflictProposalTTL`. Grepped the whole `internal/daemon` test package for
any test that sets `daemon.Options{Confirmer: ...}` via `newDaemon`: none
exists, and none is possible today — a test that tried would silently have
its override dropped, leaving `d.confirmer` `nil` and every confirmation that
otherwise passes `ValidateConfirmation` falling into `handleConfirm`'s
"no confirmation applier is wired" branch (`confirm.go:52-61`), which always
answers `ReplyConfirmFailed` regardless of what a fake confirmer would have
returned. There is also currently no `internal/daemon/confirm_test.go` (or
equivalent) at all — the entire `handleConfirm` dispatch function (TTL sweep
ordering, the `confirmationRefusalReply` mapping, the "no confirmer wired"
fallback, and the successful-application reply assembly) has zero test
coverage at the daemon-package level today.

This does **not** block T-12.9's core acceptance criteria: I confirmed
AC-12.1 through AC-12.4's "no bypass" / non-owner / unknown / expired /
superseded / already-decided cases are all independently covered at the
`internal/turn.ValidateConfirmation` unit level in `apply_test.go`
(`TestApplyConfirmedResolutionRefusesNonOwner`,
`RefusesUnknownProposal`, `RefusesCrossSessionProposal`,
`RefusesExpiredProposal`, `RefusesAlreadyDecidedProposal`,
`SupersedesOnStaleBase` — all read and confirmed to pass), which needs no
daemon fixture at all. What this gap blocks is a *daemon-level* integration
test of `handleConfirm` itself — the TTL-sweep-first ordering, the reply-kind
mapping, and the routing through the daemon's own dependency wiring — which
is a real but narrower testability gap than a missing AC.
**Suggestion:** Add the two missing override branches to `newDaemon` (`if
overrides.Confirmer != nil { opts.Confirmer = overrides.Confirmer }` and the
zero-means-default equivalent for `ConflictProposalTTL`, matching the
pattern every other field already follows) before the test-writing phase
attempts to exercise `handleConfirm` at the daemon level. This is a small,
mechanical fix.

### CR-A-010: `checkResolvedFiles`'s `errors.Join` justification does not match its one actual call site
**File:** `internal/git/worktree_apply.go:368-391` (`checkResolvedFiles`)
**Story:** US-12 / T-12.2
**Category:** Idiom / Standards

**Finding:** The function's doc comment justifies aggregating every bad path
with `errors.Join` rather than failing fast on the first one: "the same
aggregate-don't-fail-fast shape `[repos.ValidateWorktreeRoots]` already
applies... **a caller fixing a confirmed file list one comment at a time
would otherwise see only the first problem and have to resubmit to find the
second**." That justification describes an interactive edit-and-resubmit
workflow. No such workflow exists anywhere in this feature: the only call
site (`prepare.go:506-511`, inside `prepareClone`'s confirmed-apply step)
builds `files` directly from `stored.Files` — a proposal already persisted by
US-11's `CreateOrGetConflictProposal`, whose paths were already screened by
`internal/turn`'s `validateConflictResolution` before the proposal was ever
written to the store. There is no owner-facing "fix your file list and
resubmit" flow at all; US-12's own "NOT in scope" list explicitly excludes
"applying an owner's hand-edited version of a suggestion rather than the
stored one." A bad path reaching `checkResolvedFiles` today would indicate a
data-integrity problem with an already-validated, litefactory-assembled
list — not a human iteratively refining a submission — and a single
representative error would serve a maintainer just as well as an aggregate
one.

This is not a functional defect: `errors.Join` on a single-element slice
behaves identically to returning that one error, and if it ever aggregates
more than one it is still a valid, correctly-formatted error. It is a
documentation/rationale mismatch worth tightening, and the kind of
aggregate-vs-fail-fast choice this team has previously flagged when the
stated reason for the pattern doesn't hold up under scrutiny.
**Suggestion:** Either correct the doc comment to state the real reason
(uniform defense-in-depth screening of an internally-assembled list, not an
interactive resubmission flow), or simplify to fail-fast on the first
`checkName` failure, whichever the team prefers stylistically. No test or
behavior change is required either way given the current single-call-site
reality.

### CR-A-011: `everyReplyKind` in `replykind_test.go` was not extended for US-12's eight new `ReplyKind` values, despite the plan brief explicitly calling for it
**File:** `internal/report/replykind_test.go:19-39` (`everyReplyKind`)
**Story:** US-12 / T-12.8
**Category:** Standards / Cross-story

**Finding:** `plan_brief.md`'s Tests section states: "The `report.ReplyKind`
enum-exhaustiveness test passes with `ReplyRefusesSync` (US-4), then
`ReplySuggestsResolution`/`ReplyAppliesResolution` (US-11/US-12), added in
turn." `ReplySuggestsResolution` is present in `everyReplyKind` (added by
US-11). None of US-12's eight new kinds are:
`ReplyConfirmInvalid`, `ReplyConfirmNotOwner`, `ReplyConfirmUnknownProposal`,
`ReplyConfirmExpiredProposal`, `ReplyConfirmSupersededProposal`,
`ReplyConfirmAlreadyApplied`, `ReplyConfirmFailed`, and
`ReplyAppliesResolution` itself are all absent from the list. Grepped the
whole file to confirm — none of the eight names appear anywhere in it.

The underlying production code is correct: I read `reply.go`'s `String()`
switch (lines 211-225) and confirmed all eight cases exist, render distinct
non-"invalid" labels, and are included in both the `valid()` membership
switch (lines 253-255) and `IsConfirmationReply()` (lines 270-272). This is
therefore a pure test-coverage gap against an explicit, named plan
requirement — not a production defect — but it is exactly the kind of
enum-exhaustiveness regression this specific test exists to catch, and the
plan brief named it by name as something this story must do.
**Suggestion:** Add all eight `ReplyConfirm*`/`ReplyAppliesResolution` values
to `everyReplyKind` before Phase 2 test-writing closes this story out.

### CR-A-012: `MarkConflictProposalApplied`/`SupersedeConflictProposal`'s lost-race path surfaces as a raw Go error rather than an `ApplyOutcomeFailed`/"already applied" value
**File:** `internal/turn/apply.go:310-316` (`ExecuteApplyResolution`); `internal/store/conflict_proposals.go:190-233` (`SupersedeConflictProposal`, `MarkConflictProposalApplied`)
**Story:** US-12 / T-12.6, T-12.9
**Category:** Error handling / Concurrency

**Finding:** Both store methods are correctly conditioned on `WHERE ... AND
status = 'pending'` (AC-12.9's idempotence guard), and both return the
generic `store.ErrNotFound`-wrapped error when that condition does not match
— i.e., when a concurrent confirmation of the same proposal already changed
its status between this call's `ValidateConfirmation` read and its own
attempt to write. In `ExecuteApplyResolution`, the `Superseded` branch
(`apply.go:310-316`) propagates that error as a bare Go `error`
(`return ApplyResult{}, fmt.Errorf("supersede stale conflict proposal %d: %w", ...)`)
rather than the `applyFailed(...)` value every *other* failure path in this
function uses. A caller (`daemon.handleConfirm`) would then take the
`d.leave(ctx, t, log, "could not apply a confirmed conflict-resolution
proposal", err)` path — an internal-error log line and an unanswered
comment — rather than a graceful `ReplyConfirmAlreadyApplied`/
`ReplyConfirmFailed` reply.

I traced whether this race is reachable given the daemon's actual
concurrency model and confirmed it is not, currently: `Daemon.offer`'s
`inflight.enter(c.session.ID)` guarantees at most one worker per session at a
time, and `drain` processes a session's comments strictly sequentially
within that one worker, so two `/confirm` comments on the same session
cannot reach `ValidateConfirmation` concurrently under the present design.
This is therefore latent rather than live, the same class of finding as
CR-A-004 in the US-6/US-10 section above.
**Suggestion:** For robustness against a future concurrency-model change
(e.g., cross-instance confirmation dispatch), consider mapping a lost-race
`ErrNotFound` from either store call back onto a graceful `ApplyResult`
(`ApplyOutcomeSuperseded`/`ApplyOutcomeAlreadyApplied`-shaped, re-reading the
current row) rather than a hard error, matching the discipline every other
branch of this function already follows. Not required for this story to
close, given the current single-worker-per-session invariant.

---

## Verified correct

- **AC-12.1 / S-12, the compile-time gate, traced by hand at every call
  site.** `confirmedProposal` (`turn/apply.go:75-78`) has no exported fields
  and no exported constructor. Grepped every reference to `confirmedProposal`
  and `ValidateConfirmation` across the tree (`apply.go`, `prepare.go`,
  `daemon/confirm.go`, `daemon/executor.go`, `daemon/daemon.go`): the only
  function in `internal/turn` that ever produces a non-zero
  `confirmedProposal` is `ValidateConfirmation`, and `TestConfirmedProposalHasNoExportedFields`
  (a reflection test, matching `TestWorktreeHasNoExportedConstructionPath`'s
  own precedent) proves the struct's field-level guarantee directly, not by
  inspection alone. `ApplyRequest.Proposal` is an exported field of an
  unexported type: an external package can only ever place a
  `ValidateConfirmation`-produced value there, or the type's zero value —
  and `ApplyRequest.validate()`'s `r.Proposal.proposal.ID <= 0` check refuses
  the zero-value case at the very top of `ExecuteApplyResolution`, so a
  caller cannot even reach `prepareClone`'s confirmed-apply branch with an
  unconfirmed value. `ExecuteApplyResolution` is the only production caller
  of `prepareClone` with a non-nil fifth argument; `prepare()` (the ordinary
  turn) always passes `nil`. No other call site anywhere in the tree
  constructs or forwards a `confirmedProposal`. This structural claim holds.
- **The ownership-check fix, traced independently.** `ValidateConfirmation`'s
  comparison (`ownerUserID == 0 || commenterUserID == 0 || ownerUserID !=
  commenterUserID`, `apply.go:126`) is genuinely numeric and compares against
  `d.filter.Identities().OwnerUserID` — the daemon's single configured owner
  identity — never against the issue's own author, and never against a
  login/handle. This matches `turn.Filter.Classify`'s `c.Author.ID ==
  f.identities.OwnerUserID` (`classify.go:249`) and `daemon.go`'s
  `t.issue.AssignedTo(d.filter.Identities().OwnerUserID)`
  (`tick.go:1107`) precedent exactly. The reported bug — comparing against
  the issue's own author instead — would have inverted the common case (a
  stranger's issue, the daemon owner confirming); the current code does not
  do that. See CR-A-008 above for the separate, non-blocking observation
  about where this check is actually reachable from in the live pipeline.
- **S-13 path containment, traced against every named threat.**
  `containedResolvedPath` (`worktree_apply.go:421-477`) rejects, in order: a
  path not byte-for-byte present in the confirmed file list; an absolute
  path; a path whose cleaned form differs from its input or resolves to `.`
  or `..`; any path component equal to `..`; a joined target that
  `filepath.Rel` cannot place inside `root`; a symlink anywhere along an
  *existing* ancestor directory (walked component by component with
  `os.Lstat`, stopping cleanly at the first `os.IsNotExist`); and a target
  that is itself a symlink. This lives entirely inside
  `internal/git/worktree_apply.go`, is called from both
  `WriteResolvedFiles` and, independently, `StageResolvedFiles` (each
  re-screens rather than trusting the other's prior check), and is never
  implemented as a string-prefix comparison anywhere in the file. Confirmed
  by reading the function body directly, not the doc comment's own claim
  about it.
- **S-15's structural placement is correct** (the contamination issue is
  CR-B-003, a substantive-correctness defect, not a placement one). The
  fresh `RevParse` reads for `currentBranchSHA`/`currentBaseSHA`
  (`prepare.go:484-491`) happen inside the one `Options.CloneGate.Acquire`
  taken at the top of `prepareClone` (`prepare.go:452`), immediately before
  `WriteResolvedFiles` is ever called, and after — not before — that gate
  acquisition, exactly as S-15 requires. `ExecuteApplyResolution` performs no
  separate gate acquisition of its own.
- **AC-12.6 rollback, traced through every mutator.** `WriteResolvedFiles`,
  `StageResolvedFiles`, and `CompleteMerge` each call
  `w.abortInProgressMerge(ctx, before, ...)` on every failure path after the
  merge has started (`worktree_apply.go:122,126,132,135,170,175,250,255,260,272`),
  and `abortInProgressMerge` (`worktree_apply.go:309-322`) runs `git merge
  --abort` and then re-verifies the branch tip against `before`, reporting a
  mismatch as its own, more urgent failure rather than silently accepting it
  — the same discipline `recoverFromFailedMerge` already applies for
  US-2/US-10's ordinary sync path. `git.Worktree`'s exported method set
  (`TestWorktreeMethodSetIsTheAllowlist`) contains no `Reset` and
  `TestWorktreeCannotRunGeneralPurposeMutators` explicitly forbids one —
  confirmed by reading the test file, not by absence of a grep hit alone.
- **AC-12.8, `TestWorktreeMethodSetIsTheAllowlist`.** Exists, pins exactly
  the pre-US-12 read-only-plus-three-mutator surface plus the three new
  US-12 mutators (`WriteResolvedFiles`, `StageResolvedFiles`,
  `CompleteMerge`), each with a recorded reason in the test's own comment,
  and the test's own doc comment confirms it was written to fail before the
  mutators existed (T-12.1's stated ordering). `go test` confirms it passes
  against the current method set.
- **S-16, trailers and disclosure.** `TrailerConflictProposal`,
  `TrailerConfirmedBy`, and `TrailerModel` are all litefactory-controlled or
  pattern-screened values — `ConfirmedBy` is the confirming owner's own
  login (the only account that can ever reach this path, per finding #2
  above), `Model` is an `agent.Kind` string, and both are additionally
  re-screened by `agentSpan`/`confirmedBySpan` before ever being rendered
  into the posted comment (S-16, SEC-5, reject-don't-salvage). `TrailerConfirmedBy`'s
  own doc comment explicitly states it is never a `Co-authored-by:` trailer.
  Confirmed no `Co-authored-by` string appears anywhere in `commit.go` or
  `worktree_apply.go`.
- **S-14 / AC-12.2, the two other required properties.** A non-owner
  confirmation returns `confirmedProposal{}, ConfirmationNotOwner, nil`
  (`apply.go:127`) with no database write of any kind on that path — the
  proposal's row is never touched. Every `ReplyConfirm*` rendering branch in
  `reply.go` (lines 838-895) references only `r.Confirm.ProposalID` and
  fixed sentences, never raw comment text.
- **AC-12.9, idempotent double-confirmation.** A second `ValidateConfirmation`
  call against an already-applied proposal finds `proposal.Status ==
  store.ConflictProposalApplied` and returns `ConfirmationAlreadyApplied`
  immediately (`apply.go:133-134`), before `ExecuteApplyResolution` is ever
  reached a second time — confirmed against `TestApplyConfirmedResolutionRefusesAlreadyDecidedProposal`,
  which passes.
- **AC-12.10, no held-open run.** `handleConfirm` and
  `ExecuteApplyResolution` are both single synchronous call chains with no
  polling loop, no channel wait, and no `time.Sleep` anywhere in either —
  confirmed by reading both functions top to bottom.
- **The confirmation grammar (T-12.4).** `confirm.Parse` requires a positive
  integer id, then the exact phrase (case-insensitively, restricted to a
  lowercase-letters-and-spaces charset), with no partial credit — a
  malformed attempt refuses in full via `ErrInvalidRequest`, never
  salvaging "the part that looked right." `internal/confirm` has no
  dependency on storage, the forge, or an agent, matching `internal/schedule`'s
  own precedent exactly, per its `doc.go`.
- **`internal/turn/gate_test.go`'s registry edit is exactly the plan's
  TEST-10 shape.** The `sections` map is confirmed unchanged at exactly
  three entries (`prepareClone`, `commitAndPush`, `commitStoryIndex`). The
  `gated` map gains `WriteResolvedFiles`, `StageResolvedFiles`, and
  `CompleteMerge`, each scoped to `{"prepareClone"}` only, and `Push` gains
  `"prepareClone"` as a third home alongside its two existing ones — no new
  acquisition anywhere, matching the plan brief's explicit "US-12's
  application turn reuses `prepareClone` unchanged... no further edit to
  this file [beyond the gated-call list]" instruction.
- **Grammar/dispatch wiring (T-12.4, T-12.5).** `KindConfirm` never sets
  `Command.Spawns() == true` (`command.go:131`), so `handleComment` routes it
  to `handleConfirm` before ever reaching the `Spawns()`/`d.run` branch
  (`tick.go:1352-1360`) — a `/confirm` control can never start an agent run,
  matching `KindSchedule`'s own precedent exactly. `daemon/setup.go` wires
  `Confirmer: NewConfirmApplier(turnOpts)` and `ConflictProposalTTL:
  conflictProposalTTL` into `daemon.Options` in production
  (`setup.go:300-334`), confirmed by direct reading.
- **`internal/turn/report.go`'s `ConfirmKind`/`Confirm` wiring (previously
  dead, now live).** `ReplyRequest.kind()` checks `req.ConfirmKind !=
  report.ReplyInvalid` ahead of the ordinary refusal/schedule/command
  dispatch (`report.go:291-293`), `validate()` enforces the same closed-set
  membership `ScheduleKind` already has (`report.go:321-324`), and
  `ReportReply` forwards `req.Confirm` straight into `report.Reply.Confirm`
  (`report.go:612`) with no transformation — the same three-line binding
  `Schedule`/`ScheduleKind` already have. `daemon/confirm.go`'s
  `replyConfirm` is the only caller that ever sets these two fields,
  confirmed by grep.
- **The regenerated golden fixture matches the code, deliberately.**
  `reply_suggests_resolution.golden`'s new final sentence — "To apply it
  exactly as shown, comment `/confirm 42 apply this resolution`." — is
  produced by `reply.go:782`'s exact `fmt.Fprintf` call, using the same
  literal `apply this resolution` phrase `confirm.Phrase` defines (verified
  by reading both, not assuming the string literals agree). This is a
  deliberate, correct regeneration reflecting US-12's new hint on US-11's
  existing reply, not a stale or accidental diff.
- **Build/vet/format/tests, re-run directly.** `go build ./...`, `go vet
  ./...` — both clean. `gofmt -l` over every touched package — no output.
  `go test -count=1 ./internal/confirm/... ./internal/git/...
  ./internal/store/... ./internal/report/...` — all `ok`. `go test -count=1
  -run 'ApplyConfirmedResolution|Confirm' ./internal/turn/... -v` — all 8
  US-12 tests in `apply_test.go` individually PASS.

---

## Cross-story summary

US-12 integrates correctly with US-10 (rollback), US-11 (the stored
proposal), and the pre-existing `/schedule` dispatch pattern at almost every
boundary this review checked: the compile-time confirmation gate (S-12) is
genuinely structural and independently proven by a reflection test; the
ownership-check bug the implementation engineer reported fixing is
genuinely fixed, and is a numeric comparison matching this codebase's one
established pattern for it; path containment (S-13) lives entirely inside
`*git.Worktree` with no caller-side trust; the rollback discipline (S-5,
AC-12.6) is the same abort-then-reverify shape US-10 already established,
reused rather than reinvented; and the trailers/disclosure (S-16) never let
model output or raw comment text into a commit or a comment. The one place
this story falls short is a genuine correctness defect at the seam between
US-2's pre-existing `syncBase` step and US-12's new confirmed-apply step
inside the shared `prepareClone` critical section (CR-B-003): the two
doc comments describing this exact seam both assert `syncBase` does not run
for a confirmed-apply turn, and the code does not honor that assertion. This
is precisely the kind of defect that costs two engineer-fix cycles instead
of one if it reaches the test-writing phase undetected — the existing
turn-level tests happen not to construct the one scenario (a base commit
that resolves the original conflict) that would expose it.

---

## Verdict

**BLOCKING** — 1 blocking finding (CR-B-003) and 5 advisory findings
(CR-A-008 through CR-A-012) for US-12. Route CR-B-003 to the engineer owning
`internal/turn/prepare.go`/`apply.go` before Phase 2 testing proceeds on this
story. CR-A-008 through CR-A-012 are noted for the drift report and do not
block. This does not reopen or affect the verdicts already recorded above
for US-1 through US-11.


---
---

# Re-review — US-12 / CR-B-003
# Feature: Apply a confirmed resolution and complete the merge
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAR

---

## Scope

Re-review of the single prior BLOCKING finding, CR-B-003 (`prepareClone`
called `syncBase` unconditionally, even for the agentless confirm-apply
turn). Per the orchestrator's instruction, only the files tied to the fix
were reviewed:

- `internal/turn/prepare.go` (the `prepareClone` gating change)
- `internal/turn/apply.go` (doc-comment update only, per the report)
- `internal/turn/gate_test.go` (comment/prose update only, per the report)
- `internal/turn/apply_test.go` (the new test)

Nothing else in US-1 through US-12 was re-graded; the rest of US-12's prior
"Verified correct" section and its five advisory findings (CR-A-008 through
CR-A-012) stand unchanged.

Verification run directly, not taken on the report's word:
- `go build ./...` — clean
- `go vet ./...` — clean
- `gofmt -l .` — no files listed
- `go test -race -count=1 ./internal/turn/... ./internal/git/... ./internal/daemon/...` — all three packages `ok`
- The new test was independently made to fail: `internal/turn/prepare.go` was
  temporarily reverted to the exact pre-fix shape (a single unconditional
  `syncBase` call before the `confirmed == nil` branch, `sync` threaded
  through every downstream `confirmed`-branch return instead of
  `syncResult{}`), `go build ./...` confirmed the reverted file still
  compiles, and `go test -race -count=1 ./internal/turn/... -run
  TestApplyConfirmedResolutionNeverAutoSyncsBeforeReVerifying -v` was run
  against it before restoring the fixed file from a saved copy and
  re-verifying `go build`/`go vet`/`gofmt -l`/the full three-package
  `-race` suite all clean again afterward.

---

## CR-B-003: RESOLVED

**Original finding:** `prepareClone` called `syncBase` unconditionally, even
on the path `ExecuteApplyResolution` takes for a confirmed-apply turn, which
could (a) produce a second, unconfirmed auto-sync merge commit — violating
AC-12.5's "exactly one merge commit" — whenever the base had moved into a
now-cleanly-mergeable state between propose and confirm, and (b) let that
same side effect move `HEAD`/the base tip before S-15's TOCTOU
re-verification ever read them, contaminating the comparison the
re-verification exists to make honestly.

**Fix verified against all five of the orchestrator's specific questions:**

1. **`syncBase` is genuinely unreachable when `confirmed != nil`, and the
   ordinary path's behavior/step order is unchanged.** Read `prepareClone`'s
   body directly (`internal/turn/prepare.go:472-483`):
   ```go
   worktree, err := ensureWorktree(ctx, opts, clone, session, repo, branch, logger)
   if err != nil {
       return session, "", nil, syncResult{}, nil, err
   }

   if confirmed == nil {
       sync, err := syncBase(ctx, opts, req, worktree, repo, author, logger)
       if err != nil {
           return session, "", nil, syncResult{}, nil, err
       }
       return session, branch, worktree, sync, nil, nil
   }

   // US-12: ... — the confirmed-apply logic follows, with every
   // downstream return in this branch now carrying syncResult{} rather
   // than a live sync value, since none exists on this path.
   ```
   `syncBase` appears exactly once in the function body, strictly inside the
   `confirmed == nil` block, which returns immediately after — there is no
   fallthrough, no `else`, and no later unconditional call. For the ordinary
   (`confirmed == nil`) path, the sequence is `ensureWorktree` → `syncBase` →
   return — byte-for-byte the same three-step order the pre-fix code ran,
   just with the same two statements now living inside an `if` body instead
   of above it; nothing about the ordinary turn's control flow, error
   handling, or return values changed. Diffed against the pre-fix version
   directly (`git diff internal/turn/prepare.go`) to confirm this is a
   structural move, not a rewrite that could have introduced a second,
   subtler behavior change: every downstream `confirmed`-branch return
   changed only from returning the earlier `sync` value to returning
   `syncResult{}` (correct, since there is genuinely no meaningful
   `syncResult` for this turn shape now that `syncBase` never runs on it)
   — no other return value, argument, or call was altered.

2. **The new test genuinely exercises the bug's failure mode, not a
   pre-existing outcome-level distinction.** Read the test body directly
   (`internal/turn/apply_test.go:337-380`,
   `TestApplyConfirmedResolutionNeverAutoSyncsBeforeReVerifying`). It reverts
   the base's own conflicting edit in a new base commit between propose and
   confirm — a scenario the test's own comment correctly reasons makes a
   *fresh* `merge-tree` preview against the current base tip clean, while
   still constituting a genuine `stored.BaseSHA` mismatch that S-15's
   re-verification should supersede on its own merits. This is exactly the
   scenario CR-B-003 named as the one existing tests missed
   (`TestApplyConfirmedResolutionSupersedesOnStaleBase` advances the base
   with an unrelated-file commit, which leaves the fresh preview unclean and
   never triggers a merge). The assertions are on the side effect, not the
   outcome: `result.Outcome != ApplyOutcomeSuperseded` is checked (and holds
   in both the buggy and fixed version, exactly as the test's own comment
   predicts — a stale base/branch sha genuinely warrants supersession either
   way), while the load-bearing assertions are that `HEAD` in the worktree
   directory is byte-identical before and after the call, and that the tip
   commit has exactly one parent.

   Independently reproduced both directions rather than trusting the
   report's claim: reverted `prepareClone` to the exact pre-fix
   unconditional-`syncBase` shape and re-ran the test — it failed exactly as
   CR-B-003 predicted:
   ```
   apply_test.go:374: HEAD moved from dd4659ccde2920d46e52c784581a5a4c2517a7bb
       to affbf94ec41a716b4ac8e2024a8055c23c16eb70: a superseded confirmation
       must never leave a commit behind, not even one syncBase made for
       itself before re-verifying (CR-B-003)
   apply_test.go:378: HEAD has 2 parents, want exactly one: no merge commit
       of any kind — sync or confirmed — was ever made
   --- FAIL: TestApplyConfirmedResolutionNeverAutoSyncsBeforeReVerifying (0.80s)
   ```
   `Outcome` was still `ApplyOutcomeSuperseded` in that failing run (an
   `t.Fatalf` on that check would have fired first if it hadn't been), which
   independently confirms the test could not have caught this bug through
   the outcome alone — precisely the constraint the test's own doc comment
   states and reasons through. Restored the fixed file from a saved copy and
   re-ran: `PASS`, 0.79s. This is not a scenario the pre-fix code would have
   passed anyway; it is the one scenario built specifically to expose the
   defect, verified failing and passing on both sides of the fix by direct
   execution, not by re-reading the report's own claim to that effect.

3. **S-15's re-verification reads pre-any-mutation state for this turn
   shape.** For `confirmed != nil`, the call sequence between the one
   `Options.CloneGate.Acquire` at the top of `prepareClone` and the
   `currentBranchSHA`/`currentBaseSHA` reads is: `fetch` → `freezeSlug` →
   `ensureWorktree` → (no `syncBase`) → `worktree.RevParse(ctx, "HEAD", ...)`
   / `worktree.RevParse(ctx, baseRef, ...)`. Read `ensureWorktree`
   (`prepare.go:1216-1251`) directly: it only checks out or creates a
   worktree at an existing branch start via `git.EnsureWorktree` — it never
   merges, fetches a fresh base tip beyond what the earlier `fetch` step
   already did, or otherwise moves `HEAD`. With `syncBase` now excluded from
   this path entirely, nothing between the gate acquisition and the
   `RevParse` reads can move either ref out from under the comparison — the
   values being compared are genuinely the state as of this turn's own gate
   acquisition, with no self-inflicted mutation in between, which is exactly
   what S-15 requires.

4. **No second regression: `gate_test.go`'s actual allowlist data is
   unchanged, and no other call site or test relies on `syncBase` having run
   unconditionally.** Diffed `internal/turn/gate_test.go` directly
   (`git diff`): the `gated` map's `"syncBase": {"prepareClone"}`,
   `"WriteResolvedFiles"/"StageResolvedFiles"/"CompleteMerge": {"prepareClone"}`
   entries, and the unchanged three-entry `sections` map, are all
   byte-identical string values before and after — only the surrounding
   `//` prose describing *why* `syncBase` is conditional and *where*
   `WriteResolvedFiles` now sits relative to it (fourth step "in place of
   syncBase" rather than fifth step "after syncBase") was edited. Grepped
   every reference to `syncBase` across the repository
   (`internal/turn/{prepare,apply,gate_test,report,conflict,errors,result,execute,sync_test,syncconcurrency_test,syncpersist_test}.go`,
   `internal/store/{store_test,runs}.go`, `internal/config/config.go`): the
   only production caller of `syncBase` in the entire tree is the one
   `confirmed == nil` branch inside `prepareClone` itself; every test file
   that exercises `syncBase`'s own outcomes (`sync_test.go`,
   `syncconcurrency_test.go`, `syncpersist_test.go`) drives `prepare()` /
   ordinary ("US-2 shape") turns exclusively and never constructs or passes
   a `confirmedProposal`, so none of them depend on `syncBase` running
   unconditionally for the confirmed-apply shape — there was nothing else in
   the tree for this fix to break.

   Separately confirmed `internal/turn/apply.go`'s diff against its
   pre-fix-review state is doc-comment-only for this specific fix: the only
   change attributable to CR-B-003 is the "fifth and last part" → "fourth
   and last part... `syncBase` itself is skipped for this turn shape"
   rewording in `ExecuteApplyResolution`'s own doc comment
   (`apply.go:291-297`) and the matching paragraph in `ValidateConfirmation`'s
   updated numeric-ownership doc comment, which was already reviewed and
   verified correct in the prior (non-re-review) US-12 pass under CR-A-008 —
   not a new change introduced by this fix.

5. **Build/vet/format/race verification, re-run directly:**
   - `go build ./...` — clean
   - `go vet ./...` — clean
   - `gofmt -l .` — no files listed
   - `go test -race -count=1 ./internal/turn/... ./internal/git/... ./internal/daemon/...` — all three `ok`

**Disposition:** Resolved. No new BLOCKING issue found in any of the four
reviewed files. No regression to the ordinary (`confirmed == nil`) turn's
behavior or to any of US-1 through US-11's previously-verified properties.

---

## New advisory finding (does not reopen CR-B-003 or block)

### CR-A-013: two production doc comments now cite an internal review-finding ID (`CR-B-003`) that will outlive this document
**File:** `internal/turn/apply.go:295` (`ExecuteApplyResolution` doc comment); `internal/turn/apply_test.go:375` (test failure message)
**Story:** US-12 / T-12.6
**Category:** Idiom / Style
**Finding:** The fix threads the literal string `CR-B-003` into two places that ship as part of the binary/test suite rather than staying confined to this review document: `apply.go`'s doc comment ("`[syncBase]` itself is skipped for this turn shape (CR-B-003)") and the new test's `t.Errorf` message ("not even one syncBase made for itself before re-verifying (CR-B-003)"). `internal/turn/gate_test.go`'s updated comment does the same in a third place. A code-review finding ID is this session's artifact, not a stable, permanently-resolvable identifier the way an issue tracker ID or an AC number (`AC-12.5`, `S-15`) is elsewhere in this same codebase's convention — a future reader with no access to this `CODE_REVIEW.md` (or a later session that overwrites/archives it) will find a citation to nothing.
**Suggestion:** No functional change needed. Consider rewording the two production/test-facing mentions to describe the invariant itself (e.g., "a confirmed-apply turn must never make its own auto-sync merge commit before this re-verification runs") rather than the finding ID, reserving `CR-B-003`-style citations for this document and closing commentary. Cosmetic; does not block.

---

## Verdict

CLEAR — CR-B-003 is resolved, verified independently by reproducing both the
failing (pre-fix) and passing (post-fix) test runs directly rather than
trusting the engineer's report. One new cosmetic advisory (CR-A-013) noted;
it does not block. No new BLOCKING finding. This closes out both of US-12's
BLOCKING findings from this feature's review cycle (CR-B-002 was already
resolved for US-11; CR-B-003 is now resolved for US-12). Proceeding to
environment setup / next phase.
