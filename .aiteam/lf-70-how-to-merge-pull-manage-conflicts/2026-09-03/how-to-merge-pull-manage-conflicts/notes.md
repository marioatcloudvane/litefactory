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
