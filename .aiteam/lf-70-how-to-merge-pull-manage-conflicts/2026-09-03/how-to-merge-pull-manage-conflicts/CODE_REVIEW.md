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
