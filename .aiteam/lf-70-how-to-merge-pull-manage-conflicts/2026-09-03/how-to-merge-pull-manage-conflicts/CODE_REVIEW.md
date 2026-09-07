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
