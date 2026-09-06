# Drift Report — new-way-to-work-with-agent-teams (Run 20: US-9 + US-18)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-06

Scope of this report: only the two stories in this run's delivery window,
US-9 (`team add` write engine) and US-18 (root-file marker composition),
built together per plan_brief.md's explicit hard-coupling. Not a full-feature
drift report — prior runs' stories (US-1/2/3/5/6/7/8/19) are out of scope
here and were already assessed in their own runs.

## Delivered as planned

- New sibling package `internal/team` (T-18 through T-24, T-48 through T-52),
  satisfying `cli -> domain` dependency direction — no `internal/cli` changes.
- Provenance-marker lockfile at `.litefactory/team.lock.json`, two-hash
  record shape `{agentKey, harness, destPath, sourcePath, sourceHash,
  renderedHash}` exactly as specified (T-18), no `baseSourceHash`.
- `.litefactory/` bookkeeping directory bootstrap on first write if absent
  (T-19).
- Render-all-then-write-all-or-nothing atomic write engine (T-23): every
  render in the batch validated up front, `errors.Join`-aggregated failure
  reporting (post-fix, see Code Review Advisories below), nothing written on
  any validation failure.
- Per-repo flock-style lock serializing concurrent `team add` against the
  same repo (T-23), verified under `-race` with both same-artifact collapse
  (1 record from 8 concurrent identical calls) and distinct-artifact
  no-lost-update (2 concurrent calls, 2 distinct agents, both persist) tests.
- No-op short-circuit when both `sourceHash` and `renderedHash` already match
  (T-23/AC-4).
- Marker format `<!-- litefactory:team BEGIN team=<team> agent=<agent-key>
  -->` … `<!-- litefactory:team END -->`, locate-or-append upsert replacing
  only the enclosed span (T-49), content outside the span never touched.
- Shared-agent-across-teams deduplication into one block with a
  comma-separated, sorted `team=` list, updated correctly across sequential
  `Add` calls as new teams contribute (T-49/AC-2).
- Marker removal primitive (T-50) — implemented and tested directly, no
  caller wired (expected: caller is US-11's `team update`, out of scope).
- Marker-span virtual-file hashing (T-51) feeding the same `LockRecord` shape
  US-9 defines, proven independent of content outside the span (T-52/AC-4).
- Atomic writes are temp-file+rename, `O_EXCL` create-at-mode with no
  `.Chmod()` call; symlink refusal via `os.Lstat` before every write
  including the temp+rename path; every write-target path validated as
  repo-relative, forward-slash, no `..`, resolves inside an Owned Path.
- `errors.Is`/`errors.As`/`%w` sentinel handling throughout; new sentinels
  (`ErrNoLockfile`, `ErrLockfileCorrupt`, `ErrDrifted`, `ErrInexpressible`,
  `ErrUnsupportedHarness`, `ErrPathNotOwned`, `ErrRefusedSymlink`,
  `ErrSecretDetected`, `ErrMarkerNotFound`) with typed error structs.
- `doc.go` extended with three new sections documenting the write engine,
  the provenance lockfile, and root-file composition/virtual-file hashing —
  consistent with the project convention that a package's `doc.go` is its
  spec.
- All 8 Acceptance Criteria across both stories (US-9 AC-1–4, US-18 AC-1–4)
  independently verified with tests by the unit test engineer, closing
  several coverage gaps beyond what the implementation engineer's own tests
  proved (e.g. multi-harness single-batch write, distinct-agent concurrent
  no-lost-update, marker-span hash isolation from surrounding root-file
  content, sequential shared-agent team-list merge).
- `go build ./...`, `go vet ./...`, `gofmt -l internal/team`, and
  `go test -race ./internal/team/...` all clean/green throughout — including
  after the one Phase 1.5 fix cycle.

## Added scope

- Nothing beyond what T-18/19/23/24 (US-9) and T-48/49/50/51/52 (US-18)
  called for. The unit test engineer added 6 tests beyond what the
  implementation engineer wrote, but these are test-only additions closing
  AC-coverage gaps, not new production behavior.

## Dropped or deferred

- **US-9 T-20** (repo-identifier resolution CLI integration via US-19's
  fuzzy resolver): explicitly deferred per this run's task instruction — CLI
  wiring depends on US-4's scaffold, which does not exist yet. `team.Add`
  is delivered as the complete, ready-to-consume engine contract
  (`Add(root string, renders []AgentRender) (*AddResult, error)`) for a
  future `cmd_team.go` or `internal/teamsource` glue layer to call.
- **US-18 AC-3's actual trigger** (marker removal fired by `team update`):
  the `removeMarkerSection` primitive (T-50) is implemented and tested
  directly, but has no caller — that caller is US-11's scope, explicitly out
  of this run.
- **`context.Context` on `Add`**: plan_brief.md's Cross-Story Constraints
  describe a `ctx.Err()` check immediately before a write loop; `Add` takes
  no context parameter. Read as consistent with the "no concurrency beyond
  the specified per-repo flock" constraint, since nothing else in this
  engine's call chain (renderers, flock) is ctx-aware either — recorded as
  ADVISORY (CR-A-002) in `CODE_REVIEW.md`, not fixed this run, flagged for
  awareness when US-4 threads a context in from the command layer.

## Architectural deviations

- None identified against plan_brief.md's Architecture Notes for these two
  stories. The one deviation surfaced during review (fail-fast validation
  instead of `errors.Join`-aggregated validation, CR-B-001) was caught and
  fixed within Phase 1.5, not shipped as a deviation.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions. Both
deferred items (US-9 T-20's CLI integration, US-18 AC-3's `team update`
trigger) are deferrals to explicitly named future stories (US-4, US-11) per
the task's own scope boundary, not scope drops.
