# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Current suite reads the full recipe from the pinned commit, runs commands in order and stops on first non-pass. It has no targeted retry or base-comparison branch. Existing lock, interruption recovery, cleanup and unchanged-tree checks are reusable.

Evidence:

- `internal/turn/validate_suite.go:120`

## f2

Same-instance finished results are replayed unchanged. Another instance can reuse only a passed result for the identical commit. New commits receive no inherited coverage or prior-failure selection.

Evidence:

- `internal/store/suite_runs.go:154`

## f3

Suite storage retains commit, recipe/tree digests, timing, command argv, status/exit, process/cleanup identity, output digest and only a 16 KiB output tail. There is no per-test/package result, baseline link or combined coverage record in these models. Old records cannot reliably recover all failed tests.

Evidence:

- `internal/store/suite_runs.go:24`

## f4

Execution captures up to 4 MiB and marks incomplete capture as failure to obtain trustworthy evidence. Parsing before the later 16 KiB retention cut could use more output, but the inspected executor has no failure parser.

Evidence:

- `internal/bugchecks/executor.go:26`

## f5

The full recipe already declares make test plus a go-test rerun template with {tests}/{packages}. Config validates and copies rerun metadata; repository-wide production Go searches for .Rerun/BugRerun found no execution consumer. These are existing configuration contracts, not an implemented retry mechanism.

Evidence:

- `.litefactory/bug-checks.yaml:27`

## f6

make test splits ordinary packages and mixed packages, then runs selected race tests. Shell set -e means a failure in an earlier group can prevent later groups running. Therefore a failed make test cannot be assumed to have exercised the entire suite; failed-test retries plus changed packages alone can miss previously unrun groups.

Evidence:

- `Makefile:474`

## f7

Reviewers receive retained suite commands as citable checks; executed criteria require passed evidence for the tested commit. Delivery blockers also explicitly require a passed suite. Non-blocking base failures and inherited coverage touch these consumers, not only the runner.

Evidence:

- `internal/turn/validate_review.go:461`

## f8

Delivery gating keeps non-passed suites blocking and prevents settling awaiting checks while the suite fails. A new attribution model must preserve factual failure records while representing its delivery effect.

Evidence:

- `internal/turn/validate_state.go:370`

## f9

Touched surface: turn suite/store models and schema, bugchecks execution/config contracts, reviewer evidence, outcome/state/report/commands, runtime admission and suite tests, workflow docs, team source/released validation graph and review guidance. Existing graph owns ordering and repair routing; a kept repair returns to suite.

Evidence:

- `teams/source/base-team/validate/validate.dot:52`

## f10

Data/compatibility: retained suite rows and captured released teams already exist. New metadata must handle old evidence and saved graphs explicitly; existing same-commit reuse, restart reconciliation and owner-waiver tested/delivery commit separation are compatibility boundaries. No migration strategy is decided here.

Evidence:

- `docs/workflow/validate.md:24`

## f11

Incident corroboration: #1109's retained Validate report says make test failed on cf126aa5937b and delivery completed with an owner waiver. Owner waiver names two guidance-order tests failing on main and fixed in #1235. #1235 changes only two tests to locate guidance by ID; it reports package checks passing. Sources: https://github.com/marioatcloudvane/litefactory/issues/1109#issuecomment-6444122634 and https://github.com/marioatcloudvane/litefactory/pull/1235. Local merged fix is at 4899797ab778d856fadc1e8d978bcad3fbca34d4.

Evidence:

- `internal/codex/python_fastapi_test.go:40`
- `internal/harness/python_fastapi_test.go:40`

## f12

Recent suite history already added passed same-commit reuse, suite admission scheduling and safe source observation. Related issues found: #1030 output loss, #1182 missing failure details, #875 busy admission and #642 make exit classification. No existing affected-package engine was found by searches for affected/reverse dependency/go list deps in internal Go code; this is a bounded search, not proof of absence.

Evidence:

- `internal/turn/validate_suite.go:132`

## f13

Superseded behavior is always running the full recipe for a new commit after failure. Same-commit replay and full-run availability remain useful. The inspected earlier #984 brief concerns graph-owned workflow decisions rather than providing an incremental-validation design.

Evidence:

- `.aiteam/lf-984-graph-topology-still-decided-in-go/2026-10-19/graph-topology-still-decided-in-go/RESEARCH_BRIEF.md:1`

## f14

Shaping inspection confirms make test already accepts command-line PKGS selection, divides selected packages into full-race and mixed groups, and retains selected race checks for mixed packages; this is a possible existing surface for equivalent affected-package coverage without changing repository suite configuration, but recognizing completeness still needs new evidence.

Evidence:

- `Makefile:4`
- `Makefile:474`

## f15

Impact analysis carried forward from f7-f10: the feature changes persistent suite evidence, runner selection, review citations, delivery classification, command/report surfaces and source-team guidance; old result records and immutable released captures require conservative compatibility behavior, and no new external client or permission role is required by this proposed scope.

Evidence:

- `internal/store/suite_runs.go:39`
- `docs/workflow/validate.md:24`
