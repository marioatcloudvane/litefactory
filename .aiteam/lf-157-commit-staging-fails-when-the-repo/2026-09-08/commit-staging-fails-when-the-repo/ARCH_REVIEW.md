# Architect: Go Architect
# Created: 2026-09-08
# Verdict: PASS

## System Overview

The feature belongs entirely to the existing `internal/git` worktree commit boundary. `internal/turn` calls that boundary when saving successful work; the daemon, forge, transport cleanup and outcome reporting require no changes. `internal/git/doc.go` defines the protected registered-clone and mutable-worktree separation, and that separation remains intact (SEC-7).

The current commit flow first resets transport paths, obtains filtered worktree status, stages ordinary changes, resets transport paths again, checks the staged index, commits when appropriate, and resolves HEAD. The change removes the two exclude pathspecs from the staging invocation while retaining this ordering and its existing result/error contracts. Transport filtering in `internal/git/status.go` and both resets remain existing responsibilities.

### New Packages / Components Introduced

None. No interfaces, dependencies, configuration, database entities or migrations are needed.

### Key Boundaries

Production git execution remains inside `internal/git` through its existing runner. Test fixtures reuse `internal/gittest`, the documented test-only exception for direct git execution. Public `Worktree.Commit` and `CommitResult` contracts remain unchanged. No registered clone, runtime state, release binary or forge mutation is authorized by this plan.

### Data Flow

Successful task save → worktree transport reset/status → stage all ordinary changes and eligible transport files → existing transport reset → staged-index decision → task commit → existing commit result. The index and commit exclude transient transport while the working-tree transport files remain available.

### Concurrency & Context Notes

Retain existing context propagation and per-command timeouts (CONC-5). No goroutines, retries, clocks or locks are introduced.

### Shared State and Side Effects

Only the supplied task worktree index and task branch are mutated during implementation tests through isolated local fixtures. Preserve ordinary additions, modifications and deletions, transport-only successful no-op behavior, provenance trailers and genuine command failure propagation. In particular, do not reinterpret all staging exit-code-1 failures as success (STYLE-5).

## Task Hints

Task IDs below are recommended for the single story; the product owner owns final decomposition.

### T-157.1: Correct staging invocation

**Agent:** go-implementation-engineer

- Pattern and package placement: retain the existing commit pipeline in `internal/git/commit.go`; change only staging arguments to the requested `add --all -- .` invocation.
- Key interfaces: preserve `Worktree.Commit` / `CommitResult`, existing runner, resets and error wrapping.
- Integration: callers in `internal/turn` continue using the same API and success gates (CONC-8).
- Watch out for: ignored directory existence alone triggers the reported failure, so deleting run files or suppressing Git advice does not solve the requested condition.
- Not in scope: `.gitignore`, transport cleanup, outcome rendering, new abstractions or protocol changes.

### T-157.2: Regression and transport preservation coverage

**Agent:** go-unit-test-engineer

- Pattern and package placement: extend black-box commit coverage alongside `internal/git/worktree_test.go`, using existing `internal/gittest`, `openFixture`, task worktree and commit-options helpers (TEST-1 through TEST-4).
- Exercise real local Git for ignored nonempty and present-empty event directories, with a normal change requiring a commit. Inspect the resulting commit and index, not only filtered `Worktree.Status`, because status deliberately hides transport.
- Cover the nonignored transport case: show both paths staged by ordinary add, then removed by the existing reset while their bytes remain on disk; confirm the public commit operation retains normal changes and excludes transport.
- Preserve already-staged transport and transport-only no-op behavior without extending production scope. Existing ordinary change and provenance tests remain relevant.
- Keep fixture setup offline and isolated; no new integration tags or test naming convention, external services, assertion library or module dependency.

### T-157.3: Review and verification

**Agents:** go-code-reviewer and go-unit-test-engineer

- Verify AC-157-1 through AC-157-5 individually, including scoped review and tests; use those exact identifiers as evidence names (SEC-21).
- Run focused `internal/git` regression tests, then `make check` for the aggregate fmt/vet/pinned lint/race gate. Do not run `make build` or write `dist/`.
- Record the exact local Git version and scratch-fixture reproduction/verification commands with the implementation evidence (EXT-1, EXT-2). The issue reports Git 2.50.1; this review does not claim a fresh binary probe.

## Platform Constraints

- Platform: layout and interfaces — use the current internal leaf boundary; no new contract or package.
- Platform: error handling — retain typed command errors and context-bearing wrapping; never broadly discard failures.
- Platform: concurrency — retain context/timeouts; no new concurrency.
- Platform: testing — stdlib, real offline Git fixtures and table-driven cases where helpful; `make check` remains the project gate.
- Platform: dependency management — no changes.
- Platform: HTTP, database, logging — not applicable (unchanged).

## Managed Plan Structural Findings

`internal/planbrief/proposal.go` defines version/generation as JSON integers, parent as a string, stories as an array, and each story with string key/intent/title/body, integer `issue_number` (Go int), integer `issue_id` (Go int64), string `expected_base`, and string-array `dependencies`. For `create`, validation requires numeric identities `0`, `0` and base `""`; use `[]` for no dependencies. For existing stories, identities must be positive and expected base is the supplied SHA256 of title + NUL + body. No existing child identities were supplied, so one create proposal is appropriate.

`internal/planbrief/index.go` explicitly allows an em dash or empty Sub-issue cell. Use an em dash pending factory publication, with no invented issue link or local story-file link. `internal/turn/plan_index.go` replaces the entire Story Index section after publication; `internal/turn/plan_publication.go` emits `ID | Title | Sub-issue` rows with links derived from verified numeric issue identity. Shared context outside that section survives. Full bodies belong solely to the transient root proposal until the factory publishes them; do not create per-story Markdown (CONC-12).

## Deviations

None. No unresolved architectural decisions or questions.
