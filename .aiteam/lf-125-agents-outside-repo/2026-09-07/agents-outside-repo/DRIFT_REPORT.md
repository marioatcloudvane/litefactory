# Drift Report — agents-outside-repo

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
