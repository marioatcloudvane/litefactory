# Code Review
# Feature: agents-outside-repo
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAN

Scope: completed US-1 and US-2 production changes and the explicitly assigned-team runtime refusal (AC3.5). US-3 delivery, US-4 instruction conversion and US-5 runtime benchmarks are deferred, not passed. Review uses uncommitted working-tree changes and the orchestrator's architectural/acceptance hints; test authoring remains with the test engineers.

## Re-review cycle 1

Focused re-review read the previously blocking files and their immediate helpers: `internal/team/validate.go`, `internal/teamsource/manage.go`, `override.go`, `snapshot.go`, and `override_snapshot.go`. No production or test files were edited by this reviewer.

- **CR-B-001 resolved:** `Override.Closure` retains installed-path to origin-path/hash mappings for all captured dependencies. `captureOverride` bounds reads and rechecks every dependency. `overrideStatus` compares installed and upstream dependency bytes, observes new dependencies and reports closure errors. `refreshOverrideUnlocked` checks local/conflict/missing states and passes the same captured source into publication. Shared-skill replacement remains guarded. Resource-only changes no longer disappear behind root-Markdown equality.
- **CR-B-002 resolved:** `team.Validate` tracks inline identities across all modes and rejects differing descriptions, paths, tiers or ordered skills for the same ID. Identical reuse remains allowed, making the existing scoped replacement and single retained original consistent with accepted definitions.
- **CR-B-003 resolved:** `portableTeamPaths` normalizes selected inline and referenced shared definitions against the canonical package/catalog root after import capture and during central management reads. `CaptureUnlocked` returns relative definition paths matching its file/hash keys. Imported baselines and override originals are consequently portable; temporary publication validation resolves copied sources without the original absolute path.
- **CR-A-001 resolved:** `Status` emits an explicit `index.json#<team>` observation carrying origin error evidence for an installation without source hashes, so root-only teams no longer silently hide unavailable origins.

No blocking findings remain in the focused re-review. These conclusions come from reading revised production logic. The orchestrator separately reports `go test -race ./...` passes; test files were not reviewed under this specialist's scope. Historical finding details below are retained for traceability and are all resolved.

## Original BLOCKING findings (resolved in cycle 1)

### CR-B-001: Override drift and update ignore the dependency closure
**File:** `internal/teamsource/manage.go:580`; `internal/teamsource/override.go:156`
**Story:** US-2, override lifecycle and drift.
**Category:** Cross-story / Error handling.
**Finding:** Overrides capture and copy relative resources and skills, but persist only the root Markdown hash. Status compares only that Markdown, and Update skips the override entirely when its Markdown bytes equal the origin. Changing or deleting a declared resource without changing the agent therefore reports in-sync and does not refresh/repair the installed closure. Conversely, a local edit to an installed resource is not protected by the force check, which compares only the root file. Changing the root can overwrite such a local resource without detecting its drift. This breaks the promised closure-based provenance and preservation of local changes.
**Required fix:** Record and compare the complete effective override closure, including declared resources/skills. Preflight all dependency changes and local conflicts before publication; make resource-only changes, missing dependencies and origin failures visible in status and update, and require force for conflicting local bytes. Preserve the intended shared-skill conflict policy. Cover unchanged root Markdown with changed upstream resource, changed local resource and deleted installed dependency.

### CR-B-002: Team-scoped override silently collapses different mode definitions
**File:** `internal/team/validate.go:44`; `internal/teamsource/override.go:21`; `internal/teamsource/override.go:53`
**Story:** US-1 validation / US-2 override identity.
**Category:** Cross-story.
**Finding:** Validation checks duplicate IDs only within each mode, so the same inline ID may legally have different paths, tiers or skills across modes. scopedAgent selects its first occurrence, and replaceScopedAgent replaces every occurrence with that single entry. Setting an override consequently erases later modes' distinct metadata; unset restores only the first definition, permanently losing the other original mode definitions. Effective catalog validation cannot catch this because the collapsed catalog is internally consistent.
**Required fix:** Define and enforce the team-scoped identity contract. With the approved `team:<team>:<id>` key, reject conflicting inline definitions across modes before install/management (identical reuse may remain valid), or preserve all per-mode originals and metadata during override/unset. Add coverage for differing same-ID mode entries and round-trip set/unset preservation.

### CR-B-003: Absolute contained paths cannot survive import or publication preflight
**File:** `internal/teamsource/manage.go:324`; `internal/teamsource/manage.go:436`; `internal/teamsource/snapshot.go:205`
**Story:** US-1 absolute source support / US-2 independent import.
**Category:** Cross-story.
**Finding:** Capture correctly accepts an absolute path contained in its catalog and records the bytes under a relative map key. Installation copies those bytes but retains the original absolute AgentEntry.Path in the imported team/shared definitions. validatePublication then captures a temporary staged catalog: the absolute path still points at the origin (or existing central catalog), outside that stage, so valid contained absolute definitions fail preflight. The same issue prevents managing an otherwise valid central catalog containing absolute paths. Retaining the path would also defeat the specified independence from the import origin.
**Required fix:** Canonicalize supported contained source paths to portable catalog-relative definitions when assembling publication/import snapshots, consistently for shared, inline and retained base definitions. Ensure staged validation sees exactly the relocated bytes. Cover import from an absolute-contained path and subsequent origin removal, plus update/manage for a valid absolute-path catalog.

## Original ADVISORY findings (resolved in cycle 1)

### CR-A-001: Root-only teams can hide an unavailable origin
**File:** `internal/teamsource/manage.go:469`
**Story:** US-2 status.
**Category:** Consistency.
**Finding:** When every mode is empty, the installation has no source hashes. If its origin later disappears, no definition row is emitted because originErr is non-nil and no file row exists to carry that error. Status therefore returns no drift/error and the CLI succeeds despite an unavailable recorded origin.
**Suggestion:** Emit a team-level provenance observation for every installation, including root-only teams, with explicit origin evidence.

## Cross-story summary

Central paths, explicit repository association, schema-2 ordered mode rosters and shared references align across layers. SQL remains in store, source reads use bounded os.Root operations, and catalog snapshots serialize with management writes and recheck captured bytes. The new assigned-team checks reject work before classifier or work CLI invocation; no runtime transport success or token improvement is inferred. The principal unresolved boundaries are portable snapshot metadata and overrides' relationship to complete source closures. Deferred workflow bodies remain historical by explicit scope and are not reviewed as completed US-4 work. Parent-managed lint fixes are separate from these functional findings.

## Verdict

CLEAN — All three prior blocking findings and the touched advisory are resolved. Proceed with the completed US-1/US-2 subset's remaining orchestrator gates. US-3 through US-5 remain gated/deferred; this is not a full feature-completion or CLI-isolation verdict.
