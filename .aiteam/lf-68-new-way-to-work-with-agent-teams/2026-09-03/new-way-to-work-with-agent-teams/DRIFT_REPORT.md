# Drift Report — new-way-to-work-with-agent-teams (US-14)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-07

Scope of this report: this run's own delivery only — US-14 (Allowlist + forbidden-flag +
secret-scan validation). US-1 through US-13 were already complete and out of scope for this run
(verified clean on `ae7a303` before starting; not touched here).

## Delivered as planned

- T-39 — `config.ClassifySecret` + a newly-exported `config.ContainsForbiddenFlag` wired into the
  render→write pipeline, run against rendered output bytes in `internal/team/write.go`'s `Add()`,
  the single post-render validation gate both `team add` and `team update` route through.
- The `TODO(US-14)` at `internal/team/validate.go:14` resolved and removed: `internal/config`
  gained the exported, string-taking `ContainsForbiddenFlag(value string) bool` the TODO
  anticipated by name, wrapping the pre-existing `strings.Contains(value, forbiddenFlag())` check
  with no duplication (DEV-2, as recorded in `plan_brief.md`'s Deviations).
- `internal/config`'s own `validateNoForbiddenFlag` refactored to call the new export internally
  instead of the raw `strings.Contains`, so there is exactly one definition of the check —
  satisfies AC-3 and keeps SEC-1's single-grep-site guarantee for `--dangerously-skip-permissions`
  intact.
- AC-1: covered symmetrically for both content origins. Index-sourced offending content and
  US-13-override-sourced offending content both flow through the same `team.Add` rendered-bytes
  check (`team manage` itself never renders/writes directly — it only records an override pointer
  that a later `team add`/`team update` renders), so neither path is exempt.
- AC-2: `Add`'s per-file validation loop aggregates every problem across the whole batch via
  `errors.Join`/`joinSorted` (append-then-`continue`, not fail-fast) before refusing the write —
  proven by a new mixed clean/offending-batch test showing none of the clean files get written
  either. New/extended error types (`ForbiddenFlagError`, `ForbiddenFlagInIndexError`, extended
  `SecretDetectedError`) name the agent key, harness, and which check failed, and never include the
  matched flag substring or secret value.
- AC-3: no forbidden-flag or secret-pattern logic duplicated inside `internal/team` — both checks
  are pure calls into `internal/config`; confirmed by direct source read (no `strings.Contains`
  reimplementation of the flag literal in `internal/team`) and preserved by `internal/config`'s
  existing `TestValidationHasNoThirdPartyImports`/`TestForbiddenFlagIsAbsentFromTheTree` tests.
- T-40 — 12 new test functions across four files (`internal/config/policy_test.go`,
  `internal/team/validate_test.go`, `internal/team/write_forbidden_flag_test.go`,
  `internal/teamsource/render_forbidden_flag_test.go`), covering index-sourced and
  override-sourced refusal, all-or-nothing batch abort, error-naming, and leakage checks. All
  forbidden-flag test fixtures assembled from string fragments, matching the project's own
  existing convention for evading the whole-tree literal scan.

## Added scope

None beyond what T-39/T-40 called for. The engineer touched exactly the files the story's
Architecture Notes named (`internal/config/policy.go`, `internal/team/{validate,errors,write,
doc}.go`) plus the four new test files; `internal/team/doc.go`'s Invariants section was updated to
stop describing `config.ContainsForbiddenFlag` as "added by a later story," which is documentation
upkeep intrinsic to closing this story, not scope creep.

## Dropped or deferred

None. Both tasks (T-39, T-40) and all three acceptance criteria (AC-1, AC-2, AC-3) were delivered
in full within this run.

## Architectural deviations

None. The implementation follows the plan's explicit resolution of the TODO gap exactly as
prescribed by this run's own instructions: export a small string-level function from
`internal/config` under the name the TODO already anticipated, have both the existing
`validateNoForbiddenFlag` and `internal/team` call it, and never copy the flag string or the
`strings.Contains` check into `internal/team`.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (e.g. splitting the post-render
and index-level forbidden-flag checks into two distinct error types, `ForbiddenFlagError` vs.
`ForbiddenFlagInIndexError`, since the two call sites have different available context — no
`Harness` at index-decode time, since decoding is harness-agnostic prior to rendering).

## Code review

CLEAN (one non-blocking advisory, CR-A-001: `Add`'s per-file loop converts `f.Bytes` to `string`
twice — once for `ClassifySecret`, once for `ContainsForbiddenFlag` — a minor duplicate allocation,
not hot-path-critical given rendered files are small markdown documents; left unfixed by judgment).
No BLOCKING findings, no fix cycle needed — the first validation-focused story on this issue to
clear code review on the first pass with zero `errors.Join`-aggregation mistakes, a class of
mistake that was BLOCKING three times previously on this same issue.

## Tests

12 new tests, 12 passing, 0 failing. `go test -race ./...` clean across all 26 packages. `gofmt -l`
and `go vet ./...` clean. No bug loop triggered.
