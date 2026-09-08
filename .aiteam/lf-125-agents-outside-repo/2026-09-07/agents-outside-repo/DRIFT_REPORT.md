# Drift Report — agents-outside-repo

## Run 0015 — current delivery accounting

### Delivered implementation

- US-1: configurable remote index/default and named corporate sources, strict distribution/manifest contracts, SemVer selection and safe GitHub raw-file normalization. Reuses the existing central schema rather than adding a compatibility layer.
- US-2: explicit remote add/update/refresh, verified content cache, recorded source/version/pins/history, five-state drift with separate remote observations, explicit source switching, concurrent-edit preflight, and journaled whole-catalog recovery. Local package imports require explicit path syntax; bare corporate aliases cannot be shadowed by local directories (CR-B-004 resolved).
- US-3: native file-backed Claude/Codex bundles, selected-mode capture, operator-owned model mapping, catalog-free classifier setup, retained-resource integrity checks and conversation-segment identity, with durable inline delivery evidence. Deterministic acceptance testing is in progress; real CLI acceptance is separate below.
- US-4: focused canonical team/skill content, exact first-position orchestrators, coding/testing-only Implement content, read-only reviewer content for Validate, empty Spike roster, and selected runtime bootstrap. Removed production embedded agent content and acquisition; old bytes remain test/benchmark fixtures. Active repository instructions were not destructively rewritten.
- US-5: documented actual operator commands/configuration and durable evidence; reproducible instruction-byte benchmark plus a clearly unmeasured live-token template. Byte counts are not billed-token measurements.
- US-6: deterministic independently invoked content publisher, generated `teams/teams.json` and immutable initial release, immutable-first/index-last publication, manual artifact workflow and corporate hosting instructions. These are locally generated publication artifacts, not a deployed public release.

### Owner-approved adjustments

- Run 0014/0015 supersede the old unset-default clauses: `teams.index_url` defaults to the owner's literal GitHub URL. A narrow GitHub file-locator normalization to raw content on `HEAD` makes retrieval explicit before origin checks; arbitrary cross-origin redirects remain forbidden.
- No nested sandbox capability prerequisite, paid probes, outer permission bypass, live state/dist edits, commits or pushes. The historical EPERM gate is retired.

### Deferred acceptance and verification

- AC-3.7: real exact-version CLI delegation, authentication, tier/permission behavior, concurrent catalog isolation and resume evidence has not been collected in this run.
- AC-5.2: comparative live token/latency/delegation observations remain unmeasured. Retained byte fixtures and deterministic tests do not satisfy that measurement requirement.
- Code re-review is CLEAN, with CR-B-005 explicitly retained as an open release-evidence gate, not waived or counted as accepted.
- Production hosting/push is outside this run. The public default returned HTTP 404 during obsolete fixtures; generated files must reach the source's default branch before default public installation can succeed.
- 25 new scoped test functions pass, and the final full race-enabled repository suite passes with no excluded tests. `make check` passes formatting/vet but stops on 41 lint findings whose source files are unchanged from starting HEAD. Consult the run 0015 test reports, not historical counts below. Untested acceptance branches are explicitly listed by the test engineers.

### Verdict

DRIFTED / release acceptance incomplete: the implementation and full deterministic race verification are delivered, but real CLI and token acceptance are not complete and the existing lint gate remains red. No new planning cycle is needed for the owner-mandated default; suitable-environment release verification and authorized publication remain separate follow-ups.

## Historical run 0011 record

Branch: lf/125-agents-outside-repo

## Delivered as planned

- US-1: strict version-2 central registry, explicit ordered mode rosters, central paths, locked source/dependency snapshots and embedded distribution adaptation. Conflicting inline identities across modes are rejected; identical reuse remains valid.
- US-2: central default installation and complete external-package imports, management commands, full effective override/resource provenance, five drift states with force protection, portable source relocation, and explicit persisted repository association. Operator documentation is in `README.md`.
- Independent production review: CLEAN after one fix cycle; all three blocking findings and the root-only origin advisory resolved.

## Verification

- Full `go test -race ./...`, `go build ./...`, and `go vet ./...` passed. Focused source race tests also passed after the final boolean-equivalent lint cleanup.
- `make check` stops on 28 inherited pinned-linter findings. The same tool against starting HEAD reports 33 existing findings; all task-introduced warnings were fixed. This is not a green `make check` claim.
- Independent acceptance assessment added four passing cases for mixed roster ordering, unselected-mode source isolation and transitive traversal/symlink containment. Focused race coverage: team 76.5%, teamsource 77.7%. See `TEST_REPORT.md`.

## Dropped or deferred

- US-3 runtime delivery, bundle/conversation lifecycle and parity acceptance: blocked by the real Codex 0.153.4 app-server initialization permission error. Claude candidate injection received bounded positive evidence but delegated tiers/authority and concurrent isolation remain unproven. See `RUNNER_CAPABILITIES.md`.
- US-4 focused runtime workflows and bootstrap conversion depend on US-3; existing embedded workflow bodies remain until coordinated delivery can be verified.
- US-5 runtime provenance and full comparative token benchmark depend on US-3/US-4. Original full-catalog commit/blob inventory is preserved in `BASELINE_CATALOG.md`; no measured token reduction is claimed.

## Architectural deviations

The approved plan called for a coordinated five-story cutover. This run encountered its explicit CLI proof gate and delivers the central-source/management foundation only. Explicitly assigned external-team classifier/work runs fail before invoking a CLI while transport is unavailable; existing unassigned behavior is retained. This fail-closed guard implements the relevant AC-3.5 requirement, without enabling unverified delivery. Full feature completion and runtime enablement are not claimed.

## Verdict

DRIFTED — central management is implemented and reviewed; runtime delivery and downstream workflows/telemetry remain blocked by the exact-version CLI proof gate, and the required aggregate check still encounters pre-existing lint findings.
