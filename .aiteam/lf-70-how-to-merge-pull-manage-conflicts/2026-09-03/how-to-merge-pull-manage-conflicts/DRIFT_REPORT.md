# Drift Report — Config-governed defaults and size cap (US-8)
# Branch: lf/70-how-to-merge-pull-manage-conflicts | Date: 2026-09-07

## Scope note

This run implemented **US-8 only**, by explicit instruction. US-1 through
US-7 and US-9 through US-12 remain unimplemented and are out of scope for
this drift comparison. The uncommitted, never-reviewed partial US-1/US-2 work
already present in the worktree from a previously stopped run was left
untouched throughout.

## Delivered as planned

- T-8.1: `AutoSyncBase bool` (default `true`), `AutoSyncMaxBehind int`
  (default `100`), `BlockRunOnConflict bool` (default `true`) added to
  `internal/config`'s `Settings`, with defaults applied via `Defaults()`.
- T-8.2: `Config.Validate()` rejects `AutoSyncMaxBehind < 0` as a hard config
  load error (CFG-6) — never clamped to `0` or to the default.
- T-8.3: The three keys, plus the "100 is a judgment call, not a measurement"
  note, documented in `internal/config/template.go`'s generated
  `config.yaml` template (the config package's existing convention for
  operator-facing key documentation, per its CFG-3 anti-drift check — this
  repo has no separate `docs/` file enumerating individual keys).
- T-8.4: Seven unit tests across defaults-when-omitted, explicit zero
  (both via direct struct assignment and via full YAML decode), negative
  rejection (both direct and via `Decode`+`Validate`), and per-field boolean
  override with sibling-independence.
- AC-8.1 and AC-8.2 both verified directly by tests; `go test -race ./...`
  green across all 22 packages, `go vet ./...` and `gofmt -l .` clean.
- Package placement (`internal/config`), no new dependency (`purity_test.go`
  still passes), no new package created — all match the plan's Architecture
  Notes and Shared Decisions exactly.

## Added scope

- Two extra unit tests beyond the implementer's initial five, added during
  Phase 3 to close two coverage gaps: explicit-zero surviving the real
  YAML-decode-onto-defaults merge path (not just direct struct assignment),
  and sibling-field independence when only one auto-sync key is overridden.
  Both are within T-8.4's intent ("tests: default values, 0=no-cap,
  negative=error, explicit true/false override") — not new scope, just more
  thorough coverage of the same task.

## Dropped or deferred

None within US-8's own scope. Everything in the story's task table and
acceptance criteria was delivered.

(Not a drop, but worth naming: US-2's and US-7's later consumption of these
three fields — reading them inside `syncBase` and overriding
`AutoSyncBase` via the `lf:sync=` label — is explicitly NOT this story's job
per its own Architecture Notes, and was correctly left undone.)

## Architectural deviations

None. The implementer followed the plan's stated package placement,
dependency constraints, and field semantics exactly, including the
CFG-6 negative-value hard-error rule and the "judgment call, not measurement"
documentation requirement for the `100` default.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (test
coverage additions and the choice of `template.go` as the documentation
surface, both consistent with the story's stated intent and the codebase's
own conventions).

---

# Drift Report — `lf:sync=` label opt-out (US-7)
# Branch: lf/70-how-to-merge-pull-manage-conflicts | Date: 2026-09-07

## Scope note

This run implemented **US-7 only**, by explicit instruction. US-1 through
US-6 and US-9 through US-12 remain unimplemented and are out of scope for
this drift comparison; US-8 was already implemented and committed in a prior
run and was not touched. The uncommitted, never-reviewed partial US-1/US-2
work already present in the worktree from an earlier interrupted run was left
untouched throughout.

## Delivered as planned

- T-7.1: `syncLabelPrefix`, `syncRefusal` enum, `syncDecision` struct, and
  `resolveSync(labels []string) syncDecision` added in new file
  `internal/daemon/sync.go`, copied rung-for-rung from the live
  `agentLabelPrefix`/`modeLabelPrefix` ladder in `internal/daemon/agent.go`
  and `internal/daemon/mode.go` — explicitly not modeled on the dead
  `lf:tools=`/`legacyToolsLabel` pattern, per the story's Architecture Notes.
- T-7.2: `turn.Request` gained `SyncOverride *bool`
  (`internal/turn/execute.go`); `internal/daemon/tick.go` computes the
  decision via `resolveSync(t.issue.Labels)` and threads it into the existing
  `turn.Request{...}` literal without disturbing any other field in it.
- T-7.3: Seven test functions (~21 subtests) covering every ladder rung —
  absent, `on`, `off`, miscased prefix (three shapes), duplicated/ambiguous
  label (three shapes), invalid/empty value — plus sibling-label
  independence, a structural `reflect`-based check that no field on
  `syncDecision` can carry raw label text (SEC-5/S-9), and an end-to-end
  check that `turn.Request.SyncOverride` is populated (or left `nil`)
  correctly at the `Daemon.run`/tick dispatch point.
- AC-7.1 verified directly by tests: `lf:sync=off`/`lf:sync=on` resolve to a
  non-nil override; malformed, miscased, and duplicated labels all leave
  `SyncOverride` nil (never a guessed value), which is the point at which the
  config default would apply once US-2's `syncBase` reads this field.
- `go-code-reviewer` returned CLEAN — no BLOCKING or ADVISORY findings.
- `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `go test -race ./...`
  green across every package in the module (verified directly by the
  orchestrator, not just the touched packages).
- Package placement (`internal/daemon`, next to `agent.go`/`mode.go`, plus
  the `turn.Request` field addition), no new Go package, no new dependency —
  all match the plan's Shared Decisions and this story's Architecture Notes
  exactly.

## Added scope

None beyond the story's own task table. The unit test engineer's extra edge
shapes (multiple miscasing variants, multiple duplicate-label variants
beyond the single example of each in T-7.3's list) are within T-7.3's stated
intent ("tests: absent, on, off, miscased prefix, duplicated label, invalid
value") — more thorough coverage of the same six cases, not new scope.

## Dropped or deferred

None within US-7's own scope. Everything in the story's task table and its
one acceptance criterion (AC-7.1) was delivered.

(Not a drop, but worth naming, per the story's own explicit boundaries: this
story does not implement `syncBase`'s consumption of `SyncOverride` (US-2,
not yet built), does not read or act on `AutoSyncBase`/`BlockRunOnConflict`
themselves (US-8, already done separately), and does not implement
disclosure of *why* a label was refused (US-3, not yet built) — `syncDecision`
carries `Refusal`/`MiscasedPrefix` fields structured so that later story can
build on them without any redesign.)

## Architectural deviations

None. The implementer copied the live `agentLabelPrefix`/`modeLabelPrefix`
ladder exactly as instructed, kept the raw label text out of every field
(SEC-5/S-9), and left `turn.Request`'s other fields and `tick.go`'s
surrounding logic untouched.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (the test
engineer's additional edge-case variants, consistent with T-7.3's stated
intent and the codebase's own existing test conventions for
`agentLabelPrefix`/`modeLabelPrefix`).

---

# Drift Report — Measure behind/ahead before every run (US-1)
# Branch: lf/70-how-to-merge-pull-manage-conflicts | Date: 2026-09-07

## Scope note

This run implemented/verified **US-1 only**, by explicit instruction. Its
premise was unusual: a prior run (stopped by the operator at 49 minutes,
before code review, tests, or close-out) had already written `Divergence`,
`parseDivergenceCounts`, and `divergenceTimeout` in `internal/git/merge.go`
and `internal/git/exec.go`, and wired the call site into
`internal/turn/prepare.go`'s `syncBase`. This run's job was to verify that
code against US-1's acceptance criteria (not trust it because it existed),
correct it if wrong, and deliver the one task it was genuinely missing —
T-1.4, unit tests, of which zero existed anywhere in the repo. US-2's partial
work in the same file (`MergePreview`, `MergeBase`, `recoverFromFailedMerge`)
was left completely untouched throughout — not reviewed, not tested, not
reformatted.

## Delivered as planned

- T-1.1 (Phase 0 probe): already complete from an earlier phase of this plan,
  not re-litigated here.
- T-1.2 (`Divergence` type and `(*Worktree).Divergence` method): found
  already present and, on independent empirical verification (a real
  throwaway git repo, diverged by known asymmetric counts, `git rev-list
  --left-right --count` run directly), **correct as written** — no fix
  needed. `checkName` screens both `base` and `w.branch` before the range is
  assembled inside the method; `--end-of-options` matches `revParse`'s usage;
  a missing base ref classifies via `classify(err, ErrRevisionNotFound, 128)`
  through the existing `failuref`-equivalent path, not a panic or raw exit
  code.
- T-1.3 (`divergenceTimeout` const, 30s): found already present in
  `internal/git/exec.go` and correctly wired into the `runner.run` call.
- T-1.4 (unit tests): delivered in full, newly written this run —
  `internal/git/merge_test.go` (new file), 4 test functions covering all four
  ahead/behind combinations against real git repos (0/0, ahead-only,
  behind-only, both), a side-order regression test using deliberately
  asymmetric counts (base +3, branch +1) that would fail if `base...branch`
  were flipped to `branch...base` or if the parsed `behind`/`ahead` fields
  were swapped, the missing-base-ref error path asserting
  `errors.Is(err, git.ErrRevisionNotFound)` on a classified `*git.CommandError`,
  and a `checkName`-screening test confirming malformed ref names are
  rejected before git is ever shelled out. All tests reuse the existing
  `internal/git` real-git-in-temp-directory test harness
  (`internal/gittest`, `openFixture`, `newTaskWorktree`) rather than
  inventing a parallel one.
- AC-1.1 verified directly by the side-order regression test plus the four
  combination subtests — this is the exact trap the story's Architecture
  Notes called out ("the single easiest place to get this story backwards
  without any test catching it"), and it is now pinned.
- `go-code-reviewer` independently re-traced the ahead/behind ordering from
  source (not from the implementer's report) and returned **CLEAN**, zero
  BLOCKING or ADVISORY findings, scoped strictly to the `Divergence`-related
  code.
- `go test -race ./...` green across all 21 module packages, `go vet ./...`
  and `gofmt -l .` clean (re-verified directly by the orchestrator, not just
  by the sub-agents).
- Package placement (`*git.Worktree`, `internal/git/merge.go`), ref-name
  screening via `checkName` before `runner.run` (S-1), range assembly
  entirely inside `internal/git` from separately-screened components, and no
  new dependency — all match the plan's Shared Decisions and this story's
  Architecture Notes exactly.

## Added scope

None. This run's only code addition was the missing test file (T-1.4 itself,
not an addition beyond it); the production code required zero changes.

## Dropped or deferred

None within US-1's own scope. Every task in the story's table (T-1.1 through
T-1.4) and its one acceptance criterion (AC-1.1) are now satisfied.

(Not a drop, but worth naming, per the story's own explicit boundaries: this
story does not decide what to *do* with the divergence count — that's US-2,
whose partial, unreviewed `MergePreview`/`MergeBase` work sits in the same
file and was correctly left untouched throughout this run.)

## Architectural deviations

None. The pre-existing implementation already matched the plan's placement,
screening, timeout-naming, and error-classification requirements; nothing
needed to move or be restructured to bring it into compliance.

## Verdict

CLEAN — the story's only real gap (T-1.4, missing tests) is now closed, the
pre-existing implementation was verified rather than assumed correct, and no
drift from the plan was found or introduced.
